# task-05: Code generation and contract checks

- **Phase:** P0 Foundation
- **Size:** M · **TDD:** test
- **Status:** todo
- **Implements:** TRD §1.5, §6.4 · D13, NFR-33
- **Depends on:** [task-01](task-01-repository-scaffold.md)
- **Unblocks:** [task-09](../p1-domain/task-09-protocol-package.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- port the schema checks already run by hand into Go tests: every schema valid, every example valid, the six negative cases rejected, quiz-code pattern identical in `openapi.yaml` and `common.json`

## Steps

- [ ] Tests first: port the hand-run schema checks to Go (draft-07 validator): all schemas valid, all examples valid, the six negative cases rejected
- [ ] Test: the quiz-code pattern in `openapi.yaml` equals the one in `schemas/common.json`
- [ ] `oapi-codegen` config (standard `net/http` server interface) → `internal/httpapi/gen/`
- [ ] `openapi-typescript` → `client/src/api/schema.ts`; `json-schema-to-typescript` → `client/src/protocol/`
- [ ] Pin `mockgen` (`go.uber.org/mock`) as a Go tool; `//go:generate` lines in each module
- [ ] `make generate` runs all of the above; `make check` regenerates and fails on `git diff`
- [ ] Add `redocly lint` and `asyncapi validate` to `make check`, with the AsyncAPI CLI's analytics disabled for the run

## Done when

- [ ] `make generate` runs `oapi-codegen`, `openapi-typescript`, `json-schema-to-typescript`, `go generate` (mockgen)
- [ ] Generated code committed; `make check` fails if regeneration changes anything
- [ ] `redocly lint` and `asyncapi validate` in `make check` (the AsyncAPI CLI's analytics disabled for the run)
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
