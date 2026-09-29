# task-27: End-to-end and fault tests

- **Phase:** P5 Stack and verification
- **Size:** L · **TDD:** —
- **Status:** todo
- **Implements:** TRD §10.4 (E2E rows), §10.6 · FR-12, FR-22, FR-29–FR-31, NFR-14, NFR-15
- **Depends on:** [task-26](task-26-test-kit.md)
- **Unblocks:** —

## Steps

- [ ] Implement every `E2E_*` test from TRD §10.4
- [ ] Implement every fault scenario from TRD §10.6 (Toxiproxy and container kills)
- [ ] Every test asserts `score_reconciliation_mismatches_total == 0` at the end
- [ ] `make test-e2e` runs them against a fresh stack

## Done when

- [ ] Every `E2E_*` test in §10.4 passes
- [ ] Every scenario in the §10.6 fault table passes
- [ ] `score_reconciliation_mismatches_total` is 0 after each run
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
