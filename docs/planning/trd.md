# Technical requirements (TRD)

Status: **sections 1–5 draft for review** · Last updated: 2026-09-28

How the Go backend is built. It implements the agreed [requirements](requirements.md) and the [system design](../system-design/README.md), and follows the decisions in [context](context.md) (D1–D16). It doesn't repeat the architecture; it adds what's needed to write the code.

| # | Section | Status |
|---|---|---|
| 1 | [Repository and package layout](#1-repository-and-package-layout) | draft |
| 2 | [Configuration](#2-configuration) | draft |
| 3 | [Domain model](#3-domain-model) | draft |
| 4 | [Redis: keys and Lua scripts](#4-redis-keys-and-lua-scripts) | draft |
| 5 | [PostgreSQL: schema, migrations, seed data](#5-postgresql-schema-migrations-seed-data) | draft |
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
  - the answer script checks the message's question ID against the current one before touching `ans:{qid}`;
  - the transition script checks the state version it was given.
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
| `state` | `v` state version, `s` status, `i` index, `n` question count, `q` question ID, `o` opened_at, `d` deadline, `c` close_at | transition, answer (on early close, so countdowns update) |
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

- **KEYS:** `room`, `roster`, `lb`, `online`, `sched:lbdirty`
- **ARGV:** code, participant_id, display_name, top_n, ttl_s
- **Steps:**
  1. No room → `{rejected, unknown_quiz}`. `expired` → `{rejected, quiz_expired}`.
  2. `finished` → `{finished}`; the gateway serves the final results (from Redis if still present, otherwise PostgreSQL).
  3. `HSET roster` (a rejoin may update the name), `ZADD lb NX id 0`, `ZADD online id now`, `SADD sched:lbdirty code`.
  4. Return the snapshot.
- **Returns:** `{ok, room fields…, own_score, own_rank, participant_count, top N [id, name, score]…}`
- After the script, the gateway reads `HGET ans:{q_id} id` to tell a rejoining participant whether they already answered the current question. It's read-only and informational, so it doesn't need to be atomic with the join.

#### `start`

- **KEYS:** `room`, `lb`, `sched:transitions`
- **ARGV:** code, caller_id
- **Steps:** not `lobby` → `{rejected, not_in_lobby}`. Caller ≠ `host_id` → `{rejected, not_host}`. `ZCARD lb = 0` → `{rejected, no_participants}`. Already `start_requested` → `{ok}` (idempotent). Otherwise set `start_requested=1`, `next_at=now`, `ZADD sched:transitions now code`.
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
  8. **Early close:** if `HLEN ans:{qid} ≥ ZCOUNT online (now − online_window) +inf` and that count > 0, set `close_at = next_at = now`, `ZADD sched:transitions now code`, and publish a `state` event
- **Returns:** `{accepted, correct, points, total, received_ms}`
- **Arithmetic:** Lua numbers are doubles. `math.floor(100 * remaining / window)` is exact for our ranges (both ≤ 120,000 ms), and the shared test vectors (§3.5) include the boundary cases.
- **`WAIT` (non-functional §3.2):** when `REDIS_WAIT_REPLICAS > 0`, the gateway sends `WAIT n timeout` **in the same pipeline** as the `EVALSHA`. `WAIT` only counts writes made on its own connection, and with a connection pool, a separate call could land on a different connection and confirm nothing.

#### `transition`

- **KEYS:** `room`, `qids`, `sched:transitions`, `sched:flush`, `sched:lbdirty`, `lb`, `roster`, `room:{C}` (channel)
- **ARGV:** code, expected_state_ver, top_n
- **Steps:**
  1. No room → `ZREM sched:transitions code`, `{stale}`.
  2. `state_ver ≠ expected` → `{stale}` (another worker won).
  3. `now < next_at` → `ZADD sched:transitions next_at code`, `{not_due}` (repairs a stale schedule entry).
  4. Apply the §3.3 table:
     - **open question i:** `q_id = LINDEX qids i`, `opened_at = now`, `deadline = close_at = next_at = now + window`
     - **close:** `next_at = now + reveal`, `ZADD sched:flush now "q|C|q_id"`, `HINCRBY pending_flush 1`, `SADD sched:lbdirty code`
     - **finish:** `ZADD sched:flush now "final|C"`, `ZREM sched:transitions code`, publish `finished` with the final top N
     - **expire:** `ZADD sched:flush now "final|C"`, `ZREM sched:transitions code`
  5. `HINCRBY state_ver 1`. Unless terminal, `ZADD sched:transitions next_at code`. Publish `state`.
- **Returns:** `{applied, status, state_ver}` / `{stale}` / `{not_due}`
- **Worker loop:** `ZRANGEBYSCORE sched:transitions -inf now LIMIT 0 SCHED_CLAIM_BATCH`, then for each code `HGET room state_ver` and run the script with that version. Competing workers are harmless: losers get `stale`.

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
