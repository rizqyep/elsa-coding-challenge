# task-21: Worker

- **Phase:** P3 Services
- **Size:** L · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §8, §4.5 · FR-2, FR-4, FR-5, FR-25, FR-32–FR-35, NFR-10, NFR-14
- **Depends on:** [task-13](../p2-redis-scripts/task-13-scripts-transition-leaderboard.md), [task-14](../p2-redis-scripts/task-14-flush-scripts-and-postgresql-repositories.md)
- **Unblocks:** [task-22](task-22-shutdown-and-degraded-modes.md), [task-23](task-23-observability.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- loops claim and process in bounded pools
- a pass overrunning its interval skips ticks
- flush and finalise jobs per §4.5 (gomock failure paths)
- liveness fails when a loop stalls

## Steps

- [ ] Tests: loop runner skips ticks on overrun; bounded pool; liveness fails after 5× interval without a completed pass
- [ ] Tests (gomock): flush job and finalise job failure paths per TRD §4.5
- [ ] Implement the loop runner and the three loops (transitions, leaderboard, flush)
- [ ] Pass the worker's trace ID into scripts so published events carry it (non-functional §4.2)
- [ ] `cmd/worker` wiring

## Done when

- [ ] `cmd/worker` runs all three loops
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
