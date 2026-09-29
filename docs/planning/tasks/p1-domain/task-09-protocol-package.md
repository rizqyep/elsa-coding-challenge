# task-09: Protocol package

- **Phase:** P1 Domain (TDD)
- **Size:** M · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §6.3, §3.2 · FR-11, FR-20, FR-21, FR-26, NFR-33
- **Depends on:** [task-05](../p0-foundation/task-05-code-generation-and-contract-checks.md)
- **Unblocks:** [task-18](../p3-services/task-18-gateway-connections.md), [task-24](../p4-client/task-24-react-client.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- every Go message type encodes to JSON that validates against its schema
- every schema example decodes into the Go type and round-trips
- converting a `Question` into `PublicQuestion` drops the answer key
- unknown fields ignored on decode
- `v ≠ 1` rejected

## Steps

- [ ] Tests: every Go message type encodes to JSON valid against its schema; every schema example decodes and round-trips; unknown fields ignored; `v ≠ 1` and unknown `type` rejected
- [ ] Test: converting a `Question` to `PublicQuestion` drops the answer key; the protocol package has no function accepting `Question`
- [ ] Implement the envelope, the 14 message types, and the decoder (typed dispatch on `type`)
- [ ] Implement encoder helpers used by the gateway (one encode per broadcast)

## Done when

- [ ] envelope + all 14 message types
- [ ] `PublicQuestion` is the only question type the protocol package accepts
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
