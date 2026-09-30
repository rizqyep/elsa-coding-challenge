# System design

Status: **complete** · Last updated: 2026-09-30

System design for the real-time vocabulary quiz (Part 1 of the [brief](../assignment.md)). It builds on the agreed [requirements](../planning/requirements.md) and the decisions in [context](../planning/context.md).

## Documents

| Brief section | Document | Status |
|---|---|---|
| Architecture diagram | [`architecture.md`](architecture.md) §1–2 | done |
| Component descriptions, concurrency model, Redis data model | [`architecture.md`](architecture.md) §3–6 | done |
| Data flow, join → leaderboard | [`data-flow.md`](data-flow.md) | done |
| Technologies and tools, with justification | [`tech-choices.md`](tech-choices.md) | done |
| Scalability (with the capacity ceiling), performance, reliability, observability, security, trade-offs | [`non-functional.md`](non-functional.md) | done |
| Key decisions, with options and rationale | [`../planning/context.md`](../planning/context.md#decisions) (D1–D17) | done |
| AI collaboration in design | [below](#ai-collaboration-in-design), full log in [`../ai-collaboration/`](../ai-collaboration/index.md) | done |

## The design in six points

1. **Three stateless Go services from one codebase:** a REST API, a WebSocket gateway, and a worker. Each scales on its own: gateways with connections, workers with active rooms. The load balancer needs no sticky sessions.
2. **Rooms are virtual.** A quiz room is a set of Redis keys plus one pub/sub channel, not a place on any server.
3. **No process owns a quiz.** Every worker runs the same scheduler loop; the transition script decides and applies atomically, so every transition happens exactly once.
4. **One clock and one atomic step per answer.** Deadline check, dedup, and scoring all run in a single Redis script using Redis time.
5. **Broadcast once.** Each room update is one pub/sub event, published atomically by the script that makes the change. Each gateway encodes the client message once and writes the same bytes to every local socket in the room.
6. **Redis keeps only what is live or not yet saved.** Answers are batch-written to PostgreSQL after each question closes and removed from Redis once the write is confirmed.

Diagrams are Mermaid, so they render on GitHub and diff as text. Every diagram was rendered with `mermaid-cli` to confirm it parses.

## AI collaboration in design

I designed this with Claude Code (Claude Opus 5.5) as a sparring partner. I set the direction and made every decision; the AI drafted documents, laid out options with trade-offs, and challenged both my proposals and its own. The full record is in [`../ai-collaboration/log.md`](../ai-collaboration/log.md) (AI-001 to AI-016 cover design and planning).

**Where it helped most:**
- **Finding gaps in approved designs.** A crash between a state change and its publish would have left clients uninformed, so every Lua script now publishes its own event atomically (AI-012). Splitting into three services broke the in-memory database mock, which led to real PostgreSQL (AI-008, D12).
- **Pricing ideas before adopting them.** Per-recipient ranks in every leaderboard update would cost 10,000 lookups and 10,000 encodes per tick in a large room, so live updates became one shared payload (AI-006).
- **Turning my ideas into mechanisms.** My "Redis holds the live state, rooms are virtual" idea removed the AI's own quiz-ownership and lease design; the AI added the ownerless, atomically claimed scheduler it needed (AI-005, D10). My "release answers from Redis once saved" rule became the durable-acknowledgement flush (AI-007).

**Where I corrected it:**
- It claimed the brief implied host-paced quizzes; re-reading showed the brief says nothing about pacing (AI-004).
- It over-built by default: 20 task files before requirements existed, a heavier scoring formula, a host feature set nobody asked for (AI-002, AI-006).
- It listed only technical reasons for Go; mine, that I can review the code responsibly, comes first (AI-009).

**How the design was verified:**
- Every requirement traces to the brief or is marked as our own assumption ([traceability](../planning/index.md#brief-traceability)).
- Every diagram was rendered with `mermaid-cli`, and the busy ones checked as images; that caught a diagram implying the services call each other (AI-008).
- Performance claims were written as numbers so the load runs could confirm or refute them. They did both: the answer script costs about 62 µs of Redis time, not the estimated 20–25 µs, and [`non-functional.md`](non-functional.md) now carries the measured values (AI-040).

