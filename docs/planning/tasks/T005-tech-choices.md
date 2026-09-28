# T005: Technology choices and ADRs

- **Phase:** P1
- **Status:** todo
- **Depends on:** T001 (D3–D5)
- **Brief refs:** Part 1: Technologies and Tools

## Goal

List the technology for each component and justify it against the requirements from T002, including what was considered and rejected.

## Deliverables

- `docs/system-design/tech-choices.md` (summary table)
- `docs/system-design/decisions/NNN-*.md` (one short ADR per significant choice)

## Acceptance criteria

- [ ] Table: component → technology → why → alternatives considered
- [ ] ADRs (context, decision, consequences, alternatives), at least for:
  - [ ] 001 Transport: WebSocket vs SSE vs Socket.IO
  - [ ] 002 Leaderboard store: Redis sorted set vs in-memory vs SQL
  - [ ] 003 Cross-instance fan-out: Redis pub/sub vs sticky routing vs Kafka/NATS
  - [ ] 004 Language and runtime: Go server, React + TS client (decided in D1/D3; the ADR records why)
  - [ ] 005 Go WebSocket library: `coder/websocket` vs `gorilla/websocket`
  - [ ] 006 Server code organisation (D9)
- [ ] Every "why" refers back to a requirement or target in T002, not to popularity

## Notes and decisions

## Verification

## AI collaboration
