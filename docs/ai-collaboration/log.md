# AI collaboration log

One entry per significant use of AI. Each entry records what I asked, what the AI produced, what I decided, and how it was checked.

Tool for all entries so far: Claude Code (Claude Opus 5.5).

---

## Design phase

### AI-001: Reading the brief and shaping the repo

- **Date / phase:** 2026-09-28 · planning
- **Task type:** analysis, structure
- **What I asked:** "Let's try to understand the readme first … we will create a directory called `rizqyep-elsa-assignment` and later put docs/system-design, docs/planning, client, server … let's also see what other documents we can create."
- **What the AI produced:** a breakdown of the brief's deliverables, a proposed repo layout, and extra documents (protocol contract, ADRs, testing doc, AI collaboration docs, video script).
- **What I decided:**
  - Kept the layout.
  - Moved the video script and my private notes **outside** the submission repo.
  - Asked for a planning doc and task files before any design work.
- **Catches:**
  - The AI pointed out that my plan built both client and server, while the brief says "pick a component, mock the rest". This led to D1: the server is the main component and the client is a thin demo.
  - It also flagged a conflict between my own standing rule (no AI attribution in repos) and the brief's requirement to disclose AI use. I resolved it with the D7 convention: one-line code pointers to this log, and no attribution in commits.
- **Verification:** I checked every deliverable in the AI's breakdown against the brief. The "AI Collaboration in Design" section is listed only under Submission, not in Part 1, so it was added to the traceability table explicitly.

### AI-002: Planning structure

- **Date / phase:** 2026-09-28 · planning
- **Task type:** document drafting
- **What I asked:** "Create our internal plannings first on what we will tackle, starting from the system design, how to position myself as your driver."
- **What the AI produced:** a planning index with a requirement traceability table, 20 task files (T001–T020), and a private driver playbook describing how I work with the AI.
- **What I decided:** 20 task files before requirements existed was premature. I restructured planning into a sequence where each document feeds the next: `index → context → requirements → trd → tasks`. System design stays as its own deliverable, and tasks are derived from the TRD instead of guessed up front.
- **Verification:** the traceability table was re-mapped from task IDs to FR/NFR IDs, so each brief requirement points to a concrete, numbered requirement.

### AI-003: Stack and server code organisation

- **Date / phase:** 2026-09-28 · planning (D1, D3, D9)
- **Task type:** design options
- **What I asked:** Go for the backend, React for the frontend, and "we can use either DDD or module based?"
- **What the AI produced:** a recommendation for package-by-feature modules, each split into `domain.go / service.go / redis.go`, rather than full tactical DDD or a layer-based layout.
- **What I decided:** I changed the split to **domain / service / repository** per module, with the repository as an interface plus Redis and in-memory implementations. The same service can then run on Redis, on memory in tests, or on a mocked source.
- **Catch:** when adapting to my structure, the AI added a rule that repository methods must be **whole atomic operations** (e.g. `RecordAnswer` does dedup and increment together), not generic `Get`/`Save`. A `Get` + `Save` pair lets two concurrent submissions both pass the duplicate check and score twice. That race directly breaks "accurate and consistent" scoring.
- **Verification:** design-level only so far. It will be proven by a concurrent duplicate-submission test in the implementation phase.

### AI-004: Quiz mode (D2)

- **Date / phase:** 2026-09-28 · planning
- **Task type:** requirements interpretation
- **What happened:**
  1. The AI first recommended **host-paced** (a host clicks "next").
  2. I asked it to reconfirm this against the brief. On re-reading, it found that the brief **does not specify pacing at all**. It changed its recommendation to server-timed and laid out the wording that leans each way.
  3. I argued host-paced would demonstrate better. The AI agreed on demo value, with the condition that the server still enforces each question's deadline.
  4. My final decision: **the host only starts the quiz; the server drives everything after that.**
- **Why I decided this:** one timing authority (the server) makes fairness and scoring consistency easy to argue and test, and it keeps host features the brief never asked for to a minimum.
- **Catch:** the AI's first recommendation was presented as following from the brief when it didn't. I now ask for the exact brief wording whenever a recommendation claims to be required.
- **Verification:** the brief wording table and the options comparison are recorded in [`../planning/context.md`](../planning/context.md#d2-quiz-mode). The choice is labelled there as an assumption, not a brief requirement.

### AI-005: Live quiz state in Redis and virtual rooms (D10)

- **Date / phase:** 2026-09-28 · planning
- **Task type:** architecture
- **My idea:** every time the quiz advances, store the question state with its deadline in Redis, so Redis controls the state and only static questions and answers are stored persistently. That gives "room virtualisation with centralised control", without caring which WebSocket server holds which user's connection.
- **What the AI added:**
  - It confirmed the model: WebSocket servers become stateless, there's no sticky routing, and any instance can serve any quiz.
  - **Gap:** Redis stores a deadline but doesn't act on it. Keyspace expiry notifications are fire-and-forget and can fire late, so they can't drive the quiz.
  - It proposed an ownerless scheduler: every instance polls a sorted set of deadlines, and a Lua script applies each transition with a version check, so exactly one instance wins.
- **Catch:** the AI's earlier plan (task T012) had one instance *own* each quiz's clock, with lease and takeover logic. My Redis-state model made ownership unnecessary, and that earlier design was dropped.
- **Verification:** reflected in NFR-14 (exactly-once transitions, no quiz owner) and NFR-25 (stateless instances). It will be tested by killing an instance mid-quiz and by concurrent transition tests.

### AI-006: Functional and non-functional requirements

- **Date / phase:** 2026-09-28 · requirements
- **Task type:** document drafting, trade-off analysis
- **What the AI produced:** a draft [`requirements.md`](../planning/requirements.md) with 31 FRs, 35 NFRs, assumptions, and six open questions, each with a proposal.
- **What I decided:**

| Topic | AI proposal | My decision |
|---|---|---|
| Scoring | 500 base + up to 500 speed bonus | **100 + speed bonus**, "to make it simple and not overcomplicate" |
| Tie-break | Earlier to reach the score ranks higher | Not needed; the speed bonus separates players |
| Early close | Close when all connected participants have answered | Yes, **with a per-question submission tracker**; announce the result and move the state |
| Start with no participants | Error | Agreed: create room → wait for participants → start |
| Final leaderboard | Read-only until Redis data expires | **Persist it** |
| Scale targets | Fleet-wide numbers (quizzes, total connections) | **Refocused on concurrency inside one room** and efficient connection tracking. Data volume is not the constraint |
| Stale / reconnecting client | Resync with snapshot or disconnect | **Resume at the room's current point; missed questions score 0** |

- **Catches:**
  - On ties: dropping the tie-break still leaves ties possible (equal rounded bonuses, everyone on 0). The AI proposed that equal scores share a rank (1, 2, 2, 4), which keeps ranking deterministic without an extra rule. I accepted.
  - On scale: once I made room concurrency the focus, the AI flagged that its own draft (each leaderboard update carrying the recipient's personal rank) would cost 10,000 Redis lookups and 10,000 separately encoded messages per update in a 10,000-person room. We changed FR-26: live updates are the same bytes for everyone, and personal rank is sent once per question close.
- **Verification:**
  - Every FR/NFR traces back to the brief or is marked as our proposal.
  - Performance and scale requirements are numeric, so the load test can check them.
  - I reviewed the final document before committing it (commit `7febe40`).

### AI-007: Where answers are stored (gap found by me)

- **Date / phase:** 2026-09-28 · requirements (after sign-off)
- **Task type:** requirements gap, data design
- **What I raised:** the committed requirements never said where each participant's answer to each question is stored. Did we only keep the leaderboard? I proposed two paths: a **live** path (shared Redis leaderboard, only caring about points gained per question) and an **audit** path (async write to a persistent database with each answer, points per question, and final total).
- **What the AI added:**
  - **Correction:** the live path can't store only scores. Dedup (FR-18), idempotent retries (FR-30), and the early-close tracker (FR-5) all need the per-question answer record in Redis, written in the same atomic step as the score.
  - **Options for the async write:** flush each question as one batch when it closes (recommended: answers are immutable after close, and exactly one instance wins the close); a Redis Stream + worker (noted as the future scaling step); Kafka (rejected as overkill).
  - **A reconciliation check:** at finish, recompute totals from the stored answers and compare them with the leaderboard (FR-35).
- **My refinement:** a question's answer records should be removed from Redis **only once they are persisted**, instead of keeping everything for the whole quiz. My reason: a large room would otherwise put pressure on Redis memory.
- **AI's check of my refinement:**
  - It agreed, and made "durably acknowledged" the condition: an in-process queue doesn't count, because it dies with the instance.
  - It quantified the memory. Roughly 1 MB of answer records per question for a 10,000-person room, so about 20 MB for a 20-question quiz if nothing is released. One room alone is manageable. The real gain is that per-room memory stays constant regardless of quiz length, which matters with many large rooms at once.
  - Trade-off accepted: once a question's answers are released, a late retry for that question gets `question_closed` instead of the original result. The snapshot total is authoritative (FR-18).
- **My further refinement:** the only data that should live in Redis for the whole quiz is the leaderboard. The AI pointed out that the room's control record (~200 bytes: status, current question, deadline, version) and the participant roster also have to persist, because together they *are* the virtual room. It agreed that everything question-scoped should be released per question. It also found one more thing to take out of Redis: question content and answer keys are immutable, so each instance can cache them in memory instead of copying them into Redis.
- **Outcome:** FR-32 to FR-35, NFR-13a, a rewritten NFR-18, a new metric in NFR-27, and the data lifecycle table in [`../planning/context.md`](../planning/context.md#d10-quiz-state-and-room-virtualisation).
- **Verification (planned):**
  - Integration test: kill the instance between the database commit and the Redis delete, then check that the retry is idempotent and nothing is lost.
  - The reconciliation metric must read 0 after every load test.

### AI-008: System design draft, architecture and data flow

- **Date / phase:** 2026-09-28 · system design
- **Task type:** architecture drafting, diagrams
- **What the AI produced:** [`architecture.md`](../system-design/architecture.md) and [`data-flow.md`](../system-design/data-flow.md), with 10 Mermaid diagrams, plus seven design choices the flows needed that we hadn't discussed.
- **My decisions on those choices:**

| AI proposal | My decision |
|---|---|
| Redis `TIME` as the only clock | Accepted |
| Correctness checked by the server against an in-memory answer-key cache | Accepted, with the condition that the cache is **partitioned well**, since one server handles many quizzes. Result: keyed by question set, immutable entries, single-flight loading, warmed at join |
| Early close moves the deadline, so there is one close path | Accepted, **provided there is no time leak** (no submitting after a question closes). Result: the answer script checks the deadline itself, so a late close by the scheduler can't let late answers in |
| Leaderboard ticks claimed with `SPOP` by any instance | Accepted. I noted the leaderboard is owned by the quiz, not a server, and asked for care with **read + write locking**. Result: a concurrency section. Redis runs scripts one at a time, there is no read-modify-write in Go, and in-process state is sharded by room with no lock held while writing to sockets |
| Flush jobs with retry | Accepted, and **I split the backend into REST API, WebSocket gateway, and worker** from one codebase. My reasons: scale only the service under pressure (gateways for big rooms, workers for many rooms), and decoupled services are easier to scale and change |
| Presence expiring after 30 s | Accepted at current scale |
| Single Redis primary + replica | Accepted, on condition that we **document the concurrency ceiling** we expect, and run load and simulation tests if time allows |

- **Catches:**
  - **The split broke the database mock (found by the AI).** An in-memory repository works inside one process, but with three processes the worker's saved results would be invisible to the API. The AI laid out three options and recommended a real PostgreSQL. **I chose real PostgreSQL in Docker (D12)**, because it also makes the cache warm-up and delay-free answer validation easy to demonstrate, and multiple seeded question sets easy to manage.
  - **One diagram misrepresented the design (found by the AI).** After the split, the first rendering of the services diagram routed the storage edges so the API appeared to connect to the gateway, and the gateway to call the worker. The design says the services never call each other. Storage edges were removed from that diagram, and the rule is stated in text.
  - **One code path for the start (AI).** The first draft had the start command open the first question itself. Moving it to the API meant it could have become a second transition path. Instead the API only schedules the transition, and a worker applies it like any other.
- **Verification:**
  - Every Mermaid diagram was rendered with `mermaid-cli` (all 10 parse).
  - The busiest diagrams were checked visually as images. That check found both the clutter in the system diagram and the misleading edges above.
  - Design choices will be checked in implementation by: a late-answer test (submit after the deadline while the worker is paused), concurrent transition tests across two workers, and the planned load test for the Redis ceiling.

### AI-009: Technology choices and non-functional design

- **Date / phase:** 2026-09-28 · system design
- **Task type:** technology evaluation, capacity estimation, failure analysis
- **What the AI produced:** [`tech-choices.md`](../system-design/tech-choices.md) (choice, reason tied to requirement IDs, and rejected alternatives for each layer) and [`non-functional.md`](../system-design/non-functional.md) (capacity estimates, the concurrency ceiling, latency budgets, failure modes, metrics and alerts, security, trade-offs).
- **My decisions:**
  - gorilla/websocket accepted, for encode-once prepared messages.
  - Redis pub/sub accepted over Streams, since versioned snapshots make replay unnecessary.
  - **Go: my reason is that I know it best**, so I can review and advise on the implementation responsibly. The AI's draft had only technical reasons; mine is now listed first.
  - pgx accepted, noting it also gives control over transactions if later writes need them.
- **What the AI's analysis showed:**
  - **A single room is limited by gateway egress, not Redis.** Answer scripts for a 10,000-person burst use about 10% of one Redis core. Leaderboard updates cost about 400 Mbit/s across gateways during the burst. So the first limit for one big room is the gateways, and the mitigations (skip unchanged ticks, adaptive interval, binary encoding) target that.
  - **The ceiling for one Redis primary**, which I asked to be stated explicitly: about 40,000 answers per second across the system, which is about 80,000 participants answering in the same 2 s, or about 800,000 concurrent participants when rooms are staggered.
  - **Redis replication is asynchronous**, which conflicts with NFR-13 ("ack means recorded"). The AI recommended `WAIT 1 50` after each answer script, acking anyway but counting it if the replica is down, and was explicit that this narrows the loss window rather than guaranteeing zero loss.
- **Catches:**
  - The AI had planned separate ADR files. On review they would have duplicated the decision records already in `context.md` (D1–D12, each with options and rationale). The system design links to those instead.
  - A limitation found during the failure analysis: while PostgreSQL is down, a gateway that has never cached a question set can't accept joins for it. This is documented rather than hidden.
- **Verification:**
  - **The capacity numbers are estimates** from typical per-operation costs. They are labelled that way in the document, and the load test and room simulator (task-28, task-29) must replace them with measured values.
  - Library capabilities the design relies on (gorilla's `PreparedMessage` and write buffer pool, go-redis script handling) are stated from the AI's knowledge and **must be confirmed against the library docs** when implementation starts.
  - I reviewed both documents before closing the design phase.
- **Follow-up decisions:**
  - **Replica durability:** ship the simple `WAIT 1 50` version for now. Later, work out something more concrete: an async retry with backoff, or research into other Redis behaviour that guarantees replica writes better. The AI added starting points: `min-replicas-to-write`, `WAITAOF`, and durable Redis-compatible services, each with what needs checking.
  - **PostgreSQL down at join:** I asked for a careful retry with backoff on question-set loads. It is now capped exponential backoff with jitter, shared across concurrent joins through single-flight, and ends in a retryable error to the client.

### AI-010: Contracts and test strategy

- **Date / phase:** 2026-09-28 · pre-TRD
- **Task type:** engineering process, test design
- **What I asked:**
  1. An OpenAPI spec for the REST API and documented contracts for every payload, so integration is seamless.
  2. Unit and integration tests with gomock, to test edge cases in connection handling, reliability, and availability.
  3. TDD by default for critical services and core business logic, so tests can later be extended into the load test and Go simulator.
- **What the AI added:**
  - **Contracts (D13):** OpenAPI 3.1 alone doesn't describe WebSocket messages, so it proposed **AsyncAPI 3.0** for the WebSocket protocol and **JSON Schema** for every payload, spec-first. Go server types are generated with `oapi-codegen` and TypeScript types with `openapi-typescript`, with contract tests validating real responses and messages. This also settled an open TRD question: Go and TypeScript types are generated from the contracts, not hand-written twice.
  - **gomock (D14):** it recommended `go.uber.org/mock`, the maintained fork of `golang/mock`. It pointed out the limit of mocks: a mocked `RecordAnswer` can't find a race inside the Lua script. So mocks cover *our handling* of failures, and every repository also gets integration tests on real Redis and PostgreSQL. It added **Toxiproxy** to those tests so connection resets, latency, and outages are real network faults, not simulated ones.
  - **Simplification:** with gomock covering unit tests, the hand-written in-memory repositories from D9 are no longer needed. They were dropped, leaving one real implementation per interface.
  - **TDD scope:** a table of what is test-first (scoring, state machine, answer recording, transitions and claims, leaderboard, flush pipeline, connection registry and fan-out) and what isn't (wiring, routing, config).
  - **Test kit:** a shared protocol client plus scenario builders, used by integration and end-to-end tests and reused by the Go simulator at 10,000 participants. Load scenarios are then the same scenarios the tests already verify.
- **Why this matters for verification:** for TDD-scope code, I review the failing tests (written from requirement IDs) before any implementation is generated. The tests become the acceptance criteria for AI-generated code.
- **Catch (the AI's own error):** while reordering the decisions table, a `sed` command deleted the D12 row instead of moving it. The follow-up `grep` check showed D12 missing, and the row was restored from the exact text recorded earlier. It's a small example of why every edit gets checked.
- **Verification:** the decisions are recorded in `context.md` (D13, D14) and reflected in `tech-choices.md`, `non-functional.md`, and `architecture.md`. The contracts and tests themselves are the next deliverables.

### AI-011: TRD sections 1–3 (layout, configuration, domain model)

- **Date / phase:** 2026-09-28 · TRD
- **Task type:** technical specification
- **My decisions going in:**
  - **WebSocket auth:** token in the query string (D15).
  - **Quiz code:** system-generated, 6-character random alphanumeric with collision retry (D16).
- **What the AI produced:** [`trd.md`](../planning/trd.md) §1–3: repository and Go package layout with dependency rules, every configuration variable with its default, and the domain model (identifiers, question sets, room state machine, scoring, validation, events).
- **Issues the AI found while writing, for my review:**
  - **Quiz codes must be unique permanently, not just while live.** Finished quizzes stay viewable by code (FR-13), so a Redis `NX` check alone would allow a code to be reused after its live data is released. The code is reserved with a unique constraint in PostgreSQL instead.
  - **Early close must not change points.** If early close moved the single deadline, the speed bonus and audit data could silently use the shortened value. It split `Deadline` (used for scoring) from `CloseAt` (used for acceptance). The data-flow doc and Redis data model were updated to match.
  - **Scoring and transitions exist in both Go and Lua**, so they could drift apart. Shared test-vector files run against both the Go functions (unit tests) and the Lua scripts (integration tests), with integer-millisecond arithmetic so rounding can't differ.
  - **Leaking the answer key is prevented by a separate type**, not by remembering to omit a field: only `PublicQuestion` (no correct option) can reach the protocol layer.
  - **The lobby has two due events** (start, expiry). A `StartRequested` flag tells the worker which one applies.
- **Verification:** the two changed data-flow diagrams re-rendered with `mermaid-cli`. The quiz-code space (31⁶ ≈ 887 million) and collision rate (≈ 0.1% at a million stored quizzes) were computed. Section status is "draft for review" until I sign off.

### AI-012: TRD sections 4–5 (Redis scripts, PostgreSQL schema)

- **Date / phase:** 2026-09-28 · TRD
- **Task type:** technical specification
- **My decision going in:** no per-option counts in the reveal for now; `question_closed` carries only the correct option.
- **What the AI produced:** [`trd.md`](../planning/trd.md) §4 (exact Redis keys, internal event format, nine Lua scripts with inputs, steps, and returns, the flush and finalise jobs) and §5 (PostgreSQL schema, quiz creation transaction, batch insert, reconciliation query, migrations, seed sets).
- **Gaps the AI found while specifying, and design changes that followed:**
  - **A quiz could advance without anyone being told.** In the approved design, the worker ran the transition script and *then* published. A crash between the two would change the state with no message sent. Fix: every script publishes its own room event in the same atomic step. Gateways build the client message from their question cache, so workers no longer need question content at all. Flows 2, 4, 5, and 6, the architecture text, and the Redis data model were updated, and all 10 diagrams re-rendered.
  - **`WAIT` would silently confirm nothing** if sent as a separate call through a connection pool, because it only counts writes on its own connection. It must be pipelined with the answer script.
  - **Scripts must not build key names** (a Redis Cluster rule). Keys that depend on state, like the current question's answers, are computed by the caller from state it read, and the script rejects the call if that state has changed (question ID check, state version check). Answer keys are named by question ID for this reason.
  - **Removing presence on disconnect would be a bug.** A participant who moved to another gateway could be marked offline by the old one, triggering an early close too soon. Presence only expires.
  - **A duplicate flush acknowledgement could decrement the pending counter twice.** The decrement only happens if the job was actually removed.
  - **The finalise job must wait for every per-question flush.** A `pending_flush` counter in the room makes it re-queue itself until the answers are all saved.
- **Verification:** all diagrams re-rendered with `mermaid-cli` after the change. The scripts and SQL are specifications at this point. They are TDD scope (D14), so their tests come first in implementation, driven by the shared test vectors (§3.5).

### AI-013: Contracts and gateway/worker internals (TRD §6–8)

- **Date / phase:** 2026-09-28 · TRD
- **Task type:** contract authoring, technical specification
- **What the AI produced:**
  - The real contract files: [`openapi.yaml`](../api/openapi.yaml) (7 REST operations, RFC 9457 errors), [`asyncapi.yaml`](../api/asyncapi.yaml) (WebSocket protocol, envelope, versioning, close codes), and 15 JSON Schema files, each message with an example.
  - TRD §6 (contracts), §7 (gateway internals: connection lifecycle, goroutines, registry, events to messages, slow clients, presence, question cache, shutdown), and §8 (worker loops, idempotency, health, shutdown).
- **Catches from checking tools against their docs, not memory:**
  - **D13 said JSON Schema 2020-12, but AsyncAPI 3.0 only accepts draft-07** (checked in the AsyncAPI 3.0.0 spec). WebSocket schemas were written in draft-07 and D13 was corrected.
  - The AI checked the current oapi-codegen README before relying on OpenAPI 3.1. Support exists but is described as initial, so the spec avoids multi-type unions.
  - **A snapshot/event ordering race:** the gateway subscribes a connection *before* reading its snapshot (otherwise events in between would be lost), so an event can arrive before an older snapshot. Resolved by making clients apply snapshots monotonically by version too, and written into the protocol's client rules.
  - A generator script stamped the AsyncAPI file with a header claiming it was "generated from the TRD", which wasn't true since the file is maintained by hand. The AI noticed and removed it.
- **Verification (actually run):**
  - All 15 schemas are valid draft-07, and all 14 message examples validate.
  - Six **negative cases are rejected by the contract itself**: a question carrying `correctOptionId` (FR-21), a quiz code with a look-alike `0` (D16), a 21-character name (FR-15), an answer without a request ID (FR-18), 201 points (FR-19), protocol version 2.
  - `redocly lint openapi.yaml`: valid, 2 expected warnings (health probes have no 4xx).
  - `asyncapi validate asyncapi.yaml`: valid, 0 errors, 0 warnings.
  - Example arithmetic cross-checked: an answer 2.1 s into a 15 s window scores 100 + ⌊100 × 12,900 / 15,000⌋ = 186, as the example shows.
- **Note:** the AsyncAPI CLI sends anonymous usage analytics by default. It ran once with that default; it can be disabled with `asyncapi config analytics --disable`.

### AI-014: Errors and retries, test plan, local stack (TRD §9–11)

- **Date / phase:** 2026-09-29 · TRD
- **Task type:** technical specification, test design, developer experience
- **What I asked:** continue the TRD. The local stack must be **easy to start and set up, and easy to control for simulation**.
- **What the AI produced:**
  - **§9, errors:** three kinds of failure, a timeout for every call, a retry policy, degraded modes, the client reconnect policy, and a table mapping every cause to its WebSocket and HTTP error.
  - **§10, tests:** the TDD workflow with my test review before implementation, the test levels with commands, the test kit API, a requirement-to-test table, concurrency tests, gomock and Toxiproxy fault scenarios, and load scenarios whose pass criteria are the NFRs.
  - **§11, local stack**, shaped by my requirement: `make up` / `make scale` / `make sim`, optional Compose profiles (observability, Redis replica, Toxiproxy chaos), simulation scenarios as YAML with command-line overrides and timed chaos steps, `make chaos-*` switches, and `make demo` for the video.
- **Design points worth noting:**
  - **The server never retries an answer script.** If it timed out, nobody knows whether it applied. The client resends with the same request ID, and deduplication returns the original result. A server-side retry would hide that ambiguity.
  - **Simulations check correctness, not just speed.** The test kit recomputes every expected score from what clients sent and the server's receive times, and compares it with the server's totals.
- **Catches:**
  - The AI's first draft of the requirement-to-test table mapped FR-16 (submit an answer) to the quiz-creation tests. Found on re-read and fixed.
  - A script then checked that **all 33 must-have FRs** appear in the table. None are missing.
  - The nginx behaviour for picking up scaled instances (re-resolving service names) is marked "to be confirmed when the config is written" rather than asserted.
- **Verification:** links checked; the FR coverage check above; the port-exhaustion figure (≈ 28,000 connections per source address) matches the default Linux ephemeral port range (32768–60999).

### AI-015: Task catalogue from the TRD

- **Date / phase:** 2026-09-29 · planning
- **Task type:** implementation planning
- **What I asked:** update the task catalogue so it implements what we planned in the requirements and the TRD, and delete the old pre-TRD task files.
- **What the AI produced:** [`tasks.md`](../planning/tasks.md) with 32 tasks in 7 phases. Each has the TRD sections and requirement IDs it implements, its dependencies, a size, its TDD status with "tests first" listed, and "done when" criteria. The old `tasks/T001–T020` files were deleted.
- **How the catalogue encodes our process:**
  - TDD tasks list their tests first, and the working steps require my review of those tests before any implementation (D14).
  - The shared Go/Lua test vectors link domain tasks to script tasks: T07 → T12 and T08 → T13 run the same files.
  - The load-run task must replace the capacity **estimates** in `non-functional.md` with measured values.
- **Verification (script-checked):**
  - The dependency graph is **generated from each task's "Depends on" line**, not drawn by hand, so the diagram and the text can't disagree. The first hand-drawn version was missing several edges (e.g. T02 → T15, T13 → T19), which is why it was replaced.
  - The graph is acyclic (32 tasks).
  - Every must-have FR is implemented by at least one task.
  - Every TRD section 1–11 is covered.
  - The graph renders with `mermaid-cli`.

### AI-016: One file per task, grouped by phase

- **Date / phase:** 2026-09-29 · planning
- **Task type:** planning structure
- **What I asked:** make the tasks more granular by cataloguing them in folders, with one entry per task (`task-01` … `task-n`).
- **What the AI produced:**
  - `docs/planning/tasks/<phase>/task-NN-<slug>.md` for all 32 tasks. Each file has: phase, size, TDD, status, implements, depends on / unblocks (linked), tests first, **granular steps** (4–8 checkable steps each), done when, and sections for notes, verification, and AI collaboration.
  - `tasks.md` is now the index: working rules, the dependency graph, the board, the folder list.
- **How it was done:** the files were generated from the agreed catalogue by a script, so no agreed content was retyped by hand. The steps were written per task. Where a step touches something risky, it says so explicitly: seed vocabulary reviewed by hand, the nginx re-resolution confirmed rather than assumed, `WAIT` verified with a real replica.
- **Catch:** the first generated pass carried old `T07`-style IDs inside copied text (e.g. "the same vectors as T07"). A follow-up scan found them, converted every reference to `task-NN`, and confirmed none remain.
- **Verification (script-checked):**
  - all 32 files have the required sections;
  - the dependency graph in `tasks.md` matches the files' "Depends on" links exactly, and is acyclic;
  - every must-have FR and every TRD section 1–11 is covered;
  - no broken links; the graph renders.

---

## Implementation phase

### AI-017: Foundation (task-01, task-03, task-04) and first tests for review (task-02, task-05)

- **Date / phase:** 2026-09-29 · P0 foundation
- **Task type:** scaffolding, infrastructure, test authoring
- **What I asked:** "Let's do foundational tasks and scaffold!" Decisions I made along the way:
  - install Go with `dnf` (Go 1.26) rather than a user-level install or a Docker-only toolchain;
  - approve the seed vocabulary after reviewing it.
- **What the AI produced:**
  - **task-01:** Go module and package tree, React client scaffold, Makefile, lint config, `.env.example`, tools module.
  - **task-03:** Compose data stores.
  - **task-04:** migrations, 23 seed questions, `cmd/migrate`, shared Dockerfile.
  - **Tests only, no implementation yet:** task-02 (config, backoff, Redis keys) and task-05 (contract tests).
- **Catches and adjustments:**
  - Port 6379 was already in use on my machine, so host ports default to 16379/15432 in `.env`.
  - Fedora's Go sets `GOTOOLCHAIN=local`, so every pinned tool was checked to build with Go 1.26 before relying on it.
  - The Vite template now ships oxlint instead of ESLint; kept and recorded.
  - golangci-lint flagged an unchecked `db.Close()` error in `cmd/migrate`; handled rather than suppressed.
  - `docker compose up --wait` fails when a one-shot container exits, even successfully. Migrate moved to a `tools` profile and runs explicitly after the stores are healthy.
  - goose's Provider API (`WithTableName`) and the JSON Schema library's loader were **read from their source** before use, not assumed.
  - **An ambiguous verification was redone.** The first migration up/down check printed "tables left: 0" without goose's own output, which would also be true if nothing had run. It was redone with every step visible. A second attempt then failed on zsh quoting, and its empty numbers were not taken as a result.
- **Verification:**
  - `make check` green: vet, golangci-lint 0 issues, `go test -race`, oxlint, tsc, Vitest.
  - Stores healthy; persistence across `down`/`up`; wiped by `reset`.
  - Migrations idempotent; Up/Down proven on a scratch database (6 tables → 23 questions → 0 → 0 → 6).
  - Contract tests pass. **Mutation check:** temporarily allowing extra fields on `PublicQuestion` made the FR-21 test fail, and restoring it made it pass, so the test can actually detect a leak.
  - task-02 tests compile-fail against the not-yet-written API: the expected red state, **pending my review**.

### AI-018: Platform packages and code generation (task-02, task-05)

- **Date / phase:** 2026-09-29 · P0 foundation
- **Task type:** TDD implementation, code generation, tooling
- **My test review (before any implementation):** I approved the tests with changes, to avoid over-engineering:
  - keep the signing-key length check;
  - **drop the production-only dev-token rule** ("this is just an assignment, so no real production");
  - **make the leaderboard a fixed top 10** (the setting was removed everywhere);
  - keep the timeout ordering rules.
- **What the AI produced:** `platform/retry`, `config`, `redisx` (keys, client, script loader), `logging`, `postgres`, `metrics`; oapi-codegen setup; a `tools/contracts/` package with the TypeScript generators and spec validation; Makefile targets `generate`, `check-generated`, `lint-contracts`.
- **Catches:**
  - **The AI weakened its own test and then fixed it.** The key-name test was a map keyed by the actual names, so two functions returning the same name would overwrite each other's case unnoticed. Found on re-read, changed to ordered pairs.
  - **A dependency conflict was not forced.** `openapi-typescript` requires TypeScript 5, the client uses 6. Rather than `--legacy-peer-deps` (risky: the generator drives the TypeScript compiler API), the generators moved to their own package.
  - **Generated type names collided:** `interface Error` shadowed the built-in, and `Leaderboard1` / `QuizState1` appeared. Fixed at the source by titling message schemas `…Message`.
  - Lint found 8 issues after implementation. All were fixed, not suppressed, apart from one scoped exclusion (gosec in tests reading fixtures).
  - The AsyncAPI CLI's analytics (noted in AI-013) were avoided by validating with the parser library directly.
- **Verification:**
  - All task-02 tests pass under `-race`.
  - **Mutation checks on the new safety nets:** `check-generated` flagged exactly the two affected files after a spec change and failed, then passed after restore. (The FR-21 contract-test mutation check is in AI-017.)
  - `make check` green: vet, golangci-lint 0 issues, Go tests, oxlint, tsc, Vitest, OpenAPI lint, AsyncAPI validation, generated-code freshness.

### AI-019: Splitting the foundation work into atomic commits

- **Date / phase:** 2026-09-29 · P0 foundation
- **Task type:** version control
- **What I asked:** make atomic commits for the foundation work.
- **What the AI did:**
  - Six commits, one per task plus one for docs. `Makefile`, `docker-compose.yml`, and `go.mod`/`go.sum` changed across tasks, so their **intermediate versions were reconstructed**, and each commit's `go.mod`/`go.sum` was computed with `go mod tidy` in a throwaway worktree. That way every commit contains only the dependencies its own code uses.
  - No AI trailers in any commit, per my standing rule.
- **A false alarm, handled carefully:**
  - A quick one-liner counting requirements in each commit's `go.mod` reported 0 for the migrations commit, which would have meant a commit that doesn't build.
  - Rather than rewrite history on that signal, the AI inspected the commit directly. The `go.mod` was correct; the count was wrong, most likely because `grep` on this machine resolves to ugrep and treated the pattern differently.
  - It then replaced the one-liner with the check that actually matters (next bullet).
- **Verification:**
  - Each code commit was checked out in its own worktree: build, vet, golangci-lint, `go test -race`, and `go mod tidy -diff` all pass for every commit.
  - The final working tree is byte-identical to the pre-commit state (`Makefile`, Compose file, `go.mod`, `go.sum`).
  - `make check` passes on a fresh checkout of `HEAD`, with dependencies installed from scratch. This closes task-01's clean-clone criterion.
