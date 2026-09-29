# task-09: Protocol package

- **Phase:** P1 Domain (TDD)
- **Size:** M · **TDD:** yes
- **Status:** done
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

- [x] Tests: every Go message type encodes to JSON valid against its schema; every schema example decodes and round-trips; unknown fields ignored; `v ≠ 1` and unknown `type` rejected
- [x] Test: converting a `Question` to `PublicQuestion` drops the answer key; the protocol package has no function accepting `Question`
- [x] Implement the envelope, the 14 message types, and the decoder (typed dispatch on `type`)
- [x] Implement encoder helpers used by the gateway (one encode per broadcast)

## Done when

- [x] envelope + all 14 message types
- [x] `PublicQuestion` is the only question type the protocol package accepts
- [x] `make check` green

## Notes and decisions

- **Client frames are validated against the JSON Schemas at runtime** (`DecodeClient`), not by hand-written Go checks, so the gateway can't drift from the contract (NFR-20). `make generate` copies `docs/api/schemas` into the package for `go:embed` (Go can't embed outside its module); `check-generated` guards the copy.
- **Strict inbound, lenient outbound:** the schemas reject unknown fields, so the server rejects them in client frames. The "ignore unknown fields" forward-compatibility rule applies to clients reading server messages (task-24). This replaces the task's original "unknown fields ignored on decode" item.
- `quiz.Question` (with answer key) → `Public()` → `protocol.QuestionFrom`. The protocol package never references `quiz.Question`, and a test parses its source to keep it that way (FR-21).
- Error codes are `CodeInvalidMessage` etc. (not `Err…`, which in Go means an `error` value). `DecodeError` carries the code to reply with.
- Empty `top` / `finalTop` lists encode as `[]`, not `null`, because the schemas require arrays.

## Verification

- 13 protocol tests + 2 new quiz tests pass under `-race`; golangci-lint 0 issues; `make check` green (including the new schema-copy freshness check).
- **Drift guards:** Go message types = schema files; Go error codes = schema enum; every schema example decodes into its Go type and re-encodes byte-equivalently, and passes its schema.
- **Mutation checks (all caught, all restored):**
  1. a function in `protocol` taking `quiz.Question` → the source-scan test fails;
  2. removing the empty-list fix → `top: null` caught;
  3. skipping schema validation → invalid frames accepted, caught;
  4. dropping an error code → enum drift caught.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
