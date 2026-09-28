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
