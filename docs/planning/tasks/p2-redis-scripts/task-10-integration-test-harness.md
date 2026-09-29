# task-10: Integration test harness

- **Phase:** P2 Redis scripts (TDD)
- **Size:** S · **TDD:** —
- **Status:** todo
- **Implements:** TRD §10.2 · D14
- **Depends on:** [task-02](../p0-foundation/task-02-platform-packages.md), [task-04](../p0-foundation/task-04-migrations-and-seed-data.md), [task-06](../p1-domain/task-06-identifiers-quiz-codes-validation.md), [task-07](../p1-domain/task-07-scoring-and-ranks.md), [task-08](../p1-domain/task-08-quiz-state-machine.md)
- **Unblocks:** [task-11](task-11-scripts-create-room-start-join.md)

## Steps

- [ ] testcontainers helpers: Redis 7.4, PostgreSQL 16 with migrations applied, Toxiproxy with proxies for both; shared per package via `TestMain`
- [ ] Test: every embedded script loads; after `SCRIPT FLUSH`, a call reloads transparently
- [ ] Port task-04's seed-validation checks into a Go integration test against the migrated test database
- [ ] `make test-integration` (build tag `integration`) runs locally and is documented for CI

## Done when

- [ ] Helpers start Redis, PostgreSQL, and Toxiproxy with testcontainers once per package (`integration` build tag)
- [ ] Script loader test: all embedded scripts load; `NOSCRIPT` triggers reload
- [ ] `make test-integration` runs in CI and locally
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
