# task-26: Test kit

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §10.3 · D14
- **Depends on:** [task-25](task-25-full-local-stack.md)
- **Unblocks:** [task-27](task-27-end-to-end-and-fault-tests.md), [task-28](task-28-simulator-and-k6.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- the kit's own tests: schema validation of received messages, monotonic state, independent score recomputation (against task-07 vectors), latency recording

## Steps

- [ ] Tests: the kit's client validates every received message against its schema and fails on violations
- [ ] Tests: monotonic state; independent score recomputation matches task-07 vectors; latency recorder percentiles
- [ ] Implement the client, `Compose` and `InProcess` environments, the scenario builder, fault steps, and assertions

## Done when

- [ ] `testkit` supports `Compose` and `InProcess` environments, scenarios, fault steps, and assertions
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
