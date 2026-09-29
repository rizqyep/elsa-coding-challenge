# task-19: Gateway: registry and room events

- **Phase:** P3 Services
- **Size:** L · **TDD:** yes
- **Status:** done
- **Implements:** TRD §7.4, §7.5 · FR-12, FR-25, FR-26, FR-28, NFR-5a–NFR-5c
- **Depends on:** [task-18](task-18-gateway-connections.md), [task-13](../p2-redis-scripts/task-13-scripts-transition-leaderboard.md)
- **Unblocks:** [task-20](task-20-gateway-message-handlers-and-presence.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- shard locking (join/leave under concurrency)
- **broadcast writes the same prepared message to every connection**
- subscribe on first local connection, unsubscribe on last
- stale events dropped
- event → client message mapping for every event type
- personal ranks after close (pipelined)
- `kick` closes the right connections

## Steps

- [x] Tests: shard locking under concurrent join/leave; broadcast writes the **same** prepared message to every connection
- [x] Tests: subscribe on first local connection, unsubscribe on last; stale events dropped
- [x] Tests: every internal event type maps to the right client message; personal ranks after close use one pipeline
- [x] Tests: `kick` closes the other connections of that participant with 4000
- [x] Implement the registry (copy under read lock, enqueue outside the lock) and subscription reference counting
- [x] Implement the subscriber goroutine with reconnect + resubscribe + snapshot push (TRD §9.3)
- [x] Implement the event mapper (question text and correct option from the cache; one prepared message per event)

## Done when

- [x] question cache (task-17) wired in: one reference per connection (`Enter` acquires, `Leave` releases); the cache is reference-counted, so this equals holding it while the room has connections
- [x] **snapshot pushes after a subscriber reconnect are coalesced per room** (one room-state read shared by the room's connections), so a pub/sub reconnect doesn't send every connection's snapshot read to Redis at once (TRD §7.10)
- [x] registry + subscriber + event mapper
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- **Two gaps in earlier work, fixed first (own commits):**
  - the `state` event had no `next_at`, which `question_closed.nextTransitionAt` needs and gateways can't compute (the reveal is timed from the actual transition, and its length is in Redis);
  - the answer path with `WAIT` enabled ran its script by hash in a pipeline, so after a Redis restart or failover every answer failed with `NOSCRIPT` indefinitely. It now reloads and retries once; `NOSCRIPT` means the script didn't run, so that can't double-count.
- **A design bug found by the end-to-end test:** an early close republished the *current* state version, so gateways (and clients, FR-28) dropped the moved close time as stale. `quiz.EarlyClose` and `answer.lua` now both increment it.
- **Subscriber:** its own receive loop instead of go-redis's `Channel()`, which reconnects silently, hides confirmations, and drops messages when its buffer stays full (read in the library source). Joins wait for Redis to *confirm* the subscription, not just for the command to be written. Confirmations are counted per `SUBSCRIBE`. A quiet connection is pinged; an unanswered ping ends it.
- **Stampede guards (TRD §7.10):** after a reconnect, each room gets one `view` read and one `standings` read, whatever its size; concurrent resyncs in a room share one `view` read (single-flight, detached from the first caller); event-driven reads run off the subscriber goroutine, at most 16 at a time.
- **New Redis reads (session module):** `view` (read-only room snapshot part) and `standings` (score, rank, and answer for many participants plus the count, in one round trip, 500 per script call).
- **`protocol.ValidateServer`** checks outgoing frames against the schemas; every hub test and the end-to-end test validate what they receive.

## Verification

- Tests written first and confirmed failing. `make check` green; all 16 integration packages pass; the realtime suite passed 3 back-to-back runs under `-race`.
- **Unit (fake sockets and fake Redis reads):** one prepared frame per broadcast, other rooms untouched; every event type mapped and validated against the contract, including cjson's `{}` for an empty list; stale versions dropped per counter; ranks from one read, none for the host; kick keeps the named connection; enter/leave balanced under 32-goroutine churn; `Enter` undoes itself when subscribing or loading the set fails; resubscribe pushes to 41 connections from 1 view and 1 standings read; 20 concurrent resyncs share 1 view; a cancelled resync doesn't fail the one sharing its read.
- **Integration (real Redis through Toxiproxy):** `Ensure` returns only after Redis confirmed, checked with 300 ms of injected latency; drop and churn end with exactly the right subscriptions; an outage resubscribes every room and signals each once; a silently stalled (half-open) connection is detected by the ping; an idle healthy connection is not replaced; `Ensure` fails while Redis is down.
- **End to end (real scripts, question cache over PostgreSQL, real sockets):** two participants and the host see the question with its prompt from the database, the early close, the reveal with `nextTransitionAt`, their own ranks, the leaderboard, and `quiz_finished`; every frame passes its schema. A second connection for the same participant closes the first with 4000.
- **Mutations:** 21 real mutants caught. Along the way: 4 invalid mutants redone (3 didn't compile, 1 still bounded); 2 weak tests strengthened (a fake that ignored cancellation; a confirmation check without latency); 1 missing test added (an idle healthy connection must not reconnect).
- **Not covered by a test:** per-`SUBSCRIBE` confirmation counting guards a narrow interleaving (drop and re-enter while a confirmation is unread) that can't be ordered deterministically against a real `PubSub`. Chunking `standings` to 500 per call bounds how long one script blocks Redis, which a test can't pin reliably.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
