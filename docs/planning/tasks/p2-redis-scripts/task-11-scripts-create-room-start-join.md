# task-11: Scripts: `create_room`, `start`, `join`

- **Phase:** P2 Redis scripts (TDD)
- **Size:** M · **TDD:** yes
- **Status:** done
- **Implements:** TRD §4.2, §4.4 · FR-1, FR-3, FR-8–FR-13
- **Depends on:** [task-10](task-10-integration-test-harness.md)
- **Unblocks:** [task-12](task-12-script-answer.md), [task-16](../p3-services/task-16-rest-api-service.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- create writes every field and schedules expiry
- start: only host, only lobby, needs a participant, idempotent
- join: unknown/expired/finished paths, rejoin keeps score, **1,000 concurrent joins → no loss, no duplicates**

## Steps

- [x] Tests `create_room`: every field written, TTLs set, expiry scheduled; `code_in_use` when the room exists
- [x] Tests `start`: only host, only lobby, needs a participant, idempotent
- [x] Tests `join`: unknown / expired / finished paths; rejoin keeps score and updates the name; snapshot contents
- [x] Concurrency test: 1,000 concurrent joins → roster = leaderboard = 1,000
- [x] Write the three Lua scripts
- [x] Go repository wrappers decoding script replies into domain types

## Done when

- [x] scripts + Go repository wrappers
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- Scripts live with their use case: `create_room.lua` and `start.lua` in `quiz`, `join.lua` in `session` (TRD §1.4). Each module exposes `Scripts()` for loading at startup.
- **`start.lua` checks the host before the lobby status**, following the approved `quiz.Start`, not the order sketched in TRD §4.4. A parity test runs the same cases through both. TRD §4.4 updated.
- `quiz.RoomRecord` = the state machine's `Room` + code, question set, current question ID, leaderboard version, pending flushes, created-at; `ParseRoomHash` reads the Redis hash and fails on missing fields.
- `leaderboard.Entry` and `leaderboard.TopN = 10`. Snapshot ranks are computed in Go from the top-10 scores, which is exact because the top 10 is the head of the ranking. Own rank = 1 + `ZCOUNT` of strictly higher scores.
- `join` re-applies the TTL on each join and updates the display name on rejoin (FR-12); a finished quiz is reported without adding the participant.

## Verification

- 5 quiz + 8 session integration tests pass under `-race` against real Redis; full `make test-integration` and `make check` green.
- **Parity:** `TestStart_MatchesDomainRules` runs 6 cases through `quiz.Start` (Go) and `start.lua`, comparing errors and the resulting room.
- **A hang, caused by my own test:** the first run of the concurrent-join test hung until Go's 10-minute test timeout. The cause was an error channel buffered for 1,000 sends while 1,020 goroutines sent; the last 20 blocked, so `wg.Wait()` never returned. Fixed the buffer; the test now takes 0.14 s. The hung run's containers were reaped by testcontainers' Ryuk, and `make test-integration`'s timeout was lowered to 5 minutes so hangs surface sooner.
- **Mutation checks (Lua):** `ZADD` without `NX` in `join` → a rejoin reset the score to 0, caught; `start` checks swapped → the parity test fails on "not the host and not in the lobby". Both restored.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
