# task-27: End-to-end and fault tests

- **Phase:** P5 Stack and verification
- **Size:** L · **TDD:** —
- **Status:** done
- **Implements:** TRD §10.4 (E2E rows), §10.6 · FR-12, FR-22, FR-29–FR-31, NFR-14, NFR-15
- **Depends on:** [task-26](task-26-test-kit.md)
- **Unblocks:** —

## Steps

- [x] Implement every `E2E_*` test from TRD §10.4
- [x] Implement every fault scenario from TRD §10.6 (Toxiproxy and container kills)
- [x] Every room run asserts `score_reconciliation_mismatches_total == 0` at the end, plus independent per-answer and per-total checks
- [x] `make test-e2e` runs them against the running stack (`make up PROFILES=chaos` first)

## Done when

- [x] Every `E2E_*` test in §10.4 passes
- [x] Every scenario in the §10.6 fault table passes
- [x] `score_reconciliation_mismatches_total` is 0 after each run
- [x] `make check` green

## Notes and decisions

- **Tests:** `E2E_QuizRunsToFinish`, `E2E_SecondConnectionKicksFirst`, `E2E_ResendAfterReconnect_NoDoubleScore`, `E2E_ReconnectResumesAtCurrentQuestion_MissedScoresZero`, `E2E_HostDisconnectAfterStart_QuizContinues`, `E2E_TwoGateways_SameTotals`; faults: Redis latency, Redis connection reset mid-answer, Redis down 5 s, PostgreSQL paused across a close, gateway killed mid-question, worker killed during a flush, all workers stopped.
- **The kick test goes through nginx,** so it can't force the two connections onto different gateways. The cross-gateway path itself is pinned by `cmd/ws`'s `TestKick_AcrossGateways`. `E2E_TwoGateways_SameTotals` checks, mid-run, that every gateway really holds players.
- **"All workers stopped" is tested directly:** an answer sent 500 ms after `closeAt` while no worker runs must get `question_closed`. The room runner also flags any accepted answer received at or after its question's (possibly early-closed) `closeAt`.
- **Two faults have no client-visible effect,** by design: the PostgreSQL pause and the worker kill. Their check is that totals and reconciliation still match. The run can't prove the kill landed mid-flush; that exact crash is pinned by the task-14 integration test (`crash between commit and ack`).
- Each fault test restores the stack in its cleanup (reset toxics, start stopped containers, wait until healthy), so tests don't leak faults into each other.

## Verification

`make test-e2e` on the chaos stack: 13/13 pass in 305 s. What each fault did, from the logs:

| Scenario | Evidence the fault hit | Outcome |
|---|---|---|
| Redis +100 ms | NFR-7 p95 1.5 ms → 103 ms | 114/114 accepted, totals match |
| Connection reset | 4 × `server_busy`; the resends came back `duplicate` with the original results | nothing counted twice |
| Redis down 5 s | 111 × `server_busy`, retried after the outage | 114/114 accepted, quiz resumed |
| Gateway killed | 20 × close 1006, 20 reconnects | 112 answers (2 players still reconnecting missed a question, scored 0), totals match |
| All workers stopped | late answer refused with `question_closed` | quiz resumed when workers returned |
| PostgreSQL paused, worker killed | none visible (by design) | totals and reconciliation match |

Every room run: 0 schema violations, 0 point mismatches, 0 total mismatches, reconciliation metric 0.

## AI collaboration

See [AI-038](../../../ai-collaboration/log.md).
