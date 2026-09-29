# task-18: Gateway: connections

- **Phase:** P3 Services
- **Size:** L · **TDD:** yes
- **Status:** done
- **Implements:** TRD §7.1, §7.2, §7.6 · NFR-3, NFR-4, NFR-5d, NFR-17, NFR-20, NFR-21, NFR-23
- **Depends on:** [task-09](../p1-domain/task-09-protocol-package.md), [task-12](../p2-redis-scripts/task-12-script-answer.md), [task-17](task-17-question-cache.md)
- **Unblocks:** [task-19](task-19-gateway-registry-and-room-events.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- origin, token, and admission checks before the upgrade
- oversized message → close 1009
- rate limit → `rate_limited`, repeated → 4002
- **full send queue never blocks the broadcaster**
- slow client → resync snapshot, again within 30 s → 4003
- heartbeat timeout closes dead connections

## Steps

- [x] Tests (httptest + gorilla client): bad origin → 403; bad token → 401; admission exceeded → 503 + `Retry-After`
- [x] Tests: message over 4 KB → close 1009; rate limit → `rate_limited`, 100 violations in 10 s → close 4002
- [x] Test: a full send queue never blocks the broadcaster (non-blocking send)
- [x] Tests: slow client → queue drained + resync snapshot; again within 30 s → close 4003; heartbeat timeout closes dead connections
- [x] Implement the upgrader (1 KB buffers, write-buffer pool, compression off) and pre-upgrade checks
- [x] Implement the connection: read loop, single-writer write loop, send queue (prepared or personal items), ping/pong deadlines, close codes

## Done when

- [x] ~~question cache wired in~~ moved to task-19: rooms don't exist until the registry does
- [x] upgrader with pooled write buffers, read/write loops, send queue, rate limiter
- [x] tests pass under `-race`
- [x] `make check` green

## Notes and decisions

- **Burst and stampede guards** (TRD §7.10), raised from the review question "don't fry the WebSocket server on a join burst":
  - `WS_MAX_CONNECTIONS` (new, default 20,000) and the admission bucket are both checked **before** the upgrade, so a rejected client costs no goroutine or buffer. The cap is taken with a compare-and-swap, so a burst can't overshoot it.
  - `Retry-After` is jittered by 0–2 s so rejected clients don't come back as one wave.
  - Slow-client resyncs share 32 gateway-wide slots plus 0–250 ms jitter. Without this, a Redis stall makes every connection overflow and then resync against Redis at the same moment.
  - One request at a time per connection; the rate limit runs before JSON Schema decoding.
- **Docs drift fixed:** the AsyncAPI spec said a bad origin closes with 1008 and admission closes with 1013, while the TRD rejects both before the upgrade. The spec now lists the handshake statuses (401, 403, 503 with `Retry-After`).
- **Browsers can't read a failed handshake's status**, so every rejection looks like close 1006 to them. The reconnect table (TRD §9.5) now says the React client refreshes its token before `expiresAt` rather than waiting for a 401.
- **Origin:** a request without an `Origin` header is allowed. Browsers always send one; the simulator and test kit don't, and the token still authenticates them.
- **Resync ordering:** the pending mark is cleared right after a resync slot is taken, before the snapshot read. Clearing it later would drop events published between the read and the write; clearing it earlier would count events queued while waiting for a slot as a second overflow (4003).
- **Close never blocks:** gorilla's `WriteControl` waits for the write lock, which a stalled writer holds, so the close frame is written from a short-lived goroutine.
- **Decisions:** a hand-written fake socket instead of gomock for the connection tests, because the tests need writes that stall and release on cue.

## Verification

- Tests written first and confirmed failing; all pass under `-race`, 3 back-to-back runs, and `make check` is green.
- **End to end over real sockets (httptest + gorilla client):** 403 / 401 (missing, garbage, expired token) before the upgrade; 503 with a jittered `Retry-After` of 1–3 s; the cap holds exactly under a 200-connection burst against a cap of 50; 1009 for an oversized frame; invalid frames get an error and keep the connection; rate limit with `retryAfterMs`; exactly 100 violations tolerated, the 101st closes with 4002; messages handled one at a time; heartbeat drops only the dead client; a server close code reaches a client that keeps sending; `Closed` called once per connection and goroutines return to baseline after 200 connections.
- **Deterministic slow-client tests (fake socket that stalls like gorilla's):** 80,000 sends to a stalled client return within 1 s; the queue is replaced by one snapshot; a second overflow within 30 s closes with 4003 without blocking the sender; overflows 31 s apart both resync; a failed snapshot closes with 4003; 60 connections resyncing at once never exceed 4 concurrent snapshot reads; a connection waiting for a slot stops when closed.
- **Mutations:** 14, all caught by test failures in 1–7 s. The first run had two catches only by the 60 s timeout (a blocking send and a synchronous close deadlocked their tests); both tests now run the risky call under a 2 s deadline. Two mutants were invalid on the first try (one still bounded, one didn't compile) and were redone.
- **Memory:** 2,000 idle connections measured in one process: 2 goroutines per connection and 33.5 KB of heap and stack per connection **for server and client together**, so the server's share is below the 50 KB budget (NFR-4). An in-process upper bound; task-29 measures across processes.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
