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
| Go real-time server: connections, session join, quiz clock, scoring, leaderboard, fan-out across instances | React client that joins, answers, and renders the live leaderboard; minimal host view | Auth (signed tokens from a dev issuer), quiz content (static question sets), durable storage of final results | Quiz authoring UI, user accounts, payments, mobile apps, multi-region |

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
├── cmd/quizserver/main.go        # wiring only
└── internal/
    ├── quiz/                     # lifecycle state machine, questions, quiz clock
    ├── scoring/                  # scoring rules + atomic answer recording
    ├── leaderboard/              # ranking, tie-break, coalesced broadcast
    ├── session/                  # join, participants, presence
    ├── realtime/                 # WebSocket transport, connection hub, protocol codec
    ├── fanout/                   # cross-instance pub/sub
    └── platform/                 # config, logging, metrics, redis client
```

Inside each data-owning module (`quiz`, `scoring`, `leaderboard`, `session`):

```
scoring/
├── domain.go             # types and rules, no I/O
├── service.go            # use cases; depends only on the Repository interface
├── repository.go         # Repository interface
├── repository_redis.go   # Redis implementation
└── repository_memory.go  # in-memory implementation for tests, mocks, single-node runs
```

- Services receive repositories through their constructors, and `main.go` picks the implementation.
- The consuming module owns the interface (Go convention).
- **Repository methods are whole operations, not generic CRUD.** For example, `RecordAnswer(...)` deduplicates and increments in one atomic step (a Lua script in Redis, a mutex in memory). A `Get` + `Save` pair would allow check-then-write races, and two concurrent submits would both score.
- Modules without their own data (`realtime`, `fanout`, `platform`) have no repository. `fanout` exposes a `Broadcaster` interface with Redis and in-process implementations.
