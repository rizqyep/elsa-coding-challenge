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
  - **The capacity numbers are estimates** from typical per-operation costs. They are labelled that way in the document, and the load test and room simulator (T017) must replace them with measured values.
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
