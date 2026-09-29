# task-16: REST API service

- **Phase:** P3 Services
- **Size:** M · **TDD:** yes
- **Status:** done
- **Implements:** TRD §5.2, §6.2, §9.6 · FR-1, FR-3, FR-13, FR-27, NFR-15
- **Depends on:** [task-11](../p2-redis-scripts/task-11-scripts-create-room-start-join.md), [task-14](../p2-redis-scripts/task-14-flush-scripts-and-postgresql-repositories.md), [task-15](task-15-auth.md)
- **Unblocks:** [task-22](task-22-shutdown-and-degraded-modes.md), [task-23](task-23-observability.md), [task-24](../p4-client/task-24-react-client.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- every handler's responses validate against `openapi.yaml` (kin-openapi)
- create quiz retries on code collision
- Redis failure rolls back the transaction (gomock)
- start maps script outcomes to 202/403/409
- leaderboard live vs finished
- 503 + `Retry-After` on dependency failure

## Steps

- [x] Test harness: every handler response validated against `openapi.yaml` (kin-openapi)
- [x] Tests: create quiz retries on a code collision; Redis failure rolls back the transaction (gomock)
- [x] Tests: start maps script outcomes to 202 / 403 / 409; leaderboard live (Redis, paginated, shared ranks) vs finished (PostgreSQL)
- [x] Tests: dependency failure → 503 + `Retry-After`; problem+json body with `code`
- [x] Implement the generated server interface; quiz and leaderboard services; question-set listing
- [x] `cmd/api` wiring with request IDs, logging, `/healthz`, `/readyz`, `/metrics`

## Done when

- [x] `cmd/api` serves all operations in `openapi.yaml`
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- **Layers:** `httpapi` (handlers generated from `openapi.yaml`, ports `Quizzes`/`Leaderboards`) → `quiz.Service` and `leaderboard.Service` → Redis repositories and PostgreSQL stores. `cmd/api` is the only place that wires concrete types (TRD §1.4); the integration tests call the same `newHandler`.
- **Contract enforcement at runtime:** every request is routed, authenticated (bearer token, role per operation), then validated against the spec with kin-openapi before a handler runs. Auth runs before validation, so anonymous callers learn nothing about request shapes. Bodies are capped at 64 KiB.
- **Access rules fail closed:** each operation needs an entry in the access table, and it must agree with the spec's `security`; otherwise `New` refuses to start. It caught its first bug on the first run: the embedded spec names operations in their Go form (`StartQuiz`), so the table is keyed that way.
- **Errors (TRD §9.6):** business outcomes → 4xx with a code; transient infrastructure (new `retry.Transient`) → 503 + `Retry-After`; anything else → 500 without detail. Panics are contained per request. Each error is counted in `errors_total{service,code}` and logged once where it's mapped. Validation details are trimmed to "where: why" without the validator's internals.
- **Quiz creation (TRD §5.2):** insert the row, create the room inside the same transaction, store Redis's creation time, commit. A taken code (PostgreSQL) or an orphan room (Redis) retries with a new code; a Redis failure rolls back.
- **Reads:** quiz and leaderboard reads use Redis while the room exists and PostgreSQL once released. A Redis *error* never falls back to PostgreSQL, which would report a running quiz as expired.
- **Leaderboard pages:** one read-only script per page (entries, names, ranks, count), ranks from the whole leaderboard so ties keep one rank across pages. Final pages order ties by participant ID with `COLLATE "C"`, matching Redis.
- **Fixes found along the way:**
  1. Start wasn't idempotent once the first question opened (the contract says it is); fixed in Go and Lua with shared cases.
  2. The spec omitted 400 on the code and pagination routes and had no 500; both documented.
  3. `QUIZ_DATA_TTL` moved to the common config, since the API sets it at creation.
  4. `testenv.Reset` now truncates the quiz tables too.
- **Archive status is best effort:** `MarkRunning` failing doesn't fail a start; Redis is the authority on a running quiz.

## Verification

- Tests written first and confirmed failing for the start fix, quiz service, store, leaderboard pages, transient classifier, and handlers; `make check` and `make test-integration` (14 packages) green.
- **Contract harness:** every response in the handler tests is validated against `openapi.yaml` including its status code (`IncludeResponseStatus`), plus `X-Request-ID` on every response and problem+json on every API error.
- **Integration through the real wiring** (`cmd/api`): the full flow (create → join → start, including a retry after question 1 opens → answers → live leaderboard and pages → final job → archived quiz and leaderboard identical to the live ones, creation time included); Redis outage (503, rolled back, no PostgreSQL fallback); PostgreSQL outage (creation 503 with no Redis room left behind, live reads and start still work); slow Redis (503 in about a second); orphan Redis room (code skipped). Run 4 times with no flakes.
- **Mutation checks, all caught:** 3 on auth, 1 on start idempotency (Lua), 3 on the quiz service, 4 on leaderboard pages, 5 on the REST layer, 5 on the integration wiring. Three mutants initially failed to compile and were redone so they proved something.
- **Collation:** removing `COLLATE "C"` first survived, because the Alpine image sorts text bytewise. The tie-order test now switches the column to an ICU locale collation, and the mutation is caught.
- **Smoke run** of the built binary against the local stack: ready, list sets, create (201 + `Location`), get, start → 409 on an empty lobby, `/metrics`, clean shutdown on SIGTERM.
- Every commit builds, vets (including integration files), and passes unit tests on its own.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
