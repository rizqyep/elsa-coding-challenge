# Testing and load results

How the system is tested, how to run each layer, and what the load runs measured (task-29). The raw reports are in [`loadtest/results/`](../loadtest/results/).

## 1. Test layers

| Layer | What it proves | Run |
|---|---|---|
| Unit (Go, with gomock) | Domain rules, scoring vectors, state machine, handlers, config, protocol contract | `make check` |
| Unit (client, Vitest) | Room reducer version rules, socket reconnect and resend, close-code policy | `make check` |
| Integration (real Redis, PostgreSQL, Toxiproxy via testcontainers) | Every Lua script, the Go↔Lua shared vectors, concurrency races, answer saving, each service's real wiring | `make test-integration` |
| End-to-end and faults (the running stack) | Six end-to-end rows and seven fault scenarios, each with independent score checks | `make up PROFILES=chaos && make test-e2e` |
| Load and simulation | The NFR targets, at 5,000 and 10,000 participants | `make sim SCENARIO=…`, `make k6` |

Every room run, in the end-to-end tests and the simulator alike, recomputes each answer's points from what the client sent and the server's `receivedAt`, compares every participant's total with the archived result in PostgreSQL, checks that `score_reconciliation_mismatches_total` stays 0, validates messages against their JSON Schemas, and flags any answer accepted after its question closed.

## 2. Machine

All runs used one laptop: AMD Ryzen 5 4600H (6 cores, 12 threads), 16 GB RAM, Fedora (kernel 7.1.10), Docker 29.7.2, Go 1.26.8. The stack (nginx, 1 API, 2 gateways, 2 workers, Redis, PostgreSQL) and the load generator share the machine. Unless noted, the simulator connected straight to nginx's container address, not through the published port (§4.3 explains why).

## 3. Results

| Scenario | Shape | Result | Key numbers (p95) | Report |
|---|---|---|---|---|
| `big-room` | 1 room × 5,000, answers within 2 s, 3 questions | **PASS** | NFR-6 288 ms · NFR-7 50 ms (p99 84) · NFR-8 133 ms · NFR-9 1.4 ms | `big-room-20260929-190428Z` |
| `big-room` | 1 room × 10,000 | correct, **latency FAIL** | NFR-6 3.25 s · NFR-7 1.11 s · NFR-8 461 ms · NFR-9 11 ms | `big-room-20260929-185321Z` |
| `big-room` | 1 room × 10,000, 4 gateways | correct, latency FAIL | NFR-6 1.93 s · NFR-7 745 ms · NFR-8 476 ms · NFR-9 195 ms | `big-room-20260929-185743Z` |
| `many-rooms` | 200 rooms × 50 (10,000 clients), 100 ms apart | **PASS** | NFR-6 212 ms · NFR-7 21 ms · NFR-8 26 ms · NFR-9 4.5 ms · transition lag 99 ms | `many-rooms-20260929-190718Z` |
| `gateway-crash` | `big-room` 5,000, `ws-1` killed during question 2 | **PASS** | 2,500 clients dropped and all rejoined; slowest rejoin 5.9 s (< 15 s) | `gateway-crash-20260929-190015Z` |
| `slow-clients` | `big-room` 5,000, 5% reading at 200 ms/message | **PASS** | fast clients: NFR-6 225 ms · NFR-8 102 ms | `slow-clients-20260929-190118Z` |
| `redis-latency` | `big-room` 5,000, +20 ms on every Redis call | **PASS** | NFR-7 64 ms (p99 141) | `redis-latency-20260929-190254Z` |

**Correctness held in every run, including every failed one:** all participants joined and finished, every answer's points matched the independent recomputation, every archived total matched, and reconciliation mismatches were 0. At 10,000 in one room, 29,408 of 29,408 answers were accepted and scored correctly.

`gateway-crash`: the 2,500 clients on the killed gateway closed with 1006, reconnected through nginx to the other gateway, resent unanswered answers with their original ids, and finished with correct totals. `slow-clients`: the slow readers fell a question behind, so 430 of their answers were rejected (`question_closed` or `wrong_question`); none was disconnected, and their totals still matched.

k6 (`make k6 ROOMS=10 PER_ROOM=10`, an independent load generator): 100 players finished, 300 answers accepted, answer p95 2 ms, question delivery p95 2 ms, 0 errors.

## 4. Bottlenecks found

Found by measuring, in the order they appeared. The first one was in our own tooling, and it's recorded here because it made the system look far slower than it was.

### 4.1 The load generator itself (fixed)

The first 5,000 run failed badly: NFR-7 p95 5.3 s, NFR-6 8.3 s. The gateways' own histograms disagreed: every answer was recorded within 50 ms. `docker stats` showed the stack at 20–75% of a core per container, and `/usr/bin/time` showed the simulator using 240 CPU-seconds in 62 s. Its CPU profile: **44% JSON Schema validation**, most of the rest JSON decoding and garbage collection. Every one of 5,000 clients validated and fully decoded every message, about 25,000 leaderboard updates per second.

Fix: in the simulator, each client validates the first message of each type and then every 100th (the end-to-end tests still validate all of them), and leaderboard messages decode only their version. Simulator CPU fell from 240 s to 48 s, and the same run passed every target.

At 10,000, a second harness problem appeared: a mutex profile showed 108,000 s of accumulated waiting on one run-wide lock that every client took for every leaderboard arrival. Measurements are now kept per client and merged after the run (contention fell about 150×). That improved NFR-7 only from 3.2 s to 2.5 s, so it was real but not the main cause.

### 4.2 nginx descriptor limit (fixed)

At 10,000, joins stopped at 6,011: `socket() failed (24: No file descriptors available)`. Each proxied WebSocket holds two descriptors in nginx, and the container's limit was 1,024 per worker. Fix: `worker_rlimit_nofile 65536` and a 65,536 `nofile` ulimit on the nginx container.

**The same failure exposed a token leak.** nginx's alert line for it quoted the full request line, token included (D15). The error log had been set to `crit` in task-25, but `alert` is more severe than `crit`. It's now `emerg`, so no error-level line can quote a request. Failures still show in the access log's status codes. See §6 for the lasting fix.

### 4.3 Docker's userland port proxy (a local artifact)

Through the published port, every byte of every connection passes through `docker-proxy`, one process on the host (4 min 14 s of its CPU during the runs). Connecting straight to nginx's container address improved the 10,000 run: NFR-9 1.07 s → 11 ms, NFR-7 2.5 s → 1.1 s. A real deployment puts a load balancer in front of nginx instead, so the reported runs bypass it (`sim -base http://<nginx IP>:8080 -origin http://localhost:8080`).

### 4.4 One machine at 10,000 in one room (documented, not fixed)

After the fixes above, 10,000 in one room still missed NFR-6, NFR-7 and NFR-8. Ruled out by measurement:

- **Gateways:** 4 gateways instead of 2 barely changed the latencies, and each sat at about 40% of a core. The gateway profile shows two thirds of its CPU in socket write syscalls, one per message per connection, but it wasn't saturated.
- **Redis:** about 15% CPU, and answers were recorded with p95 ≈ 100 ms server-side.
- **nginx worker imbalance:** connections were spread evenly, about 1,650 descriptors on each of 12 workers.

What remained: sampling `/proc/stat` during an answer burst showed **the whole machine at about 95% on all 12 threads** (user 51%, system 27%, softirq 15%). The load generator, nginx, the gateways, and the kernel relaying about 50,000 messages a second over two virtual network hops all share one laptop. At this size, client-observed latency can't be separated from the machine running out of CPU. Validating 10,000 in one room needs the load generator on separate machines. The design's scaling steps (non-functional §1.4) aren't the limiting factor here.

The requirement for this build is to demonstrate 5,000 in one room on one machine (NFR-1), and that passes every target. The 10,000 design size is met for correctness and for server-side timings, not for client-observed latency on this hardware.

## 5. Measured capacity

These replace the estimates in [non-functional §1](system-design/non-functional.md).

| Quantity | Estimated | Measured |
|---|---|---|
| Answer script cost in Redis | ≈ 20–25 µs | **≈ 62 µs** (14,462 `EVALSHA` over the 5,000-player answer phase; the inner commands total ≈ 17 µs, the rest is Lua and script-call overhead) |
| Answers per second on one Redis core | ≈ 40,000 | **≈ 16,000** |
| Memory per connection (NFR-4 ≤ 50 KB) | ≈ 20–30 KB | **≈ 25 KB heap, ≈ 41 KB resident** (after a forced GC, about 5,000 connections per gateway; 2 goroutines each) |
| Leaderboard frame (NFR-11 < 2 KB) | ≈ 1 KB | **1,035 bytes** at the largest (10 entries, 20-character names) |
| Recording an answer, server side (NFR-7) | ≈ 2–5 ms | p95 ≤ 5 ms at 5,000; ≈ 100 ms at 10,000 in one burst |
| Question/leaderboard fan-out per gateway | ≈ 10–50 ms | 98% within 100 ms at 10,000 |

## 6. What's left open

- **Tokens in the WebSocket URL (D15).** Any nginx error line quotes the URL, so the only safe setting is an error log that records almost nothing. Moving the token into the `Sec-WebSocket-Protocol` header would remove the trade-off. That's a protocol change; the owner kept the query-string token for this build (the focus is the real-time path) and left the header move as a follow-up.
- **10,000 in one room needs a distributed load test** to confirm client-observed latency (§4.4).
- **Gateway writes:** each message is one write syscall per socket. If a room outgrows its gateways, batching writes per connection is the first optimisation.
