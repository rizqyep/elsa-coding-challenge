# task-17: Question cache

- **Phase:** P3 Services
- **Size:** M · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §7.8 · NFR-9, D12
- **Depends on:** [task-02](../p0-foundation/task-02-platform-packages.md), [task-04](../p0-foundation/task-04-migrations-and-seed-data.md)
- **Unblocks:** [task-18](task-18-gateway-connections.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- single-flight (1,000 concurrent gets → 1 load)
- PostgreSQL fails 3× then succeeds → loaded with backoff (gomock)
- budget exhausted → `server_busy`
- malformed set refused
- reference counting and LRU eviction

## Steps

- [ ] Tests with gomock repository and a fake clock: 1,000 concurrent gets → 1 load (single-flight)
- [ ] Tests: PostgreSQL fails 3× then succeeds → loaded with backoff; budget exhausted → `server_busy`
- [ ] Tests: malformed set refused and logged; reference counting; LRU eviction beyond the cap
- [ ] Implement the cache and the PostgreSQL question-set repository

## Done when

- [ ] cache used by the gateway
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
