# task-26: Test kit

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** yes
- **Status:** done
- **Implements:** TRD §10.3 · D14
- **Depends on:** [task-25](task-25-full-local-stack.md)
- **Unblocks:** [task-27](task-27-end-to-end-and-fault-tests.md), [task-28](task-28-simulator-and-k6.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- the kit's own tests: schema validation of received messages, monotonic state, independent score recomputation (against task-07 vectors), latency recording

## Steps

- [x] Tests: the kit's client validates every received message against its schema and fails on violations
- [x] Tests: version rules (older ignored and counted, conflicting same-version events flagged); independent score recomputation matches the task-07 vectors; latency percentiles; NFR-6 coverage rule; answer choice ratios; 503 retry
- [x] Implement the client, the `Compose` environment, the room scenario runner, fault steps, and result checks

## Done when

- [x] `testkit` supports the `Compose` environment, scenarios, fault steps, and assertions (no `InProcess` environment, see Notes)
- [x] `make check` green

## Notes and decisions

- **Compose only.** An `InProcess` environment would need the service wiring moved out of the `main` packages; the `cmd/*` integration tests already cover that wiring in-process. TRD §10.3 now describes the kit as built.
- **Scenarios pick a question set.** The create-quiz API has no question-count option, so the TRD's `questions: 5` can't be honoured without an API change.
- **Answer keys come from PostgreSQL**, as harness access: clients never see a key before the reveal (FR-21), but the kit needs one to answer right or wrong on purpose.
- **NFR-6 is approximated** (documented in TRD §10.3): the first leaderboard version that begins arriving after an answer is taken to include it.
- **Large runs:** `DialOptions.NoLog` keeps counters only, so 10,000 simulated clients don't each hold every message.
- `metricstest.Parse` was split out so the kit can read worker metrics through `docker exec` into the nginx container.

## Verification

- Unit tests written first and seen failing: score vectors, percentiles, the client against a fake server (schema violation, stale versions, conflicting versions, close code, 503 retry), NFR-6 coverage, answer ratios.
- **First live run** (`TestE2E_QuizRunsToFinish`, 30 players, 3 questions): 30/30 joined and finished, 82 answers accepted, every answer's points and every archived total matched the independent recomputation, reconciliation mismatches 0. NFR-6 p95 168 ms, NFR-7 p95 1.4 ms, NFR-8 p95 2.8 ms, NFR-9 p95 0.9 ms.
- **The check can fail:** with the kit's formula off by one point, the same run reported `server 198, expected 199` for each answer and failed.
- `make check` green, with `e2e`-tagged files now linted too.

## AI collaboration

See [AI-037](../../../ai-collaboration/log.md).
