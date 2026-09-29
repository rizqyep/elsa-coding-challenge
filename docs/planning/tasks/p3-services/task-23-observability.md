# task-23: Observability scaffold

- **Phase:** P3 Services
- **Size:** S · **TDD:** partial
- **Status:** done
- **Implements:** TRD §2, non-functional §4 (scaffold, D17) · NFR-29
- **Depends on:** [task-16](task-16-rest-api-service.md), [task-20](task-20-gateway-message-handlers-and-presence.md), [task-21](task-21-worker.md)
- **Unblocks:** [task-25](../p5-stack-and-verification/task-25-full-local-stack.md)

## Scope (D17)

A scaffold, not the full design: the endpoints and the few metrics the real-time path and the load runs need. Tracing, Grafana dashboards and alert rules stay documented in non-functional §4 as the target and are not built.

## Tests first

- each service serves `/metrics`, `/healthz`, `/readyz`
- the core metrics below are registered; no quiz code is used as a label

## Steps

- [x] Tests: endpoints on all three services; core metrics registered with bounded labels
- [x] Core metrics: `ws_connections` (gauge, task-25), `answer_duration_seconds` and `broadcast_fanout_seconds` (histograms, buckets around NFR-6/7/8), `transitions_total{to}`, `score_reconciliation_mismatches_total`, `errors_total{service, code}` (gateway, API)
- [x] `observability` Compose profile: Prometheus scraping every instance through Docker DNS (task-25)

## Done when

- [x] `/metrics`, `/healthz`, `/readyz` on every service
- [x] core metrics visible while a simulation runs (checked in task-28/29 runs)
- [x] `make check` green

## Notes and decisions

- **`score_reconciliation_mismatches_total`** was added here, beyond the task's list, because task-27 and every simulation assert it's 0. It hangs off the existing `OnMismatch` hook.
- **Hooks stay small:** `answer_duration_seconds` and `transitions_total` are decorators in `cmd/ws` and `cmd/worker`; only `broadcast_fanout_seconds` needed a hub option (`OnBroadcast`). `transitions_total{to}` is pre-initialised for each status so it's exported at 0.
- **Transition lag isn't a metric:** the simulator measures it from server timestamps (next `openedAt` minus the announced `nextTransitionAt`).
- **Label rule tested, not just stated:** `metricstest.AssertBoundedLabels` fails on any quiz, participant, question, or connection label.

## Verification

- `cmd/worker`: after two workers run a 3-question quiz, `transitions_total` is 3 / 3 / 1 for question_open / question_closed / finished across both, and mismatches are exported at 0 (the test failed before the metrics existed).
- `cmd/ws`: after the end-to-end flow, `answer_duration_seconds_count` ≥ 3 and `broadcast_fanout_seconds_count` ≥ 5 (failed first). Hub unit test: one duration per broadcast, none for a dropped stale event; removing the hook call is caught.
- `cmd/api`: `/healthz`, `/readyz`, `/metrics` answer 200; every service's labels are bounded.
- `make check` green; integration suite 20/20.

## AI collaboration

See [AI-036](../../../ai-collaboration/log.md).
