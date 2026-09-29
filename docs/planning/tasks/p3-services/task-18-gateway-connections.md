# task-18: Gateway: connections

- **Phase:** P3 Services
- **Size:** L · **TDD:** yes
- **Status:** todo
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

- [ ] Tests (httptest + gorilla client): bad origin → 403; bad token → 401; admission exceeded → 503 + `Retry-After`
- [ ] Tests: message over 4 KB → close 1009; rate limit → `rate_limited`, 100 violations in 10 s → close 4002
- [ ] Test: a full send queue never blocks the broadcaster (non-blocking send)
- [ ] Tests: slow client → queue drained + resync snapshot; again within 30 s → close 4003; heartbeat timeout closes dead connections
- [ ] Implement the upgrader (1 KB buffers, write-buffer pool, compression off) and pre-upgrade checks
- [ ] Implement the connection: read loop, single-writer write loop, send queue (prepared or personal items), ping/pong deadlines, close codes

## Done when

- [ ] upgrader with pooled write buffers, read/write loops, send queue, rate limiter
- [ ] tests pass under `-race`
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
