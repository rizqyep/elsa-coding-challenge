# task-02: Platform packages

- **Phase:** P0 Foundation
- **Size:** M · **TDD:** partial
- **Status:** done
- **Implements:** TRD §2, §9.2, §9.3 · NFR-28, NFR-34
- **Depends on:** [task-01](task-01-repository-scaffold.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md), [task-15](../p3-services/task-15-auth.md), [task-17](../p3-services/task-17-question-cache.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- config reports **all** invalid variables at once
- required variables
- range checks. Backoff: full-jitter bounds, cap, budget exhaustion

## Steps

- [x] Tests: config reports all invalid variables in one error; required variables; range checks; each service loads only its own subset
- [x] Implement `platform/config` (typed structs per service, validation collecting all errors)
- [x] Implement `platform/logging`: slog JSON handler; context helpers for quiz, participant, connection, request IDs
- [x] Tests: backoff stays within `[0, min(cap, base·2^n)]`, stops at the budget, respects context cancellation (injectable random source and clock)
- [x] Implement `platform/retry`
- [x] Implement `platform/redisx`: client from config; `keys.go` with every key from TRD §4.2; test that all of a room's keys hash to the same Cluster slot (CRC16 of the hash tag)
- [x] Implement the script loader (its integration test against real Redis is in task-10): `//go:embed`, `SCRIPT LOAD` at startup, `EVALSHA` with reload on `NOSCRIPT`
- [x] Implement `platform/postgres` (pool, ping, per-call timeouts) and the `platform/metrics` registry + `/metrics` handler

## Done when

- [x] `platform/config` for all variables in TRD §2, per service
- [x] `platform/logging` (slog JSON with quiz/participant/connection/request ID helpers)
- [x] `platform/retry` (full-jitter backoff with budget)
- [x] `platform/redisx` (client, `//go:embed` script loader with `EVALSHA`/reload, `keys.go`)
- [x] `platform/postgres` (pool, ping)
- [x] `platform/metrics` registry skeleton
- [x] `make check` green

## Notes and decisions

- **My test review decided (to avoid over-engineering):**
  - keep the ≥ 32-byte `AUTH_SIGNING_KEY` check;
  - no production-only rule for dev tokens, since there's no real production;
  - the leaderboard is a fixed top 10, so `LEADERBOARD_TOP_N` was removed from config, `.env.example`, and the TRD;
  - keep the ping/pong and presence ordering rules.
- `config.Variables()` is derived by running every loader against a recording lookup. `.env.example` is tested against it, so the two can't drift.
- The script loader wraps go-redis `Script`. `Run` falls back from `EVALSHA` to `EVAL`, but that fallback can't happen inside a pipeline, so `LoadScripts` at startup is required. The doc comment says why.
- Added post-hoc tests for logging context fields; that package wasn't in the tests-first list.

## Verification

- retry 7/7, config (defaults × 3, 25 invalid values, all-errors-at-once, per-service isolation, overrides, `.env.example` sync), redisx keys 3/3, logging 2/2: all pass under `-race`.
- **Weak test fixed:** my key-name test was a map keyed by the actual names. Two functions wrongly returning the same name would silently overwrite each other's case. Changed to an ordered list of pairs.
- Lint found 8 issues after implementation: missing doc comments, an unchecked `Close` in a test, formatting, and a password inside a test DSN. All fixed rather than suppressed. The only exclusion is gosec in `_test.go` files, for reading repo fixtures.
- `make check` exit 0.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
