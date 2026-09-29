# task-08: Quiz state machine

- **Phase:** P1 Domain (TDD)
- **Size:** M · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §3.3, §3.7 · FR-2, FR-3, FR-4, FR-5, FR-6, FR-7
- **Depends on:** [task-01](../p0-foundation/task-01-repository-scaffold.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- `testdata/transition_cases.json`: every row of the transition table, not-due cases, start vs lobby expiry, last question → finished, early close (`CloseAt` moves, `Deadline` doesn't), terminal states, version increments

## Steps

- [ ] Write `internal/quiz/testdata/transition_cases.json`: one case per transition-table row, plus not due, start vs lobby expiry, last question → finished, early close, terminal states
- [ ] Tests: `Next` over every vector; `Start` and `EarlyClose` command guards; version increments; returned events
- [ ] Implement `Room`, `Status`, `Next`, commands, and events (TRD §3.3, §3.7)
- [ ] Note in the file header that task-13 runs the same vectors against Lua

## Done when

- [ ] `Next` and the commands pass all vectors
- [ ] events returned per §3.7
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
