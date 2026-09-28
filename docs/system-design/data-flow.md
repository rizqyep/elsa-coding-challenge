# Data flow

How data moves through the system, from the moment a host creates a quiz to the moment every participant sees the final leaderboard. The components are described in [`architecture.md`](architecture.md). Requirement IDs refer to [`../planning/requirements.md`](../planning/requirements.md).

The diagrams use the three backend services from the architecture: **REST API**, **Gateway** (WebSocket gateway), and **Worker**. Each runs as several instances, and which instance handles a step doesn't matter; that is the point of the design.

## Overview

```mermaid
stateDiagram-v2
    [*] --> lobby: host creates quiz
    lobby --> question_open: host starts (at least 1 participant)
    lobby --> expired: idle timeout
    question_open --> question_closed: deadline reached, or everyone online has answered
    question_closed --> question_open: reveal over, more questions
    question_closed --> finished: reveal over, last question
    finished --> [*]: final results persisted, Redis data released
    expired --> [*]
```

| # | Flow | Triggered by |
|---|---|---|
| 1 | Create a quiz and join | Host request, participant join |
| 2 | Start and open a question | Host `start`, then the scheduler |
| 3 | Submit an answer | Participant |
| 4 | Leaderboard update | Scheduler tick after score changes |
| 5 | Close a question, reveal, flush answers | Scheduler (deadline or early close) |
| 6 | Finish the quiz | Scheduler after the last reveal |
| 7 | Reconnect and instance failure | Network or instance failure |

## 1. Create a quiz and join

```mermaid
sequenceDiagram
    autonumber
    actor H as Host
    actor P as Participant
    participant S1 as REST API
    participant S2 as Gateway
    participant R as Redis
    participant DB as PostgreSQL

    H->>S1: POST /quizzes with question set ID and host token
    S1->>DB: load question set, if not already cached
    S1->>R: create room record, status lobby, version 1
    S1->>R: schedule lobby expiry in sched:transitions
    S1-->>H: quiz code

    P->>S2: open WebSocket with token
    P->>S2: join with quiz code and display name
    S2->>R: join script
    Note over R: check room exists and is not expired<br/>add to roster, add to leaderboard at 0 if new<br/>mark online, mark leaderboard dirty
    R-->>S2: room record, top 10, own score and rank
    S2->>S2: add socket to registry under this room
    S2->>R: SUBSCRIBE room channel, if first local socket for this room
    S2->>DB: warm question cache, if this question set is not cached yet
    S2-->>P: joined snapshot
```

- **FR-8, FR-9:** the join script is atomic, so many simultaneous joins can't lose or duplicate a participant.
- **FR-12:** rejoining with the same identity finds the existing roster and leaderboard entries and keeps the score.
- **FR-11:** the snapshot includes the current question without its answer, and the deadline.
- The question cache is warmed at join, so validating this participant's answers later needs no external read. It's keyed by question set and loaded once per gateway.
- If the same identity is already connected elsewhere, the gateway publishes a control message on the room channel, and the gateway holding the older socket closes it.
- Marking the leaderboard dirty makes the next tick (flow 4) broadcast the new participant count (FR-14).

## 2. Start and open a question

```mermaid
sequenceDiagram
    autonumber
    actor H as Host
    participant API as REST API
    participant R as Redis
    participant W as Worker
    participant ALL as Every gateway
    actor PS as All participants

    H->>API: POST start for the quiz, host token
    API->>R: start script
    Note over R: check caller is the host, status is lobby,<br/>at least 1 participant<br/>schedule the first transition for now
    R-->>API: accepted
    API-->>H: 202 Accepted
    W->>R: due transition found on next poll
    W->>R: transition script, expect status lobby and version v
    Note over R: status question_open, question 1<br/>deadline = TIME + window, version v+1<br/>reschedule in sched:transitions at deadline
    R-->>W: new room state
    W->>W: build question message from cached question set, no answer key
    W->>R: PUBLISH room channel, question message
    R-->>ALL: question message, once per subscribed gateway
    ALL-->>PS: same bytes written to every local socket in the room
```

- **FR-3:** only a `host` token can start, only from `lobby`, and only with at least one participant.
- **One transition path:** the API doesn't open the first question itself. It schedules the transition for "now", and a worker applies it like any other. Only workers publish room state, and there is one code path for every transition. The cost is at most one poll interval (100 ms) between pressing Start and the first question.
- **FR-4:** from here on, nobody sends commands. Every later transition comes from the workers (flow 5).
- **NFR-8:** the question reaches every participant through one publish. Fairness depends only on pub/sub and socket-write latency, not on which instance a participant is connected to.
- **FR-21:** the answer key never leaves the server's memory.

## 3. Submit an answer

```mermaid
sequenceDiagram
    autonumber
    actor P as Participant
    participant S2 as Gateway
    participant R as Redis

    P->>S2: submit_answer with request ID, question ID, option
    S2->>S2: validate schema and size, apply rate limit
    S2->>S2: correct or not, from cached answer key
    S2->>R: answer script
    Note over R: all in one atomic step<br/>1. status is question_open and question ID matches<br/>2. TIME is before the deadline<br/>3. no existing answer for this participant (HSETNX)<br/>4. points = correct ? 100 + floor(100 x remaining / window) : 0<br/>5. add points to leaderboard, mark leaderboard dirty<br/>6. if answered count reaches online count, move deadline to now
    alt accepted
        R-->>S2: correct, points, new total
        S2-->>P: answer_result accepted
    else duplicate
        R-->>S2: the stored original result
        S2-->>P: answer_result duplicate, with original result
    else late or wrong question
        R-->>S2: rejected, reason
        S2-->>P: error question_closed
    end
```

- **NFR-12, FR-17, FR-18:** duplicate detection, the deadline check, and scoring happen in one script, so concurrent or repeated submissions can't score twice. An answer racing the close has only two outcomes: the script runs before the close transition (accepted) or after it (rejected). Redis runs scripts one at a time, so there is no in-between.
- **No submissions after the close.** The answer script checks Redis `TIME` against the deadline itself; it doesn't rely on the status alone. So even if the workers are late closing the question (poll delay, a worker restart, a Redis failover), an answer received after the deadline is still rejected. The early close only ever moves the deadline *earlier*, never later. The question ID check stops answers to a previous question once the next one opens. An answer sent before the deadline but received after it is rejected, because server receive time is the rule (assumption in the requirements).
- **NFR-13:** the client is acknowledged only after the script has written to Redis. If the reply is lost, resending the same request returns the stored result (FR-30).
- **FR-19:** points use Redis `TIME`, not the client's or the instance's clock.
- **FR-5:** when everyone online has answered, the script moves the deadline to now. The close then happens through the normal transition path (flow 5), so there is only one close code path.
- **FR-20, FR-21:** only the submitter learns whether they were right, straight away. Nothing is broadcast per answer.

## 4. Leaderboard update

```mermaid
sequenceDiagram
    autonumber
    participant Sx as Worker
    participant R as Redis
    participant ALL as Every gateway
    actor PS as All participants

    loop every 200 ms, on every worker
        Sx->>R: SPOP sched:lbdirty, up to 100 quiz IDs
    end
    Note over Sx,R: SPOP is atomic, so each dirty quiz<br/>is taken by exactly one worker per tick
    Sx->>R: leaderboard script for quiz Q
    Note over R: top 10 with scores, participant count,<br/>increment leaderboard version
    R-->>Sx: top 10, count, version
    Sx->>R: fetch display names for the top 10 from roster
    Sx->>Sx: encode leaderboard message once
    Sx->>R: PUBLISH room channel, leaderboard message
    R-->>ALL: leaderboard message
    ALL-->>PS: same bytes to every local socket
```

- **FR-25, NFR-10:** a burst of 10,000 answers produces at most 5 leaderboard messages per second per room, not 10,000.
- **FR-26, NFR-5c:** the message is identical for everyone, so it's encoded once and written as the same bytes to every socket.
- **FR-28:** clients drop any leaderboard with a version lower than the last one they rendered.
- **NFR-6:** worst-case added delay is one tick (200 ms) plus publish and write time.

## 5. Close a question, reveal, flush answers

```mermaid
sequenceDiagram
    autonumber
    participant Sx as Worker running the transition
    participant R as Redis
    participant ALL as Every gateway
    actor PS as All participants
    participant Sy as Worker running the flush, any instance
    participant DB as PostgreSQL

    loop every 100 ms, on every worker
        Sx->>R: quiz IDs in sched:transitions due by now
    end
    Sx->>R: transition script, expect question_open and version v
    Note over R: re-check TIME against deadline<br/>status question_closed, version v+1<br/>schedule next transition at TIME + reveal<br/>add flush job for this question, due now
    alt this worker won
        R-->>Sx: new state
        Sx->>R: PUBLISH question_closed with the correct option
        R-->>ALL: question_closed
        ALL->>R: pipelined rank lookups for local participants
        ALL-->>PS: question_closed, then each participant's own rank
    else another worker already did it
        R-->>Sx: version mismatch, nothing to do
    end

    Sy->>R: claim due flush job, push its retry time 30 s ahead
    Sy->>R: read all answers for the question
    Sy->>DB: batch insert, ON CONFLICT DO NOTHING
    DB-->>Sy: commit confirmed
    Sy->>R: delete the question's answers, remove the flush job
```

- **NFR-14:** several instances may notice the same due quiz. The version check lets exactly one apply the transition; the rest get a version mismatch and do nothing.
- **FR-26:** personal ranks are computed once per question, by each instance for its own sockets, in a pipelined batch.
- **FR-33, FR-34, NFR-13a:**
  - The flush happens after the burst, as one batch per question.
  - Redis data is deleted only after the database confirms the commit.
  - If the flushing instance dies at any point, the job becomes due again 30 s later and another instance retries it. The unique key (quiz, question, participant) makes the retry harmless.
- The next transition, after the reveal, runs the same flow in reverse: `question_closed → question_open` for the next question, published as in flow 2.

## 6. Finish the quiz

```mermaid
sequenceDiagram
    autonumber
    participant Sx as Worker
    participant R as Redis
    participant ALL as Every gateway
    actor PS as All participants
    participant DB as PostgreSQL

    Sx->>R: transition script after the last reveal
    Note over R: status finished, version v+1<br/>add final-results flush job
    R-->>Sx: new state
    Sx->>R: PUBLISH quiz finished with final top 10
    R-->>ALL: quiz finished
    ALL-->>PS: final standings, full leaderboard available on request

    Sx->>R: claim final-results job, only after all question flushes are done
    Sx->>R: read full leaderboard and roster
    Sx->>DB: insert final results, recompute totals from answer history
    DB-->>Sx: commit confirmed, totals match
    Sx->>R: release all remaining room keys
```

- **FR-27, FR-27a:** the final leaderboard is persisted, so after the Redis data is released a finished quiz can still be viewed read-only from the database (FR-13).
- **FR-35:** totals recomputed from stored answers must equal the leaderboard. A mismatch is logged and counted, and would indicate a scoring bug.
- **NFR-18:** the room's Redis keys are released only after the final write is confirmed. The 24 h TTL is only a safety net.

## 7. Reconnect and instance failure

```mermaid
sequenceDiagram
    autonumber
    actor P as Participant
    participant S1 as Gateway G1
    participant LB as Load balancer
    participant S3 as Gateway G3
    participant R as Redis

    P->>S1: connected, in quiz Q
    Note over S1: G1 crashes
    P->>P: socket closed, back off with jitter
    P->>LB: reconnect
    LB->>S3: routed to any healthy gateway
    P->>S3: join quiz Q, same identity
    S3->>R: join script
    R-->>S3: current room state, top 10, own score and rank
    S3-->>P: snapshot at the room's current point
    Note over R: quiz Q never stopped:<br/>workers kept running its transitions.<br/>G1 presence entries expire after 30 s
```

- **FR-29, NFR-17:** the participant resumes where the room is now. Questions that closed while they were away score 0 for them.
- **FR-30:** if G1 crashed after the answer script ran but before the reply was sent, the client resends the answer after reconnecting. While the question is still open it gets the original result back as a duplicate. After the close, the total in the snapshot already includes it.
- **NFR-14:** no quiz belonged to G1, so nothing needs to be taken over. The same holds for a worker crash: the other workers pick up its due work on their next poll.
- Jittered backoff spreads the reconnect burst after an instance dies, so 20,000 clients don't all hit the survivors at the same moment.

## How the flows meet the acceptance criteria

| Brief | Flows |
|---|---|
| Join a quiz by unique ID; many users at once | 1, 7 |
| Scores update in real time, accurately and consistently | 3, 5 |
| Leaderboard of all participants, updated promptly | 4, 5, 6 |
