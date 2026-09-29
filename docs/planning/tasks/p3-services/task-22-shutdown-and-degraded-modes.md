# task-22: Shutdown and degraded modes

- **Phase:** P3 Services
- **Size:** S · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §7.9, §8.4, §9.4 · NFR-15, NFR-16
- **Depends on:** [task-16](task-16-rest-api-service.md), [task-20](task-20-gateway-message-handlers-and-presence.md), [task-21](task-21-worker.md)
- **Unblocks:** [task-25](../p5-stack-and-verification/task-25-full-local-stack.md)

## Scope (D17)

Only the drain that rolling deploys and scaling out or in rely on. The separate Redis health monitor is dropped: readiness already pings Redis and PostgreSQL.

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- `SIGTERM`: readiness goes false first
- gateway closes open connections with 1012, spread over a jittered window within the timeout, so clients don't reconnect all at once
- worker stops claiming and finishes in-flight work

## Steps

- [ ] Tests: readiness false before anything else on shutdown
- [ ] Tests: gateway closes with 1012 in jittered batches within the timeout
- [ ] Tests: worker stops claiming and finishes in-flight items; API drains HTTP requests
- [ ] Implement per-service drain

## Done when

- [ ] all three services drain cleanly
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
