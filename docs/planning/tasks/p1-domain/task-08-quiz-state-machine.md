# task-08: Quiz state machine

- **Phase:** P1 Domain (TDD)
- **Size:** M · **TDD:** yes
- **Status:** done (awaiting my code review)
- **Implements:** TRD §3.3, §3.7 · FR-2, FR-3, FR-4, FR-5, FR-6, FR-7
- **Depends on:** [task-01](../p0-foundation/task-01-repository-scaffold.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- `testdata/transition_cases.json`: every row of the transition table, not-due cases, start vs lobby expiry, last question → finished, early close (`CloseAt` moves, `Deadline` doesn't), terminal states, version increments

## Steps

- [x] Write `internal/quiz/testdata/transition_cases.json`: one case per transition-table row, plus not due, start vs lobby expiry, last question → finished, early close, terminal states
- [x] Tests: `Next` over every vector; `Start` and `EarlyClose` command guards; version increments; returned events
- [x] Implement `Room`, `Status`, `Next`, commands, and events (TRD §3.3, §3.7)
- [x] Note in the file header that task-13 runs the same vectors against Lua

## Done when

- [x] `Next` and the commands pass all vectors
- [x] events returned per §3.7
- [x] `make check` green

## Notes and decisions

- `Next(room, now) (Room, Outcome, Event)` is pure. Outcomes are `applied`, `not_due`, and `terminal`; `StateVersion` increments only on `applied`.
- **Rules made explicit by the vectors (worth checking in review):**
  - When start and lobby expiry are both due, **start wins**.
  - After a late close (e.g. worker downtime), the **reveal is timed from the actual close**, so participants still get the full reveal.
  - A question opened late gets a **full window from when it actually opens**. Nobody loses answer time to an outage.
- `Start` doesn't bump the version: it only makes the transition due, and the worker's transition bumps it. A second start is a no-op (FR-3).
- `EarlyClose` moves only `CloseAt`/`NextTransitionAt`, never `Deadline` (the speed bonus uses it).
- `Room` carries JSON tags so the shared vector file maps onto it directly. The test decodes with unknown fields disallowed, so a renamed field in the vectors can't be silently ignored.

## Verification

- **Vectors cross-checked before implementing:** a separate Python model of the TRD §3.3 table agreed with all 14 hand-written expectations.
- 14 vector cases, 5 `Start` cases, 6 `EarlyClose` cases: all pass under `-race`; golangci-lint 0 issues.
- The strict decoder caught one thing on first run: the file's top-level `description` wasn't declared in the test. It was added, and the strictness kept.
- **Mutation checks:** (1) early close also moving `Deadline` → caught by `TestEarlyClose`; (2) reveal timed from `CloseAt` instead of the actual close → caught by the "close applied late" vector. Both restored and green.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
