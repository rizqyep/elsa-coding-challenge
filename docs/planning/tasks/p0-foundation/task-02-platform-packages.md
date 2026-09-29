# task-02: Platform packages

- **Phase:** P0 Foundation
- **Size:** M · **TDD:** partial
- **Status:** todo
- **Implements:** TRD §2, §9.2, §9.3 · NFR-28, NFR-34
- **Depends on:** [task-01](task-01-repository-scaffold.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md), [task-15](../p3-services/task-15-auth.md), [task-17](../p3-services/task-17-question-cache.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- config reports **all** invalid variables at once
- required variables
- range checks. Backoff: full-jitter bounds, cap, budget exhaustion

## Steps

- [ ] Tests: config reports all invalid variables in one error; required variables; range checks; each service loads only its own subset
- [ ] Implement `platform/config` (typed structs per service, validation collecting all errors)
- [ ] Implement `platform/logging`: slog JSON handler; context helpers for quiz, participant, connection, request IDs
- [ ] Tests: backoff stays within `[0, min(cap, base·2^n)]`, stops at the budget, respects context cancellation (injectable random source and clock)
- [ ] Implement `platform/retry`
- [ ] Implement `platform/redisx`: client from config; `keys.go` with every key from TRD §4.2; test that all of a room's keys hash to the same Cluster slot (CRC16 of the hash tag)
- [ ] Implement the script loader: `//go:embed`, `SCRIPT LOAD` at startup, `EVALSHA` with reload on `NOSCRIPT`
- [ ] Implement `platform/pgx` (pool, ping, per-call timeouts) and the `platform/metrics` registry + `/metrics` handler

## Done when

- [ ] `platform/config` for all variables in TRD §2, per service
- [ ] `platform/logging` (slog JSON with quiz/participant/connection/request ID helpers)
- [ ] `platform/retry` (full-jitter backoff with budget)
- [ ] `platform/redisx` (client, `//go:embed` script loader with `EVALSHA`/reload, `keys.go`)
- [ ] `platform/pgx` (pool, ping)
- [ ] `platform/metrics` registry skeleton
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
