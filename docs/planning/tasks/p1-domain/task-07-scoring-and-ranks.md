# task-07: Scoring and ranks

- **Phase:** P1 Domain (TDD)
- **Size:** S · **TDD:** yes
- **Status:** done (awaiting my code review)
- **Implements:** TRD §3.4, §3.5 · FR-19, FR-24
- **Depends on:** [task-01](../p0-foundation/task-01-repository-scaffold.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- `testdata/points_cases.json` covering: answer at `openedAt` (200), 1 ms before the deadline, exactly at the deadline, wrong answer (0), early close doesn't change points, window 5 s and 120 s. Shared ranks 1, 2, 2, 4

## Steps

- [x] Write `internal/scoring/testdata/points_cases.json`: at `openedAt` (200), 1 ms after, 1 ms before the deadline, at the deadline, wrong answer (0), early close unchanged, windows of 5 s and 120 s
- [x] Tests: `Points` over every vector; `Ranks` table (ties → 1, 2, 2, 4; all zero; single participant)
- [x] Implement `Points` (integer milliseconds) and `Ranks`
- [x] Note in the file header that task-12 runs the same vectors against Lua

## Done when

- [x] `Points` and `Ranks` pass all vectors
- [x] integer-millisecond arithmetic only
- [x] `make check` green

## Notes and decisions

- **Integer milliseconds, not `time.Time`:** `Points(correct, openedAt, deadline, receivedAt int64)`. The Lua script receives the same integers from Redis `TIME`, so both sides do identical integer arithmetic on identical inputs. (TRD §3.4 sketched a `time.Time` signature; this is the stricter choice.)
- `deadline` is the original deadline, so an early close can't change points; one vector covers this explicitly.
- `CompetitionRanks` works on unsorted input and keeps positions. `RankOf(higher)` is what the gateway uses with `ZCOUNT` at question close.
- `testdata/points_cases.json` has a header saying it is shared with task-12: edit, never copy.

## Verification

- **Vectors checked independently before implementing:** all 14 expected values recomputed from the formula in Python; 0 mismatches.
- 6 tests pass under `-race`, including range (100–200 for correct, 0 for wrong), "a later answer never scores more", and a zero-length-window guard. golangci-lint 0 issues.
- **Mutation check:** rounding instead of flooring failed the shared vectors; restored and green.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
