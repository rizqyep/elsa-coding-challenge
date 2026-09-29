# task-16: REST API service

- **Phase:** P3 Services
- **Size:** M · **TDD:** yes
- **Status:** todo
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

- [ ] Test harness: every handler response validated against `openapi.yaml` (kin-openapi)
- [ ] Tests: create quiz retries on a code collision; Redis failure rolls back the transaction (gomock)
- [ ] Tests: start maps script outcomes to 202 / 403 / 409; leaderboard live (Redis, paginated, shared ranks) vs finished (PostgreSQL)
- [ ] Tests: dependency failure → 503 + `Retry-After`; problem+json body with `code`
- [ ] Implement the generated server interface; quiz and leaderboard services; question-set listing
- [ ] `cmd/api` wiring with request IDs, logging, `/healthz`, `/readyz`, `/metrics`

## Done when

- [ ] `cmd/api` serves all operations in `openapi.yaml`
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
