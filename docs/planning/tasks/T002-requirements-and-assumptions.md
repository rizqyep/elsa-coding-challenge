# T002: Requirements, rules, and scale assumptions

- **Phase:** P1
- **Status:** todo
- **Depends on:** T001
- **Brief refs:** Acceptance Criteria 1–3, Part 2.4

## Goal

Turn the brief's short acceptance criteria into precise, testable rules. Everything later (protocol, scoring code, tests, load test) is checked against this document.

## Deliverable

`docs/system-design/requirements.md`

## Acceptance criteria

- [ ] **Functional requirements** restated as numbered, testable statements (FR1…)
- [ ] **Quiz lifecycle** (D2: host starts, server-synchronized) defined as a state machine: `lobby → question_open → question_closed → … → finished`
  - host command: `start` only, valid only in `lobby`
  - every transition after `start` is driven by server deadlines (question duration, reveal/intermission duration)
  - which transitions are valid from which state; invalid commands get an error
- [ ] **Scoring rules** written out exactly:
  - points for a correct answer (flat, or time-weighted using the **server** receive time)
  - wrong, late, and duplicate answers
  - tie-breaking on the leaderboard
- [ ] **Consistency rules**: one accepted answer per (quiz, question, user); the server is authoritative; the answer key never reaches the client before the question closes
- [ ] **Non-functional targets** with numbers (proposal, to be confirmed):
  - participants per quiz: up to 1,000
  - concurrent quizzes: 10,000 (design), measured single-machine figure for the demo
  - answer received → leaderboard delivered: p95 < 300 ms within a region
  - broadcast coalescing window: ≤ 250 ms
- [ ] **Assumptions and mocks** listed: auth, question bank, persistence
- [ ] **Out of scope** listed

## Open questions

- Can a user join after the quiz has started? Proposal: yes, with score 0, joining at the current question.
- Leaderboard size in broadcasts: full list, or top N plus the user's own rank? Proposal: top 10 + own rank, with the full list available on request.
- Display names: unique per quiz, or not?
- Can the host also play? Proposal: no; the host only starts the quiz and is not on the leaderboard.
- Close a question early once every connected participant has answered? Proposal: yes. It shortens idle time, especially in the demo, and it is still a server decision.
- Lobby with nobody but the host, or a quiz that is never started: auto-expire after an idle timeout? Proposal: yes.
- Who creates a quiz and gets the host role? Proposal: mocked REST `POST /quizzes` returns the quiz ID and a host token.

## Notes and decisions

## Verification

- Every FR is traceable to at least one test in T016.

## AI collaboration
