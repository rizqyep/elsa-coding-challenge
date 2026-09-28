# Technical requirements (TRD)

Status: **sections 1–3 draft for review** · Last updated: 2026-09-28

How the Go backend is built. It implements the agreed [requirements](requirements.md) and the [system design](../system-design/README.md), and follows the decisions in [context](context.md) (D1–D16). It doesn't repeat the architecture; it adds what's needed to write the code.

| # | Section | Status |
|---|---|---|
| 1 | [Repository and package layout](#1-repository-and-package-layout) | draft |
| 2 | [Configuration](#2-configuration) | draft |
| 3 | [Domain model](#3-domain-model) | draft |
| 4 | Redis: keys and Lua scripts | pending |
| 5 | PostgreSQL: schema, migrations, seed data | pending |
| 6 | Contracts: OpenAPI, AsyncAPI, JSON Schema | pending |
| 7 | Gateway internals | pending |
| 8 | Worker internals | pending |
| 9 | Errors and retries | pending |
| 10 | Test plan | pending |
| 11 | Local stack | pending |

---

## 1. Repository and package layout

### 1.1 Top level

```
rizqyep-elsa-assignment/
├── README.md                  # overview, quick start, links
├── Makefile                   # generate, test, up, down, load targets
├── docker-compose.yml         # nginx, api, 2× ws, 2× worker, redis, postgres
├── deploy/
│   └── nginx/nginx.conf       # /api → api, /ws → gateways, query strings not logged
├── docs/
│   ├── api/
│   │   ├── openapi.yaml       # REST contract (D13)
│   │   ├── asyncapi.yaml      # WebSocket contract (D13)
│   │   └── schemas/           # JSON Schema for every payload
│   ├── planning/  system-design/  ai-collaboration/
├── server/                    # Go module (backend)
├── client/                    # React + TypeScript (Vite)
└── loadtest/
    └── k6/                    # k6 scenarios
```

### 1.2 Go module

Module path: `github.com/rizqyep/rizqyep-elsa-assignment/server` _(assumption: to be adjusted when the remote exists)_.

```
server/
├── cmd/
│   ├── api/main.go            # REST API: config, wiring, HTTP server, graceful shutdown
│   ├── ws/main.go             # WebSocket gateway
│   ├── worker/main.go         # scheduler loops
│   └── sim/main.go            # room simulator: drives testkit scenarios at scale
├── internal/
│   ├── quiz/                  # quiz lifecycle, question sets, quiz code, question cache
│   ├── scoring/               # scoring rules, answer recording
│   ├── leaderboard/           # snapshots, shared ranks, final results
│   ├── session/               # join, roster, presence, connection replacement
│   ├── history/               # answer and final-result flushes, reconciliation
│   ├── scheduler/             # claim loops: transitions, leaderboard ticks, flush jobs
│   ├── realtime/              # WebSocket conn, read/write loops, registry, dispatcher
│   ├── fanout/                # room channel publish/subscribe
│   ├── protocol/              # WebSocket message types + codec (match docs/api/schemas)
│   ├── httpapi/               # REST handlers; gen/ holds oapi-codegen output
│   ├── auth/                  # token issue (dev) and verification
│   └── platform/
│       ├── config/            # env parsing and validation
│       ├── logging/           # slog setup, context fields
│       ├── metrics/           # Prometheus registry and metric definitions
│       ├── tracing/           # OpenTelemetry setup
│       ├── redisx/            # client, script loader, key names
│       ├── pgx/               # pool, migrations runner
│       └── retry/             # capped exponential backoff with jitter
├── migrations/                # goose SQL migrations and seed question sets
└── testkit/                   # protocol client, scenario builders, assertions (D14)
```

### 1.3 Inside a data-owning module

Using `scoring` as the example (D9):

```
internal/scoring/
├── domain.go                  # Points(), validation: pure, no I/O
├── domain_test.go             # table-driven, shared test vectors (§3.5)
├── service.go                 # SubmitAnswer use case
├── service_test.go            # gomock: repository errors, timeouts, WAIT timeouts
├── repository.go              # interface: RecordAnswer(ctx, AnswerInput) (AnswerResult, error)
├── repository_redis.go        # calls the answer script
├── repository_redis_test.go   # testcontainers + Toxiproxy (§10)
├── scripts/answer.lua         # embedded with //go:embed
└── mocks/                     # //go:generate mockgen -source=repository.go -destination=mocks/repository.go -package=mocks
```

### 1.4 Dependency rules

- `domain.go` imports nothing from `internal/` except shared ID types. It has no I/O and no `context`.
- A service depends on its own domain plus **interfaces**. It never depends on another module's repository.
- Cross-module calls go through small interfaces defined by the caller. For example, `realtime` depends on a `session.Joiner` and a `scoring.Submitter`, not on their concrete types.
- `cmd/*` is the only place that constructs concrete implementations and wires them together.
- **Redis key names live in one place** (`platform/redisx/keys.go`). Each Lua script belongs to the module whose use case it implements, even when it touches another module's keys. The answer script, owned by `scoring`, updates the leaderboard key, because atomicity requires one script. This is deliberate coupling at the storage level, kept visible in one file.
- `testkit/` is outside `internal/` so `cmd/sim` and tests in any package can use it. It depends only on `protocol/` and the public contracts.

### 1.5 Generated code

| Source | Generator | Output | Command |
|---|---|---|---|
| `docs/api/openapi.yaml` | `oapi-codegen` | `server/internal/httpapi/gen/` | `make generate` |
| `docs/api/openapi.yaml` | `openapi-typescript` | `client/src/api/schema.ts` | `make generate` |
| `docs/api/schemas/*.json` | `json-schema-to-typescript` | `client/src/protocol/` | `make generate` |
| Module interfaces | `mockgen` (`go.uber.org/mock`) | `internal/*/mocks/` | `go generate ./...` |

Generated files are committed so the repo builds without the generators installed. CI (and `make check`) regenerates and fails on any diff.

---

## 2. Configuration

All configuration comes from environment variables, parsed and validated at startup (NFR-34). A service with invalid config exits immediately, listing **every** problem, not just the first.

### 2.1 Common (all services)

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `local` | `local` or `production`. `production` forbids dev tokens |
| `HTTP_ADDR` | `:8080` | Serves the service's endpoints plus `/healthz`, `/readyz`, `/metrics` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `REDIS_ADDR` | `redis:6379` | |
| `REDIS_PASSWORD` | — | |
| `POSTGRES_DSN` | — | Required by api, ws, worker |
| `AUTH_SIGNING_KEY` | — | Required. HS256 secret for the mocked identity provider |
| `SHUTDOWN_TIMEOUT` | `30s` | Drain budget (NFR-16) |
| `DB_RETRY_BASE` / `DB_RETRY_MAX` / `DB_RETRY_BUDGET` | `100ms` / `5s` / `10s` | Question-set load retries (non-functional §3.1) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Tracing off when unset |

### 2.2 REST API

| Variable | Default | Notes |
|---|---|---|
| `DEV_TOKENS_ENABLED` | `true` | Enables `POST /api/dev/tokens`. Must be `false` in production |
| `AUTH_TOKEN_TTL` | `15m` | Only needs to be valid when a socket connects (D15) |
| `QUIZ_QUESTION_WINDOW_DEFAULT` | `15s` | Per-quiz override allowed within 5–120 s (FR-7) |
| `QUIZ_REVEAL_DEFAULT` | `5s` | Per-quiz override allowed within 2–30 s |
| `QUIZ_LOBBY_TIMEOUT` | `30m` | FR-6 |
| `QUIZ_CODE_MAX_ATTEMPTS` | `5` | Collision retries (D16) |

### 2.3 WebSocket gateway

| Variable | Default | Notes |
|---|---|---|
| `WS_ALLOWED_ORIGINS` | `http://localhost:5173` | Comma-separated (NFR-23) |
| `WS_MAX_MESSAGE_BYTES` | `4096` | NFR-20 |
| `WS_READ_BUFFER_BYTES` / `WS_WRITE_BUFFER_BYTES` | `1024` / `1024` | Write buffers are pooled (NFR-4) |
| `WS_SEND_QUEUE_SIZE` | `64` | Per-connection outbound queue (NFR-17) |
| `WS_PING_INTERVAL` / `WS_PONG_TIMEOUT` | `25s` / `60s` | Heartbeats; dead connections are dropped (NFR-5e) |
| `WS_RATE_PER_SEC` / `WS_RATE_BURST` | `20` / `40` | Per connection (NFR-21) |
| `WS_JOIN_ADMISSION_PER_SEC` | `500` | Per gateway; excess joins get "retry later" |
| `REGISTRY_SHARDS` | `64` | Connection registry shards |
| `PRESENCE_REFRESH` / `PRESENCE_TTL` | `10s` / `30s` | |
| `QUESTION_CACHE_MAX_SETS` | `256` | Memory cap on cached question sets |
| `REDIS_WAIT_REPLICAS` / `REDIS_WAIT_TIMEOUT` | `0` / `50ms` | `0` disables `WAIT` (the local stack has no replica). Production: `1` (non-functional §3.2) |

### 2.4 Worker

| Variable | Default | Notes |
|---|---|---|
| `SCHED_TRANSITION_POLL` | `100ms` | |
| `SCHED_LEADERBOARD_TICK` | `200ms` | NFR-10 |
| `SCHED_FLUSH_POLL` | `1s` | |
| `SCHED_CLAIM_BATCH` | `100` | Max items claimed per poll |
| `FLUSH_VISIBILITY_TIMEOUT` | `30s` | A claimed job becomes due again after this |
| `LEADERBOARD_TOP_N` | `10` | FR-26 |
| `QUIZ_DATA_TTL` | `24h` | Safety-net TTL on room keys (NFR-18) |

---

## 3. Domain model

Pure Go types and functions. No I/O, fully unit-tested (TDD scope, D14).

### 3.1 Identifiers

| Type | Format | Source |
|---|---|---|
| `QuizCode` | 6 chars from `23456789ABCDEFGHJKMNPQRSTUVWXYZ` (31 symbols, no `0 O 1 I L`) → ≈ 887 million codes | Generated by the API (D16) |
| `ParticipantID` | Token `sub`, opaque string ≤ 64 chars | Identity provider |
| `QuestionSetID` / `QuestionID` / `OptionID` | Short stable strings from the seed data | PostgreSQL |

**Quiz code generation:** `crypto/rand` picks each character uniformly from the alphabet, using rejection sampling to avoid modulo bias. The code is reserved with an `INSERT` into the `quizzes` table, which has a unique constraint on `code`. On a unique violation, a new code is generated, up to `QUIZ_CODE_MAX_ATTEMPTS` times. Codes are unique for good, so a finished quiz's results are always found under its code (FR-13). At a million quizzes, the chance that a new code collides is about 0.1%, so retries are rare.

### 3.2 Question sets

```go
type QuestionSet struct {
    ID        QuestionSetID
    Title     string
    Questions []Question // ordered; 1..50
}

type Question struct {
    ID              QuestionID
    Prompt          string   // e.g. "Choose the synonym of 'rapid'"
    Options         []Option // 2..4
    CorrectOptionID OptionID // answer key: never serialised to clients
}

type Option struct {
    ID   OptionID
    Text string
}
```

- `PublicQuestion` is a separate type **without** `CorrectOptionID`, and it's the only form that reaches `protocol/`. Leaking the answer key (FR-21) then requires deliberately converting the wrong type, not forgetting to omit a field.
- Question sets are immutable once loaded, so they can be cached and shared without locks (architecture §4).

### 3.3 Room state and the state machine

```go
type Status string // lobby | question_open | question_closed | finished | expired

type Room struct {
    Code               QuizCode
    QuestionSetID      QuestionSetID
    HostID             ParticipantID
    Status             Status
    QuestionIndex      int           // -1 in lobby
    QuestionCount      int
    Window, Reveal     time.Duration
    OpenedAt           time.Time     // current question opened
    Deadline           time.Time     // original deadline: used for the speed bonus
    CloseAt            time.Time     // effective close: Deadline, or earlier on early close
    NextTransitionAt   time.Time     // what the scheduler waits for
    StartRequested     bool
    LobbyExpiresAt     time.Time
    StateVersion       int64         // +1 on every transition
    LeaderboardVersion int64         // +1 on every leaderboard snapshot
}
```

**Transition table.** `Next(room, now) (Room, Event, error)` is a pure function. The Redis transition script implements the same table, and both are tested against the same cases (§3.5).

| From | Condition when due | To | Effects |
|---|---|---|---|
| `lobby` | `StartRequested` | `question_open` (index 0) | `OpenedAt = now`, `Deadline = CloseAt = now + Window`, `NextTransitionAt = CloseAt`. Event `QuestionOpened` |
| `lobby` | not started and `now ≥ LobbyExpiresAt` | `expired` | no next transition. Event `QuizExpired` |
| `question_open` (i) | `now ≥ CloseAt` | `question_closed` (i) | `NextTransitionAt = now + Reveal`; enqueue flush job for question i. Event `QuestionClosed` (with correct option) |
| `question_closed` (i) | `i < QuestionCount − 1` | `question_open` (i + 1) | as for index 0. Event `QuestionOpened` |
| `question_closed` (i) | `i = QuestionCount − 1` | `finished` | enqueue finalise job. Event `QuizFinished` |
| `finished`, `expired` | — | — | terminal; nothing scheduled |

Every transition increments `StateVersion`. A transition is applied only if the stored version equals the version the worker read (exactly-once, NFR-14).

**Commands** (not transitions; they change fields the scheduler later acts on):

| Command | Allowed when | Effect |
|---|---|---|
| `Start` (host) | `lobby`, caller is host, ≥ 1 participant | `StartRequested = true`, `NextTransitionAt = now` (FR-3) |
| `EarlyClose` (from the answer script) | `question_open`, accepted answers ≥ online participants | `CloseAt = now`, `NextTransitionAt = now` (FR-5). `Deadline` is unchanged |

> **Why `Deadline` and `CloseAt` are separate** (found while writing this section): if early close simply moved the one deadline, anything computed from it later (speed bonus, audit rows, a reconnecting client's countdown) would silently use the shortened value. Keeping the original `Deadline` for scoring and a separate `CloseAt` for acceptance means early close can never change anyone's points.

### 3.4 Scoring

```go
// Points for one accepted answer. Pure; mirrored exactly by answer.lua.
func Points(correct bool, receivedAt, openedAt, deadline time.Time) int
```

| Rule | Detail |
|---|---|
| Wrong answer | 0 |
| Correct answer | `100 + bonus` |
| Bonus | `floor(100 × remainingMs / windowMs)`, with `remainingMs = deadline − receivedAt` clamped to `[0, windowMs]` and `windowMs = deadline − openedAt` |
| Range | correct: 100–200; wrong: 0 (FR-19) |
| Arithmetic | integers in milliseconds only, no floats, so Go and Lua can't round differently |

**Acceptance** (checked by the answer script, in this order; the first failure is the result):

| # | Check | Result on failure |
|---|---|---|
| 1 | `Status = question_open` | `question_closed` |
| 2 | message `QuestionID` = current question | `wrong_question` |
| 3 | `receivedAt < CloseAt` (Redis `TIME`) | `question_closed` |
| 4 | participant is in the roster | `not_joined` |
| 5 | no stored answer for this participant and question | `duplicate`, returning the **stored** result (FR-18) |

`OptionID` must belong to the current question. The gateway checks this against the question cache before calling the script and returns `invalid_option`.

**Ranks** (FR-24): `rank = 1 + number of participants with a strictly higher total`. Equal totals share a rank, e.g. 1, 2, 2, 4. Display order within a tie is by participant ID, which is stable.

### 3.5 Keeping Go and Lua in sync

Scoring and transitions exist twice: in Go (`domain.go`, the specification, unit-tested) and in Lua (the atomic implementation). Divergence is prevented by **shared test vectors**:

- `server/internal/scoring/testdata/points_cases.json` and `server/internal/quiz/testdata/transition_cases.json` list inputs and expected outputs, including edge cases: answer exactly at `OpenedAt`, one millisecond before `CloseAt`, exactly at `CloseAt`, after early close, index at last question, and so on.
- Domain unit tests run every case against the Go function.
- Integration tests run **the same file** against the Lua script on real Redis.
- A case added for a bug fix is therefore checked on both sides automatically.

### 3.6 Validation

| Input | Rule |
|---|---|
| Display name | Trimmed, 1–20 characters (Unicode letters, digits, spaces, `-_.'`), no control characters (FR-15) |
| Quiz code in a join | Exactly 6 characters from the alphabet, case-insensitive (upper-cased before lookup) |
| Question window / reveal overrides | Within the ranges in §2.2 |
| Request ID | Client-generated, 1–64 characters; used for idempotent resend (FR-18) |

### 3.7 Domain events

Transitions and commands return events. The worker turns them into protocol messages and publishes them. Services never write to sockets directly.

| Event | Published as | Audience |
|---|---|---|
| `QuestionOpened` | `question` (public question, `closeAt`, index/count) | room |
| `QuestionClosed` | `question_closed` (correct option, per-option counts) | room; then personal `rank` per participant |
| `LeaderboardUpdated` | `leaderboard` (top N, participant count, version) | room |
| `QuizFinished` | `quiz_finished` (final top N) | room |
| `QuizExpired` | `quiz_state` (`expired`) | room |

> **Open question:** per-option answer counts in `question_closed` are a nice reveal ("62% chose B"), but they aren't in the requirements. They cost one `HVALS` pass over the question's answers at close. Include them, or keep the reveal to just the correct option?
