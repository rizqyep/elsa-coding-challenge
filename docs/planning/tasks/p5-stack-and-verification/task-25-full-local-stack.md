# task-25: Full local stack

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** —
- **Status:** done
- **Implements:** TRD §11 · NFR-35
- **Depends on:** [task-22](../p3-services/task-22-shutdown-and-degraded-modes.md), [task-23](../p3-services/task-23-observability.md), [task-24](../p4-client/task-24-react-client.md)
- **Unblocks:** [task-26](task-26-test-kit.md)

## Steps

- [x] Multi-stage Dockerfiles for the Go services (distroless runtime) and the client build (`deploy/nginx/Dockerfile`: Node build, nginx runtime)
- [x] Compose: api, ws × 2, worker × 2, migrate ordering, healthchecks, `nofile` ulimit on ws
- [x] `nginx.conf`: routing, WebSocket upgrade headers, service-name re-resolution for scaling (**confirmed**), log format without query strings
- [x] Profiles: `observability` (Prometheus only, D17), `replica` (sets `REDIS_WAIT_REPLICAS=1`), `chaos` (Toxiproxy config + env overrides)
- [x] Make targets: `scale`, `demo`, `chaos-*`; `up` itself waits until healthy, so no separate `wait`
- [x] Verify: `make scale WS=4` spreads new connections across all four (check `ws_connections`)

## Done when

- [x] Compose runs nginx, api, 2× ws, 2× worker, redis, postgres, migrate; `make up` waits until healthy
- [x] nginx routes `/`, `/api`, `/ws`; scaled instances join the rotation (confirmed, see Verification); query strings not logged
- [x] Profiles `observability`, `replica`, `chaos` work
- [x] `make scale`, `make demo`, `make chaos-*` work
- [x] `make check` green

## Notes and decisions

- **One command:** Compose v5's `--wait` accepts a one-shot service that exited 0, so `migrate` is an ordinary service and the Go services depend on it with `service_completed_successfully`. `make up` is a single `docker compose up -d --build --wait` (the old two-step `up` + `run migrate` is gone; `make migrate` still exists).
- **Healthchecks on distroless:** there's no shell, curl, or wget in the Go images, so each binary takes `healthcheck` as its argument and probes its own `/readyz` (`internal/platform/health`, unit-tested). gosec's taint check flags the URL built from `HTTP_ADDR`; it's excluded for that one file in `.golangci.yml` with the reason, not suppressed inline.
- **nginx re-resolution:** upstreams use `server ws:8080 resolve` with `zone` (open-source nginx 1.27.3+, image `nginx:1.28-alpine`) and `resolver 127.0.0.11 valid=5s`. `/ws` uses `least_conn`, since WebSockets are long-lived and round-robin by request would skew after reconnect waves.
- **Tokens out of logs (D15):** the access log uses `$uri`, which has no query string. nginx's error log quotes the whole request line at `error` level (for example on an upstream failure), so it's set to `crit`; upstream failures still appear in the access log as the status code and `upstream=` address.
- **`ws_connections`** (gauge over the gateway's existing connection counter) was added here, ahead of the task-23 scaffold, because this task's scaling check needs it.
- **Profiles set env through make:** `PROFILES=replica` exports `REDIS_WAIT_REPLICAS=1`; `PROFILES=chaos` points `REDIS_ADDR` and `POSTGRES_DSN` at Toxiproxy. `make down` / `reset` use `--profile '*'` so profile services stop too.
- **`make demo` prints steps rather than creating a quiz:** the host's token lives in the browser tab that creates the quiz, so a quiz created from the shell couldn't be started from the page.
- **Dropped:** `chaos-slow-client` (slow clients are a simulator option, task-28); Grafana and dashboards (D17).
- **Contract gap found:** an early close re-sends `question` with the same `questionId`. The TRD said so, but `asyncapi.yaml` didn't tell a client what to do with it. A description on `send_question` now does.
- **Root README:** a short run-it-locally README, so a reviewer can start the stack now; task-30 expands it.

## Verification

- **Fresh start:** `make reset` (0 containers, 0 volumes left), then `make up`: exit 0 in 16.8 s with images cached (the first build took 3 min 54 s). Migrate applied schema and seed; every service healthy.
- **Routing through nginx (:8080):** `/` 200 `text/html`, `/?code=…` 200 (SPA fallback), `/api/v1/question-sets` 200 with a token, `/ws` 101 alternating between the two gateways.
- **Tokens not logged:** across nginx, api, ws, and worker logs after the whole session (routing, scaling, chaos), 0 lines contain `?token` or the real token. My first check was invalid: the token was empty, so the grep pattern had an empty branch; it was redone with a real 160-character token.
- **Scaling:** `make scale WS=4`, 40 held connections: `ws_connections` 10/10/10/10. `make scale WS=2`, 20 connections: 10/10, 0 × 502.
- **Mutation on the key config:** with `resolve` removed (static `server ws:8080;`), after scaling to 4 the connections went 20/20/0/0: the new gateways never joined. Config restored and rebuilt.
- **Profiles (all three together):** replica connected (`connected_slaves:1`), gateways run with `REDIS_WAIT_REPLICAS=1`, services use `toxiproxy:26379` / `toxiproxy:25432`, Prometheus has 5 targets (api, 2 ws, 2 workers).
- **End to end on that stack:** a script played a full `demo-quick` quiz through nginx with 3 players: every answer accepted, early close on every question, totals equal `quiz_finished`, quiz archived, 9 answers in PostgreSQL. Repeated on the default stack after `make reset`.
- **Failure switches (chaos profile):** baseline create 6 ms; `chaos-redis-latency MS=300` → 305 ms, `chaos-reset` → 6 ms; `chaos-redis-down` → 503 in 1.7 s, 201 after restore; `chaos-pg-down` → create and question-sets 503 at the 5 s request timeout, 200/201 after; `chaos-kill S=ws-1` → 6/6 new connections upgraded on `ws-2`, 0 × 502; `chaos-stop S=worker` → both exited; `chaos-start` → all healthy. One reading (question-sets 200 during a PostgreSQL pause) was my timing: the request started as the pause ended. Rerun with a longer pause: 503.
- **Checks:** `make check` green (0 lint issues, 31 client tests); integration suite 20/20 packages; new unit tests for the probe and an integration test for the gauge, written first and seen failing.

## AI collaboration

See [AI-034](../../../ai-collaboration/log.md). The AI built the stack and ran every check above; the scaling claim is backed by the `resolve` mutation, not only the passing run.
