# task-11: Scripts: `create_room`, `start`, `join`

- **Phase:** P2 Redis scripts (TDD)
- **Size:** M · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §4.2, §4.4 · FR-1, FR-3, FR-8–FR-13
- **Depends on:** [task-10](task-10-integration-test-harness.md)
- **Unblocks:** [task-12](task-12-script-answer.md), [task-16](../p3-services/task-16-rest-api-service.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- create writes every field and schedules expiry
- start: only host, only lobby, needs a participant, idempotent
- join: unknown/expired/finished paths, rejoin keeps score, **1,000 concurrent joins → no loss, no duplicates**

## Steps

- [ ] Tests `create_room`: every field written, TTLs set, expiry scheduled; `code_in_use` when the room exists
- [ ] Tests `start`: only host, only lobby, needs a participant, idempotent
- [ ] Tests `join`: unknown / expired / finished paths; rejoin keeps score and updates the name; snapshot contents
- [ ] Concurrency test: 1,000 concurrent joins → roster = leaderboard = 1,000
- [ ] Write the three Lua scripts
- [ ] Go repository wrappers decoding script replies into domain types

## Done when

- [ ] scripts + Go repository wrappers
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
