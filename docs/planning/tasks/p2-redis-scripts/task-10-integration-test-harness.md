# task-10: Integration test harness

- **Phase:** P2 Redis scripts (TDD)
- **Size:** S · **TDD:** —
- **Status:** done
- **Implements:** TRD §10.2 · D14
- **Depends on:** [task-02](../p0-foundation/task-02-platform-packages.md), [task-04](../p0-foundation/task-04-migrations-and-seed-data.md), [task-06](../p1-domain/task-06-identifiers-quiz-codes-validation.md), [task-07](../p1-domain/task-07-scoring-and-ranks.md), [task-08](../p1-domain/task-08-quiz-state-machine.md)
- **Unblocks:** [task-11](task-11-scripts-create-room-start-join.md)

## Steps

- [x] testcontainers helpers: Redis 7.4, PostgreSQL 16 with migrations applied, Toxiproxy with proxies for both; shared per package via `TestMain`
- [x] Test: every embedded script loads; after `SCRIPT FLUSH`, a call reloads transparently
- [x] Port task-04's seed-validation checks into a Go integration test against the migrated test database
- [x] `make test-integration` (build tag `integration`) runs locally and is documented for CI

## Done when

- [x] Helpers start Redis, PostgreSQL, and Toxiproxy with testcontainers once per package (`integration` build tag)
- [x] Script loader test: all embedded scripts load; `NOSCRIPT` triggers reload
- [x] `make test-integration` runs in CI and locally
- [x] `make check` green

## Notes and decisions

- `internal/platform/testenv` (build tag `integration`) starts Redis 7.4, PostgreSQL 16 (schema + seed applied), and Toxiproxy on one Docker network, once per test package, via `TestMain`: `os.Exit(testenv.Run(m, &env))`.
- `Env` has direct clients and `*ViaProxy` clients/DSNs; faults injected on the proxies (`env.Proxy(t, testenv.RedisProxy)`) only affect the proxied connections. `env.Reset(t)` flushes Redis and removes all faults.
- Goose logic moved from `cmd/migrate` into `migrations.Apply`, so the command and the tests migrate identically.
- `make test-integration` runs everything with `-tags integration` (needs Docker, ~30 s). It isn't part of `make check`, which stays Docker-free. golangci-lint now lints integration files too (`build-tags: [integration]`).

## Verification

- `make test-integration`: all packages pass (≈ 30 s).
- Ran `-v` to confirm the integration tests actually executed (9 tests), not just compiled.
- **Toxiproxy smoke test:** 300 ms latency toxic → ping ≥ 300 ms; proxy disabled → ping fails; reset → ping works. **Mutation:** latency aimed at the wrong proxy → ping 0.2 ms and "succeeded while disabled", test fails. Restored.
- **Script loader:** after `SCRIPT FLUSH`, `EvalSha` returns `NOSCRIPT` (why scripts must be loaded at startup for pipelines), `Run` recovers.
- Seed validation (ported from task-04's SQL checks) passes on a freshly migrated database.
- Cleanup: no Redis/PostgreSQL/Toxiproxy containers left afterwards; the one remaining container was testcontainers' Ryuk reaper, which exits by itself.
- `make check` green; `cmd/migrate` still works after the refactor (`make migrate` → nothing to apply).

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
