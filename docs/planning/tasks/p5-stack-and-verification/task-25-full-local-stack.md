# task-25: Full local stack

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** —
- **Status:** todo
- **Implements:** TRD §11 · NFR-35
- **Depends on:** [task-22](../p3-services/task-22-shutdown-and-degraded-modes.md), [task-23](../p3-services/task-23-observability.md), [task-24](../p4-client/task-24-react-client.md)
- **Unblocks:** [task-26](task-26-test-kit.md)

## Steps

- [ ] Multi-stage Dockerfiles for the Go services (distroless runtime) and the client build
- [ ] Compose: api, ws × 2, worker × 2, migrate ordering, healthchecks, `nofile` ulimit on ws
- [ ] `nginx.conf`: routing, WebSocket upgrade headers, service-name re-resolution for scaling (**confirm it works**), log format without query strings
- [ ] Profiles: `observability`, `replica` (sets `REDIS_WAIT_REPLICAS=1`), `chaos` (Toxiproxy config + env overrides)
- [ ] Make targets: `scale`, `demo`, `chaos-*`, `wait` (until healthy)
- [ ] Verify: `make scale WS=4` spreads new connections across all four (check `ws_connections`)

## Done when

- [ ] Compose runs nginx, api, 2× ws, 2× worker, redis, postgres, migrate; `make up` waits until healthy
- [ ] nginx routes `/`, `/api`, `/ws`; scaled instances join the rotation (confirm the re-resolve approach, TRD §11.2); query strings not logged
- [ ] Profiles `observability`, `replica`, `chaos` work
- [ ] `make scale`, `make demo`, `make chaos-*` work
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
