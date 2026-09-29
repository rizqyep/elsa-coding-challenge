# task-15: Auth

- **Phase:** P3 Services
- **Size:** S · **TDD:** yes
- **Status:** done
- **Implements:** TRD §2.2, §6.2 · NFR-19, D15
- **Depends on:** [task-02](../p0-foundation/task-02-platform-packages.md)
- **Unblocks:** [task-16](task-16-rest-api-service.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- issue/verify round trip
- expired, wrong signature, and wrong role rejected
- dev endpoint disabled when `DEV_TOKENS_ENABLED=false` (tested with the handler in task-16)

## Steps

- [x] Tests: issue/verify round trip; expired, bad signature, wrong role rejected
- [x] Tests: dev endpoint absent when `DEV_TOKENS_ENABLED=false` (in task-16, where the handler lives)
- [x] Implement HS256 tokens (`sub`, `role`, `exp`) with `golang-jwt`
- [x] Middleware: bearer header for REST, `token` query parameter for WebSocket

## Done when

- [x] `auth` package + dev token handler
- [x] `make check` green

## Notes and decisions

- `auth.Tokens` issues and verifies HS256 tokens (`sub`, `role`, `exp`). Verification pins the algorithm to HS256, requires `exp`, and rejects empty or over-long subjects and unknown roles. Every rejection wraps `ErrInvalidToken`, so callers map one error to 401.
- `FromBearer` (REST) and `FromQuery` (WebSocket, D15) extract the token; `WithClaims`/`ClaimsFrom` carry the verified caller in the context.
- The REST middleware and the dev-token handler live in `httpapi`, because they depend on the OpenAPI operations; they ship with task-16.
- No `APP_ENV=production` rule: dropped in the task-02 review (there's no real production here). `DEV_TOKENS_ENABLED` alone controls the endpoint.
- `auth` uses plain strings for participant IDs, so it doesn't depend on `quiz`.

## Verification

- Tests written first and confirmed failing (`undefined: auth.Tokens`), then implemented; all pass under `-race`.
- Rejected tokens: expired (at exactly `exp` and long after), another key, tampered payload, `alg: none`, HS512 with the right key, missing `exp`/`sub`/`role`, unknown role, empty, garbage.
- Mutation checks, each caught by its intended test and restored: removing algorithm pinning (HS512 accepted), not requiring `exp`, not validating the role.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
