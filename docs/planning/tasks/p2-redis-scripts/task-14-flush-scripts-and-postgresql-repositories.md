# task-14: Flush scripts and PostgreSQL repositories

- **Phase:** P2 Redis scripts (TDD)
- **Size:** M · **TDD:** yes
- **Status:** todo
- **Implements:** TRD §4.4, §4.5, §5.2–5.4 · FR-27a, FR-32–FR-35, NFR-13a, NFR-18
- **Depends on:** [task-13](task-13-scripts-transition-leaderboard.md)
- **Unblocks:** [task-16](../p3-services/task-16-rest-api-service.md), [task-21](../p3-services/task-21-worker.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- claim with visibility timeout (two workers → one claim)
- `flush_ack` decrements only once
- batch insert is idempotent
- **commit fails → answers not deleted** (gomock)
- finalise waits for `pending_flush = 0`
- reconciliation detects a planted mismatch
- `release` deletes all room keys

## Steps

- [ ] Tests `claim`: two workers → one claim; claimed job due again after the visibility timeout
- [ ] Tests `flush_ack`: `pending_flush` decrements once even when acked twice
- [ ] Test: batch insert twice → no duplicates; gomock: commit fails → `flush_ack` never called
- [ ] Tests finalise: waits while `pending_flush > 0`; reconciliation detects a planted mismatch; `release` deletes every room key
- [ ] Write `claim.lua`, `flush_ack.lua`, `release.lua`
- [ ] `history` PostgreSQL repository (`unnest` insert, results transaction with reconciliation) and services for both jobs

## Done when

- [ ] scripts + `history` repositories and services
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
