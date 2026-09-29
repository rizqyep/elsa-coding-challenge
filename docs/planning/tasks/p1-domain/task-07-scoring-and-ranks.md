# task-07: Scoring and ranks

- **Phase:** P1 Domain (TDD)
- **Size:** S · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §3.4, §3.5 · FR-19, FR-24
- **Depends on:** [task-01](../p0-foundation/task-01-repository-scaffold.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- `testdata/points_cases.json` covering: answer at `openedAt` (200), 1 ms before the deadline, exactly at the deadline, wrong answer (0), early close doesn't change points, window 5 s and 120 s. Shared ranks 1, 2, 2, 4

## Steps

- [ ] Write `internal/scoring/testdata/points_cases.json`: at `openedAt` (200), 1 ms after, 1 ms before the deadline, at the deadline, wrong answer (0), early close unchanged, windows of 5 s and 120 s
- [ ] Tests: `Points` over every vector; `Ranks` table (ties → 1, 2, 2, 4; all zero; single participant)
- [ ] Implement `Points` (integer milliseconds) and `Ranks`
- [ ] Note in the file header that task-12 runs the same vectors against Lua

## Done when

- [ ] `Points` and `Ranks` pass all vectors
- [ ] integer-millisecond arithmetic only
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
