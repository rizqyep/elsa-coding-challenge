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

### AI-020: Domain core (task-06, task-07, task-08)

- **Date / phase:** 2026-09-29 · P1 domain
- **Task type:** TDD implementation
- **What I asked, and my decisions:**
  - Start the domain tasks. **I chose to review tests and code together after implementation** for this batch, instead of reviewing the tests first (a deliberate relaxation of the D14 gate).
  - Mid-way: "keep only necessary inline comments on the code; others should refer to memory entries or docs entries".
- **How the AI kept TDD honest without my up-front review:**
  - For each task, tests were written first and confirmed failing (red), then implemented to green.
  - **Test vectors were checked independently before any Go code existed:** the 14 scoring vectors were recomputed from the formula in Python (0 mismatches); the 14 transition vectors were compared with a separate Python model of the TRD §3.3 table (0 mismatches). A wrong expected value would otherwise have been "fixed" by bending the implementation to match.
  - **Every key rule got a mutation check:** rejection sampling disabled → bias and skip tests fail; rounding instead of flooring → 3 vectors fail; early close moving `Deadline` → `TestEarlyClose` fails; reveal timed from `CloseAt` → the "late close" vector fails. All restored and green.
- **Catches:**
  - The first bias test depended on which partial code was discarded at the end; redesigned (six full rounds → exactly 48 of each symbol) before implementing.
  - One mutation (limit set to 256) failed to *compile*, because 256 doesn't fit in a byte. It was discarded as proving nothing and replaced with a real behavioural mutation.
  - The strict vector decoder (unknown fields rejected, so a renamed field can't be silently ignored) caught an undeclared top-level `description`. The field was declared and the strictness kept.
  - A pointless test helper that only wrapped `bytes.NewReader` was removed.
- **Comment cleanup (my request):** explanatory comments were cut to one-liners pointing to the TRD or the vector files. The rule is now in TRD §1.4 and saved as a standing preference.
- **Deviation noted for review:** scoring uses integer milliseconds (`int64`) rather than the `time.Time` sketched in TRD §3.4, so Go and Lua do identical arithmetic on identical inputs.
- **Verification:** `make check` green after each task commit and after the comment cleanup.
- **My review:** approved all three tasks. I agreed with the three state-machine rules the vectors made explicit (start wins over lobby expiry; full reveal after a late close; full answer window for a late-opened question) and judged the scoring solid.

### AI-021: Protocol package (task-09)

- **Date / phase:** 2026-09-29 · P1 domain
- **Task type:** TDD implementation
- **What I asked:** continue with task-09 and commit atomically as usual; I had verified the previous code.
- **Design decision proposed by the AI, applied, and recorded for my review:**
  - Validate client frames against the JSON Schemas **at runtime** instead of hand-written Go checks, so the gateway and the contract can't drift. The cost is small at our client message rates.
  - Consequence: the server is strict about unknown fields in client frames, and the forward-compatibility rule ("ignore unknown fields") applies to clients reading server messages. This replaced the task's original "unknown fields ignored on decode" item.
- **Catches:**
  - The AI's first test draft named error-code constants `Err…`, which in Go signals an `error` value. Renamed to `Code…` before implementing.
  - It removed a pointless decode in one test.
- **Verification:**
  - Tests written first and confirmed red.
  - All passed on the first run, so **four mutation checks** confirmed each safety net can fail: a `quiz.Question` reference in the protocol package; `top: null` for empty lists; skipping schema validation; a dropped error code. All four were caught by their intended tests, and all files were verified restored byte-for-byte.
  - `make check` green.

### AI-022: Integration harness and the first Redis scripts (task-10, task-11)

- **Date / phase:** 2026-09-29 · P2 Redis scripts
- **My input:**
  - Agreed with strict validation on both sides (runtime schema validation, strict inbound / lenient outbound). My reason: it's worse if a message gets dropped or silently ignored because of a schema mismatch; validation everywhere makes problems traceable.
  - "Let's go with P2".
- **What the AI produced:**
  - `testenv`: real Redis, PostgreSQL (migrated and seeded), and Toxiproxy per test package.
  - `migrations.Apply`, shared by `cmd/migrate` and the tests.
  - The `create_room`, `start`, and `join` Lua scripts with Go repositories, `quiz.RoomRecord`, and `leaderboard.Entry`.
- **Catches:**
  - **The AI broke one of its own test assertions and fixed it before running.** A schedule check in the start test zeroed a value before using it, so it could never fail meaningfully. Rewritten to capture the value first.
  - **The AI caused a 10-minute hang.** Its concurrent-join test sent from 1,020 goroutines into a channel buffered for 1,000, and was only drained after `wg.Wait()`, which never returned. The AI read the partial output, found the cause, and fixed it (0.14 s now). It confirmed the killed run's containers were reaped, and lowered the integration timeout to 5 minutes so any future hang surfaces faster.
  - **Parity with approved Go code over the TRD sketch:** TRD §4.4 had `start` check "not in lobby" before "not host", while the approved `quiz.Start` checks the host first. The script follows the Go rules, a test runs identical cases through both, and the TRD was corrected.
  - A mutation first aimed at the wrong target proved nothing; the AI kept every mutation behavioural and compiling.
- **Verification:**
  - **Toxiproxy smoke test:** latency and outages verifiably reach the client. Mutation: latency on the wrong proxy made the test fail.
  - **Script loader:** `EVALSHA` fails after `SCRIPT FLUSH`, which is why scripts must be loaded at startup for pipelines.
  - Seed checks ported from task-04.
  - **Lua mutations:** `ZADD` without `NX` → a rejoin reset the score, caught; swapped `start` checks → parity test fails.
  - `make test-integration` and `make check` green.

### AI-023: The answer script (task-12)

- **Date / phase:** 2026-09-29 · P2 Redis scripts
- **Task type:** TDD implementation, the scoring core
- **Design problem and choice:** the answer script takes its time from Redis `TIME`, so the shared scoring vectors can't set the receive time.
  - The AI **rejected adding a time-override parameter** to the production script: any caller able to pass one could pick their receive time.
  - Instead, the scoring function lives in its own `points.lua`, prepended to `answer.lua` at load, and tests run that exact function over the vectors.
  - Real answers are also checked against Go's `Points()` using the receive time Redis returned.
- **Catches:**
  - The AI assumed go-redis pipelines have a `Wait` method; the compiler said no. It checked the library source and used the generic `Do("wait", …)` on the pipeline, keeping `EVALSHA` and `WAIT` on one connection.
  - It recorded a limit of its own test honestly: the replica test can't prove `WAIT` shares the script's connection, because an idle connection's `WAIT` also reports the replica. That guarantee rests on the pipelining.
- **Verification:**
  - 10 integration tests on real Redis (plus a replica container) and 5 gomock service tests. The gomock tests assert **exactly one repository call** on failure: no retry, per TRD §9.3.
  - **Mutation checks, all caught:** Lua rounding (vector parity fails); duplicate check removed (**100 accepted answers, score 19,300 instead of 193**, the exact double-scoring bug the design prevents); early close moving the deadline; `WAIT` dropped.
  - `make check` and `make test-integration` green.

### AI-024: Transition and leaderboard scripts (task-13), and a design claim corrected by mutation testing

- **Date / phase:** 2026-09-29 · P2 Redis scripts
- **Task type:** TDD implementation
- **What the AI produced:** `next.lua` (a direct port of `quiz.Next`), `transition.lua`, `top.lua`, `leaderboard.lua`, and the worker-facing repository methods. Tests cover the shared vectors in Lua, every lifecycle step with its side effects, 20 competing workers, and answers racing the close.
- **Catches:**
  - The AI's first draft left out the answers-racing-the-close test the task required. Found on re-read and added.
  - Lint caught a `!=` comparison with an error value in a new test.
- **Mutation testing overturned a documented design claim:**
  - Removing the transition script's expected-version check was **not caught**.
  - The AI didn't weaken or rig the test to make the mutation fail. It analysed why: exactly-once comes from the script deciding and applying atomically (after any transition the next is in the future), not from the version check, which is only an extra guard.
  - The TRD claimed otherwise and was corrected. Whether to keep the check was raised as a design question for me.
- **Mutation coverage mapped instead of assumed:** removing the answer deadline check passed the race test, because that gap is ~1 ms wide, but failed task-12's dedicated late-answer test. The AI confirmed the property is covered and recorded which test covers it.
- **Verification:** Lua `next_state` passes all 14 shared vectors; exactly one of 20 workers applies a transition; the race test's invariants (accepted before close, stored = accepted, leaderboard = accepted points) hold. `make check` and `make test-integration` green.

### AI-025: Removing the version check and tightening the transition tests

- **Date / phase:** 2026-09-29 · P2 Redis scripts
- **My decision:** "unnecessary implementation for now, but let's tighten the test as well, add more edge cases to see how it behaves under certain cases."
- **What the AI did:**
  - Removed the expected-version check from the script, the Go API, and six docs.
  - Replaced the two version-based tests with edge-case tests that pin the real mechanism, the atomic due check, **at every stage** of a quiz, plus seven behaviour cases: repeated applies, schedule consistency, early close, worker downtime, start versus expiry, independent rooms, and concurrent snapshots.
- **Catches:**
  - One new expectation was wrong: at the finish stage the losing workers see `terminal`, not `not_due`. The AI fixed the test after confirming the code's behaviour was the correct one.
  - A zsh glob quirk broke a diagram re-render step; it was rerun under bash, with all 8 diagrams re-rendered.
- **Verification:**
  - All new tests pass.
  - **Removing the due check**, now the only mechanism, is caught. The failure shows the exact bug it prevents: two workers apply back to back, and a question closes and the next opens immediately, **skipping the reveal**.
  - Counting `pending_flush` on every call (1 → 22) and publishing events when nothing is due (20 events instead of 1) are also caught.
  - `make check` and `make test-integration` green.

### AI-026: Persistence jobs (task-14), completing P2

- **Date / phase:** 2026-09-29 · P2 Redis scripts
- **My input:** I reviewed the task-13 code, re-ran the tests to look for flakiness (none found), and asked to continue.
- **What the AI produced:** the `history` module: job parsing, `LiveStore`/`Store` interfaces with Redis and PostgreSQL implementations and gomock mocks, the `claim`/`flush_ack`/`release` scripts, and `Service.Process` for flush and final jobs.
- **Catches:**
  - The AI's first draft of a service test contained a nonsensical rank assertion (a slice compared to an array inside a meaningless boolean). Rewritten before running.
  - It removed a leftover no-op line from a test.
  - **A mutation revealed a test that could hang:** with claimed jobs not hidden, the competing-workers test looped for 190 s until Go's timeout killed it. The AI bounded the loop so the same bug now fails in about 7 s with a clear message, and checked the other polling loops for the same risk.
- **Verification:**
  - Unit tests enforce the order save → acknowledge with `gomock.InOrder`, and assert a failed save is never acknowledged.
  - Integration tests use **real failures**: PostgreSQL cut off through Toxiproxy mid-flush (answers stay in Redis, the retry saves them), and a simulated crash between commit and acknowledgement (no duplicate rows).
  - End-to-end finalisation reconciles with 0 mismatches.
  - Four mutations, all caught: `pending_flush` going negative, acknowledging after a failed save, claims not hidden, finalising before flushes.
  - `make check` and `make test-integration` green.

### AI-027: Auth, REST API, and end-to-end leaderboard tests (task-15, task-16)

- **Date / phase:** 2026-09-29 · P3 Services
- **My input:** I chose to do auth and the REST API before the gateway, so the leaderboard flow could be tested end to end first, and asked for more integration tests afterwards.
- **What the AI produced:**
  - HS256 tokens with the algorithm pinned;
  - the quiz service and PostgreSQL store;
  - paginated leaderboards from Redis or final results;
  - a shared transient-error classifier;
  - the REST handlers behind a contract middleware (route → authenticate → validate against `openapi.yaml`);
  - `cmd/api` wiring;
  - integration tests through that wiring, with Toxiproxy faults.
- **Catches:**
  - **Spec vs implementation:** `openapi.yaml` promised start is idempotent, but the rule returned 409 once question 1 opened, so a host retrying a timed-out start got a false error. Fixed in Go and Lua with shared cases.
  - **The contract tests found spec gaps:** validating every response *including its status* showed the spec omitted 400 on the code and pagination routes and documented no 500 at all.
  - **The fail-closed access table worked on its first run.** The embedded spec names operations `StartQuiz`, not `startQuiz`, so every operation looked unprotected and the API refused to start, instead of silently running without auth.
  - **Error details leaked validator internals.** A throwaway test printed the real 400 bodies, which showed the cleanup never ran because kin-openapi nests the text in `SchemaError.Reason`. Fixed and pinned with a test.
  - **Tie order could differ between live and archived leaderboards** on a database with a locale collation. `COLLATE "C"` fixes it, but the mutation first *survived* because the Alpine test image sorts bytewise. The test now forces an ICU collation, and the mutation is caught.
  - **Suspicions that turned out false, checked rather than assumed:**
    - a PostgreSQL millisecond-rounding risk (a million values round-trip exactly);
    - a "failing" expired-token test (the fixture's expiry was later than the current UTC time);
    - a deterministic failure first taken for a flaky test (pgx's cached statement plan predating the test's `ALTER`).
  - **Three mutants didn't compile**, so they proved nothing; they were redone.
- **Verification:**
  - Tests were written first for each piece.
  - Every handler-test response is validated against the spec.
  - The full flow runs through the real wiring and checks that archived results equal the live ones.
  - 21 mutations were caught.
  - The integration package was run 4 times with no flakes, and a smoke run of the real binary succeeded.
  - Each commit builds and passes unit tests on its own.
  - `make check` and `make test-integration` are green.

### AI-028: Question cache (task-17)

- **Date / phase:** 2026-09-29 · P3 Services
- **My input:** I asked to continue with task-17 after reviewing tasks 15–16.
- **What the AI produced:**
  - the question-set validator and answer-path lookups;
  - the PostgreSQL set loader;
  - the gateway question cache: single-flight loading, transient-only retries within the budget, reference counting, LRU eviction of unreferenced sets.
- **Design points the AI raised:**
  - **A load must not be tied to the first caller's context.** Otherwise one cancelled join fails everyone waiting on the same load.
  - **The load caches the set before returning.** Otherwise a join arriving just after the flight ends starts a second load.
  - **A question with no options must reach validation**, so the loader left-joins options instead of silently dropping such a question.
- **Catches:**
  - **Mutants caught only as hangs.** In the first mutation run, 3 mutants (no single-flight, retrying every error, wrong eviction) didn't fail; they hung until Go's 10-minute timeout. A gomock over-call inside the single-flight goroutine exits that goroutine, so its callers never return. The AI changed every cache-test `Acquire` to a 5 s deadline and made the single-flight test count loads itself; the same mutants now fail in about 5 s. This is the second time this class of weakness has appeared (see AI-026), so polling or waiting tests now get a deadline by default.
  - **Tooling mistakes during the mutation run**, both corrected:
    - A first mutation run silently tested nothing, because zsh doesn't word-split `-run Pattern` held in a variable.
    - An over-broad `pkill -f` killed its own shell, and a later kill loop cut one mutant's test short. That batch was discarded and rerun cleanly, and every file was confirmed restored before anything else changed.
  - **My own test bug:** gomock kept answering the "recovered" call from the unbounded outage expectation. The outage is now a switch.
  - **Test isolation:** the malformed-set test now deletes its rows. Question sets survive `Reset`, and another test expects exactly the seeded three.
- **Verification:**
  - Unit tests with a gomock loader and a fake clock.
  - Integration tests against real PostgreSQL.
  - 7 mutations caught with a 60 s backstop timeout.
  - Repeated and shuffled runs.
  - `make check` and the integration suite are green.

### AI-029: Gateway connections and burst protection (task-18)

- **Date / phase:** 2026-09-29 · P3 Services
- **My input:**
  - I asked whether single-flight was really proven, i.e. that a join burst costs one PostgreSQL read.
  - I asked that task-18 not "fry the WebSocket server on a join burst".
  - Mid-task I added that avoiding the database load isn't enough: the next thing that can bring the system down is a **cache stampede**.
- **What the AI answered on single-flight:** yes, with three caveats I hadn't made explicit:
  - The proof is a unit test with a mocked loader (1,000 concurrent joins → 1 load; the no-single-flight mutant was caught). A real burst over sockets is proven in task-29.
  - It is one read **per gateway**, not per system.
  - It only covers question content. Each join still runs one Redis `join` script, and that is the burst cost task-18 has to bound.
- **What the AI produced:**
  - A list of every shared dependency a burst could pile onto, and a guard for each (TRD §7.10).
  - The gateway's connection layer: checks before the upgrade (origin, token, the new `WS_MAX_CONNECTIONS` cap, the admission bucket, jittered `Retry-After`); a read loop on the handler goroutine and a single writer; a non-blocking send queue; the slow-client policy; rate limiting; heartbeats; close handling.
- **Stampedes the AI found beyond the question cache**, which follow from my stampede point:
  - **Resync stampede.** When Redis stalls, every connection's queue overflows at once, and each resync reads a snapshot from Redis, exactly when Redis is weakest. Fixed with 32 gateway-wide resync slots plus 0–250 ms jitter. The TRD had no guard for this.
  - **Subscriber-reconnect stampede.** TRD §9.3 pushes a snapshot to every local connection after a pub/sub reconnect. Carried into task-19 as a requirement to coalesce snapshot reads per room.
  - **Rejected clients retrying together.** `Retry-After` is jittered.
- **Other catches:**
  - **Docs drift:** the AsyncAPI spec said a bad origin closes with 1008 and admission closes with 1013, while the TRD rejects both before the upgrade (cheaper under a burst). The spec was corrected.
  - **Browsers can't read a failed handshake's HTTP status**; every rejection looks like close 1006. So `Retry-After` only helps non-browser clients, and the React client must refresh its token before expiry instead of waiting for a 401 (TRD §9.5).
  - **Close must not block:** gorilla's `WriteControl` waits for the write lock, which a stalled writer holds. The AI made the fake socket behave the same way so the test would catch a blocking close.
- **What the AI got wrong:**
  - **The deadline rule from AI-028 wasn't applied.** Two new tests (a blocking send, a synchronous close) caught their mutants only as 60 s hangs. Both now run the risky call under a 2 s deadline and fail in about 2 s.
  - **A racy test:** one test sent a frame before the resync had run, while the connection was correctly dropping frames. The code was right; the test now waits for the resync.
  - **Two invalid mutants:** one still took a resync slot, so it stayed bounded ("missed" for the wrong reason); one didn't compile. Both were redone and caught.
  - The linter flagged unclosed handshake response bodies in the tests; the dial helper now closes them.
- **Verification:**
  - Tests were written first and confirmed failing.
  - Real-socket tests and deterministic fake-socket tests all pass under `-race`, 3 runs.
  - 14 mutations caught by test failures in 1–7 s.
  - An in-process measurement: 2 goroutines and at most 33.5 KB per idle connection, server and client together, against the 50 KB budget.
  - `make check` is green.

### AI-030: Gateway registry, room events, and the subscriber (task-19)

- **Date / phase:** 2026-09-29 · P3 Services
- **My input:** I asked to continue with task-19 after reviewing task-18, carrying over my stampede concern: a pub/sub reconnect must not turn into one Redis read per connection.
- **What the AI produced:**
  - the sharded connection registry and the hub that maps room events to client messages;
  - one prepared frame per broadcast, and ranks after each close from one read;
  - per-room snapshot pushes after a reconnect;
  - a Redis subscriber with its own receive loop;
  - two read-only Redis scripts (`view`, `standings`);
  - a server-side schema validator used by every test that receives frames.
- **What the AI found by reading the library instead of assuming:** go-redis's `Channel()` reconnects and resubscribes silently, hides subscription confirmations, and drops messages if its buffer stays full for a minute. Using it would have left rooms stuck on stale state with nothing triggering a resync. The subscriber therefore owns the connection: it waits for Redis to *confirm* each subscription before a join reads its snapshot, pings quiet connections, and signals each room once its resubscription is confirmed.
- **Bugs found in earlier, already-committed work (each fixed in its own commit):**
  - **Early close was invisible to clients.** `answer.lua` moved `close_at` but republished the current state version, so the gateway (and clients, under FR-28's "apply only newer" rule) dropped it as stale. Found by the end-to-end test; the AI traced it by logging the raw events, which showed two events with `"v":2`. Fixed in Go and Lua, with tests in both.
  - **Answers would fail indefinitely after a Redis restart with `WAIT` enabled.** The pipelined `EVALSHA` can't fall back to `EVAL`. Reproduced with `SCRIPT FLUSH`, then fixed with a reload and one retry.
  - **The `state` event lacked `next_at`**, which the reveal message needs and gateways can't compute.
- **What the AI got wrong:**
  - **An off-by-one in its own subscriber:** a new room's state started at "confirmed in epoch 0", which matched the epoch before the first connect, so `Ensure` could return with nothing subscribed. Caught by the integration tests on the first run.
  - **A connection-breaking test helper:** a short read deadline used to assert "no rank for the host" killed the gorilla connection (read timeouts are permanent there), and the next assertion failed for the wrong reason.
  - **Weak tests exposed by mutation testing:** a fake `View` ignored cancellation, a confirmation test ran without latency so an early return couldn't be seen, and nothing checked that a healthy idle connection stays up. All three fixed and re-verified. Four mutants were invalid on the first try and were redone.
  - **Tooling slips:** placeholder imports left in test files twice (`var _ = …`), a Python edit that failed to parse (and so, correctly, changed nothing), and a doc reference to a config setting that doesn't exist. All corrected before commit.
  - **Reported as not covered by a test:** per-`SUBSCRIBE` confirmation counting (the interleaving it guards can't be ordered deterministically against a real `PubSub`) and the 500-per-call chunking of `standings`.
- **Verification:**
  - Unit tests on fake sockets and fake Redis reads.
  - Integration tests on real Redis with Toxiproxy: latency, outage, and a half-open connection.
  - An end-to-end test with real scripts, the question cache over PostgreSQL, and real WebSocket clients, where every frame is validated against its schema.
  - 21 mutations caught, 3 clean repeated runs, all 16 integration packages, and `make check` green.
  - **One unexplained failure.** In the per-commit check, commit `12b52d9` failed its unit tests once. It didn't reproduce in 4 reruns of that commit, 15 runs of the realtime package under CPU load, or 5 full race-detector runs of every package. The check script kept only a count of `FAIL` lines, so the failing test is unknown. From now on, per-commit checks keep their full output so a one-off failure can be identified.

### AI-031: Gateway message handlers, presence, and the build focus (task-20, D17)

- **Date / phase:** 2026-09-29 · P3 Services
- **My input:**
  - I asked for task-20 after reviewing task-19.
  - Mid-task I set the build focus (D17): the real-time path gets the full treatment, including simulation and load runs that show the architecture is ready. Supporting concerns stay minimal scaffolds that keep the scalable shape, and observability shouldn't go deep.
- **What the AI produced:**
  - the message handler: join, watch, submit and ping, with one error mapping for TRD §9.6;
  - the `presence` script and loop, and a gateway clock aligned to Redis `TIME`;
  - the connection kick published inside `join`;
  - the archived final read for finished quizzes;
  - the `cmd/ws` wiring with health, readiness and `errors_total`;
  - the D17 rewrite of task-22 and task-23.
- **Burst decisions carried from my task-18 concern:** a join into a room the gateway already has skips the room lookup, and the own-answer read runs only mid-question, so a lobby join burst costs one script per participant.
- **Found by checking every frame against the contract:** `pong` echoed a request `id` its schema didn't allow, and the finished-quiz snapshot had an empty display name. The schema was fixed (the change is additive); the name comes from the join message.
- **Found by noticing test timings:** the new gateway tests took 15 s and 30 s. That was the subscriber from task-19 waiting out its 15 s health interval on shutdown, because a blocked read ignores cancellation. The task-19 tests used a 200 ms interval and hid it. Fixed by closing the connection on cancel, with a test that fails without the fix.
- **What the AI got wrong:**
  - a test assumed the gateway accepts lowercase codes, but the contract rejects them (the client uppercases);
  - a transition helper didn't force `close_at`;
  - a clock source passed a single `TIME` reading instead of a function;
  - `prometheus/testutil` would have added a dependency for one assertion;
  - a backup file went to `/tmp` instead of the scratchpad (deleted);
  - it first checked off "two browser tabs" when no client exists yet, then reworded the step.
- **Verification:**
  - handler unit tests on fakes, and integration tests for the scripts and `Final`;
  - the task-19 end-to-end test switched to the real handler;
  - a new `cmd/ws` test through the real wiring, including a kick across two gateways;
  - 29 deliberate breaks, all caught;
  - `make check` and all 17 integration packages green, with full logs saved;
  - a smoke run of the real `api` and `ws` binaries against Compose with Bun WebSocket clients.

### AI-032: The worker (task-21)

- **Date / phase:** 2026-09-29 · P3 Services
- **My input:** I asked to continue after task-20. Under D17 the worker is core: nothing advances a quiz without it.
- **What the AI produced:**
  - a generic loop runner in `internal/scheduler`: one pass at a time per loop, skipped ticks on overrun, bounded item pools, liveness;
  - the three loops (transitions, leaderboard ticks, persistence jobs);
  - the `cmd/worker` wiring with `/healthz`, `/readyz` and a drain on stop;
  - a refactor that moved the Redis-aligned clock to `platform/redisx` and removed the empty `fanout` placeholder.
- **Decisions the AI proposed, for my review:**
  - Transition claims use Redis-aligned time, so they agree with the scripts' due checks.
  - Liveness counts a *failed* pass as completed, because restarting doesn't fix Redis; it only catches a stuck loop.
  - Stopping never cancels a pass already running.
  - The trace-ID step is dropped under D17.
- **What the AI checked instead of redoing:** the gomock failure paths for flush and finalise that the task asked for already existed from task-14, so none were duplicated.
- **What the AI got wrong:**
  - a test stored `nil` in an `atomic.Value`, which panics;
  - a liveness test would have flagged the healthy loop too, right after the fake clock jumped;
  - three mutants proved nothing on the first run: an unused import, an undefined type, and the zsh word-splitting trap again (`-tags integration` passed as one argument, so the package never ran). All were redone until they built and were caught;
  - lint caught an `http.Get` without a context and an empty-bodied loop in the tests.
- **Verification:**
  - scheduler unit tests, run 4 times because they depend on timing;
  - an integration test in which **two workers run a whole quiz on their own**: early closes, about 1 s for three 5 s questions, every state version published exactly once, leaderboard updates, answers and results saved, room released, liveness 200;
  - 16 deliberate breaks, all caught;
  - `make check` and all 19 integration packages green, with logs saved.
