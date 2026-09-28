# T011: Leaderboard and broadcast

- **Phase:** P2
- **Status:** todo
- **Depends on:** T010

## Goal

Every participant sees current standings, updated promptly (R4).

## Acceptance criteria

- [ ] Leaderboard stored as a Redis sorted set per quiz, with the T002 tie-break encoded
- [ ] Broadcasts coalesced per quiz (one update per window, not one per answer)
- [ ] Payload: top N + recipient's own rank, with a monotonically increasing `version`
- [ ] Full leaderboard available on request / at quiz end
- [ ] Tests: ordering, ties, coalescing under a burst of answers

## Notes and decisions

## Verification

## AI collaboration
