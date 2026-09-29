# task-13: Scripts: `transition`, `leaderboard`

- **Phase:** P2 Redis scripts (TDD)
- **Size:** L · **TDD:** yes
- **Status:** done (one design question open: see notes)
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

- [x] Test: run task-08's `transition_cases.json` against the Lua script
- [x] Tests: stale version → `stale`; not due → `not_due` and schedule repaired; terminal cleanup
- [x] Race: two workers, one due transition → exactly one `applied`
- [x] Race: answers fired across `closeAt` while the transition runs → each accepted with correct points or rejected, never both
- [x] Tests: published `state`/`finished` events (subscribe in the test and check fields); leaderboard order, names, version, shared ranks
- [x] Write `transition.lua`, `leaderboard.lua`; internal event types with a tolerant decoder (`{}` accepted as an empty list)

## Done when

- [x] scripts + repository wrappers
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- **Lua state machine tested against the shared vectors without a time override:** `next.lua` is a direct port of `quiz.Next`, prepended to `transition.lua`; tests call that exact function over `transition_cases.json` (same approach as `points.lua` in task-12). `transition.lua` reads the room, calls `next_state`, then applies the side effects: question ID from the list, schedule, flush job + `pending_flush` + dirty mark at close, final-results job at finish/expiry, and one event (`state`, or `finished` with the final top 10).
- `top.lua` (top entries with names) is shared by `transition` and `leaderboard`. It lives in `quiz`, and `leaderboard` prepends `quiz.TopLua`, because `leaderboard` already imports `quiz` and the reverse would be a cycle. `quiz.TopN = 10`; `leaderboard.TopN` refers to it.
- Worker-facing API: `DueTransitions(now, limit)`, `ApplyTransition(code)` (reads the version, then applies), `ApplyTransitionAt(code, ver)`; `leaderboard.PopDirty(n)` (`SPOP`) and `PublishSnapshot(code)`.
- **Open design question: the expected-version check.** A mutation that removed it was *not* caught by the competing-workers test, and on analysis it can't be. Exactly-once comes from the script deciding what is due and applying it atomically: after any applied transition the next one is in the future, so the other workers see `not_due`. The version check only makes a worker holding a stale read skip a transition that is genuinely due, which another worker (or its next poll) applies anyway. It costs one `HGET` per attempt. Keep as defense in depth, or remove for simplicity? The TRD now describes the actual mechanism.

## Verification

- 13 transition tests + 4 leaderboard tests on real Redis, all passing under `-race`; `make test-integration` and `make check` green.
- **Parity:** the Lua `next_state` passes all 14 shared transition vectors.
- **Exactly-once:** 20 workers applying the same due transition → exactly 1 applied, version +1.
- **Answers racing the close:** 20 answers spread across the close time while a worker applies it; every accepted answer was received before the final close time, stored answers = accepted answers, and the leaderboard total = sum of accepted points.
- **Mutation checks:**
  1. Lua reveal timed from `close_at` → vectors fail. Caught.
  2. Version check removed → **not caught**; analysis above (open question).
  3. Answer deadline check removed → **not caught by the race test** (the gap between `close_at` and the worker's close is ~1 ms), but caught by task-12's dedicated late-answer test. The two properties are covered by different tests, as intended.
  4. Leaderboard version not incremented → monotonic-version test fails. Caught.
- Lint caught a `!=` comparison with an error value in a new test; changed to `errors.Is`.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
