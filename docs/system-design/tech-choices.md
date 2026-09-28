# Technologies and tools

Each choice is justified against our requirements ([`../planning/requirements.md`](../planning/requirements.md)) and decisions ([`../planning/context.md`](../planning/context.md)), not against popularity. Alternatives we considered are listed with the reason they lost.

## Summary

| Layer | Choice | Main reason |
|---|---|---|
| Backend language | **Go** | Cheap goroutine per connection, uses all cores in one process, low memory per socket (NFR-3, NFR-4) |
| Real-time transport | **WebSocket**, JSON messages | One bidirectional connection for answers and broadcasts; supported by every browser |
| WebSocket library | **gorilla/websocket** | Prepared messages: encode a broadcast once, write it to many sockets (NFR-5c) |
| Live state, schedules, fan-out | **Redis 7** | Atomic Lua scripts, sorted sets, pub/sub, sub-millisecond latency (NFR-12, NFR-14, D10) |
| Persistent storage | **PostgreSQL 16** | Question sets, answer history, final results; idempotent batch inserts (FR-32 to FR-35, D12) |
| Load balancer | **nginx** locally; managed L7 balancer in production | WebSocket upgrade, path routing to API and gateway, no sticky sessions needed |
| Frontend | **React + TypeScript + Vite**, native `WebSocket` | Thin demo; typed protocol messages; no socket library needed |
| Observability | **Prometheus, Grafana, OpenTelemetry, `log/slog`** | Standard, vendor-neutral, first-class Go support (NFR-27 to NFR-30) |
| Testing | **`go test -race`, testcontainers-go, k6**, plus a Go room simulator | Real Redis and PostgreSQL in tests; measured load against NFR targets |
| Packaging | **Docker, Docker Compose** | One command starts everything (NFR-35) |

## Backend: Go

**Why:**
- **It's the language I know best.** I can review AI-generated code line by line and catch concurrency mistakes myself, which is a responsibility the brief explicitly places on me.
- **One goroutine per connection is cheap.** Stacks start at a few KB, so a reader and a writer goroutine per socket fits the ≤ 50 KB per connection budget (NFR-4) with simple, blocking-style code.
- **Uses every core in one process.** Fanning a message out to 20,000 local sockets (NFR-3) spreads across CPUs without clustering or worker processes.
- **Concurrency primitives match the design:** channels for each connection's bounded outbound queue, `sync.RWMutex` for the sharded connection registry, `context` for shutdown and timeouts.
- **Operationally simple:** a static binary per service (D11), small container images, fast startup.
- **Standard library covers a lot:** `net/http` with method and path routing, `log/slog` for structured logs, `encoding/json`.

**Alternatives:**

| Option | Why not |
|---|---|
| Node.js (TypeScript) | Would share types with the client. But one event loop per process means more processes per machine to use all cores, and JSON encoding for large fan-outs competes with socket handling on the same thread |
| Elixir / Phoenix | Very strong fit: Channels and Presence solve much of this out of the box. Not chosen because of team familiarity and hiring, and because building the mechanisms ourselves is part of what this challenge demonstrates |
| Java / Kotlin (Netty, Vert.x) | Capable, but heavier runtime footprint per instance and more framework for a small service |

## Transport: WebSocket with JSON

**Why:**
- The client both sends (join, answers) and receives (questions, results, leaderboards) in real time. One full-duplex connection covers both, with small per-message framing overhead.
- Supported natively by every browser. No client library needed.
- JSON messages are easy to inspect in browser dev tools and logs. At our sizes (a leaderboard update is < 2 KB, NFR-11), encoding cost is small, especially since shared messages are encoded once (NFR-5c).

**Alternatives:**

| Option | Why not |
|---|---|
| Server-Sent Events + HTTP POST for answers | Works, but splits one session across two channels. Answers would pay HTTP request overhead, and the answer result would arrive on a different channel from the one that sent it |
| Socket.IO | Adds its own protocol on top of WebSocket plus fallbacks we don't need. The server ecosystem is Node-first |
| WebTransport | Not yet universally supported in browsers; no benefit for messages this small |
| Binary encoding (MessagePack, Protobuf) | Smaller payloads, but harder to debug. A **future step** if bandwidth becomes the bottleneck (see `non-functional.md`) |

## WebSocket library: gorilla/websocket

**Why:**
- **Prepared messages.** `NewPreparedMessage` encodes and frames a message once, and `WritePreparedMessage` sends it to many connections. That is NFR-5c ("encode once, write the same bytes to everyone") supported directly by the library.
- **Buffer control.** Read and write buffer sizes are configurable, and a shared write buffer pool lets idle connections hold no write buffer. Both matter for memory per connection (NFR-4).
- Mature and widely used, with a straightforward API for ping/pong heartbeats and close codes.

**Alternatives:**

| Option | Why not |
|---|---|
| coder/websocket (formerly nhooyr.io/websocket) | Clean, `context`-based API. No prepared-message equivalent, so each write re-frames the payload |
| gobwas/ws (with an epoll-based event loop) | Lowest memory per connection, because it avoids a goroutine per socket. Much more low-level code. The **next step** if one instance must hold far more than 20,000 connections |

## Live state, schedules, and fan-out: Redis 7

**Why:**
- **Atomic Lua scripts** give exactly-once scoring (NFR-12) and exactly-once transitions (NFR-14) without distributed locks. Redis runs one script at a time, and each script sees consistent state.
- **Sorted sets** are a leaderboard: O(log N) score updates, and top 10 and rank lookups built in.
- **Pub/sub** gives cross-instance fan-out on the same infrastructure, with one message per room update (NFR-5b).
- **`TIME` inside scripts** gives one clock for deadlines and answer receive times.
- Sub-millisecond latency within a region, and managed offerings everywhere.
- **Scaling path:** Redis Cluster, where the `{quizId}` hash tag keeps every key of a room in one slot, plus sharded pub/sub (`SSUBSCRIBE`, Redis 7).

**Alternatives:**

| Option | Why not |
|---|---|
| In-memory state per server with sticky routing | Fast, but a server crash loses its quizzes' state, and every quiz is tied to one server. Contradicts D10 |
| PostgreSQL for live state | Row locks under a burst of 10,000 answers into one quiz's rows, higher latency, and `LISTEN/NOTIFY` isn't built for high-rate fan-out |
| NATS (JetStream) for fan-out | Excellent messaging, but a second piece of infrastructure next to Redis, which we need anyway for state |
| Kafka | Durable and high-throughput, but its latency profile and operational weight are wrong for sub-second room broadcasts |
| Redis Streams instead of pub/sub | Durable and replayable. We don't need replay: every room message is a full snapshot with a version, so a missed message is healed by the next one (FR-28). Pub/sub is simpler |

**Client library:** `redis/go-redis` v9, which supports pipelining, pub/sub, and Lua scripts (sent once, then called by hash).

## Persistent storage: PostgreSQL 16

**Why:**
- Relational data with clear keys: question sets → questions, and answers keyed by (quiz, question, participant). A **unique constraint** makes batch flushes safe to retry (`ON CONFLICT DO NOTHING`, NFR-13a).
- Recomputing totals from answer history for the reconciliation check (FR-35) is one `GROUP BY` query.
- Runs for real in Docker Compose (D12), with SQL migrations and several **seeded question sets**. This exercises warming the question cache at join and batch flushes against a real database.

**Alternatives:**

| Option | Why not |
|---|---|
| In-memory mock | Can't be shared across the three services (D11) |
| A document store (e.g. MongoDB) | No advantage for this relational, append-mostly data, and a weaker fit for the uniqueness guarantee |

**Libraries:** `jackc/pgx` v5 with a connection pool; multi-row inserts for batch flushes; `goose` for migrations and seed data. pgx also gives direct control over transactions (isolation levels, batched statements) if later writes need more than single-statement atomicity, e.g. writing final results and marking the quiz archived in one transaction.

## Load balancer

- **Locally: nginx**, configured for WebSocket upgrade and path routing (`/api` → REST API, `/ws` → gateways).
- **In production:** a managed L7 load balancer with WebSocket support (e.g. AWS ALB or Google Cloud HTTPS LB).
- **No sticky sessions** in either case: any gateway can serve any quiz (D10).

## Frontend: React + TypeScript + Vite

- A thin demo (D1): join, answer, live leaderboard, host view with a Start button.
- **TypeScript** types mirror the protocol, so a mismatch with the server's messages shows up at compile time. How the types are kept in sync is decided in the protocol doc.
- The browser's native `WebSocket` API with a small reconnect helper (exponential backoff with jitter). No socket library.
- Vite for a fast dev server and a static build that nginx can serve.

## Observability

| Concern | Tool |
|---|---|
| Metrics | `prometheus/client_golang` on every service at `/metrics`; histograms for the NFR-6/7/8 latencies |
| Dashboards and alerts | Grafana, Prometheus alerting rules (thresholds in `non-functional.md`) |
| Traces | OpenTelemetry Go SDK: answer path (receive → script → reply) and broadcast path (publish → socket write) |
| Logs | `log/slog` JSON with quiz, participant, connection, and request IDs (NFR-28) |

All vendor-neutral: the same signals work with any hosted backend.

## Testing and load

| Level | Tools |
|---|---|
| Unit | `go test -race`; pure domain logic (scoring, state machine) tested without I/O; in-memory repositories |
| Integration | `testcontainers-go` starts real Redis and PostgreSQL, so Lua scripts and SQL are tested exactly as they run |
| Concurrency | Parallel duplicate submits, answers racing the deadline, two workers claiming the same transition |
| Load | **k6** (WebSocket API) for many clients across many rooms, plus a **Go room simulator** for one very large room. A Go client can open thousands of sockets from one machine at lower cost per virtual user, and measure answer → leaderboard latency end to end |

## Supporting libraries

| Need | Choice |
|---|---|
| Token signing and verification (mocked identity provider) | `golang-jwt/jwt` v5 |
| HTTP routing | Standard library `net/http` (method and path patterns) |
| Config | Environment variables, validated at startup (NFR-34) |
| Single-flight loading of the question cache | `golang.org/x/sync/singleflight` |
