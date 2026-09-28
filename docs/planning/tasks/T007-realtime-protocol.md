# T007: Real-time protocol contract v1

- **Phase:** P1
- **Status:** todo
- **Depends on:** T002, T004
- **Brief refs:** Part 2.2

## Goal

Freeze the client ↔ server contract so the server and the client (or a mock client) can be built independently.

## Deliverable

`docs/api/realtime-protocol.md`, plus Go structs in `server/internal/realtime` and matching TS types in `client/src/protocol`. Decide here whether the TS types are hand-written or generated (e.g. from JSON Schema), and how drift between the two is caught.

## Acceptance criteria

- [ ] Envelope format: `type`, `id` (for request/ack correlation), `payload`, protocol version
- [ ] Client → server messages: `join`, `submit_answer`, `ping`, and host-only `start_quiz`
- [ ] Server → client messages: `joined` (snapshot), `question` (with server `deadline`), `question_closed` (correct answer), `answer_result`, `leaderboard`, `quiz_state`, `error`, `pong`
- [ ] Clock rule: clients render countdowns from the server `deadline` plus a clock offset estimated from ping/pong; they never decide whether an answer is late
- [ ] Error codes: unknown quiz, quiz finished, question closed, duplicate answer, invalid payload, rate limited, unauthorized
- [ ] Ordering and versioning rules (leaderboard `version`, handling stale messages)
- [ ] Any REST endpoints (create quiz, health, metrics)
- [ ] Example payload for every message

## Notes and decisions

## Verification

- Schemas validated in code; invalid-payload tests in T016.

## AI collaboration
