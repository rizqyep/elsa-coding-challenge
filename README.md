# Real-time vocabulary quiz

Players join a quiz with a code, answer timed questions, and watch scores and the leaderboard update live. A Go backend (REST API, WebSocket gateways, workers) coordinates through Redis and stores results in PostgreSQL; a small React client covers the host and player screens.

## Run it locally

Needs Docker with Compose v2.23+ and `make`.

```bash
make up      # builds images, migrates and seeds, starts everything, waits until healthy
```

Then open http://localhost:8080:

1. Choose **Host a quiz**, pick **Quick demo**, and create it.
2. Open the join link it shows in another tab or window, one per player, and join with a name.
3. Back on the host tab, press **Start**. Questions advance on their own.

The first `make up` builds the images (about 4 minutes); later starts take about 20 seconds. `make down` stops the stack and keeps the data; `make reset` also deletes it. If port 8080 is taken, set `HTTP_HOST_PORT` in `.env`.

### What's running

| Service | Instances | Role |
|---|---|---|
| nginx | 1 | Serves the client on port 8080, routes `/api` and `/ws` |
| api | 1 | REST: create and start quizzes, leaderboards |
| ws | 2 | WebSocket gateways: joins, answers, live updates |
| worker | 2 | Advances quizzes, sends leaderboard updates, saves answers |
| redis, postgres | 1 each | Live state; permanent results and question sets |

`make help` lists every target. The useful ones for trying the design:

```bash
make scale WS=4 WORKER=3                           # more gateways and workers, no restart
make up PROFILES="observability replica chaos"     # Prometheus on :9090, a Redis replica, fault injection
make chaos-kill S=ws-1                             # kill a gateway; make chaos-start brings it back
make chaos-redis-latency MS=100                    # needs PROFILES=chaos; make chaos-reset removes it
make chaos-pg-down SEC=10                          # pause PostgreSQL
```

## Tests

Needs Go 1.26+ and Node 22+.

```bash
make check              # lint, unit tests, contract checks, generated-code freshness
make test-integration   # against real Redis, PostgreSQL, and Toxiproxy (Docker)
```

## Documentation

- [System design](docs/system-design/README.md): architecture, data flow, technology choices, non-functional design
- [API contracts](docs/api/): `openapi.yaml` (REST), `asyncapi.yaml` (WebSocket), JSON Schemas
- [Planning](docs/planning/index.md): requirements, TRD, and the task board
- [AI collaboration](docs/ai-collaboration/index.md): how AI was used and how its output was verified
