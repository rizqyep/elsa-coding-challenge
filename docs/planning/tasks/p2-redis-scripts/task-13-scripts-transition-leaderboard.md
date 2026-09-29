# task-13: Scripts: `transition`, `leaderboard`

- **Phase:** P2 Redis scripts (TDD)
- **Size:** L · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §4.3, §4.4 · FR-2, FR-4, FR-23–FR-26, NFR-14
- **Depends on:** [task-12](task-12-script-answer.md)
- **Unblocks:** [task-14](task-14-flush-scripts-and-postgresql-repositories.md), [task-19](../p3-services/task-19-gateway-registry-and-room-events.md), [task-21](../p3-services/task-21-worker.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- the **same** `transition_cases.json` as task-08, against Lua
- stale version → `stale`
- not due → `not_due` + schedule repaired
- **two workers, one due transition → one `applied`, one `stale`**
- events published with the right fields
- leaderboard order, names, version increments, shared ranks

## Steps

- [ ] Test: run task-08's `transition_cases.json` against the Lua script
- [ ] Tests: stale version → `stale`; not due → `not_due` and schedule repaired; terminal cleanup
- [ ] Race: two workers, one due transition → exactly one `applied`
- [ ] Race: answers fired across `closeAt` while the transition runs → each accepted with correct points or rejected, never both
- [ ] Tests: published `state`/`finished` events (subscribe in the test and check fields); leaderboard order, names, version, shared ranks
- [ ] Write `transition.lua`, `leaderboard.lua`; internal event types with a tolerant decoder (`{}` accepted as an empty list)

## Done when

- [ ] scripts + repository wrappers
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
