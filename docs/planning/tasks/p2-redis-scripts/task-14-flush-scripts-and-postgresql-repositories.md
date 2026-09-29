# task-14: Flush scripts and PostgreSQL repositories

- **Phase:** P2 Redis scripts (TDD)
- **Size:** M · **TDD:** yes
- **Status:** done
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

- [x] Tests `claim`: two workers → one claim; claimed job due again after the visibility timeout
- [x] Tests `flush_ack`: `pending_flush` decrements once even when acked twice
- [x] Test: batch insert twice → no duplicates; gomock: commit fails → `flush_ack` never called
- [x] Tests finalise: waits while `pending_flush > 0`; reconciliation detects a planted mismatch; `release` deletes every room key
- [x] Write `claim.lua`, `flush_ack.lua`, `release.lua`
- [x] `history` PostgreSQL repository (`unnest` insert, results transaction with reconciliation) and services for both jobs

## Done when

- [x] scripts + `history` repositories and services
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- `history` holds two interfaces, `LiveStore` (Redis) and `Store` (PostgreSQL), with Redis/PostgreSQL implementations and gomock mocks. `Service.Process(job)` runs a claimed job:
  - **flush:** refresh TTLs → read answers → batch insert → acknowledge. The acknowledgement happens only after the insert commits (FR-34). With nothing to save (already flushed, or nobody answered), it just acknowledges.
  - **final:** while flushes are pending, defer the job by `FinalRetryDelay`. Otherwise read the whole leaderboard, rank it, and in one transaction write results + reconcile + mark the quiz finished/expired; report mismatches through `OnMismatch` (for the FR-35 metric); release the room. A room that's already gone drops its job instead of retrying forever.
- Lua: `claim` (due jobs, oldest first, pushed ahead by the retry timeout), `flush_ack` (the counter decrements only if the job was still queued), `release`. Malformed job IDs are removed on claim.
- Reconciliation also flags answers from participants missing from the leaderboard (live total 0).
- Integer conversions for PostgreSQL `int` columns are bounded (points ≤ 200, ranks ≤ participants); the two gosec warnings are annotated with that reason.

## Verification

- 10 unit tests (gomock, order enforced with `InOrder`) + 15 integration tests on real Redis and PostgreSQL pass under `-race`; `make check` and `make test-integration` green.
- **Real failure paths:** PostgreSQL made unreachable through Toxiproxy mid-flush → answers and the pending counter stay in Redis, and the retry after recovery saves everything. A simulated crash between commit and acknowledgement → the retry inserts no duplicates. The final job defers while a flush is pending, then writes 3 results (including a participant who never answered) with 0 mismatches and releases the room.
- **Mutation checks (all caught, all restored):**
  1. `flush_ack` decrementing unconditionally → `pending_flush = -1`;
  2. acknowledging despite a failed save → the outage test fails;
  3. `claim` not hiding claimed jobs → caught;
  4. finalizing without waiting for pending flushes → caught.
- **Test fixed after mutation 3:** it was caught as a **190-second hang** (the worker loop re-claimed the same jobs forever), not a failure. The loop is now bounded with a clear message, and the same mutation fails in about 7 s.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
