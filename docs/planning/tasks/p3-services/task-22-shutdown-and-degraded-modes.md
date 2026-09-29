# task-22: Shutdown and degraded modes

- **Phase:** P3 Services
- **Size:** S · **TDD:** yes
- **Status:** done
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

- [x] Tests: readiness false before anything else on shutdown (`health.Readiness.Shutdown`)
- [x] Tests: gateway closes with 1012 across a jittered window within the timeout, refuses new upgrades, and stops at its deadline
- [x] Worker stops claiming and finishes in-flight items (already built in task-21); API drains HTTP requests (`http.Server.Shutdown`)
- [x] Implement per-service drain

## Done when

- [x] all three services drain cleanly
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- **One readiness type for all three services** (`platform/health.Readiness`): `/readyz` answers from the dependency check until shutdown starts, then 503. `Shutdown(steps...)` marks not-ready first and runs the steps in order, which is what the "readiness false first" test pins.
- **Gateway drain window = `SHUTDOWN_TIMEOUT / 3`** (10 s by default) rather than a new setting. The rest of the timeout covers the HTTP server and the background loops.
- **Two stack changes the drain needs:** `stop_grace_period: 35s` on the Go services (Compose's 10 s default would SIGKILL mid-drain), and `proxy_next_upstream error timeout http_503` on nginx's `/ws`, so a reconnect that hits the draining gateway is retried on another. Without it, `least_conn` would favour the draining gateway, because it has the fewest sockets.

## Verification

- Unit tests written first, seen failing: readiness states and ordering; drain closes 8 sockets with 1012 spread across a 400 ms window; upgrades refused with 503 and `Retry-After` while draining; Drain returns what's still open at its deadline.
- **Mutations, all caught:** no refusal while draining, no spread (every close at 0), readiness ignoring draining, not-ready set after the steps.
- **On the stack:** 40 sockets over two gateways, `docker stop quiz-ws-2`: all 20 on ws-2 closed with 1012 between about 0.2 s and 9.9 s after the stop, all 20 reconnected through nginx, 0 × 502/503 in nginx's log, ws-2 exited 0 (StopTimeout 35 s), the 20 on ws-1 untouched.
- `make check` green; integration suite 20/20.

## AI collaboration

See [AI-036](../../../ai-collaboration/log.md).
