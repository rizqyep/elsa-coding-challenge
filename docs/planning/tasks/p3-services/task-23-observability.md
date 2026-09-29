# task-23: Observability scaffold

- **Phase:** P3 Services
- **Size:** S · **TDD:** partial
- **Status:** todo
- **Implements:** TRD §2, non-functional §4 (scaffold, D17) · NFR-29
- **Depends on:** [task-16](task-16-rest-api-service.md), [task-20](task-20-gateway-message-handlers-and-presence.md), [task-21](task-21-worker.md)
- **Unblocks:** [task-25](../p5-stack-and-verification/task-25-full-local-stack.md)

## Scope (D17)

A scaffold, not the full design: the endpoints and the few metrics the real-time path and the load runs need. Tracing, Grafana dashboards and alert rules stay documented in non-functional §4 as the target and are not built.

## Tests first

- each service serves `/metrics`, `/healthz`, `/readyz`
- the core metrics below are registered; no quiz code is used as a label

## Steps

- [ ] Tests: endpoints on all three services; core metrics registered with bounded labels
- [ ] Core metrics: `ws_connections` (gauge), `answer_duration_seconds` (histogram, buckets around NFR-7), `broadcast_fanout_seconds` (histogram, buckets around NFR-6/NFR-8), `transitions_total`, `errors_total{service, code}` (the gateway already has it)
- [ ] `observability` Compose profile: Prometheus with a scrape config for the services, nothing more

## Done when

- [ ] `/metrics`, `/healthz`, `/readyz` on every service
- [ ] core metrics visible while a simulation runs
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
