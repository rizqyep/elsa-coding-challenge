# task-23: Observability

- **Phase:** P3 Services
- **Size:** M · **TDD:** partial
- **Status:** todo
- **Implements:** TRD §2, non-functional §4 · NFR-27–NFR-30
- **Depends on:** [task-16](task-16-rest-api-service.md), [task-20](task-20-gateway-message-handlers-and-presence.md), [task-21](task-21-worker.md)
- **Unblocks:** [task-25](../p5-stack-and-verification/task-25-full-local-stack.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- every metric in non-functional §4.1 is registered and increments on its path
- no quiz code used as a label

## Steps

- [ ] Tests: every metric in non-functional §4.1 is registered and moves on its code path; no quiz code used as a label
- [ ] Implement the metrics across the three services; histogram buckets around the NFR targets
- [ ] OpenTelemetry spans on the answer path and the broadcast path
- [ ] Prometheus config, alert rules (non-functional §4.4), and three Grafana dashboards (§4.5) as files for the `observability` profile

## Done when

- [ ] `/metrics`, `/healthz`, `/readyz` on every service
- [ ] OpenTelemetry spans on the answer and broadcast paths
- [ ] Grafana dashboards (non-functional §4.5) and alert rules (§4.4) as files for the `observability` profile
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
