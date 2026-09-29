# task-05: Code generation and contract checks

- **Phase:** P0 Foundation
- **Size:** M · **TDD:** test
- **Status:** done (per-module `//go:generate` mockgen lines are added with each interface, from task-11 on)
- **Implements:** TRD §1.5, §6.4 · D13, NFR-33
- **Depends on:** [task-01](task-01-repository-scaffold.md)
- **Unblocks:** [task-09](../p1-domain/task-09-protocol-package.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- port the schema checks already run by hand into Go tests: every schema valid, every example valid, the six negative cases rejected, quiz-code pattern identical in `openapi.yaml` and `common.json`

## Steps

- [x] Tests first: port the hand-run schema checks to Go (draft-07 validator): all schemas valid, all examples valid, the six negative cases rejected
- [x] Test: the quiz-code pattern in `openapi.yaml` equals the one in `schemas/common.json`
- [x] `oapi-codegen` config (standard `net/http` server interface) → `internal/httpapi/gen/`
- [x] `openapi-typescript` → `client/src/api/schema.ts`; `json-schema-to-typescript` → `client/src/protocol/`
- [x] Pin `mockgen` (`go.uber.org/mock`) as a Go tool; `//go:generate` lines in each module
- [x] `make generate` runs all of the above; `make check` regenerates and fails on `git diff`
- [x] Add OpenAPI lint (Redocly) and AsyncAPI validation to `make check` (via the AsyncAPI parser library: same validation as the CLI, no analytics)

## Done when

- [x] `make generate` runs `oapi-codegen`, `openapi-typescript`, `json-schema-to-typescript`, `go generate` (mockgen)
- [x] Generated code committed; `make check` fails if regeneration changes anything
- [x] OpenAPI lint and AsyncAPI validation in `make check`
- [x] `make check` green

## Notes and decisions

- **TypeScript generators live in `tools/contracts/`, not in the client.** `openapi-typescript` (latest 7.13 and `next`) requires TypeScript 5, while the client uses TypeScript 6. Forcing the install with `--legacy-peer-deps` was rejected, because the generator uses the TypeScript compiler API and could break subtly. The separate package mirrors the Go tools module and keeps the client's dependencies clean.
- **AsyncAPI is validated with `@asyncapi/parser` directly** instead of the CLI: same validation, far smaller install, no usage analytics.
- **Message schema titles now end in `Message`** (`ErrorMessage`, `LeaderboardMessage`, …). Without the suffix, the generated types collided: `interface Error` shadowed TypeScript's built-in `Error`, and `Leaderboard` / `QuizState` clashed with the shared definitions (`Leaderboard1`, `QuizState1`).
- **Protocol types are generated into one file** (`client/src/protocol/messages.ts`), so shared definitions appear once instead of per message.
- oapi-codegen's initial OpenAPI 3.1 support handled the spec without changes: models, all 8 operations, and the strict server interface.

## Verification

- Contract tests: all pass. Mutation check: loosening `PublicQuestion` made the FR-21 test fail, and restoring it passed.
- Generated Go code compiles; golangci-lint skips it as generated (0 issues). The client typechecks against the generated types.
- `lint-contracts`: OpenAPI valid (2 expected warnings), AsyncAPI valid (14 messages, 0 warnings).
- **`check-generated` mutation check:** changing one OpenAPI summary made it report exactly the two affected files as stale (not the protocol types) and fail. After restoring, it passes with no leftovers.
- `make check` exit 0.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
