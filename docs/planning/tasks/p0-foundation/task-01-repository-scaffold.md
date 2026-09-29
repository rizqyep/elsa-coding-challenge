# task-01: Repository scaffold

- **Phase:** P0 Foundation
- **Size:** S · **TDD:** —
- **Status:** done
- **Implements:** TRD §1 · NFR-31
- **Depends on:** —
- **Unblocks:** [task-02](task-02-platform-packages.md), [task-03](task-03-data-stores-in-docker-compose.md), [task-05](task-05-code-generation-and-contract-checks.md), [task-06](../p1-domain/task-06-identifiers-quiz-codes-validation.md), [task-07](../p1-domain/task-07-scoring-and-ranks.md), [task-08](../p1-domain/task-08-quiz-state-machine.md)

## Steps

- [x] `go mod init` with the module path from TRD §1.2; pin the Go version (`go` and `toolchain` lines)
- [x] Create every package from TRD §1.2 with a `doc.go` stating its single responsibility
- [x] `cmd/api`, `cmd/ws`, `cmd/worker`, `cmd/sim`: `main.go` that loads nothing yet and exits cleanly (wiring comes later)
- [x] Scaffold `client/` with Vite + React + TypeScript; strict `tsconfig`, ESLint, Vitest
- [x] Add `.golangci.yml` (govet, staticcheck, errcheck, gosec, revive), `.editorconfig`
- [x] Makefile: `check`, `lint`, `test-unit`, `generate` (stub), `up`/`down` (stubs)
- [x] `.env.example` listing every variable from TRD §2 with its default and a one-line comment
- [x] Run `make check` from a clean checkout: green

## Done when

- [x] `server/` Go module and the package tree from TRD §1.2, with empty packages compiling
- [x] `client/` Vite + React + TypeScript app that builds
- [x] `Makefile` with `check`, `test`, `generate` (stubbed where needed), `.gitignore`, `.golangci.yml`, `.env.example`
- [x] `make check` runs lint + vet + unit tests and passes
- [x] `make check` green

## Notes and decisions

- **Go 1.26**, not the latest 1.27: Go is installed from Fedora's package (my choice), which ships 1.26.8. `go.mod` says `go 1.26`.
- **oxlint instead of ESLint** for the client: it's the current Vite template's default and much faster. Tests use Vitest.
- **`internal/platform/postgres`** instead of `platform/pgx`, so the package name doesn't clash with the `pgx` driver import. TRD §1.2 updated.
- **Lint tools are pinned in a separate `server/tools/go.mod`** and run with `go tool -modfile=tools/go.mod …`, so tool dependencies don't mix with the service's (set up once Go is installed).
- `.env.example` covers every TRD §2 variable, plus Compose-only variables (host ports, database credentials).

## Verification

- Client: `npm run typecheck`, `npm run lint`, `npm test` (1 test), and `npm run build` all pass.
- `make help` lists all targets; `.env` is created from `.env.example` automatically.
- Go 1.26.8 (Fedora package; `GOTOOLCHAIN=local`). Tools pinned in `server/tools/go.mod` and all confirmed to build with 1.26: golangci-lint 2.14.0, mockgen 0.6.0, oapi-codegen v2, goose 3.28.0.
- `make check` exit 0: `go vet`, golangci-lint (0 issues), `go test -race ./...`, oxlint, `tsc`, Vitest.
- **Clean checkout:** `make check` on a fresh worktree of the committed `HEAD` (dependencies installed from scratch) exited 0.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
