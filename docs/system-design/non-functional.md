# Scalability, performance, reliability, observability

How the design meets the non-functional requirements (NFR-n in [`../planning/requirements.md`](../planning/requirements.md)), where its limits are, and what it gives up. Components and flows are in [`architecture.md`](architecture.md) and [`data-flow.md`](data-flow.md).

> **All capacity numbers in this document are estimates** from typical per-operation costs, used to find the bottleneck and set test targets. The load test and room simulator replace them with measured values, reported in `docs/testing.md` alongside the machine they ran on.

## 1. Scalability

### 1.1 What we scale for

The hard part is **concurrency inside one room**, not data volume (requirements, "Scale focus"). The reference workload:

| Parameter | Value |
|---|---|
| Participants in one room | 10,000 (design), ≥ 5,000 demonstrated (NFR-1) |
| Question window / reveal | 15 s / 5 s, so one question cycle ≈ 20 s |
| Answer burst | the whole room answers within 2 s, i.e. 5,000 answers/s (NFR-5) |
| Leaderboard tick | every 200 ms, only while scores are changing (NFR-10) |
| Connections per gateway | 20,000 (NFR-3) |

### 1.2 Cost of one 10,000-person room

**Redis**

| Work | Rate during burst | Estimated cost |
|---|---|---|
| Answer script (≈ 8 commands: state read, `TIME`, `HSETNX`, `ZINCRBY`, `SADD`, counts) | 5,000/s | ≈ 20 µs each → **≈ 10% of one Redis core** |
| Leaderboard snapshot (top 10, count, version) + roster lookup for 10 names | ≤ 5/s | negligible |
| `PUBLISH` of a room message | ≤ 5/s + transitions | one delivery per subscribed **gateway**, not per participant |
| Personal ranks at question close | 10,000 lookups per question, pipelined per gateway | ≈ 10–20 ms of Redis time per question |
| Presence refresh (every 10 s) | 1,000/s, pipelined | negligible |
| Memory: leaderboard + roster + presence + current question's answers | — | ≈ 4 MB per room |

**Gateways**

| Work | Estimate |
|---|---|
| Memory per idle connection: 2 goroutines, 1 KB read buffer, pooled write buffer (none held when idle), queue slots | ≈ 20–30 KB → **≈ 0.5 GB for 20,000 connections** (budget ≤ 50 KB, NFR-4). TLS is terminated at the load balancer, so there are no TLS buffers in the gateway |
| Leaderboard egress: ~1 KB × 10,000 recipients × 5/s | **≈ 50 MB/s (≈ 400 Mbit/s) across all gateways holding the room**, only during the few seconds scores change |
| Socket writes for that egress: 50,000 prepared-message writes/s | ≈ 0.25 core in total |
| Question broadcast: ~0.5 KB × 10,000, once per question | negligible |

**Workers:** about 1 transition every 10 s per room, and ≤ 5 leaderboard snapshots per second during bursts. Polling costs ≈ 16 Redis calls per second per worker, regardless of room count.

**PostgreSQL:** one batch of 10,000 answer rows per question, written during the reveal, off the answer path.

### 1.3 The ceiling with one Redis primary + replica

The shared resource is the single Redis thread that runs the answer scripts. At ≈ 20–25 µs per script, **one Redis primary handles roughly 40,000 answers per second**. What that means:

| Scenario | Estimated ceiling |
|---|---|
| **One room**, everyone answering within 2 s | ≈ 80,000 participants before Redis saturates. Well above the 10,000 target, so **for a single room the gateways' leaderboard egress limits first, not Redis** |
| **Many rooms with bursts aligned** (worst case: every room answers in the same 2 s) | ≈ 80,000 participants answering at once across all rooms |
| **Many rooms with bursts spread out** (rooms start at different times, one answer per participant per ~20 s cycle) | ≈ 800,000 concurrent participants |
| Gateways needed at that point | 800,000 / 20,000 = 40 gateways. They scale linearly because they share nothing |
| Redis memory | not a constraint: ≈ 4 MB per 10,000-person room |

**Our stated ceiling for this design:** one room of 10,000 (target) up to ~50,000 before leaderboard egress needs the mitigations in §1.4, and **around 40,000 answers per second across the whole system** before Redis must be sharded. The load test checks the per-script cost behind this number.

### 1.4 What breaks first, and the next step

| Limit reached | First sign | Next step |
|---|---|---|
| Leaderboard egress in a very large room | Gateway CPU/network saturated during bursts; NFR-6 latency rises | Skip ticks when the top 10 hasn't changed; widen the tick interval for very large rooms (trading NFR-6 latency); binary encoding; permessage-deflate compression (costs CPU) |
| Redis script throughput (many simultaneous bursts) | Redis CPU > 70%; answer latency (NFR-7) rises | **Redis Cluster**: every key of a room shares the `{quizId}` hash tag, so rooms spread across shards and scripts stay single-slot. Schedules split into per-shard keys. **Sharded pub/sub** (`SSUBSCRIBE`), so messages stay on the owning shard instead of going to every node |
| Connections per gateway | Memory per connection × count nears the instance limit | Add gateways (linear). Beyond ~100k per instance: an epoll-based server (gobwas/ws) that avoids a goroutine per socket |
| PostgreSQL write rate for answer history | Flush jobs start queuing; pending-flush metric grows | Partition the answers table (by time or quiz), use `COPY` into a staging table, or move history writing to a Redis Stream consumed by a dedicated writer |
| Worker throughput with very many rooms | Transition lag (actual vs scheduled time) grows | Add workers (claims are atomic, so there is no coordination cost) |

## 2. Performance

### 2.1 Latency budgets

| Requirement | Path | Budget (estimate) | Target |
|---|---|---|---|
| NFR-7: answer → result to submitter | validate (µs) + 1 Redis round trip incl. script (≈ 1 ms) + reply | **≈ 2–5 ms** typical | p95 < 100 ms |
| NFR-6: answer → leaderboard at every participant | wait for next tick (≤ 200 ms, avg 100) + snapshot + publish (≈ 2 ms) + pub/sub delivery (≈ 1 ms) + fan-out to local sockets (≈ 10–50 ms) | **≈ 260 ms worst case** | p95 < 500 ms |
| NFR-8: question open → every participant | transition + publish + fan-out | **≈ 50 ms** | p95 < 200 ms spread |
| NFR-9: join → snapshot | join script + snapshot (+ one database read if this gateway hasn't cached the question set yet) | **≈ 5–20 ms** | p95 < 300 ms |

### 2.2 Techniques that keep it fast

- **Nothing slow on the answer path.** Correctness comes from the in-memory question cache. One Redis script does everything else. PostgreSQL is never touched (FR-33).
- **Encode once, write many (NFR-5c).** Workers publish ready-to-send bytes. Gateways wrap them once in a gorilla prepared message and write the same frame to every local socket in the room.
- **Batching.** Leaderboard updates are coalesced per room (≤ 5/s instead of one per answer). Presence refreshes and personal-rank lookups are pipelined. Answer history is written in one batch per question.
- **Registry indexed by room (NFR-5a).** A broadcast touches only that room's connections, and no lock is held during socket writes (architecture §4).
- **Backpressure isolation (NFR-5d, NFR-17).** Each connection has a bounded queue and its own writer goroutine, so a slow client can't slow anyone else down.
- **Scripts are loaded once and called by hash (`EVALSHA`)**, so script source isn't resent on every call.

## 3. Reliability

### 3.1 Failure modes

| Failure | What happens | Recovery | Requirement |
|---|---|---|---|
| **Gateway crashes** | Its sockets drop | Clients reconnect with jittered backoff to any gateway and get a snapshot at the room's current point. Its presence entries expire after 30 s. The room is unaffected | FR-29, NFR-14 |
| **Answer applied but reply lost** (crash or network) | Client doesn't know if its answer counted | Client resends with the same request ID. While the question is open it gets the stored original result. After the close, its snapshot total already includes it | FR-18, FR-30 |
| **Worker crashes** | Its claimed work stops | Other workers pick up due transitions and leaderboard ticks on their next poll (≤ 100–200 ms). Flush jobs it held become due again after 30 s | NFR-14, NFR-13a |
| **All workers down** | Quizzes stop advancing; answers are still recorded until each deadline | Deadlines are stored, so transitions resume on restart. The answer script still enforces deadlines, so nothing can be submitted late. **Paging alert:** no healthy workers | NFR-14 |
| **Redis primary fails** | Seconds of unavailability during failover | Sentinel or managed failover to the replica. See §3.2 for the data loss window | NFR-13, NFR-15 |
| **Redis unreachable** | Nothing live can be read or written | Gateways reject answers with a retryable error (never a false ack). Readiness fails, so the load balancer stops sending new connections. On reconnect, gateways resubscribe and push a fresh snapshot to every local room | NFR-15 |
| **Pub/sub message missed** (gateway's Redis connection blips) | Some sockets miss an update | Every room message is a full snapshot with a version, so the next one heals it. After resubscribing, the gateway also pushes a fresh snapshot itself | FR-28 |
| **PostgreSQL down** | Flushes fail; new question sets can't be loaded | Running quizzes continue (PostgreSQL isn't on the live path). Flushes retry with backoff while Redis keeps the data and its TTL is extended. **Question-set loads retry with capped exponential backoff and jitter.** Concurrent joins share one in-flight load (single-flight), so a burst of joins doesn't multiply the retries. If the retry budget runs out, the join fails with a retryable error and the client backs off too. Creating a quiz fails the same way | NFR-13a, NFR-18 |
| **Slow client** | Its outbound queue fills | Queue dropped and resynced with a fresh snapshot, or disconnected if it stays behind | NFR-17 |
| **Reconnect storm** (e.g. 20,000 clients after a gateway dies) | Burst of joins on the survivors | Client backoff with jitter, plus a per-gateway join admission limit that sheds excess with "retry later" | NFR-17 |
| **Deploy / scale-in** | Gateways restart | Graceful drain: stop accepting, send close code 1012 ("service restart") to clients in jittered batches over ≤ 30 s, then exit. Workers stop claiming and finish their current job | NFR-16 |
| **Clock skew between machines** | — | No effect on correctness: deadlines and receive times come from Redis `TIME`. Local clocks only decide *when to poll* | FR-17 |

### 3.2 Redis durability: the replication gap

Redis replicates to its replica **asynchronously**: it replies before the replica has the write. If the primary dies, the last few milliseconds of acknowledged answers may be missing on the promoted replica. That conflicts with NFR-13 (an ack means the answer is recorded).

| Option | Effect | Cost |
|---|---|---|
| Accept the gap | Simplest | Rare lost answers on failover |
| **`WAIT 1 50` after each answer script (recommended)** | The gateway acks only after at least one replica confirms, waiting at most 50 ms. Shrinks the loss window to failures during those few milliseconds | ≈ 1 ms added per answer, within NFR-7 |
| If `WAIT` times out (replica down) | Ack anyway, but count it as `answers_acked_unreplicated` and alert | We prefer staying available over rejecting every answer while the replica is down |

`WAIT` reduces the loss window but doesn't make Redis strongly consistent, so this is a documented trade-off, not a guarantee. The primary also runs with AOF (`appendfsync everysec`) so a plain restart loses at most about a second. The reconciliation check at finish (FR-35) detects any answer that was acknowledged but not scored.

**Decision: ship the simple `WAIT 1 50` version first**, and treat stronger replica durability as follow-up research. Starting points:

| Direction | Idea | To check |
|---|---|---|
| Async retry with backoff | Ack after the primary write, then track unconfirmed answers and re-verify them against the replica in the background, repairing on failover | Complexity vs. benefit; how the repair interacts with the leaderboard |
| `min-replicas-to-write` / `min-replicas-max-lag` | The primary refuses writes when no replica is recent enough, so an ack can't come from an isolated primary | Turns replica problems into answer rejections; needs a clear retryable error |
| `WAITAOF` (Redis 7.2+) | Wait until the write is fsynced to the append-only file locally and/or on replicas | Latency cost of fsync on the answer path |
| Durable Redis-compatible services (e.g. AWS MemoryDB) | Writes are acknowledged only after a multi-zone transaction log commits | Latency, cost, and feature parity with our Lua scripts |

## 4. Observability

### 4.1 Metrics (Prometheus, NFR-27)

| Metric | Type | Service | Why |
|---|---|---|---|
| `ws_connections` | gauge | gateway | Capacity per instance (NFR-3) |
| `ws_joins_total{result}` | counter | gateway | Join failures, reconnect storms |
| `answers_total{result}` (accepted, duplicate, late, invalid) | counter | gateway | Scoring behaviour; spikes of `late` suggest clock or latency problems |
| `answer_result_seconds` | histogram | gateway | NFR-7 |
| `leaderboard_delivery_seconds` (answer accepted → written to socket) | histogram | gateway | NFR-6, measured with the version and a timestamp in the message |
| `question_delivery_spread_seconds` | histogram | gateway | NFR-8 fairness |
| `broadcast_fanout_seconds`, `broadcast_recipients` | histograms | gateway | Cost of writing to a room's sockets |
| `ws_outbound_dropped_total`, `ws_slow_client_disconnects_total` | counters | gateway | NFR-17 |
| `transition_lag_seconds` (actual − scheduled) | histogram | worker | Quiz clock health, worker capacity |
| `scheduler_claims_total{kind,result}` | counter | worker | Transition, tick, flush throughput; lost races are normal |
| `flush_pending`, `flush_failures_total` | gauge, counter | worker | NFR-13a |
| `score_reconciliation_mismatches_total` | counter | worker | FR-35. **Must be 0** |
| `redis_command_seconds{op}`, `postgres_query_seconds{op}` | histograms | all | Dependency latency |
| `answers_acked_unreplicated_total` | counter | gateway | §3.2 |
| Standard Go runtime metrics (goroutines, heap, GC) | — | all | Memory per connection (NFR-4) |

Quiz IDs are **never** metric labels (unbounded cardinality). Per-quiz detail goes in logs and traces.

### 4.2 Logs and traces

- **Logs (NFR-28):** `log/slog` JSON with `quiz_id`, `participant_id`, `conn_id`, `request_id`, and `service`. Logged: state changes, rejections with their reason, failures. Not logged per message on the hot path, except sampled at debug level.
- **Traces:** OpenTelemetry spans on the answer path (socket receive → script → reply) and the broadcast path (tick → publish → per-gateway fan-out). Sampled to keep overhead low. The trace ID is carried in the pub/sub message, so one trace spans worker and gateways.

### 4.3 Health endpoints (NFR-29)

- `/healthz`: the process is alive.
- `/readyz`: Redis reachable (and PostgreSQL for the API). A gateway that isn't ready gets no new connections. A draining gateway reports not ready.

### 4.4 Alerts (NFR-30)

| Alert | Condition | Severity |
|---|---|---|
| No healthy workers | healthy worker count = 0 for 30 s | page |
| Leaderboard latency SLO | NFR-6 p95 > 500 ms for 5 min | page |
| Answer latency SLO | NFR-7 p95 > 100 ms for 5 min | page |
| Transition lag | p95 > 1 s for 5 min | page |
| Score reconciliation mismatch | any increase | page (correctness bug) |
| Redis CPU | > 70% for 10 min | ticket: plan sharding (§1.4) |
| Flush backlog | `flush_pending` rising for 15 min | ticket |
| Unreplicated acks | any increase | ticket: replica health |
| Gateway near capacity | connections > 80% of target | ticket: scale out |

### 4.5 Dashboards

1. **Room health:** connections, joins, answers by result, the three latency histograms, drops.
2. **Quiz clock:** transition lag, claims by kind, flush backlog, reconciliation.
3. **Dependencies:** Redis and PostgreSQL latency, Redis CPU and memory, replication.

## 5. Security

| Concern | Measure | Requirement |
|---|---|---|
| Identity spoofing | Signed tokens carry the participant ID and role. The server never trusts IDs sent in messages. Host commands require the `host` role | NFR-19 |
| Seeing answers early | Answer keys live only in server memory. Only `question_closed` reveals the correct option, after the deadline | FR-21, NFR-22 |
| Answering late | The answer script checks Redis `TIME` against the deadline itself | FR-17 |
| Malformed or oversized input | Schema validation, 4 KB message limit, unknown types rejected | NFR-20 |
| Flooding | Per-connection rate limit (20 msgs/s, burst 40); join admission limit per gateway | NFR-21 |
| Cross-site WebSocket hijacking | Origin check on upgrade; TLS at the load balancer | NFR-23 |

## 6. Maintainability

- **Module layout (D9):** each module has a pure `domain` core, a `service`, and a `repository` interface with a Redis or PostgreSQL implementation, mocked with gomock in unit tests. Three thin `cmd/` entry points (D11).
- **Rules live in one place:** scoring rules and the state machine are pure Go and unit-tested directly. Redis scripts are small, one per operation, and tested against real Redis.
- **Tests (NFR-32, D14):** four levels: domain unit tests, service unit tests with gomock failure injection, integration tests on real Redis and PostgreSQL with Toxiproxy fault injection, and end-to-end/load tests. Every must-have FR maps to at least one test.
- **Contracts (NFR-33, D13):** OpenAPI for REST, AsyncAPI + JSON Schema for WebSocket messages, written before the code; types generated from them; contract tests catch drift.
- **Config (NFR-34):** environment variables, validated at startup. Tick and poll intervals, TTLs, and limits are all configurable.

## 7. Trade-offs

| Decision | Gain | Cost |
|---|---|---|
| All live state in Redis; stateless services (D10) | Any instance serves any quiz; failures lose no quiz state; no sticky sessions | Every operation pays a Redis round trip (≈ 1 ms); Redis is the one shared dependency |
| One atomic script per operation | Exactly-once scoring and transitions without locks | All answers of a room go through one Redis thread (hot key); logic split between Go and Lua |
| Ownerless scheduler (polling) | No leases or takeover; any worker can die | Up to 100 ms extra per transition; a little constant Redis polling load |
| Shared leaderboard payload (FR-26) | Encode once; cost independent of room size | Personal rank arrives only at question close, not live |
| Leaderboard coalescing (200 ms) | Bounded broadcast rate (≤ 5/s per room) | Up to 200 ms added to NFR-6 |
| Pub/sub, not streams | Simple, low latency | Messages can be missed; healed by versioned snapshots |
| Early release of answer records (FR-34) | Redis memory per room doesn't grow with quiz length | A retry after the flush gets `question_closed` instead of the original result |
| Three services (D11) | Independent scaling, contained failures | More deployables; no workers means no quiz progress |
| Server receive time decides lateness | One authoritative, cheat-proof rule | Participants with slower networks get slightly smaller speed bonuses |
| `WAIT 1` for answers (§3.2) | Much smaller loss window on failover | ≈ 1 ms per answer; not a strict guarantee |
