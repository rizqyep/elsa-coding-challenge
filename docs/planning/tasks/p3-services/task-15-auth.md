# task-15: Auth

- **Phase:** P3 Services
- **Size:** S · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §2.2, §6.2 · NFR-19, D15
- **Depends on:** [task-02](../p0-foundation/task-02-platform-packages.md)
- **Unblocks:** [task-16](task-16-rest-api-service.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- issue/verify round trip
- expired, wrong signature, and wrong role rejected
- dev endpoint disabled when `DEV_TOKENS_ENABLED=false` or `APP_ENV=production`

## Steps

- [ ] Tests: issue/verify round trip; expired, bad signature, wrong role rejected
- [ ] Tests: dev endpoint absent when `DEV_TOKENS_ENABLED=false` or `APP_ENV=production`
- [ ] Implement HS256 tokens (`sub`, `role`, `exp`) with `golang-jwt`
- [ ] Middleware: bearer header for REST, `token` query parameter for WebSocket

## Done when

- [ ] `auth` package + dev token handler
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
