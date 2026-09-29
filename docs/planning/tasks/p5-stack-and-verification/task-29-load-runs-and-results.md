# task-29: Load runs and results

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** —
- **Status:** todo
- **Implements:** TRD §10.7, non-functional §1–2 · NFR-1–NFR-11
- **Depends on:** [task-28](task-28-simulator-and-k6.md)
- **Unblocks:** [task-30](../p6-submission/task-30-root-readme-and-run-instructions.md), [task-31](../p6-submission/task-31-ai-collaboration-docs.md)

## Steps

- [ ] Run all five scenarios; `big-room` at 5,000 then 10,000 participants
- [ ] Profile the hottest path (pprof on the gateway, Redis `INFO commandstats`); fix or document at least one bottleneck
- [ ] Write `docs/testing.md`: machine spec, config, results per scenario, what was fixed
- [ ] Replace or confirm every estimate in `non-functional.md` with the measured value (script cost, memory per connection, ceiling)

## Done when

- [ ] All five scenarios run; `big-room` at 5,000 and then 10,000 participants
- [ ] `docs/testing.md` records the results with the machine spec
- [ ] The **estimates in `non-functional.md` are replaced or confirmed** with measured values: per-script Redis cost, memory per connection, the ceiling
- [ ] At least one bottleneck found and fixed or documented
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
