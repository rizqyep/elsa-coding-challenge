# Context

Last updated: 2026-09-28

Background, scope, and the decisions that everything else builds on. The original brief is in [`../assignment.md`](../assignment.md).

## Problem

ELSA wants a real-time quiz feature for an English learning app. Learners join a vocabulary quiz session with a quiz ID, answer questions, and compete on a leaderboard that updates live.

The challenge has two parts:

1. **System design** of the whole feature: architecture diagram, components, data flow, technology choices.
2. **Implementation** of one real-time component at production quality, with everything else mocked. It must handle scalability, performance, reliability, maintainability, and observability.

Using AI tools is mandatory, and documenting how their output was verified is a key evaluation criterion.

## What the brief stresses

- **"Accurate and consistent" scoring.** This is the strongest wording in the acceptance criteria. Expect probing on duplicate submissions, races, and cheating.
- **"Promptly" and "real-time"** have no numbers attached. We set the targets ourselves in [`requirements.md`](requirements.md) and then measure them.
- **"Design and implement … with scalability in mind."** Scalability has to be shown in running code, not only described.
- **AI collaboration** is required in both design and implementation, with concrete verification evidence.

## Scope

| Build properly | Build thin (demo only) | Mock | Out of scope |
|---|---|---|---|
| Go backend (REST API, WebSocket gateway, worker): connections, join, quiz clock, scoring, leaderboard, fan-out, answer history. Real Redis and PostgreSQL | React client that joins, answers, and renders the live leaderboard; minimal host view | Auth (signed tokens from a dev issuer). Question sets are seeded data, not an authoring service | Quiz authoring UI, user accounts, payments, mobile apps, multi-region |

## Decisions

A decision is `decided` only when its rationale is written down, either here or in an ADR under `docs/system-design/decisions/`.

| ID | Decision | Outcome | Status |
|---|---|---|---|
| D1 | Main component | **Go server** at production quality; **React client** as a thin demo | decided |
| D2 | Quiz mode | **Host starts, server runs the rest** on its own clock ([notes](#d2-quiz-mode)) | decided |
| D3 | Language and runtime | **Go** server; **React + TypeScript (Vite)** client | decided |
| D4 | Real-time transport | Plain WebSocket with a documented protocol; Go library chosen in the TRD | proposed |
| D5 | State and fan-out | **Redis** for all live state (see D10): sorted sets for leaderboards, pub/sub for cross-instance broadcast | decided |
| D6 | Scale targets | Set in [`requirements.md`](requirements.md#scale-assumptions) | proposed |
| D7 | AI disclosure | Detail in `docs/ai-collaboration/`; code comments are one-line pointers, e.g. `// AI-assisted: AI-012` | decided |
| D8 | Version control | Local git for now; remote later | decided |
| D9 | Server code organisation | Module-based; each module has domain / service / repository ([notes](#d9-server-code-organisation)) | decided |
| D10 | Where quiz state lives | **Live state in Redis, static content persistent, WS servers stateless** ([notes](#d10-quiz-state-and-room-virtualisation)) | decided |
| D11 | Deployable services | **One codebase, three services: REST API, WebSocket gateway, worker** ([notes](#d11-three-services-from-one-codebase)) | decided |
| D12 | Persistent storage locally | **Real PostgreSQL in Docker Compose** with migrations and seeded question sets. The three services must share persistent data, which an in-memory mock can't do. It also makes the cache warm-up and delay-free answer validation demonstrable, and multiple question sets easy to manage | decided |
| D13 | API contracts | **Spec-first contracts:** OpenAPI 3.1 for the REST API, AsyncAPI 3.0 for the WebSocket protocol, JSON Schema **draft-07** for every WebSocket payload (the draft AsyncAPI 3.0 supports). Contract tests on both sides ([notes](#d13-contracts)) | decided |
| D14 | Test strategy | **TDD by default for critical services and core business logic.** Four levels: pure domain unit tests; service unit tests with **gomock** (incl. injected failures); integration tests on real Redis/PostgreSQL (testcontainers, with fault injection); end-to-end and load ([notes](#d14-test-strategy)) | decided |
| D15 | WebSocket authentication | **Signed token in the query string** (`/ws?token=…`) over TLS. Tokens are short-lived; query strings are excluded from load balancer and gateway logs. **Re-confirmed after task-29** (nginx's error log can quote the URL, so it runs at `emerg`): the token stays in the URL for this build, since the goal is demonstrating real-time delivery. Moving it to the `Sec-WebSocket-Protocol` header is a recorded follow-up | decided |
| D16 | Quiz code | **System-generated, 6 characters, random alphanumeric** from an alphabet without look-alikes. Collisions retried at creation; unique for good, since finished quizzes stay viewable (FR-13) | decided |
| D17 | Build focus | **The real-time path gets the full treatment** (gateway, worker, client, simulation and load runs that show the architecture is ready). **Supporting concerns are minimal scaffolds that keep the scalable shape:** observability is endpoints plus a few core metrics, with no tracing, dashboards or alert files; shutdown keeps only the drain that rolling deploys need. The full observability design stays documented in non-functional §4 as the target | decided |

### D2: quiz mode

The brief does not say how questions are paced, so our choice is an assumption and is stated as one in the requirements.

| Brief wording | Leans towards |
|---|---|
| "join a quiz **session** using a unique quiz ID" | a shared, live event that people join |
| "answer questions in real-time, **compete** with others" | a shared event, but self-paced racing also fits |
| "As users submit answers, their scores should be updated in real-time" | either mode |
| No mention of a host, timers, or question rounds | nothing requires a host role |

| Option | What it means | Upside | Cost |
|---|---|---|---|
| Host-paced | A host clicks "next"; everyone gets the same question at once | Classic live quiz | Host role and host UI the brief never mentions; timing depends on a human |
| **Host starts, server-synchronized** | Host starts; the server advances questions on a timer; everyone sees the same question | One timing authority; realistic answer bursts to test under load; minimal host UI | Server-side timer per quiz that must survive instance failure |
| Self-paced | Same question set, each user at their own speed | Smallest scope | Load spreads out, so the heavy-load story is weaker; looser fairness |

**Decision:** the host's only control is `start`. After that the server's clock drives the quiz: open question → deadline → close and reveal → intermission → next question → … → finished.

- Everyone sees the same question at the same time, so the design still has to handle fan-out to all participants, answer bursts near deadlines, and answers racing the close.
- The server is the single authority on timing. Neither the host nor clients can change it, which keeps fairness and scoring consistency easy to argue and to test.
- If the host disconnects after `start`, the quiz carries on.
- Durations are configurable per quiz, so the demo can use short timers.
- Cost we accept: a `host` role (mocked auth claim), one host-only command, a "Start" button in the client, and a Redis-backed quiz clock (see D10).

### D10: quiz state and room virtualisation

Each time the quiz moves on, the new state (current question, deadline, state version) is written to Redis. **Redis controls the quiz state.** Persistent storage holds only static question sets with their answer keys, plus final results (both mocked).

A quiz room is therefore virtual. It exists as Redis keys plus a pub/sub channel, not inside any one WebSocket server:

- A WebSocket server only holds its own sockets and a local index of which sockets are in which quiz.
- It doesn't matter which server a participant connects to, so there are no sticky sessions and no quiz-to-server routing.
- Any server can handle any message for any quiz: read or update state in Redis, then publish to the quiz channel.
- If a server dies, its users reconnect anywhere and get a snapshot from Redis.

**Something still has to act on deadlines.** Redis stores the deadline but won't advance the quiz on its own. Keyspace expiry notifications are fire-and-forget and can fire late, so they're unsuitable. The proposed mechanism, to be finalised in the TRD:

- Every instance polls a Redis sorted set `quiz:deadlines` (score = deadline) at a short interval (e.g. 100 ms) for due quizzes.
- A transition is a Lua script that checks the expected state version and applies the change atomically. If several instances see the same due quiz, exactly one wins and the rest no-op.
- The winner publishes the new state on the quiz channel, and every instance pushes it to its local sockets.
- The early close (everyone answered, FR-5) goes through the same script, so there is one code path for transitions.

**Data lifecycle.** Redis holds only what is live or not yet persisted. PostgreSQL holds everything that has been persisted.

Only the **leaderboard** carries data across the whole quiz. Everything else in Redis is either a small control record or belongs to a single question.

| Data | Scope | In Redis | Released when |
|---|---|---|---|
| Leaderboard (participant → total) | quiz lifetime | yes | final results persisted |
| Room control record: status, current question index, deadline, version | quiz lifetime, but a fixed ~200 bytes; it *is* the virtual room (D10) | yes | final results persisted |
| Participant roster (display names), presence | quiz lifetime; one entry per participant | yes | final results persisted |
| Current question's answer records (dedup, retries, early-close tracker) | one question | yes | that question's batch write is durably acknowledged |
| Question content and answer keys | static | **no**: loaded from persistent storage and cached in each instance's memory (immutable) | — |
| Answer history, final results | permanent | no (PostgreSQL) | — |

Per-room Redis memory is therefore the leaderboard + roster (each grows with participants, not questions), plus one question's answer records (about 1 MB at 10,000 participants). It does not grow with quiz length. The rule is "delete only after a durable acknowledgement". An in-memory queue inside an instance doesn't count, because it dies with the instance. Pending flushes are tracked in Redis so any instance can retry them.

This removes quiz ownership entirely, so we don't need a lease or takeover logic. The cost is Redis load from polling, which is small with one sorted set query per interval per instance, and a deadline slip bounded by the poll interval.

### D9: server code organisation

Full tactical DDD (aggregates, domain events, application services per use case) is more ceremony than this system needs. A layer-based layout (`handlers/`, `services/`, `repositories/`) spreads each feature across the tree. We use **package-by-feature modules**, each with a pure domain core:

```
server/
├── cmd/
│   ├── api/main.go               # REST API: wiring only (D11)
│   ├── ws/main.go                # WebSocket gateway: wiring only
│   └── worker/main.go            # worker: wiring only
└── internal/
    ├── quiz/                     # lifecycle state machine, questions, quiz clock
    ├── scoring/                  # scoring rules + atomic answer recording
    ├── leaderboard/              # ranking, tie-break, coalesced broadcast
    ├── session/                  # join, participants, presence
    ├── history/                  # batch writes of answers and final results to persistent storage
    ├── realtime/                 # WebSocket transport, connection registry by room, protocol codec
    ├── fanout/                   # cross-instance pub/sub
    ├── scheduler/                # per-instance loop: due transitions, leaderboard ticks, pending flushes
    └── platform/                 # config, logging, metrics, redis client
```

Inside each data-owning module (`quiz`, `scoring`, `leaderboard`, `session`):

```
scoring/
├── domain.go             # types and rules, no I/O
├── service.go            # use cases; depends only on the Repository interface
├── repository.go         # Repository interface
├── repository_redis.go   # Redis (or PostgreSQL) implementation
├── mocks/                # gomock mocks, generated by mockgen from the interfaces (D14)
├── service_test.go       # unit tests: service + mocked repository, incl. failure injection
└── repository_redis_test.go  # integration tests: real Redis/PostgreSQL via testcontainers
```

- Services receive repositories through their constructors, and `main.go` wires in the real implementation. Unit tests wire in gomock mocks instead (D14), so no hand-written in-memory implementations have to be maintained.
- The consuming module owns the interface (Go convention).
- **Repository methods are whole operations, not generic CRUD.** For example, `RecordAnswer(...)` deduplicates and increments in one atomic step (one Lua script in Redis). A `Get` + `Save` pair would allow check-then-write races, and two concurrent submits would both score.
- Modules without their own data (`realtime`, `fanout`, `platform`) have no repository. `fanout` exposes `Publisher` and `Subscriber` interfaces with a Redis implementation, mocked the same way in unit tests.

### D11: three services from one codebase

The backend is one Go module with three entry points: `cmd/api`, `cmd/ws`, and `cmd/worker`. Each wires together only the `internal/` modules it needs, and each deploys and scales on its own.

| Service | Scales with | Holds |
|---|---|---|
| REST API | host request rate (low) | nothing |
| WebSocket gateway | concurrent connections | its sockets and the connection registry |
| Worker | number of active rooms (transitions, leaderboard ticks, flushes) | nothing; all work is claimed from Redis |

**Why:**
- **Scale only what is under pressure.** A 10,000-person room needs more gateways, not more workers. Many small rooms at once need more workers, not more gateways. With one combined server, both would scale together.
- **Decoupled services.** The services never call each other. They share only Redis and PostgreSQL, so each can be deployed, restarted, or scaled without the others.
- **Failures stay contained.** A gateway crash drops only its own sockets. A worker crash is invisible, because another worker picks up its due work. A slow flush to PostgreSQL can't take CPU from socket writes.

**Costs:**
- Three deployables instead of one. Locally they run from one `docker compose` file.
- **If all workers are down, quizzes stop advancing**, so run at least 2 workers and alert when none are healthy.
- Starting a quiz goes API → Redis schedule → worker, adding up to one poll interval (100 ms) before the first question.

### D13: contracts

Every boundary between the client and the backend has a machine-readable contract in `docs/api/`, written **before** the code that implements it.

| Boundary | Contract | Used for |
|---|---|---|
| REST API | `docs/api/openapi.yaml` (OpenAPI 3.1) | Go server interfaces and types generated with `oapi-codegen`; TypeScript types generated with `openapi-typescript`; request/response validation in tests |
| WebSocket protocol | `docs/api/asyncapi.yaml` (AsyncAPI 3.0), referencing the JSON Schemas | The documented channel, every message in each direction, examples |
| WebSocket payloads | `docs/api/schemas/` (JSON Schema draft-07; AsyncAPI 3.0 supports only draft-07) | Single source for field names, types, required fields, limits; TypeScript types generated from them; Go structs checked against them in tests |
| REST payloads | Inline in `openapi.yaml` (OpenAPI 3.1's JSON Schema dialect) | As above, via the OpenAPI generators |

- **Drift is caught by tests, not reviews.** Go handler responses are validated against the OpenAPI spec in tests. Every WebSocket message the gateway sends in tests is validated against its JSON Schema. Every documented example must validate too.
- **This settles type sync between Go and TypeScript:** generated from the contracts on both sides instead of hand-written twice.

### D14: test strategy

| Level | What it proves | Tools |
|---|---|---|
| 1. Domain unit | Scoring rules, state machine, validation, exact arithmetic and edge values | `go test`, table-driven tests, no mocks needed |
| 2. Service unit | How services behave when dependencies misbehave: Redis timeouts, script errors, publish failures, PostgreSQL down, retries and backoff, slow or dead connections | **gomock** (`go.uber.org/mock`): mocks generated with `mockgen` from the module interfaces, with expectations on call order and arguments |
| 3. Integration | Lua scripts and SQL behave exactly as designed; races (concurrent duplicate submits, answer vs close, two workers claiming the same transition); recovery from real network faults | `testcontainers-go` (real Redis, PostgreSQL), **Toxiproxy** between services and Redis/PostgreSQL to inject latency, resets, and outages |
| 4. End-to-end and load | The full stack with 2 gateways, 2 workers, nginx: kill a gateway or worker mid-quiz, reconnect storms, NFR latency targets | Docker Compose, Go room simulator, k6 |

**TDD by default for critical services and core business logic.** The test is written first and must fail, then the implementation makes it pass:

| In TDD scope | Why |
|---|---|
| Scoring rules and the quiz state machine (domain) | The "accurate and consistent" core; pure functions, fastest feedback |
| Answer recording (service + Lua script) | Dedup, deadline, idempotent retry, early-close trigger |
| Transitions and scheduler claims | Exactly-once under competing workers |
| Leaderboard snapshots and ranks | Ordering, shared ranks, version monotonicity |
| Flush pipeline and reconciliation | Delete-only-after-commit, retry safety |
| Connection registry, fan-out, slow-client policy | Concurrency, backpressure, no lock held during writes |

Thin wiring (`cmd/`, HTTP routing boilerplate, config parsing) is tested after the fact.

**Tests are built to grow into the load test and simulator.** Integration and end-to-end tests drive the system through a shared **test kit**:
- a protocol client that speaks the documented WebSocket contract,
- scenario builders ("room of N participants, all answer within T seconds, one reconnects mid-question"),
- assertions on outcomes (scores, leaderboard versions, latencies).

The Go room simulator reuses the same test kit with N raised from 10 to 10,000. Load scenarios are then the same scenarios the tests already verify, just scaled up and measured.

**This is also how AI-generated code gets verified:** for TDD-scope code, the failing tests are written from the requirement IDs and reviewed by me *before* any implementation is generated. The implementation is accepted only when those tests pass under `-race`.

- **Mocks prove our handling of failures; they don't prove the scripts or SQL are right.** A mocked `RecordAnswer` can't find a race inside the Lua script, so every repository also has level-3 tests against the real thing.
- Every must-have FR maps to at least one test (NFR-32). The mapping lives in the TRD test plan.
- All tests run with `-race`.
