# Requirements

Status: **agreed, pending final read-through** · Last updated: 2026-09-28

Functional and non-functional requirements for the real-time quiz. Sources: the brief ([`../assignment.md`](../assignment.md)) and the decisions in [`context.md`](context.md). Items marked _(proposal)_ are ours, not the brief's, and need sign-off.

Priority: **M** = must, **S** = should, **C** = could.

## 1. Actors

| Actor | Description |
|---|---|
| Participant | A learner who joins a quiz with its ID, answers questions, and sees their score and the leaderboard |
| Host | Creates a quiz and starts it. Not a participant: does not answer and is not ranked |
| Quiz clock | The server-side process that moves a started quiz through its questions |
| Operator | Runs and monitors the service |

## 2. Glossary

| Term | Meaning |
|---|---|
| Quiz | One live session built from a question set. Identified by a quiz ID |
| Quiz ID | Short, shareable code a participant types to join (e.g. 6 characters, unambiguous alphabet) |
| Question set | Ordered list of multiple-choice vocabulary questions (mocked content) |
| Question window | Time between a question opening and its deadline |
| Reveal | Short phase after a question closes, where the correct answer and updated standings are shown |
| Accepted answer | The first valid answer from a participant for a question. Only accepted answers score |

## 3. Functional requirements

### 3.1 Quiz creation and lifecycle

| ID | Requirement | Pri | Brief |
|---|---|---|---|
| FR-1 | A host can create a quiz from a question set. The system returns a unique quiz ID and a host credential | M | AC 1 |
| FR-2 | A quiz moves through these states: `lobby → question_open → question_closed → … → finished`. A lobby that is never started ends in `expired` | M | — |
| FR-3 | Only the host can start a quiz, only while it is in `lobby`, and only once at least one participant has joined. Expected flow: host creates the room, participants join, host starts | M | — |
| FR-4 | After start, the server advances the quiz on its own: each question stays open for the configured window, then closes, shows the reveal for the configured duration, and moves to the next question. After the last reveal the quiz is `finished` | M | — |
| FR-5 | The server tracks accepted submissions per question. Once every connected participant has an accepted answer, the question closes early: the result is announced (reveal) and the state moves on, exactly as if the deadline had passed. Disconnected participants don't hold the question open | M | — |
| FR-6 | A quiz left in `lobby` past an idle timeout becomes `expired` _(proposal: 30 min)_ | S | — |
| FR-7 | Question window and reveal durations are configurable per quiz _(proposal: defaults 15 s and 5 s)_ | M | — |

### 3.2 Participation

| ID | Requirement | Pri | Brief |
|---|---|---|---|
| FR-8 | A participant joins with a quiz ID and a display name. Identity comes from a signed token (mocked issuer) | M | AC 1 |
| FR-9 | Many participants can join the same quiz at the same time without losing or duplicating anyone | M | AC 1 |
| FR-10 | Joining is allowed in `lobby` and while the quiz is running. A late joiner starts at 0 points and can answer the current question if it is still open | M | AC 1 |
| FR-11 | On join, the participant receives a snapshot: quiz state, current question (without the answer) and its deadline, the leaderboard, and their own score | M | AC 2, 3 |
| FR-12 | Rejoining with the same identity restores the same participant and score; no duplicate entry is created. If the same identity has two connections, the newer one replaces the older | M | AC 2 |
| FR-13 | Joining an unknown or expired quiz returns a clear error. Joining a finished quiz returns its final leaderboard, read-only, served from persistent storage once the live data is released | M | AC 1 |
| FR-14 | Everyone in the quiz sees the participant count update as people join and leave | S | AC 1 |
| FR-15 | Display names are 1–20 characters and validated. They need not be unique; ranking is by identity, not name _(proposal)_ | M | — |

### 3.3 Answering and scoring

| ID | Requirement | Pri | Brief |
|---|---|---|---|
| FR-16 | A participant can submit one answer (an option ID) to the currently open question | M | AC 2 |
| FR-17 | The server accepts an answer only if: the quiz is in `question_open`, the question ID matches the current question, the server received it before the deadline, and the participant has no accepted answer for that question yet | M | AC 2 |
| FR-18 | The first accepted answer is final. Later submissions for the same question are rejected as duplicates and do not change the score. A resend while the question is open returns the original result. After the question closes, a resend is rejected as `question_closed`, and the participant's total in their snapshot is authoritative | M | AC 2 |
| FR-19 | A correct answer scores **100 points + a speed bonus of up to 100**: `bonus = floor(100 × time_remaining / window)`, using server receive time. Wrong answers score 0. Scores never go negative. Maximum per question: 200 | M | AC 2 |
| FR-20 | The submitter immediately receives the result: accepted or rejected (with reason), correct or not, points earned, and new total | M | AC 2 |
| FR-21 | The correct answer is never sent to any client before the question closes. The only early signal is a submitter learning whether their own answer was correct | M | AC 2 |
| FR-22 | A participant's total is the sum of their accepted answers' points, kept server-side, and identical no matter which server instance handled each answer | M | AC 2 |

### 3.4 Leaderboard

| ID | Requirement | Pri | Brief |
|---|---|---|---|
| FR-23 | The leaderboard ranks every participant, including those with 0 points, by total score (highest first) | M | AC 3 |
| FR-24 | Equal scores share a rank (1, 2, 2, 4). The speed bonus makes ties uncommon, so no extra tie-break rule is needed. Display order within a tie is stable | M | AC 3 |
| FR-25 | After any score change, all participants and the host receive an updated leaderboard within the latency target (NFR-6). Updates are batched per quiz rather than sent once per answer | M | AC 3 |
| FR-26 | Live updates are **the same message for everyone** in the room: top 10, participant count, and a version number that only ever increases. Each participant's own score arrives with their answer result (FR-20). Their own rank is sent at every question close | M | AC 3 |
| FR-27 | The full leaderboard can be requested at any time and is sent to everyone when the quiz finishes | M | AC 3 |
| FR-27a | When a quiz finishes, its final leaderboard is written to persistent storage and stays retrievable after the live Redis data expires | M | AC 3 |
| FR-28 | Clients never show an older leaderboard after a newer one (they drop messages with a lower version) | M | AC 3 |

### 3.5 Connection handling

| ID | Requirement | Pri | Brief |
|---|---|---|---|
| FR-29 | A disconnected or stale participant resumes from a fresh snapshot at the room's current point. Questions that closed while they were away score 0 for them; there is no catch-up. They can answer the current question if it is still open | M | AC 2, 3 |
| FR-30 | An answer accepted before a disconnect counts exactly once, even if the client resends it after reconnecting | M | AC 2 |
| FR-31 | The host disconnecting after start does not affect the quiz | M | — |

### 3.6 Answer history

| ID | Requirement | Pri | Brief |
|---|---|---|---|
| FR-32 | Every accepted answer is stored persistently: quiz, question, participant, chosen option, correct or not, points, server receive time | M | AC 2 |
| FR-33 | A closed question's answers are written to persistent storage as one batch, after the question closes (not during the answer burst) | M | AC 2 |
| FR-34 | A question's answer records are removed from Redis **only after** the batch write is durably acknowledged. The same rule applies to the leaderboard and final results when the quiz finishes | M | AC 2 |
| FR-35 | At quiz finish, each participant's total recomputed from the stored answers must equal their live leaderboard score. Any mismatch is logged and counted (NFR-27) | S | AC 2 |

## 4. Non-functional requirements

### Scale focus

The hard part of the load is **concurrency inside one room**, not data volume. Per-room state is small (a few KB of Redis keys), so the number of rooms is a secondary concern. What matters is how many live connections a room can hold, and how cheaply every answer burst and leaderboard update reaches all of them. The targets reflect that.

| ID | Assumption | Target _(proposal)_ |
|---|---|---|
| NFR-1 | Participants in a single room | design for 10,000; demo at least 5,000 in one room on one machine |
| NFR-2 | Concurrent active rooms | not a primary constraint; state per room is small and independent |
| NFR-3 | Connections per server instance | 20,000 |
| NFR-4 | Memory per idle connection | ≤ 50 KB (goroutines + buffers), measured |
| NFR-5 | Peak answer burst in one room | the whole room answering within 2 s (10,000 answers in 2 s at design size) |

### Connection management and fan-out efficiency

| ID | Requirement |
|---|---|
| NFR-5a | Each instance keeps a connection registry indexed by room: room ID → its local connections. Joining, leaving, and broadcasting touch only that room's entry, never a scan of all connections |
| NFR-5b | A room broadcast costs **one** pub/sub message in total, regardless of room size. Each instance receives it once and fans it out to its local connections |
| NFR-5c | Each broadcast payload is **encoded once per instance** and the same bytes are written to every connection. There is no per-recipient serialisation for shared messages (this is why FR-26 keeps live updates identical for everyone) |
| NFR-5d | Writes to a connection never block the broadcast loop. Each connection has a bounded outbound queue (see NFR-17) |
| NFR-5e | The registry drops closed or dead connections promptly (heartbeat timeout) so they stop costing memory or fan-out time |

### Performance

| ID | Requirement | Target _(proposal)_ |
|---|---|---|
| NFR-6 | Answer accepted → updated leaderboard delivered to every participant in the quiz | p95 < 500 ms (includes batching window) |
| NFR-7 | Answer received → result sent to the submitter (server-side) | p95 < 100 ms, p99 < 250 ms |
| NFR-8 | Question opened → delivered to every participant (fairness spread) | p95 < 200 ms |
| NFR-9 | Join → snapshot received | p95 < 300 ms |
| NFR-10 | Leaderboard batching window | ≤ 200 ms |
| NFR-11 | Leaderboard update payload | < 2 KB |

### Consistency and reliability

| ID | Requirement |
|---|---|
| NFR-12 | **Exactly-once scoring** per (quiz, question, participant) under concurrent submissions, retries, reconnects, and multiple instances |
| NFR-13 | An answer is acknowledged only after it is durably recorded in the shared store. No ack means the client may retry safely |
| NFR-13a | Persistent writes are never on the answer path. Batch flushes are idempotent (unique key on quiz, question, participant) and crash-safe: a pending flush survives the death of the instance that started it and is retried by any instance until acknowledged |
| NFR-14 | No instance owns a quiz. If an instance dies, its clients reconnect to any other instance and the quiz keeps running on schedule. Every state transition (open, close, next, finish) happens **exactly once**, even when several instances notice the same deadline. Deadline slip ≤ 250 ms _(proposal)_ |
| NFR-15 | If the shared store is unavailable, answers are rejected with a retryable error (never falsely acknowledged), and the instance reports not-ready |
| NFR-16 | Graceful shutdown: stop accepting connections, tell clients to reconnect elsewhere, drain within 30 s |
| NFR-17 | Slow clients cannot block delivery to others. If a client's outbound queue overflows, its queued messages are dropped and it is resynced with a fresh snapshot, or disconnected if it stays behind. A stale client always resumes **at the room's current point**; nothing is replayed |
| NFR-18 | Redis holds only data that is live or not yet persisted. Answer records are released per question after their flush (FR-34); the rest of a quiz's data is released after its final results are persisted. A TTL _(proposal: 24 h)_ remains as a safety net, and it is extended while a flush is still pending, so unpersisted data is never lost to expiry |

### Security

| ID | Requirement |
|---|---|
| NFR-19 | Identity and role come from a signed token. Clients cannot choose their user ID or claim the host role |
| NFR-20 | Every inbound message is validated (schema, size ≤ 4 KB). Invalid messages get an error, never a crash |
| NFR-21 | Per-connection rate limit _(proposal: 20 messages/s, burst 40)_ |
| NFR-22 | Answer keys stay server-side until the question closes (see FR-21) |
| NFR-23 | WebSocket origin check; TLS terminated at the load balancer |

### Scalability

| ID | Requirement |
|---|---|
| NFR-24 | Server instances scale horizontally. Participants of one quiz can be connected to different instances and still see the same state |
| NFR-25 | WebSocket instances hold no quiz state, only their own open connections. All live quiz state (lifecycle state, current question, deadline, participants, answers, scores, leaderboard) lives in Redis with a TTL. Only static question sets (including answer keys) and final results are stored persistently |
| NFR-26 | Adding instances increases connection capacity roughly linearly until the shared store becomes the bottleneck. That limit and the next scaling step are documented |

### Observability

| ID | Requirement |
|---|---|
| NFR-27 | Metrics endpoint with at least: active connections, joins, answers by outcome, NFR-6/7/8 latencies as histograms, broadcast duration, store latency, dropped messages, errors by code, pending and failed flushes, score reconciliation mismatches (FR-35, expected 0) |
| NFR-28 | Structured logs carrying quiz ID, participant ID, connection ID, and request ID |
| NFR-29 | Liveness and readiness endpoints. Readiness reflects shared-store health |
| NFR-30 | Documented alerts with thresholds tied to the targets above |

### Maintainability and operability

| ID | Requirement |
|---|---|
| NFR-31 | Code follows the module layout in D9. Domain logic has no I/O and is unit-tested directly |
| NFR-32 | Every M-priority FR has at least one automated test |
| NFR-33 | The client–server protocol is versioned and documented, with an example for every message |
| NFR-34 | Configuration comes from environment variables and is validated at startup |
| NFR-35 | One command starts the full stack locally (server instances, Redis, load balancer, client) |

## 5. Assumptions

- Questions are multiple choice with 2–4 options. Question sets are seeded into PostgreSQL; there is no authoring UI.
- Auth is mocked: a dev endpoint issues signed tokens for a participant or host identity.
- Persistent storage (question sets, answer history, final results) is PostgreSQL, run for real in the local stack with seeded question sets (D12). Live state lives in Redis.
- Single region. Server clocks are NTP-synced (skew well under 100 ms), so any instance can act on a deadline written by another instance. (Alternatively, use Redis `TIME` as the single clock; decided in the TRD.)
- Network latency differences between participants are not compensated. Speed bonuses use server receive time.
- Collusion (participants sharing answers out of band) is out of scope.

## 6. Out of scope

Quiz authoring UI, user accounts and profiles, persistent history and analytics, payments, native mobile clients, multi-region deployment, question types other than multiple choice.

## 7. Resolved questions

| # | Question | Resolution (2026-09-28) |
|---|---|---|
| Q1 | Scoring | 100 points for correct + speed bonus up to 100 (FR-19) |
| Q2 | Tie-break | No extra rule; the speed bonus makes ties rare, and equal scores share a rank (FR-24) |
| Q3 | Close early when everyone has answered? | Yes. Track submissions per question, announce the result, move the state (FR-5) |
| Q4 | Start with no participants? | No. Create room → wait for participants → start (FR-3) |
| Q5 | Final leaderboard after the quiz? | Persisted, read-only (FR-13, FR-27a) |
| Q6 | Scale focus | Concurrency within one room and efficient connection tracking and fan-out, not data volume (scale focus, NFR-5a to NFR-5e) |
| Q7 | Submitter learns correctness immediately? | Yes (FR-20, FR-21) |
| Q8 | Stale or reconnecting client | Resumes at the room's current point; missed questions score 0 (FR-29, NFR-17) |
