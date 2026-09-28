# T010: Scoring engine

- **Phase:** P2
- **Status:** todo
- **Depends on:** T009

## Goal

Accurate and consistent scoring (R3). This is the correctness core of the submission.

## Acceptance criteria

- [ ] Implements the scoring rules from T002 exactly
- [ ] One accepted answer per (quiz, question, user), enforced atomically in Redis, not just in process memory
- [ ] Score increment and dedup in a single atomic operation (Lua script or equivalent)
- [ ] Uses server receive time; rejects answers after the question closes
- [ ] Sender gets `answer_result` (correct/incorrect, points, new total)
- [ ] Pure scoring function separated from I/O so it can be unit-tested
- [ ] Tests: duplicate submits, concurrent submits from one user, late submits, a submit that races with question close

## Notes and decisions

## Verification

## AI collaboration
