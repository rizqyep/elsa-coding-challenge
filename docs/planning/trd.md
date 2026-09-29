# Technical requirements (TRD)

Status: **complete** (all sections reviewed) · Last updated: 2026-09-28

How the Go backend is built. It implements the agreed [requirements](requirements.md) and the [system design](../system-design/README.md), and follows the decisions in [context](context.md) (D1–D16). It doesn't repeat the architecture; it adds what's needed to write the code.

| # | Section | Status |
|---|---|---|
| 1 | [Repository and package layout](#1-repository-and-package-layout) | draft |
| 2 | [Configuration](#2-configuration) | draft |
| 3 | [Domain model](#3-domain-model) | draft |
| 4 | [Redis: keys and Lua scripts](#4-redis-keys-and-lua-scripts) | draft |
| 5 | [PostgreSQL: schema, migrations, seed data](#5-postgresql-schema-migrations-seed-data) | draft |
| 6 | [Contracts](#6-contracts) | draft |
| 7 | [Gateway internals](#7-gateway-internals) | draft |
| 8 | [Worker internals](#8-worker-internals) | draft |
| 9 | [Errors, timeouts, and retries](#9-errors-timeouts-and-retries) | draft |
| 10 | [Test plan](#10-test-plan) | draft |
| 11 | [Local stack and simulation control](#11-local-stack-and-simulation-control) | draft |

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
├── tools/
│   └── contracts/             # TypeScript generators + OpenAPI/AsyncAPI validation (own package.json)
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
│   ├── realtime/              # WebSocket conn, read/write loops, registry, handlers, room subscriber
│   ├── protocol/              # WebSocket message types + codec (match docs/api/schemas)
│   ├── httpapi/               # REST handlers; gen/ holds oapi-codegen output
│   ├── auth/                  # token issue (dev) and verification
│   └── platform/
│       ├── config/            # env parsing and validation
│       ├── logging/           # slog setup, context fields
│       ├── metrics/           # Prometheus registry and metric definitions
│       ├── tracing/           # OpenTelemetry setup
│       ├── redisx/            # client, script loader, key names
│       ├── postgres/          # pool and helpers (named to avoid clashing with the pgx import)
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
- **Comments stay minimal:** one-line doc comments on exported names (required by the linter), and a short comment only where the code would otherwise mislead. Rationale and edge-case reasoning live in the docs (this TRD, task files, `context.md` decisions), and the comment points there, e.g. `// TRD §3.3`. AI-assistance pointers follow the same one-line rule (D7).

### 1.5 Generated code

| Source | Generator | Output | Command |
|---|---|---|---|
| `docs/api/openapi.yaml` | `oapi-codegen` | `server/internal/httpapi/gen/` | `make generate` |
| `docs/api/openapi.yaml` | `openapi-typescript` (in `tools/contracts/`) | `client/src/api/schema.ts` | `make generate` |
| `docs/api/schemas/*.json` | `json-schema-to-typescript` (in `tools/contracts/`) | `client/src/protocol/messages.ts` (one file) | `make generate` |
| Module interfaces | `mockgen` (`go.uber.org/mock`) | `internal/*/mocks/` | `go generate ./...` |

Generated files are committed so the repo builds without the generators installed. CI (and `make check`) regenerates and fails on any diff.

---

## 2. Configuration

All configuration comes from environment variables, parsed and validated at startup (NFR-34). A service with invalid config exits immediately, listing **every** problem, not just the first.

### 2.1 Common (all services)

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `local` | `local` or `production`. Seed data is applied only in `local` |
| `HTTP_ADDR` | `:8080` | Serves the service's endpoints plus `/healthz`, `/readyz`, `/metrics` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `REDIS_ADDR` | `redis:6379` | |
| `REDIS_PASSWORD` | — | |
| `POSTGRES_DSN` | — | Required by api, ws, worker |
| `AUTH_SIGNING_KEY` | — | Required, at least 32 bytes. HS256 secret for the mocked identity provider |
| `SHUTDOWN_TIMEOUT` | `30s` | Drain budget (NFR-16) |
| `DB_RETRY_BASE` / `DB_RETRY_MAX` / `DB_RETRY_BUDGET` | `100ms` / `5s` / `10s` | Question-set load retries (non-functional §3.1) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Tracing off when unset |
| `QUIZ_DATA_TTL` | `24h` | Safety-net TTL on room keys (NFR-18): set by the API at creation, refreshed by gateway joins and worker flushes |

### 2.2 REST API

| Variable | Default | Notes |
|---|---|---|
| `DEV_TOKENS_ENABLED` | `true` | Enables `POST /api/v1/dev/tokens` |
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
| `WS_MAX_CONNECTIONS` | `20000` | Per gateway (NFR-3); over it, upgrades get 503 before any goroutine or buffer is allocated |
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

Every transition increments `StateVersion`. **Exactly-once (NFR-14) comes from the transition script deciding and applying in one atomic step:** after any applied transition the next one is in the future (window ≥ 5 s, reveal ≥ 2 s), so competing workers find nothing due. An earlier expected-version argument was removed as redundant (task-13 mutation finding).

**Commands** (not transitions; they change fields the scheduler later acts on):

| Command | Allowed when | Effect |
|---|---|---|
| `Start` (host) | `lobby`, caller is host, ≥ 1 participant | `StartRequested = true`, `NextTransitionAt = now` (FR-3). Repeating it while the quiz runs changes nothing and succeeds |
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

Room events are published **by the Lua script that causes them**, in the same atomic step (TRD §4). The event on the channel is a compact internal format. Each gateway turns it into the client-facing protocol message, using its question cache for question text and the correct option, encodes it once, and writes it to its local sockets. Services never write room messages to sockets directly.

| Event | Published as | Audience |
|---|---|---|
| `QuestionOpened` | `question` (public question, `closeAt`, index/count) | room |
| `QuestionClosed` | `question_closed` (correct option) | room; then personal `rank` per participant |
| `LeaderboardUpdated` | `leaderboard` (top N, participant count, version) | room |
| `QuizFinished` | `quiz_finished` (final top N) | room |
| `QuizExpired` | `quiz_state` (`expired`) | room |

Per-option answer counts in the reveal were considered and left out for now: not in the requirements (decided 2026-09-28).

---

## 4. Redis: keys and Lua scripts

### 4.1 Conventions

- **Hash tag:** every key of a room contains `{C}`, where `C` is the quiz code, so a room's keys share one Cluster slot.
- **Time:** scripts read `TIME` and work in integer milliseconds. Services never pass their own clock into a decision.
- **Scripts never build key names.** Every key a script touches is passed in `KEYS` (a Redis Cluster rule). When a key depends on state (e.g. the current question's answers), the caller computes it from state it has read, and the script rejects the call if that state has since changed:
  - the answer script checks the message's question ID against the current one before touching `ans:{qid}`.
- **Loading:** scripts are embedded with `//go:embed`, loaded at startup, and called by hash (`EVALSHA`, with automatic reload on `NOSCRIPT`).
- **Returns:** always an array whose first element is a status string (`ok`, `accepted`, `duplicate`, `rejected`, `stale`, …). Business outcomes are return values, never Redis errors. A Redis error means an infrastructure failure.
- **Events:** a script that changes what the room should see publishes the event itself (§3.7), JSON-encoded with Redis' built-in `cjson`. The channel name is passed in `KEYS` too. Plain `PUBLISH` doesn't require it, but sharded pub/sub (`SPUBLISH`, the Cluster step) does.

### 4.2 Keys

| Key | Type | Fields / members | Written by | TTL |
|---|---|---|---|---|
| `quiz:{C}:room` | hash | `set_id`, `host_id`, `status`, `q_index`, `q_id`, `q_count`, `window_ms`, `reveal_ms`, `opened_at`, `deadline`, `close_at`, `next_at`, `start_requested`, `lobby_expires_at`, `state_ver`, `lb_ver`, `pending_flush`, `created_at` | create, start, answer (early close), transition, leaderboard | `QUIZ_DATA_TTL` |
| `quiz:{C}:qids` | list | question IDs in order | create | same |
| `quiz:{C}:roster` | hash | participant ID → display name | join | same |
| `quiz:{C}:online` | sorted set | participant ID → last-seen ms | join, presence refresh | same |
| `quiz:{C}:lb` | sorted set | participant ID → total score | join (0), answer | same |
| `quiz:{C}:ans:{qid}` | hash | participant ID → `option|correct|points|received_ms` | answer | same; deleted after flush |
| `sched:transitions` | sorted set | quiz code → `next_at` | create, start, answer (early close), transition | none |
| `sched:lbdirty` | set | quiz codes | join, answer, transition (close) | none |
| `sched:flush` | sorted set | job ID → next attempt ms. Job IDs: `q|C|qid` (answers), `final|C` (results) | transition, claim | none |
| `room:{C}` | channel | internal events (§4.3) | scripts; `kick` by gateways | — |

**TTL while a flush is pending:** before processing a claimed job, the worker re-applies `QUIZ_DATA_TTL` to that quiz's keys, so unpersisted data can't expire while it's being retried (NFR-18).

### 4.3 Internal events on `room:{C}`

Not part of the client contract. They're decoded only by gateways, which ignore unknown types (forward compatible).

| `t` | Fields | Published by |
|---|---|---|
| `state` | `v` state version, `s` status, `i` index, `n` question count, `q` question ID, `o` opened_at, `d` deadline, `c` close_at, `x` next_at (0 once terminal). Gateways need `x` for `question_closed.nextTransitionAt`: the reveal is timed from the actual transition, so they can't compute it | transition, answer (on early close, so countdowns update) |
| `lb` | `v` leaderboard version, `n` participant count, `top` `[[id, name, score], …]` | leaderboard snapshot |
| `finished` | `v`, `n`, `top` (final top N) | transition (to `finished`) |
| `kick` | `p` participant ID, `k` connection ID to keep | gateway, when an identity reconnects elsewhere (FR-12) |

`cjson` encodes an empty Lua table as `{}`, not `[]`. `top` is never empty here, because a leaderboard event needs at least one participant. The gateway decoder still accepts `{}` as an empty list.

### 4.4 Scripts

| Script | Caller | Purpose |
|---|---|---|
| `create_room` | API | Create the room in `lobby` and schedule its expiry |
| `join` | gateway | Add or restore a participant; return the snapshot |
| `start` | API | Host requests the start (FR-3) |
| `answer` | gateway | Record one answer atomically (FR-16 to FR-22, FR-5) |
| `transition` | worker | Apply one due transition (§3.3) and publish the state event |
| `leaderboard` | worker | Publish a leaderboard snapshot and bump its version |
| `claim` | worker | Claim due flush jobs with a visibility timeout |
| `flush_ack` | worker | Delete flushed answers and complete the job |
| `release` | worker | Delete a finished or expired room's keys |

Presence refresh (`ZADD online`) and personal rank lookups (`ZSCORE` + `ZCOUNT`) are plain pipelined commands; they need no atomicity.

#### `create_room`

- **KEYS:** `room`, `qids`, `sched:transitions`
- **ARGV:** code, set_id, host_id, window_ms, reveal_ms, lobby_timeout_ms, ttl_s, question IDs…
- **Steps:** if `room` exists → `{rejected, code_in_use}`. Otherwise write the room hash (`status=lobby`, `q_index=-1`, `q_count=#ids`, `state_ver=1`, `lb_ver=0`, `pending_flush=0`, `start_requested=0`, `lobby_expires_at=now+timeout`, `next_at=lobby_expires_at`), `RPUSH qids`, set TTLs, `ZADD sched:transitions next_at code`.
- **Returns:** `{ok, created_at}`

#### `join`

- **KEYS:** `room`, `roster`, `lb`, `online`, `sched:lbdirty`, `room:{C}` channel
- **ARGV:** code, participant_id, display_name, top_n, ttl_s, conn_id
- **Steps:**
  1. No room → `{rejected, unknown_quiz}`. `expired` → `{rejected, quiz_expired}`.
  2. `finished` → `{finished}`; the gateway serves the final results (from Redis if still present, otherwise PostgreSQL).
  3. `HSET roster` (a rejoin may update the name), `ZADD lb NX id 0`, `ZADD online id now`, `SADD sched:lbdirty code`.
  4. If `conn_id` is set, `PUBLISH` `{t: kick, p, k: conn_id}`, so other gateways close this participant's older connections (FR-12) without an extra round trip.
  5. Return the snapshot.
- **Returns:** `{ok, room fields…, own_score, own_rank, participant_count, top N [id, name, score]…}`
- After the script, only when a question is open or closed, the gateway reads the participant's standing for the current question (the `standings` read) to tell a rejoining participant whether they already answered it. It's read-only and informational, so it doesn't need to be atomic with the join, and a lobby join burst never pays for it. If it fails, the snapshot goes without it; a resend still returns the original result (FR-30).

#### `presence`

- **KEYS:** `room`, `online`. **ARGV:** participant IDs (at most 500 per call).
- **Steps:** room gone (`PTTL ≤ 0`) → return 0, so a released room's online set is never recreated without an expiry. Otherwise `ZADD online <Redis TIME> id…` and `PEXPIRE online <room PTTL>`.

#### `start`

- **KEYS:** `room`, `lb`, `sched:transitions`
- **ARGV:** code, caller_id
- **Steps** (same order as `quiz.Start`): caller ≠ `host_id` → `{rejected, not_host}`. Already `start_requested` and still running (`lobby`, `question_open`, `question_closed`) → `{ok}`, so a host retrying a timed-out start gets 202, not a false 409. Not `lobby` (finished, expired) → `{rejected, not_in_lobby}`. `ZCARD lb = 0` → `{rejected, no_participants}`. Otherwise set `start_requested=1`, `next_at=now`, `ZADD sched:transitions now code`.
- **Returns:** `{ok}`

#### `answer`

- **KEYS:** `room`, `ans:{qid}` (qid from the client message), `lb`, `roster`, `online`, `sched:lbdirty`, `sched:transitions`, `room:{C}` (channel)
- **ARGV:** code, participant_id, qid, option_id, correct (`0`/`1`, from the gateway's answer-key cache), online_window_ms, ttl_s
- **Steps** (acceptance order from §3.4):
  1. `status ≠ question_open` → `{rejected, question_closed}`
  2. `q_id ≠ qid` → `{rejected, wrong_question}`
  3. `now ≥ close_at` → `{rejected, question_closed}`
  4. not in roster → `{rejected, not_joined}`
  5. stored answer exists → `{duplicate, option, correct, points, received_ms, current_total}`
  6. `points` per §3.4 using `deadline`, `opened_at`, `window_ms`
  7. `HSET ans:{qid} id "option|correct|points|now"` (set TTL on first write), `ZINCRBY lb points id`, `SADD sched:lbdirty code`
  8. **Early close:** if `HLEN ans:{qid} ≥ ZCOUNT online (now − online_window) +inf` and that count > 0, set `close_at = next_at = now`, `HINCRBY state_ver 1`, `ZADD sched:transitions now code`, and publish a `state` event with the new version. Without the new version, gateways and clients (FR-28) would drop the moved close time as stale
- **Returns:** `{accepted, correct, points, total, received_ms}`
- **Arithmetic:** Lua numbers are doubles. `math.floor(100 * remaining / window)` is exact for our ranges (both ≤ 120,000 ms), and the shared test vectors (§3.5) include the boundary cases.
- **`WAIT` (non-functional §3.2):** when `REDIS_WAIT_REPLICAS > 0`, the gateway sends `WAIT n timeout` **in the same pipeline** as the `EVALSHA`. `WAIT` only counts writes made on its own connection, and with a connection pool, a separate call could land on a different connection and confirm nothing.

#### `transition`

- **KEYS:** `room`, `qids`, `sched:transitions`, `sched:flush`, `sched:lbdirty`, `lb`, `roster`, `room:{C}` (channel)
- **ARGV:** code, top_n
- **Steps:**
  1. No room → `ZREM sched:transitions code`, `{stale}`.
  2. `now < next_at` → `ZADD sched:transitions next_at code`, `{not_due}` (repairs a stale schedule entry). **This check is what makes competing workers harmless:** after any applied transition the next one is in the future, so the others find nothing due.
  3. Apply the §3.3 table:
     - **open question i:** `q_id = LINDEX qids i`, `opened_at = now`, `deadline = close_at = next_at = now + window`
     - **close:** `next_at = now + reveal`, `ZADD sched:flush now "q|C|q_id"`, `HINCRBY pending_flush 1`, `SADD sched:lbdirty code`
     - **finish:** `ZADD sched:flush now "final|C"`, `ZREM sched:transitions code`, publish `finished` with the final top N
     - **expire:** `ZADD sched:flush now "final|C"`, `ZREM sched:transitions code`
  4. `HINCRBY state_ver 1`. Unless terminal, `ZADD sched:transitions next_at code`. Publish `state`.
- **Returns:** `{applied, status, state_ver}` / `{stale}` / `{not_due}`
- **Worker loop:** `ZRANGEBYSCORE sched:transitions -inf now LIMIT 0 SCHED_CLAIM_BATCH`, then run the script for each code. Competing workers are harmless: every worker after the first gets `not_due`.

#### `leaderboard`

- **KEYS:** `room`, `lb`, `roster`, `room:{C}` (channel)
- **ARGV:** code, top_n
- **Steps:** no room → `{stale}`. `ZREVRANGE lb 0 top_n−1 WITHSCORES`, `HMGET roster` for those IDs, `ZCARD lb`, `HINCRBY room lb_ver 1`, publish `lb`.
- **Returns:** `{ok, lb_ver}`
- **Worker loop:** `SPOP sched:lbdirty SCHED_CLAIM_BATCH`, then run the script per code. A code popped by a worker that then crashes is re-marked by the next answer or question close (data-flow §4).

#### `claim`

- **KEYS:** `sched:flush`
- **ARGV:** batch, visibility_ms
- **Steps:** `ZRANGEBYSCORE sched:flush -inf now LIMIT 0 batch`, then `ZADD sched:flush (now + visibility) job` for each.
- **Returns:** the claimed job IDs. Two workers can't claim the same job at the same time, because the read and the push-ahead happen in one script.

#### `flush_ack`

- **KEYS:** `room`, `ans:{qid}`, `sched:flush`
- **ARGV:** job_id
- **Steps:** `DEL ans:{qid}`; `HINCRBY room pending_flush -1` **only if** `ZREM sched:flush job_id` removed something (so a duplicate ack can't decrement twice).
- **Returns:** `{ok}`

#### `release`

- **KEYS:** `room`, `qids`, `roster`, `online`, `lb`, `sched:flush`, `sched:transitions`
- **ARGV:** code, job_id
- **Steps:** `DEL` the room keys, `ZREM` the job and any schedule entry.
- **Returns:** `{ok}`

### 4.5 Flush and finalise jobs (worker)

| Job | Process |
|---|---|
| `q|C|qid` | Refresh TTLs → `HGETALL ans:{qid}` → one batch insert into `answers` (§5.3), `ON CONFLICT DO NOTHING` → commit → `flush_ack`. Empty or missing hash (already flushed) → straight to `flush_ack` |
| `final|C` | If `pending_flush > 0`, push the job 1 s ahead and stop. Otherwise read `lb` and `roster`, compute shared ranks, then **one transaction:** insert `quiz_results`, run the reconciliation query (§5.4), update `quizzes.status`/`finished_at`. Commit → `release`. A mismatch doesn't block the commit: it is logged with details and counted (FR-35) |

A worker that dies mid-job leaves the job claimed. It becomes due again after `FLUSH_VISIBILITY_TIMEOUT`, and every step is safe to repeat.

### 4.6 Known limits of this layout

- `sched:*` keys are global, so scripts touching both a room and a schedule key are fine on a single primary but **cross-slot on Redis Cluster**. The Cluster step (non-functional §1.4) splits schedules into per-shard keys and moves the schedule write out of the room scripts into an idempotent follow-up command, with a periodic sweep for anything missed. It's not needed at our current ceiling.
- Presence is never removed on disconnect; it expires after `PRESENCE_TTL`. Removing it on disconnect could wrongly mark someone offline who has already reconnected through another gateway, and that would let the early close fire too soon. The cost: someone who leaves keeps the question open until their presence expires (≤ 30 s) or the deadline passes, whichever is first.

---

## 5. PostgreSQL: schema, migrations, seed data

### 5.1 Schema

```sql
CREATE TABLE question_sets (
    id          text PRIMARY KEY,
    title       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE questions (
    id                 text PRIMARY KEY,
    set_id             text NOT NULL REFERENCES question_sets(id),
    position           int  NOT NULL CHECK (position >= 0),
    prompt             text NOT NULL,
    correct_option_id  text NOT NULL,
    UNIQUE (set_id, position)
);

CREATE TABLE options (
    id           text PRIMARY KEY,
    question_id  text NOT NULL REFERENCES questions(id),
    position     int  NOT NULL CHECK (position >= 0),
    text         text NOT NULL,
    UNIQUE (question_id, position)
);

CREATE TABLE quizzes (
    code             char(6) PRIMARY KEY,           -- D16: unique for good
    question_set_id  text NOT NULL REFERENCES question_sets(id),
    host_id          text NOT NULL,
    status           text NOT NULL CHECK (status IN ('lobby','running','finished','expired')),
    window_ms        int  NOT NULL,
    reveal_ms        int  NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz
);

CREATE TABLE answers (                                -- FR-32
    quiz_code       char(6) NOT NULL REFERENCES quizzes(code),
    question_id     text    NOT NULL REFERENCES questions(id),
    participant_id  text    NOT NULL,
    option_id       text    NOT NULL,
    correct         boolean NOT NULL,
    points          int     NOT NULL CHECK (points BETWEEN 0 AND 200),
    received_at     timestamptz NOT NULL,
    PRIMARY KEY (quiz_code, question_id, participant_id)   -- idempotent flushes
);

CREATE TABLE quiz_results (                           -- FR-27a
    quiz_code       char(6) NOT NULL REFERENCES quizzes(code),
    participant_id  text    NOT NULL,
    display_name    text    NOT NULL,
    total_score     int     NOT NULL CHECK (total_score >= 0),
    rank            int     NOT NULL CHECK (rank >= 1),
    PRIMARY KEY (quiz_code, participant_id)
);
CREATE INDEX quiz_results_by_rank ON quiz_results (quiz_code, rank);
```

- `questions.correct_option_id` pointing at one of *its own* options isn't enforced by a foreign key (it would be circular). It's checked by a seed-validation test and by the question-set loader, which refuses a malformed set.
- `quizzes.status` is coarse: `lobby` at creation, `running` once the API accepts the start, `finished`/`expired` from the finalise job. The fine-grained state lives only in Redis.

### 5.2 Quiz creation (API)

One transaction:
1. `INSERT INTO quizzes`. On a unique violation, generate a new code, up to `QUIZ_CODE_MAX_ATTEMPTS` times (D16).
2. Run `create_room` in Redis.
3. Commit.

If Redis fails, the transaction rolls back and the code is freed. If the commit fails after Redis succeeded, the orphan room expires as an unstarted lobby.

### 5.3 Answer batch insert

One statement per flush, with arrays as parameters:

```sql
INSERT INTO answers (quiz_code, question_id, participant_id, option_id, correct, points, received_at)
SELECT $1, $2, p, o, c, pts, to_timestamp(r / 1000.0)
FROM unnest($3::text[], $4::text[], $5::bool[], $6::int[], $7::bigint[]) AS t(p, o, c, pts, r)
ON CONFLICT DO NOTHING;
```

### 5.4 Reconciliation (FR-35)

```sql
SELECT participant_id, COALESCE(SUM(points), 0) AS total
FROM answers WHERE quiz_code = $1
GROUP BY participant_id;
```

This is compared with the Redis leaderboard. A participant with no stored answers must have a live total of 0.

### 5.5 Migrations and seed data

- `goose` SQL migrations in `server/migrations/`, run by a one-shot `migrate` service in Docker Compose that finishes before `api`, `ws`, and `worker` start.
- Seed question sets, as a migration applied only when `APP_ENV=local`:

| Set | Questions | Purpose |
|---|---|---|
| `demo-quick` | 3 | Video demo: a whole quiz in about a minute with short windows |
| `synonyms-everyday` | 10 | Everyday vocabulary: synonyms |
| `business-english` | 10 | Workplace vocabulary |

- A seed-validation test loads every set and checks: 2–4 options per question, `correct_option_id` belongs to the question, positions are contiguous.

---

## 6. Contracts

The contracts are real files, written before the code (D13). This section explains them; the files are the source of truth.

### 6.1 Files

| File | Describes | Format |
|---|---|---|
| [`docs/api/openapi.yaml`](../api/openapi.yaml) | REST API: 7 operations, request/response schemas, error model | OpenAPI 3.1 |
| [`docs/api/asyncapi.yaml`](../api/asyncapi.yaml) | WebSocket protocol: channel, auth, envelope, versioning, close codes, 4 client and 10 server messages with request/reply pairs | AsyncAPI 3.0 |
| [`docs/api/schemas/common.json`](../api/schemas/common.json) | Shared WebSocket types: IDs, quiz code, public question, leaderboard, error codes | JSON Schema draft-07 |
| `docs/api/schemas/ws/client/*.json`, `…/server/*.json` | One schema per WebSocket message, each with an example | JSON Schema draft-07 |
| [`docs/api/redocly.yaml`](../api/redocly.yaml) | Lint rules for the OpenAPI file | Redocly |

**Why two schema dialects:** AsyncAPI 3.0 accepts JSON Schema **draft-07** only (checked in the AsyncAPI 3.0.0 spec), so WebSocket payloads use draft-07. REST schemas live inside the OpenAPI 3.1 file. The only overlap is the quiz-code pattern, which appears in both; a contract test checks the two patterns are identical.

### 6.2 REST API

| Method and path | Who | Success | Main errors |
|---|---|---|---|
| `POST /api/v1/dev/tokens` | anyone (dev only) | 201 token | 404 when disabled |
| `GET /api/v1/question-sets` | host | 200 list | 403 |
| `POST /api/v1/quizzes` | host | 201 quiz in lobby | 404 `question_set_not_found`, 503 |
| `GET /api/v1/quizzes/{code}` | any token | 200 quiz (live from Redis, archived from PostgreSQL) | 400 bad code, 404 `unknown_quiz` |
| `POST /api/v1/quizzes/{code}/start` | the quiz's host | 202 start accepted (idempotent while running) | 400, 403 `forbidden` / `not_host`, 409 `not_in_lobby` / `no_participants` |
| `GET /api/v1/quizzes/{code}/leaderboard?offset&limit` | any token | 200 page (live from Redis, final from PostgreSQL) | 400 bad code or range, 404 |
| `GET /healthz`, `GET /readyz` | none | 200 / 503 | — |

- Errors are RFC 9457 problem details with a machine-readable `code`. Every API operation also documents 500 `internal` (a bug; no detail is returned).
- Every request is routed, authenticated, then validated against `openapi.yaml` before a handler runs; an operation without an access rule stops the API from starting.
- Dependency outages return 503 with `Retry-After`.
- REST timestamps are RFC 3339. WebSocket timestamps are epoch milliseconds, because clients compute countdowns from them.

### 6.3 WebSocket protocol

| Client → server | Reply |
|---|---|
| `join` {quizCode, displayName} | `snapshot` or `error` |
| `watch` {quizCode} (host) | `snapshot` or `error` |
| `submit_answer` {questionId, optionId} | `answer_result` (`accepted`/`duplicate`) or `error` |
| `ping` {clientTime} | `pong` {clientTime, serverTime} |

| Server → client (pushed) | When |
|---|---|
| `question` | a question opens, or its `closeAt` moves earlier (early close); idempotent by `questionId` |
| `question_closed` | a question closes; carries the correct option |
| `rank` | after each close, per participant |
| `leaderboard` | batched leaderboard update |
| `quiz_finished` | the quiz ends |
| `quiz_state` | a lifecycle change without a question (e.g. `expired`) |
| `snapshot` | reply to join/watch, and resync after a slow-client drop |
| `error` | request failure or connection-level problem |

**Server rule:** every client frame is validated against its JSON Schema at runtime (embedded copy of `docs/api/schemas`) before it is dispatched. Unknown fields in client frames are rejected.

**Client rules:**
- Apply state only if its version is newer, **including snapshots**. The gateway subscribes a connection to its room *before* reading the snapshot, so an event can arrive before an older snapshot. Monotonic versions make that ordering harmless.
- Ignore unknown fields and unknown message types (additive changes within v1).
- Resend `submit_answer` with the same `id` after a reconnect if no reply arrived (FR-18, FR-30).

### 6.4 Contract checks

| Check | When | Tool |
|---|---|---|
| Every schema is valid draft-07; every schema example validates; negative cases are rejected (answer key in a question, look-alike in a quiz code, over-long name, missing request ID, points over 200, wrong protocol version) | CI, `make check` | JSON Schema validator. Already run on the current files: all pass |
| `openapi.yaml` lints clean (2 expected warnings: health probes have no 4xx) | CI | `redocly lint` (already run) |
| `asyncapi.yaml` validates (0 errors, 0 warnings) | CI | `asyncapi validate` (already run) |
| Every REST response in handler tests validates against the spec | `go test` | `kin-openapi` response validation |
| Every WebSocket message sent in gateway tests validates against its schema | `go test` | JSON Schema validator in the test kit |
| Generated code is up to date | CI | `make generate` then `git diff --exit-code` |

---

## 7. Gateway internals

### 7.1 Connection lifecycle

```
HTTP GET /ws?token=…
  → origin allowed?            no  → 403
  → token valid, not expired?  no  → 401
  → under WS_MAX_CONNECTIONS?  no  → 503 + Retry-After
  → join admission bucket ok?  no  → 503 + Retry-After (jittered)   (before upgrading: no goroutine or buffer yet)
  → upgrade (gorilla Upgrader: 1 KB read/write buffers, shared write-buffer pool, compression off)
  → start readLoop + writeLoop goroutines
  → state: connected → (join | watch) → in room → closed
```

- **Messages on one connection are handled one at a time**, on its read goroutine. A client can't have two requests in flight, so it can't fan out load on its own.
- **The rate limit runs before decoding**, so a flood costs a token-bucket check, not JSON Schema validation.
- **One quiz per connection.** A second `join`/`watch` gets `already_joined`. Switching quizzes means opening a new connection.
- **Allowed messages by state:** before joining: `join`, `watch`, `ping`. After joining: `submit_answer` (participants only), `ping`. Anything else → `error` (`not_joined` / `forbidden` / `unknown_type`).

### 7.2 Goroutines per connection

| Goroutine | Does | Limits |
|---|---|---|
| `readLoop` | Reads frames, decodes the envelope, applies the rate limiter, dispatches | `SetReadLimit(4096)`; read deadline `WS_PONG_TIMEOUT`, extended by each pong |
| `writeLoop` | The **only** writer to the socket (gorilla allows one concurrent writer). Drains the send queue, sends pings every `WS_PING_INTERVAL` | Write deadline 10 s per frame; any write error closes the connection |

The read loop runs on the HTTP handler's own goroutine, so a connection costs exactly **two goroutines**. `Close` never blocks its caller: the close frame is written from a short-lived goroutine, because gorilla's `WriteControl` waits for a write that may be stalled.

The send queue is a buffered channel of `WS_SEND_QUEUE_SIZE` items. An item is either a shared `*websocket.PreparedMessage` (room broadcasts) or a personal `[]byte` (replies, ranks).

### 7.3 Handling client messages

| Message | Steps |
|---|---|
| `join` | Validate code and name → ensure the question set is cached (single-flight, retry with backoff, §9) → **register in the registry and subscribe to the room first** → `join` script → `HGET` current answer → send `snapshot`. If this identity already has a local connection in the room, close the old one with 4000; also publish `kick` so other gateways do the same (FR-12) |
| `watch` | Host role and `host_id` match → register as a watcher (no roster entry, no presence) → snapshot |
| Both | A participant token can't `watch`; a host token can't `join` (the host doesn't play). One quiz per connection: a second `join`/`watch` → `already_joined`. A room this gateway already has skips the room lookup, so a join burst reads the room once per gateway. A failed join leaves the room, so the client can retry on the same connection. A finished quiz isn't joined: the reply is a read-only finished snapshot, from Redis while the room exists, then from the archived results (FR-13) |
| `submit_answer` | Must be a joined participant → cached question: does `optionId` belong to `questionId`? (`invalid_option`) → `correct` from the cached answer key → `answer` script (+ `WAIT` in the same pipeline when enabled) → `answer_result` or `error`. A question not in the cached set → `wrong_question` without calling Redis |
| `ping` | Reply `pong` with `TIME`-aligned server time (the gateway tracks its offset from Redis `TIME`, refreshed every 30 s) |

**Rate limiting:** a token bucket per connection (`WS_RATE_PER_SEC`, `WS_RATE_BURST`). Over the limit → `error rate_limited` with `retryAfterMs`. More than 100 violations in 10 s → close with 4002.

### 7.4 Connection registry

```go
type registry struct {
    shards [REGISTRY_SHARDS]struct {
        mu    sync.RWMutex
        rooms map[QuizCode]*room // room: conns map[*Conn]struct{}, participants map[ParticipantID]*Conn, lastStateVer, lastLbVer
    }
}
```

- Shard = `fnv32(code) % REGISTRY_SHARDS`. Join and leave take the shard's write lock only.
- **Broadcast:** take the read lock, copy the room's connection list, release the lock, then for each connection do a **non-blocking** send to its queue. A full queue triggers the slow-client policy. No lock is held while touching sockets or queues.
- **Entering a room** (`Hub.Enter`): take a question-set reference (one per connection; the cache is reference-counted), register, then make sure the room is subscribed and **wait for Redis to confirm it**. Writing `SUBSCRIBE` isn't enough: until Redis has processed it, a publish can still be missed, and the join reads its snapshot right after `Enter` returns. A newer connection for the same participant replaces the older one, which is closed with 4000.
- **Leaving:** unregister and release the reference. The last connection out drops the subscription, unless the room was re-entered in the meantime (the subscriber re-checks the registry under its own lock).

### 7.5 Room events → client messages

One subscriber goroutine per gateway reads the Redis pub/sub connection in order. Redis runs scripts one at a time and delivers publishes in order, so events arrive in version order. The gateway still drops any event whose version isn't newer than the room's last seen version (`state` and `finished` share one counter, `lb` has its own).

**The subscriber runs its own receive loop, not go-redis's `Channel()`.** `Channel()` reconnects and resubscribes silently, hides subscription confirmations, and drops messages when its buffer stays full, so the gateway couldn't tell when to resync. The subscriber instead:
- counts confirmations per `SUBSCRIBE` sent, so a late confirmation of an earlier subscription can't release a newer join early;
- pings after 15 s of silence, and treats an unanswered ping as a dead connection (a half-open TCP connection never errors on its own);
- on any error, closes the connection, backs off (full jitter, 100 ms → 5 s), reconnects and resubscribes every local room in one command;
- signals each room **once its own resubscription is confirmed**, so the snapshot push that follows can't miss an event.

Redis reads made because of an event (ranks, resubscribe snapshots) run off the subscriber goroutine, at most 16 at a time per gateway.

| Internal event | Client message(s) |
|---|---|
| `state`, status `question_open` | `question`: question text and options from the cache, times from the event. Built once, broadcast as a prepared message. An early close sends the same question with an earlier `closeAt` and a newer version |
| `state`, status `question_closed` | `question_closed` with the correct option from the cache and `nextTransitionAt` from `x` (broadcast). Then personal ranks: one `standings` read for every local participant (score, participants with a higher score, and the count, in one round trip), and a `rank` message to each |
| `state`, status `finished` / `expired` | `quiz_finished` from the `finished` event / `quiz_state` |
| `lb` | `leaderboard` (broadcast) |
| `kick` | Close local connections of that participant, except the one named in the event, with 4000 |

### 7.6 Slow clients (NFR-17)

A non-blocking send that finds the queue full:
1. Empties the connection's queue and marks a resync as pending. Until the resync starts, further frames for this connection are dropped: the snapshot supersedes them.
2. The writer waits a random 0–250 ms and for one of 32 **gateway-wide resync slots**, then clears the pending mark and reads a fresh `snapshot` from Redis. When Redis stalls, every connection overflows at once; the slots and the jitter stop their resyncs from hitting Redis together.
3. If the same connection overflows again within 30 s, it is closed with 4003. A failed snapshot read also closes with 4003; the client reconnects and gets a snapshot then.

### 7.7 Presence

Every `PRESENCE_REFRESH`, for each room with local participants, one small script call sets `ZADD online <Redis TIME> id…` for all of them. Using Redis time avoids clock skew between gateways and Redis in the early-close check. Presence is never removed on disconnect (§4.6). Rooms of more than 500 local participants take one call per 500 (the `presence` script, §4.4). Watchers are skipped.

**Server time for `pong`:** each gateway keeps its offset from Redis `TIME`, measured at the midpoint of the round trip and refreshed every 30 s; a failed refresh keeps the last offset. Countdowns built from `pong` then line up with the deadlines Redis stamped.

### 7.8 Question cache

| Aspect | Rule |
|---|---|
| Key | question-set ID |
| Load | single-flight; retry with capped exponential backoff and jitter within `DB_RETRY_BUDGET`; failure → join gets `server_busy` (retryable) |
| Validation on load | 2–4 options per question, correct option belongs to the question; otherwise the set is refused and logged |
| Lifetime | reference-counted by local rooms using it; entries with no references are evicted LRU once over `QUESTION_CACHE_MAX_SETS` |
| Concurrency | entries are immutable; the map is behind an `RWMutex` |

### 7.9 Shutdown (NFR-16)

On `SIGTERM`:
1. `/readyz` returns 503, so the load balancer stops sending new connections.
2. New upgrades get 503.
3. Existing connections are closed with 1012 in jittered batches spread over `SHUTDOWN_TIMEOUT` (default 30 s), so clients don't all reconnect at once.
4. Unsubscribe, close Redis and PostgreSQL pools, exit.

### 7.10 Burst and stampede guards

Every shared dependency a burst could pile onto has a guard:

| Burst | Guard |
|---|---|
| Joins missing the question cache | Single-flight per question set (§7.8); entries never expire by time |
| Reconnect storm after a gateway dies | `WS_MAX_CONNECTIONS` and the join admission bucket, checked **before** the upgrade, so a rejected client costs no goroutine or buffer |
| Rejected clients retrying together | `Retry-After` jittered by 0–2 s; browsers, which can't read it, use full-jitter reconnect backoff (§9.5) |
| Every connection resyncing when Redis stalls | 32 gateway-wide resync slots plus 0–250 ms jitter (§7.6) |
| Subscriber reconnect pushing snapshots to every connection (§9.3) | One room read (`view`) and one standings read per room, whatever its size; at most 16 rooms at a time. Concurrent slow-client resyncs in a room also share one room read |
| One client flooding requests | One request at a time per connection, and the rate limit before decoding (§7.1, §7.3) |

---

## 8. Worker internals

### 8.1 Loops

Each worker runs three independent loops, each on its own `time.Ticker`. A pass that overruns its interval simply skips ticks rather than piling up.

| Loop | Every | Claim | Work per item | Parallelism |
|---|---|---|---|---|
| Transitions | `SCHED_TRANSITION_POLL` (100 ms) | `ZRANGEBYSCORE sched:transitions -inf now LIMIT 0 N` | `transition` script | bounded pool, 16 |
| Leaderboard | `SCHED_LEADERBOARD_TICK` (200 ms) | `SPOP sched:lbdirty N` | `leaderboard` script | bounded pool, 16 |
| Flush | `SCHED_FLUSH_POLL` (1 s) | `claim` script | answer flush or finalise (§4.5) | bounded pool, 4 |

Several workers run the same loops. Transitions are guarded by the script's due check, leaderboard codes by `SPOP`, and flush jobs by the claim's visibility timeout, so extra workers never duplicate work. They only compete for it.

### 8.2 Idempotency

Every step is safe to repeat, which is what makes crash recovery a no-op:

| Step | Why it is safe to repeat |
|---|---|
| Transition | Due check: a repeat finds nothing due (`not_due`) |
| Leaderboard | Publishing a newer snapshot is always correct; the version only goes up |
| Answer flush insert | `ON CONFLICT DO NOTHING` on the primary key |
| `flush_ack` | `DEL` is idempotent; the counter only decrements if the job still existed |
| Finalise | Results insert, reconciliation, and status update in one transaction, so it either happened or didn't. `release` is idempotent |

### 8.3 Health

- **Readiness:** Redis and PostgreSQL reachable, and not shutting down.
- **Liveness:** each loop records its last completed pass. If any loop hasn't completed a pass in 5× its interval, `/healthz` fails and the process is restarted. This is what the "no healthy workers" alert (non-functional §4.4) counts.

### 8.4 Shutdown

On `SIGTERM`: stop claiming new work, let in-flight items finish (up to `SHUTDOWN_TIMEOUT`), then exit. A job still claimed at exit becomes due again after its visibility timeout, so nothing is lost.

---

## 9. Errors, timeouts, and retries

### 9.1 Three kinds of failure

| Kind | Examples | Handling |
|---|---|---|
| **Business outcome** | question closed, duplicate answer, not the host, unknown quiz | A normal return value from a script or service, mapped to a protocol `error` code or HTTP 4xx. Never retried by the server. Not logged as an error |
| **Transient infrastructure** | Redis or PostgreSQL timeout, connection refused, failover in progress | Returned as retryable (`server_busy` over WebSocket, 503 + `Retry-After` over REST). Retried only where the operation is idempotent (§9.3) |
| **Bug** | panic, impossible state, script returning an unknown status | Recovered per connection or request, logged with stack and IDs, counted, returned as `internal`. The process keeps serving everyone else |

### 9.2 Timeouts

Every call carries a `context` deadline. Nothing waits forever.

| Call | Timeout | Reason |
|---|---|---|
| Answer script (+ `WAIT`) | 250 ms | Inside the NFR-7 budget (p99 < 250 ms) |
| Join script, snapshot reads | 500 ms | |
| Transition, leaderboard, claim scripts | 500 ms | The next poll retries anyway |
| Question-set load (PostgreSQL) | 2 s per attempt, 10 s budget | §9.3 |
| Answer batch insert | 10 s | Up to 10,000 rows |
| Finalise transaction | 15 s | Results + reconciliation |
| REST request | 5 s | Whole handler, including the create transaction |
| Socket write | 10 s per frame | A stuck socket is closed, not waited on |

### 9.3 Retry policy

**Rule: retry only what is safe to repeat, and let the component that owns idempotency do the retrying.**

| Operation | Retried by | How | Why that's safe |
|---|---|---|---|
| Answer script timed out or failed | **Nobody on the server.** The client resends with the same request ID | Client backoff (§9.5) | If the first attempt did apply, the resend gets `duplicate` with the original result (FR-18). A server-side retry would hide the ambiguity |
| Question-set load | Gateway / API, inside single-flight | Full-jitter backoff: base 100 ms, cap 5 s, budget 10 s | Read-only |
| Quiz creation | Client (host) after 503 | — | Transaction rolls back on failure; no half-created quiz |
| Transition, leaderboard tick | Next scheduler poll | Automatic (100 / 200 ms) | The transition script applies only what is due, atomically; leaderboard versions only increase |
| Answer flush, finalise | Visibility timeout | Job due again after 30 s; repeated failures raise `flush_failures_total` | Primary key + `ON CONFLICT DO NOTHING`; one transaction |
| Pub/sub subscription dropped, or a health-check ping unanswered | Gateway | Reconnect with full-jitter backoff (100 ms → 5 s), resubscribe every local room in one command, **then, once each room's resubscription is confirmed, send its connections a fresh `snapshot`**: one room read and one standings read per room, not per connection (§7.10) | Snapshots are complete and versioned, so nothing missed while disconnected matters |
| Presence refresh failed | Next refresh | — | Last-seen only moves forward |

**Backoff formula (full jitter):** `sleep = random(0, min(cap, base × 2^attempt))`. Spreading retries randomly avoids synchronised waves of retries after an outage.

### 9.4 Degraded modes

| Condition | Detection | Behaviour |
|---|---|---|
| Redis unhealthy | 3 failed pings in a row (1 s apart) | `/readyz` fails; the gateway refuses new upgrades and joins; answers get `server_busy` |
| PostgreSQL unhealthy | failed ping | API: 503 for create/results. Gateway: joins on uncached question sets fail after the retry budget. Worker: flushes wait. **Live quizzes keep running** |
| Worker loop stalled | no completed pass in 5× its interval | `/healthz` fails and the orchestrator restarts it |

### 9.5 Client reconnect policy (React app and test kit)

| Close / error | Client action |
|---|---|
| Network drop, 1001, 1006, 1012 (service restart) | Reconnect with full-jitter backoff (base 500 ms, cap 15 s), re-`join`, resend any unanswered `submit_answer` with its original `id` |
| HTTP 503 or `server_busy` | Same, but wait at least `Retry-After` / `retryAfterMs` |
| 4000 (replaced by a newer connection) | **Don't reconnect.** Another tab or device owns the session |
| 4003 (slow consumer) | Reconnect after a delay; the new snapshot resyncs |
| HTTP 401 / 403 (token, origin) | Don't reconnect; get a new token first |

Browsers can't read a failed handshake's HTTP status: every pre-upgrade rejection looks like 1006, so a browser falls back to the jittered backoff above. The React client therefore refreshes its token before `expiresAt` instead of waiting for a 401. The Go test kit and simulator read the status directly.

### 9.6 Error mapping

| Cause | WebSocket `error.code` | HTTP |
|---|---|---|
| Schema or size violation | `invalid_message` (size: close 1009) | 400 `invalid_request` |
| Unknown message type | `unknown_type` | — |
| `v` ≠ 1 | `unsupported_version` | — |
| Rate limit | `rate_limited` (+ `retryAfterMs`) | — |
| Script `unknown_quiz` / `quiz_expired` | `unknown_quiz` / `quiz_expired` | 404 `unknown_quiz` |
| Script `not_host` / `not_in_lobby` / `no_participants` | — | 403 `not_host` / 409 |
| Script `wrong_question` / `question_closed` / `not_joined` | same code | — |
| Option not in question (cache check) | `invalid_option` | — |
| Redis/PostgreSQL timeout or unavailable | `server_busy`, `retryable: true` | 503 `unavailable` + `Retry-After` |
| Panic, unknown script status | `internal` | 500 `internal` |

Every error increments `errors_total{service, code}` and is logged once, at the point it is mapped, with quiz, participant, connection, and request IDs.

---

## 10. Test plan

### 10.1 How TDD works here (D14)

For every unit in TDD scope:
1. **Write the failing tests** from the requirement IDs they cover (table in §10.4), including edge cases and shared test vectors (§3.5).
2. **I review the tests** before any implementation is written or generated. They are the acceptance criteria for AI-generated code.
3. **Implement** until they pass under `-race`.
4. **Record** the AI collaboration entry (D7).

### 10.2 Levels, locations, commands

| Level | Where | Build tag | Command | Runs against |
|---|---|---|---|---|
| Domain unit | `internal/*/domain_test.go` | — | `make test-unit` | pure Go |
| Service unit | `internal/*/service_test.go` | — | `make test-unit` | gomock mocks |
| Contract | `internal/httpapi`, `internal/protocol`, `testkit` | — | `make test-contract` | OpenAPI + JSON Schemas |
| Integration | `internal/*/*_redis_test.go`, `*_pg_test.go` | `integration` | `make test-integration` | testcontainers: Redis, PostgreSQL, Toxiproxy |
| End-to-end | `server/e2e/` | `e2e` | `make test-e2e` | the Docker Compose stack (§11) |
| Load / simulation | `cmd/sim`, `loadtest/k6` | — | `make sim …`, `make k6 …` | the Docker Compose stack |

`make test` runs unit + contract + integration. `make check` adds lint (`golangci-lint`), `go vet`, and "generated code is up to date".

### 10.3 The test kit

One package drives the system in tests **and** in the simulator, so load scenarios are scenarios the tests have already verified (D14).

```go
env := testkit.Compose("http://localhost:8080")      // or testkit.InProcess(t) for integration tests
host := env.Host(t)                                   // host token + REST client
quiz := host.CreateQuiz(ctx, "demo-quick", testkit.Window(5*time.Second))

room := testkit.Room(quiz).
    Participants(50).
    Answers(testkit.Within(2*time.Second), testkit.CorrectRatio(0.7))

res := room.Run(ctx, env, func(r *testkit.Run) {
    r.At(1*time.Second, host.Start(quiz))
    r.AtQuestion(1, testkit.KillGateway(0))           // fault steps (ignored when unsupported by env)
})

res.AssertScoresMatchExpected(t)                     // recompute from submitted answers + server times
res.AssertLeaderboardVersionsMonotonic(t)
res.AssertLatencies(t, testkit.NFR6(500*time.Millisecond), testkit.NFR7(100*time.Millisecond))
```

- `testkit.Client`: speaks the documented protocol, validates every received message against its JSON Schema, applies the monotonic-version rules, and records timings.
- **Expected scores are computed independently** from what the clients sent and the `receivedAt` the server returned, using the Go scoring function. A run proves the whole pipeline scored correctly, not just that it returned numbers.

### 10.4 Requirement → test mapping (must-haves)

| FR | Test(s) |
|---|---|
| FR-1 | `TestCreateQuiz_ReturnsUniqueCode`, `TestCreateQuiz_RetriesOnCodeCollision` (gomock: repository returns a unique violation twice) |
| FR-2, FR-4, FR-7 | `TestNext_TransitionTable` (vectors), `TestTransitionScript_Vectors` (same vectors, real Redis), `E2E_QuizRunsToFinish` |
| FR-3 | `TestStartScript_OnlyHost`, `…_OnlyInLobby`, `…_NeedsParticipant`, `…_Idempotent` |
| FR-5 | `TestAnswerScript_EarlyCloseWhenAllOnlineAnswered`, `…_IgnoresOfflineParticipants`, `TestEarlyClose_DoesNotChangePoints` |
| FR-8, FR-9 | `TestJoinScript_ConcurrentJoins_NoLossNoDuplicates` (1,000 goroutines) |
| FR-10 | `TestJoin_LateJoinerStartsAtZeroAndCanAnswerOpenQuestion` |
| FR-11 | `TestSnapshot_HasNoAnswerKey` (+ schema), `TestJoin_SnapshotContents` |
| FR-12 | `TestJoin_RejoinKeepsScore`, `E2E_SecondConnectionKicksFirst_AcrossGateways` |
| FR-13, FR-27a | `TestLeaderboardAPI_FinishedQuizFromPostgres`, `TestJoin_FinishedQuizReturnsFinal` |
| FR-15 | `TestValidateDisplayName` (table) |
| FR-16, FR-17 | `TestAnswerScript_AcceptanceOrder` (vectors), `TestAnswerScript_LateByOneMillisecondRejected`, `TestSubmitAnswer_OnlyCurrentQuestion` |
| FR-18, FR-30 | `TestAnswerScript_DuplicateReturnsOriginal`, `TestAnswerScript_ConcurrentSubmitsSameUser_ScoreOnce` (100 goroutines), `E2E_ResendAfterReconnect_NoDoubleScore` |
| FR-19 | `TestPoints_Vectors` (Go), `TestAnswerScript_PointsVectors` (Lua, same file) |
| FR-20 | `TestSubmitAnswer_ReplyContents` |
| FR-21 | `TestPublicQuestion_HasNoCorrectOption`, contract negative case, `TestGateway_NoAnswerKeyBeforeClose` (inspects every frame sent while open) |
| FR-22 | `E2E_TwoGateways_SameTotals` |
| FR-23, FR-24 | `TestRanks_SharedRanks` (1, 2, 2, 4), `TestLeaderboardScript_OrderAndNames` |
| FR-25, FR-26 | `TestLeaderboardTick_CoalescesBurst` (1,000 answers → ≤ 5 updates/s), `TestBroadcast_SameBytesForAllRecipients` |
| FR-27 | `TestLeaderboardAPI_LivePagination` |
| FR-28 | `TestClient_IgnoresOlderVersions` (test kit + React reducer test), `TestGateway_DropsStaleEvents` |
| FR-29 | `E2E_ReconnectResumesAtCurrentQuestion_MissedScoresZero` |
| FR-31 | `E2E_HostDisconnectAfterStart_QuizContinues` |
| FR-32, FR-33 | `TestFlushJob_WritesAllAnswers`, `TestFlushJob_NotDuringOpenQuestion` |
| FR-34 | `TestFlushJob_DeletesOnlyAfterCommit` (gomock: commit fails → no delete), `Integration_KillWorkerBetweenCommitAndDelete` |
| FR-35 (S) | `TestFinalise_ReconciliationMismatchCounted`, and `score_reconciliation_mismatches_total == 0` after every simulation |

### 10.5 Concurrency tests (integration, real Redis)

| Race | Test |
|---|---|
| Same participant submits twice at once | 100 goroutines, one answer → exactly one `accepted`, total counted once |
| Answer vs close | Answers fired across `closeAt` while a transition runs → each answer is either accepted with correct points or rejected; never both, never lost |
| Two workers, one due transition | Both run the script → exactly one `applied`, one `stale` |
| Two workers, one flush job | Both claim → only one gets it; `pending_flush` ends at 0 |
| Join burst | 1,000 concurrent joins → roster = leaderboard = 1,000 |

### 10.6 Failure injection

**With gomock (service unit):**
- Redis timeout on answer → `server_busy`, no retry.
- Script returns an unknown status → `internal` and logged.
- PostgreSQL fails 3 times then succeeds on a question-set load → one load, backoff timings respected, singleflight shared.
- Commit fails during flush → no `flush_ack`.
- Publish-side errors don't exist, since events are published inside scripts; tests assert that services never call publish directly.

**With Toxiproxy and containers (integration / e2e):**

| Scenario | Injection | Expected |
|---|---|---|
| Redis latency | +100 ms on the Redis proxy | Answers still accepted; NFR-7 degrades but no errors; nothing scored twice |
| Redis connection reset mid-answer | reset after request sent | Client gets `server_busy`, resends → `duplicate` or `accepted`, never counted twice |
| Redis down 5 s | disable proxy | Gateways not ready; answers `server_busy`; after recovery, resubscribe + snapshots; quiz resumes on stored deadlines |
| PostgreSQL down during a question close | pause container | Quiz continues; flush job retries; data kept in Redis; flushed after recovery |
| Gateway killed mid-question | `docker kill ws-1` | Its clients reconnect to ws-2 with a snapshot; no answer lost or double-counted |
| Worker killed mid-flush | `docker kill worker-1` | Job due again after 30 s; flushed exactly once |
| All workers stopped for 10 s | `docker stop` both | No late answers accepted (deadline enforced by the script); quiz resumes on restart |

### 10.7 Load and simulation scenarios

Scenario files in `loadtest/scenarios/` are run by `cmd/sim` (§11.4). **Pass criteria are the NFRs.** A run fails if a target is missed, and results are written to `loadtest/results/` and summarised in `docs/testing.md` with the machine spec.

| Scenario | Shape | Pass criteria |
|---|---|---|
| `big-room` | 1 room, 5,000 participants (then 10,000), all answer within 2 s, 5 questions | NFR-6/7/8 p95 met, 0 errors, reconciliation 0 |
| `many-rooms` | 200 rooms × 50 participants, staggered starts | same, plus transition lag p95 < 250 ms |
| `gateway-crash` | `big-room` with `ws-1` killed during question 2 | all clients back within 15 s, reconciliation 0 |
| `slow-clients` | `big-room` with 5% of clients reading slowly | others unaffected (NFR-6 met for fast clients); slow clients resynced or closed with 4003 |
| `redis-latency` | `big-room` with +20 ms Redis latency | NFR-7 still met |

---

## 11. Local stack and simulation control

Goal: **one command to start everything, one to scale it, one to run a simulation, and simple switches for failures while a simulation runs.**

### 11.1 Quick start

```bash
cp .env.example .env          # optional; make up does this if .env is missing
make up                       # build, migrate, seed, start everything, wait until healthy
open http://localhost:8080    # client (participant + host views)
make sim SCENARIO=big-room    # run a simulation against the running stack
make down                     # stop (keep data)  |  make reset: stop and delete volumes
```

Prerequisites: Docker with Compose v2, and `make`. Go 1.23+ only for running tests or the simulator outside Docker (`make sim` falls back to a container when Go isn't installed).

### 11.2 Services

| Service | Replicas (default) | Port on host | Notes |
|---|---|---|---|
| `nginx` | 1 | **8080** | Serves the built client at `/`, routes `/api` → api, `/ws` → ws. Re-resolves service names every 5 s (Docker DNS resolver + variable upstream) so scaled instances join the rotation; to be confirmed when the config is written. Query strings not logged (D15) |
| `api` | 1 | — | |
| `ws` | 2 | — | `nofile` ulimit 65,536 |
| `worker` | 2 | — | |
| `migrate` | one-shot | — | goose up + seed; the Go services start only after it completes successfully |
| `redis` | 1 | 6379 (localhost only) | AOF on |
| `postgres` | 1 | 5432 (localhost only) | named volume |
| **Optional profiles** | | | |
| `observability` | Prometheus, Grafana | 9090, 3000 | Grafana comes provisioned with the three dashboards from non-functional §4.5 |
| `replica` | `redis-replica` | — | Also sets `REDIS_WAIT_REPLICAS=1`, to exercise `WAIT` |
| `chaos` | `toxiproxy` | 8474 (API) | Services reach Redis and PostgreSQL through Toxiproxy, so latency and outages can be injected |

`make up PROFILES="observability chaos"` enables profiles. Everything else works without them.

### 11.3 Make targets

| Target | Does |
|---|---|
| `make up` / `down` / `reset` | Start (build, migrate, seed, wait healthy) / stop / stop and wipe volumes |
| `make ps` / `logs S=ws` | Status / follow logs for one service |
| `make scale WS=4 WORKER=3` | Change replica counts without restarting the rest |
| `make redis-cli` / `psql` | Shells into the data stores for inspection |
| `make generate` | Run all code generators (§1.5) |
| `make test` / `test-e2e` / `check` | §10.2 |
| `make sim SCENARIO=… [overrides]` | Run a simulation scenario (§11.4) |
| `make k6 SCENARIO=…` | Run a k6 scenario from `loadtest/k6/` |
| `make chaos-…` | Failure switches (§11.5) |
| `make demo` | `up`, then create a `demo-quick` quiz and print the host and participant URLs, for the video |

### 11.4 Simulation control

`cmd/sim` runs a scenario file through the test kit against the running stack. Everything in the file can be overridden from the command line, so a run can be adjusted without editing files.

```yaml
# loadtest/scenarios/big-room.yaml
name: big-room
questionSet: synonyms-everyday
questions: 5                 # use the first N questions of the set
window: 10s
reveal: 3s
rooms: 1
participantsPerRoom: 5000
joinRampUp: 20s              # spread joins; 0 = all at once (join storm)
answers:
  within: 2s                 # all answers land within 2 s of the question opening
  correctRatio: 0.7
  noAnswerRatio: 0.02        # some participants don't answer (tests the deadline path)
slowClients: 0               # ratio of clients that read slowly
chaos:                       # optional timed faults
  - at: question:2+1s
    action: kill ws-1
assert: [nfr6, nfr7, nfr8, reconciliation]
```

```bash
make sim SCENARIO=big-room PARTICIPANTS=10000 WINDOW=15s
make sim SCENARIO=many-rooms ROOMS=500
make sim SCENARIO=big-room CHAOS=off
```

**Output:**
- A live summary in the terminal: connected, answered, current p50/p95/p99 for NFR-6/7/8, errors by code.
- A JSON report in `loadtest/results/<scenario>-<timestamp>.json` with the config, machine info, and final percentiles.
- Exit code ≠ 0 if any `assert` fails, so a scenario can gate CI.

**Docker access:** chaos steps that kill or pause containers call Docker. When the simulator runs on the host it uses the local Docker CLI. The containerised fallback mounts the Docker socket only for scenarios that contain such steps.

**Client-side limits:** the simulator raises its own open-file limit. One machine can open about 28,000 connections to a single address:port before running out of local ports. That covers the 10,000-person target. Beyond it, the simulator can spread connections across several source IPs.

### 11.5 Failure switches

Usable on their own, or as `chaos` steps in a scenario.

| Target | Effect |
|---|---|
| `make chaos-kill S=ws-1` | Kill one container (gateway or worker) |
| `make chaos-stop S=worker` | Stop all instances of a service; `chaos-start` brings them back |
| `make chaos-redis-latency MS=50` | Add latency between services and Redis (requires the `chaos` profile) |
| `make chaos-redis-down SEC=5` | Cut Redis off for N seconds, then restore |
| `make chaos-pg-down SEC=30` | Pause PostgreSQL for N seconds |
| `make chaos-slow-client` | Start clients that read slowly, alongside a running simulation |
| `make chaos-reset` | Remove all injected faults |

### 11.6 Configuration for demos and simulations

- **Per quiz:** question window and reveal durations can be set when the quiz is created (5–120 s and 2–30 s), so demos can use 10 s questions without changing server config.
- **Per stack:** everything in §2 can be overridden in `.env`, e.g. `SCHED_LEADERBOARD_TICK=100ms` to compare batching windows.
- **`.env.example`** documents every variable with its default. `make up` refuses to start if a required value is missing, and prints which one.
