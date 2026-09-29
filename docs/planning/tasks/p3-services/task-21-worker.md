# task-21: Worker

- **Phase:** P3 Services
- **Size:** L · **TDD:** yes
- **Status:** done
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

- [x] Tests: loop runner skips ticks on overrun; bounded pool; liveness fails after 5× interval without a completed pass
- [x] Tests (gomock): flush job and finalise job failure paths per TRD §4.5 (already covered by task-14's `history` service tests)
- [x] Implement the loop runner and the three loops (transitions, leaderboard, flush)
- [ ] ~~Pass the worker's trace ID into scripts~~ dropped: tracing is out of scope under D17
- [x] `cmd/worker` wiring

## Done when

- [x] `cmd/worker` runs all three loops
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- **Package:** `internal/scheduler` (the placeholder from task-01): a generic `Runner` plus the three loops, over small interfaces the existing repositories already satisfy.
- **Runner:** one goroutine per loop on a `time.Ticker`. A pass that overruns makes the ticker drop ticks, so passes never overlap or pile up. Loops don't wait for each other. Items run in a bounded pool (16 / 16 / 4); one failed item is logged and never stops the rest.
- **Stopping:** passes get a context detached from the stop signal, so claimed work finishes (TRD §8.4); per-item deadlines bound it. The 1012-style drain for the gateway stays in task-22.
- **Liveness:** fails when a loop hasn't completed a pass in 5× its interval. A failed pass counts as completed, because a restart wouldn't fix Redis; readiness covers that.
- **Clock:** claims use Redis-aligned time. The clock moved from `realtime` to `platform/redisx`, so the worker doesn't import the WebSocket package. The empty `fanout` placeholder is removed (its work lives in `realtime`).
- **Dropped (D17):** passing trace IDs into scripts. Metrics for the loops belong to the task-23 scaffold.
- **Gomock failure paths for flush and finalise** were already written in task-14's `history` service tests, so none are duplicated here.

## Verification

- Scheduler unit tests: passes never overlap and overruns skip ticks; loops are independent; stop lets the in-flight pass finish uncancelled and starts nothing new; liveness names only the stalled loop, not a failing one; the pool is bounded and complete; each loop claims with the right clock, batch and visibility, gives every item a deadline, and reports failures. Run 4 times in a row, since they depend on timing.
- Integration, through the real `cmd/worker` wiring: **two workers run a quiz on their own**, with nothing forcing time. Three questions open, close early once both participants answered, reveal and advance, finishing in about 1 s despite 5 s windows. Every state version is published exactly once despite two competing workers. Leaderboard updates are published, the answers and results are saved (6 answers; the leader's saved total equals the points returned), the room is released, and `/healthz` stays 200.
- 16 deliberate breaks, all caught. Three harness mistakes (an unused import, an undefined type, and zsh not splitting `-tags integration`) were redone until the mutants built.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
