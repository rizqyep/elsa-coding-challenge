# task-12: Script: `answer`

- **Phase:** P2 Redis scripts (TDD)
- **Size:** L · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §3.4, §4.4 · FR-5, FR-16–FR-22, NFR-12, NFR-13
- **Depends on:** [task-11](task-11-scripts-create-room-start-join.md)
- **Unblocks:** [task-13](task-13-scripts-transition-leaderboard.md), [task-18](../p3-services/task-18-gateway-connections.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- the **same** `points_cases.json` vectors as task-07, run against Lua
- acceptance order; late by 1 ms rejected
- duplicate returns the original; **100 concurrent submits by one participant score once**
- answers racing a transition are either accepted correctly or rejected, never both
- early close when all online have answered; offline participants ignored
- `WAIT` sent in the same pipeline (verified with a replica container)

## Steps

- [ ] Test: run task-07's `points_cases.json` against the Lua script (same file, no copies)
- [ ] Tests: acceptance order table; late by 1 ms rejected; `not_joined`; duplicate returns the stored original
- [ ] Concurrency: 100 concurrent submits by one participant → one `accepted`, total counted once
- [ ] Tests: early close when all online have answered; offline participants ignored; `Deadline` unchanged
- [ ] Test with a replica container: `WAIT` sent in the same pipeline actually waits
- [ ] Write `answer.lua`; the scoring repository (pipeline `EVALSHA` + `WAIT`)
- [ ] Scoring service with gomock tests: timeout → `server_busy`, **no retry**; unknown script status → `internal`

## Done when

- [ ] script + repository + `scoring` service (gomock tests: timeout → `server_busy`, no retry)
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
