# System design

Status: **draft for review** · Last updated: 2026-09-28

System design for the real-time vocabulary quiz (Part 1 of the [brief](../assignment.md)). It builds on the agreed [requirements](../planning/requirements.md) and the decisions in [context](../planning/context.md).

## Documents

| Brief section | Document | Status |
|---|---|---|
| Architecture diagram | [`architecture.md`](architecture.md) §1–2 | draft |
| Component descriptions | [`architecture.md`](architecture.md) §3–5 | draft |
| Data flow, join → leaderboard | [`data-flow.md`](data-flow.md) | draft |
| Technologies and tools, with justification | [`tech-choices.md`](tech-choices.md) | draft |
| Scalability, performance, reliability, observability, trade-offs | `non-functional.md` | not started |
| Key decisions | `decisions/` (ADRs) | not started |
| AI collaboration in design | [`../ai-collaboration/`](../ai-collaboration/index.md) | ongoing |

## The design in six points

1. **Three stateless Go services from one codebase:** a REST API, a WebSocket gateway, and a worker. Each scales on its own: gateways with connections, workers with active rooms. The load balancer needs no sticky sessions.
2. **Rooms are virtual.** A quiz room is a set of Redis keys plus one pub/sub channel, not a place on any server.
3. **No process owns a quiz.** Every worker runs the same scheduler loop, and version-checked Lua scripts make every transition happen exactly once.
4. **One clock and one atomic step per answer.** Deadline check, dedup, and scoring all run in a single Redis script using Redis time.
5. **Broadcast once.** Each room update is one pub/sub message, encoded once and written as the same bytes to every socket in the room.
6. **Redis keeps only what is live or not yet saved.** Answers are batch-written to PostgreSQL after each question closes and removed from Redis once the write is confirmed.

Diagrams are Mermaid, so they render on GitHub and diff as text. Every diagram was rendered with `mermaid-cli` to confirm it parses.
