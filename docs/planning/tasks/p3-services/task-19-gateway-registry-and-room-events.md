# task-19: Gateway: registry and room events

- **Phase:** P3 Services
- **Size:** L · **TDD:** yes
- **Status:** todo
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

- [ ] Tests: shard locking under concurrent join/leave; broadcast writes the **same** prepared message to every connection
- [ ] Tests: subscribe on first local connection, unsubscribe on last; stale events dropped
- [ ] Tests: every internal event type maps to the right client message; personal ranks after close use one pipeline
- [ ] Tests: `kick` closes the other connections of that participant with 4000
- [ ] Implement the registry (copy under read lock, enqueue outside the lock) and subscription reference counting
- [ ] Implement the subscriber goroutine with reconnect + resubscribe + snapshot push (TRD §9.3)
- [ ] Implement the event mapper (question text and correct option from the cache; one prepared message per event)

## Done when

- [ ] question cache (task-17) wired in: `Acquire` on a room's first local connection, `Release` when its last one leaves
- [ ] **snapshot pushes after a subscriber reconnect are coalesced per room** (one room-state read shared by the room's connections), so a pub/sub reconnect doesn't send every connection's snapshot read to Redis at once (TRD §7.10)
- [ ] registry + subscriber + event mapper
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
