# Real-time vocabulary quiz

Players join a quiz with a code, answer timed vocabulary questions, and watch their scores and the leaderboard update live. This is my submission for the ELSA real-time quiz challenge ([brief](docs/assignment.md)).

The component I built in full is the **real-time backend** in Go: a REST API, WebSocket gateways, and workers that coordinate through Redis and store results in PostgreSQL. A small React client covers the host and player screens so the whole flow can be tried in a browser. Only the identity provider is mocked (the API issues dev tokens).

## The design in brief

- **Three stateless services from one codebase.** The API creates and starts quizzes, gateways hold the WebSocket connections, workers run the quiz clock and save answers. Each scales on its own, and none of them owns a quiz.
- **Rooms are virtual.** A quiz is a handful of Redis keys and one pub/sub channel, so any gateway can serve any player and any worker can advance any quiz.
- **One atomic step per answer.** A Lua script checks the deadline against Redis time, rejects duplicates, scores the answer, and updates the leaderboard in one go. Every state change publishes its own event in the same step, so nothing changes without clients being told.
- **Encode once, send to everyone.** Each gateway turns a room event into one prepared message and writes the same bytes to every local socket.
- **Redis holds only what is live.** Each question's answers are saved to PostgreSQL in one batch after it closes, and released from Redis only once the write is confirmed. At the end, totals are recomputed from the saved answers and must match the leaderboard.

The full design, with diagrams, is in [`docs/system-design/`](docs/system-design/README.md).

## Run it locally

Needs Docker with Compose v2.23+ and `make`.

```bash
make up      # builds images, migrates and seeds, starts everything, waits until healthy
```

Then open http://localhost:8080:

1. Choose **Host a quiz**, pick **Quick demo**, and create it.
2. Open the join link it shows in another tab or window, one per player, and join with a name.
3. Back on the host tab, press **Start**. Questions advance on their own, and close early once everyone has answered.

The first `make up` builds the images (about 4 minutes); later starts take about 20 seconds. `make down` stops the stack and keeps the data; `make reset` also deletes it. If port 8080 is taken, set `HTTP_HOST_PORT` in `.env` (created from `.env.example` on the first run).

### What's running

| Service | Instances | Role |
|---|---|---|
| nginx | 1 | Serves the client on port 8080, routes `/api` and `/ws` |
| api | 1 | REST: create and start quizzes, leaderboards |
| ws | 2 | WebSocket gateways: joins, answers, live updates |
| worker | 2 | Advances quizzes, sends leaderboard updates, saves answers |
| redis, postgres | 1 each | Live state; question sets and permanent results |

### Trying the design

`make help` lists every target. These show the scaling and failure behaviour:

```bash
make scale WS=4 WORKER=3                           # more gateways and workers, no restart
make up PROFILES="observability replica chaos"     # Prometheus on :9090, a Redis replica, fault injection
make chaos-kill S=ws-1                             # kill a gateway mid-quiz; its players reconnect elsewhere
make chaos-start                                   # bring it back
make chaos-redis-latency MS=100                    # slow every Redis call (needs PROFILES=chaos)
make chaos-pg-down SEC=10                          # pause PostgreSQL; live quizzes keep running
make chaos-reset                                   # remove every injected fault
```

## Tests

Needs Go 1.26+ and Node 22+ in addition to Docker.

| Command | What it runs | Time |
|---|---|---|
| `make check` | Lint (golangci-lint with gosec, oxlint, tsc), unit tests with the race detector, contract validation, generated-code freshness | ~30 s |
| `make test-integration` | Every Redis script and repository against real Redis and PostgreSQL, with Toxiproxy faults | ~30 s |
| `make test-e2e` | End-to-end and fault tests against the running stack (`make up PROFILES=chaos` first) | ~5 min |

Critical code was written test-first, and every critical rule was checked by breaking it on purpose and confirming a test failed. [`docs/testing.md`](docs/testing.md) describes each test layer.

### Load and simulation

```bash
make sim SCENARIO=big-room PARTICIPANTS=5000       # 5,000 players in one room
make sim SCENARIO=gateway-crash                    # kill a gateway mid-question
make k6                                            # many rooms, from an independent load generator
```

Scenarios live in [`loadtest/scenarios/`](loadtest/scenarios/). Each run checks its latency targets and every player's score, writes a JSON report to `loadtest/results/`, and exits non-zero on any miss.

Results on one laptop (12 threads), p95:

| Scenario | Leaderboard update | Answer result | Question delivery | Result |
|---|---|---|---|---|
| 5,000 in one room | 288 ms | 50 ms | 133 ms | pass |
| 200 rooms × 50 | pass | pass | pass | pass |
| Gateway killed mid-question | 2,500 players reconnected, slowest in 5.9 s | | | pass |
| 10,000 in one room | scored correctly, but client-observed latency missed its targets | | | see below |

Every run scored every answer correctly, with zero mismatches between live and saved totals. The 10,000 run is limited by the test machine: the load generator, nginx, the gateways and kernel networking shared 12 threads that ran at about 95% during answer bursts. Details and the bottlenecks found on the way are in [`docs/testing.md`](docs/testing.md).

## Repository map

```
.
├── server/                 Go module
│   ├── cmd/                api, ws, worker, migrate, sim
│   ├── internal/           domain modules: quiz, scoring, session, leaderboard, history,
│   │                       realtime, scheduler, httpapi, protocol, auth, platform/*
│   ├── migrations/         schema and seed question sets
│   └── testkit/            protocol client and scenario runner shared by e2e tests and the simulator
├── client/                 React + TypeScript (Vite): host and player screens
├── deploy/                 nginx, Prometheus, and Toxiproxy config
├── loadtest/               simulator scenarios, k6 script, committed reports
├── tools/contracts/        code generators and spec validators
└── docs/
    ├── assignment.md       the brief
    ├── system-design/      architecture, data flow, technology choices, non-functional design
    ├── api/                openapi.yaml, asyncapi.yaml, JSON Schemas for every message
    ├── planning/           context and decisions, requirements, TRD, task board
    ├── ai-collaboration/   how AI was used and verified, with a per-task log
    └── testing.md          test layers and load results
```

Inside `server/internal`, each module keeps its rules in pure domain code, its use cases in a service, and its storage behind a repository interface, so services can be unit-tested with gomock and the repositories against real Redis and PostgreSQL.

## Known limitations

- **10,000 players in one room is not proven end to end.** Server-side timings were within target, but client-observed latency needs a load generator on separate machines.
- **The WebSocket token travels in the URL** (D15). Logs never record it: the access log drops query strings and nginx's error log is set to emergencies only. Sending it in the `Sec-WebSocket-Protocol` header instead is the follow-up.
- **Auth is mocked.** The API issues short-lived dev tokens; there are no accounts.
- **Redis replication is asynchronous.** With the `replica` profile, each answer waits for the replica, which narrows the loss window on a failover but doesn't close it ([non-functional §3](docs/system-design/non-functional.md)).
- **A crashed tab stays in the lobby list**, because only a clean leave or tab close removes a player before the start.
- **A reload asks for the name again**, though the player comes back with the same identity and score.
- **WebSocket `invalid_message` errors include the validator's schema path**, the same kind of leak already fixed for REST.
- **Observability is a scaffold:** health endpoints and core metrics with Prometheus, no tracing or dashboards (D17). The full design is in [non-functional §4](docs/system-design/non-functional.md).

## Documentation

- [System design](docs/system-design/README.md): architecture, data flow, technology choices, non-functional design, AI collaboration in design
- [API contracts](docs/api/): `openapi.yaml` (REST), `asyncapi.yaml` (WebSocket), JSON Schemas
- [Testing and load results](docs/testing.md)
- [Planning](docs/planning/index.md): decisions, requirements, TRD, and the task board
- [AI collaboration](docs/ai-collaboration/index.md): tools, how the work was split, how every piece of AI output was verified, and the full log
