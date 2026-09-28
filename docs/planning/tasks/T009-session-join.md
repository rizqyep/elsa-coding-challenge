# T009: Session join and presence

- **Phase:** P2
- **Status:** todo
- **Depends on:** T008

## Goal

Users join a quiz by ID, and many users can be in the same quiz at once. (R1, R2)

## Acceptance criteria

- [ ] Join validates identity (mocked auth) and quiz ID
- [ ] Joining user gets a snapshot: quiz state, current question (without answer), leaderboard
- [ ] Participant list / count broadcast to the quiz
- [ ] Rejoining with the same identity reuses the participant; nothing is duplicated
- [ ] Errors for unknown or finished quiz, per the protocol

## Notes and decisions

## Verification

## AI collaboration
