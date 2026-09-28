# T003: Architecture diagram and components

- **Phase:** P1
- **Status:** todo
- **Depends on:** T002
- **Brief refs:** Part 1: Architecture Diagram, Component Description

## Goal

One diagram and one page that explain how the whole system fits together. This covers the full production design, not only the part being built.

## Deliverable

`docs/system-design/architecture.md` (Mermaid source in the file, so it renders on GitHub and diffs cleanly)

## Acceptance criteria

- [ ] Diagram shows: clients, load balancer, WebSocket gateway/quiz service instances, Redis (leaderboard + pub/sub), durable DB, quiz content service, auth, observability stack
- [ ] Built / thin / mocked components are visibly marked in the diagram
- [ ] Each component described in terms of: responsibility, state it owns, how it scales, what happens when it fails
- [ ] Draws a clear boundary: which component is authoritative for scores and for the leaderboard

## Components to cover (starting list)

| Component | Role |
|---|---|
| Client (web) | Joins the quiz, sends answers, renders questions and the leaderboard |
| Load balancer | TLS, WebSocket upgrade, spreading connections across instances |
| Real-time quiz server | Connection handling, session join, answer validation, scoring, broadcast |
| Redis | Leaderboard sorted sets, answer dedup keys, pub/sub for cross-instance fan-out |
| Durable store (mocked) | Final results, quiz history |
| Quiz content service (mocked) | Questions and answer keys |
| Auth (mocked) | Issues a signed identity the server trusts |
| Observability | Metrics, logs, traces |

## Open questions

- Should the host (quiz master) be a separate client role in the diagram? Depends on D2.

## Notes and decisions

## Verification

- Walk the diagram against each FR from T002: every FR has a component that owns it.

## AI collaboration
