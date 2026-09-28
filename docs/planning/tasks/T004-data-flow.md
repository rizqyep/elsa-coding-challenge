# T004: Data flow and sequence diagrams

- **Phase:** P1
- **Status:** todo
- **Depends on:** T003
- **Brief refs:** Part 1: Data Flow

## Goal

Show how data moves from the moment a user joins to the moment every participant sees the updated leaderboard, including what happens when things go wrong.

## Deliverable

`docs/system-design/data-flow.md` with Mermaid sequence diagrams

## Acceptance criteria

- [ ] **Join:** connect → authenticate → validate quiz ID → register participant → receive snapshot (state, current question, leaderboard)
- [ ] **Question lifecycle:** host starts → question broadcast → timer → close → correct answer revealed
- [ ] **Submit answer:** receive → validate (quiz state, question open, not duplicate) → score atomically → ack to sender → leaderboard update published
- [ ] **Leaderboard fan-out:** score change → coalesce → publish → every instance pushes to its local sockets
- [ ] **Reconnect:** client drops → reconnects → resumes with a fresh snapshot, no double scoring
- [ ] Each flow notes which step is the **source of truth** and where ordering matters

## Open questions

- Push full leaderboard snapshots or diffs? Proposal: snapshots with a version number. They are simpler, recover automatically, and are small enough for the top N.

## Notes and decisions

## Verification

- Each flow maps to at least one integration test in T016.

## AI collaboration
