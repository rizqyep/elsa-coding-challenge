# T006: Scalability, performance, reliability, observability

- **Phase:** P1
- **Status:** todo
- **Depends on:** T003, T004
- **Brief refs:** Part 2.4: Build For the Future

## Goal

Explain how the design meets each quality attribute and what it gives up to do so.

## Deliverable

`docs/system-design/non-functional.md`

## Acceptance criteria

- [ ] **Scalability:** horizontal scale of stateless gateways; per-quiz Redis keys; what breaks first at 10x load and the next step (Redis Cluster sharding by quiz ID, dedicated fan-out tier)
- [ ] **Performance:** broadcast coalescing, top-N payloads, O(log N) sorted set updates, backpressure on slow sockets, message size budget
- [ ] **Reliability:** idempotent answer submission, atomic score updates (Lua / MULTI), reconnect with snapshot, heartbeats, graceful shutdown and connection draining, Redis failure behaviour
- [ ] **Maintainability:** module boundaries, typed protocol, tests, linting
- [ ] **Observability:** the metrics list (connections, joins/s, answers/s, answer→broadcast latency, broadcast fan-out time, Redis latency, dropped/slow sockets), structured logs with quiz/user/request IDs, tracing plan, dashboards and alerts
- [ ] **Trade-offs table:** decision → what we gain → what we give up

## Notes and decisions

## Verification

- Each claim here is either implemented (link to code) or clearly labelled "design only".

## AI collaboration
