# task-01: Repository scaffold

- **Phase:** P0 Foundation
- **Size:** S · **TDD:** —
- **Status:** todo
- **Implements:** TRD §1 · NFR-31
- **Depends on:** —
- **Unblocks:** [task-02](task-02-platform-packages.md), [task-03](task-03-data-stores-in-docker-compose.md), [task-05](task-05-code-generation-and-contract-checks.md), [task-06](../p1-domain/task-06-identifiers-quiz-codes-validation.md), [task-07](../p1-domain/task-07-scoring-and-ranks.md), [task-08](../p1-domain/task-08-quiz-state-machine.md)

## Steps

- [ ] `go mod init` with the module path from TRD §1.2; pin the Go version (`go` and `toolchain` lines)
- [ ] Create every package from TRD §1.2 with a `doc.go` stating its single responsibility
- [ ] `cmd/api`, `cmd/ws`, `cmd/worker`, `cmd/sim`: `main.go` that loads nothing yet and exits cleanly (wiring comes later)
- [ ] Scaffold `client/` with Vite + React + TypeScript; strict `tsconfig`, ESLint, Vitest
- [ ] Add `.golangci.yml` (govet, staticcheck, errcheck, gosec, revive), `.editorconfig`
- [ ] Makefile: `check`, `lint`, `test-unit`, `generate` (stub), `up`/`down` (stubs)
- [ ] `.env.example` listing every variable from TRD §2 with its default and a one-line comment
- [ ] Run `make check` from a clean clone: green

## Done when

- [ ] `server/` Go module and the package tree from TRD §1.2, with empty packages compiling
- [ ] `client/` Vite + React + TypeScript app that builds
- [ ] `Makefile` with `check`, `test`, `generate` (stubbed where needed), `.gitignore`, `.golangci.yml`, `.env.example`
- [ ] `make check` runs lint + vet + unit tests and passes
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
