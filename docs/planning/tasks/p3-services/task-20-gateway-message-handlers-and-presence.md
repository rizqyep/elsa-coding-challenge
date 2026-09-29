# task-20: Gateway: message handlers and presence

- **Phase:** P3 Services
- **Size:** M · **TDD:** yes
- **Status:** done
- **Implements:** TRD §7.3, §7.7, §9.5, §9.6 · FR-8–FR-12, FR-16–FR-21, FR-29, FR-30
- **Depends on:** [task-19](task-19-gateway-registry-and-room-events.md)
- **Unblocks:** [task-22](task-22-shutdown-and-degraded-modes.md), [task-23](task-23-observability.md), [task-24](../p4-client/task-24-react-client.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- join registers and subscribes **before** reading the snapshot
- watch requires the host
- one quiz per connection
- `invalid_option` / `wrong_question` resolved from the cache without Redis
- error mapping table (§9.6)
- presence refresh uses Redis `TIME`

## Steps

- [x] Test: join registers and subscribes **before** reading the snapshot
- [x] Tests: allowed messages per connection state; watch requires the host; one quiz per connection
- [x] Tests: `invalid_option` and `wrong_question` resolved from the cache without calling Redis
- [x] Tests: error mapping table (TRD §9.6); `errors_total{code}` incremented once per error
- [x] Implement the dispatcher and handlers for join, watch, submit_answer, ping (server time aligned to Redis `TIME`)
- [x] Implement the presence refresher (one script call per room, Redis `TIME`)
- [x] `cmd/ws` wiring; run end to end against Redis (smoke run with two WebSocket clients; the browser-tab check moves to task-24)

## Done when

- [x] join in the order the task-19 end-to-end test proved: look up the room's question set → `Hub.Enter` (waits for the confirmed subscription) → `join` script → snapshot from the join result via `realtime.BuildSnapshot`; a finished quiz gets its final results instead
- [x] join publishes `kick {p, k}` so other gateways close this participant's older connections (FR-12); the hub already handles receiving it
- [x] `Handler.Snapshot` → `Hub.Snapshot`, `Handler.Closed` → `Hub.Leave`
- [x] `cmd/ws` wires the hub, the subscriber (`Run` in its own goroutine), the session repository, and the question cache
- [x] `cmd/ws` runs end to end against Redis
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- **Join burst cost:** a room this gateway already has skips the room lookup (the registry knows its question set), so N joins cost 1 room read + N `join` scripts per gateway. The own-answer read runs only when a question is open or closed, so a lobby burst never pays for it.
- **Kick inside `join`:** the script publishes `{t: kick, p, k: conn_id}` itself, instead of a separate `PUBLISH`, so replacing older connections costs no extra round trip.
- **Roles:** a host token can't `join` (the host doesn't play); a participant token can't `watch`; `watch` also checks `host_id`. One quiz per connection (`already_joined`). A failed join leaves the room, so a retry on the same connection works.
- **Finished quiz (FR-13):** never joined. While the room is in Redis the reply is a finished snapshot with the room's real version; once released it comes from the archived results (`leaderboard.Final`, one read for the top 10 and the caller's own row) with version 0. The contract now says a finished state is terminal and applied whatever its version. The Redis-side reply uses the name the participant just sent, because standings carry no names and adding them would slow the per-question rank fan-out.
- **Errors:** one mapping (`classify`) for TRD §9.6. Every error sent, including connection-level ones, goes through one point that counts `errors_total{service, code}` and logs once with quiz, participant, connection and request IDs; the cause is logged, never sent. `server_busy` carries `retryAfterMs`.
- **Timeouts (TRD §9.2):** 500 ms per Redis call, the question-set load keeps its 10 s budget, 2 s for the archived read, 250 ms for the answer script.
- **Presence:** one script call per room per 500 participants, stamped with Redis `TIME`; a released room's online set is never recreated. `pong` uses the gateway's offset from Redis `TIME`.
- **Contract fixes found by validating frames:** `pong` now allows the echoed `id` (every other reply already did); the finished-quiz snapshot needed a display name.
- **Subscriber stop bug (from task-19):** a blocked `ReceiveTimeout` ignores cancellation, so stopping a gateway waited up to the 15 s health interval. The pub/sub connection is now closed on cancel. Found because the new gateway tests took 15 s and 30 s; the task-19 tests used a 200 ms interval, which hid it.
- **Follow-up, not fixed here:** WebSocket `invalid_message` errors include validator internals (the embedded schema URL), the same leak task-16 fixed for REST.
- **Codes:** the WebSocket contract only accepts uppercase codes, so the client (task-24) uppercases what people type.

## Verification

- Handler unit tests (fakes for every dependency): join order (subscribe before the join script), burst path, roles, one quiz per connection, retry after failure, finished and archived replies, cache-only answer checks, error mapping, metric counting, pong time, presence per room. Every frame is checked against the JSON Schemas.
- Integration: `join` kick and `presence` scripts, `leaderboard.Final`, the task-19 end-to-end test now on the real handler, and a new `cmd/ws` test through the real wiring: watch, three joins, question, correct/wrong/duplicate/invalid answers, leaderboard, reveal and ranks, finish, jobs, then a late join served from the archive; plus a kick across two gateways.
- 29 deliberate breaks, all caught: 5 on the scripts, 21 on the handler and helpers, 3 on the wiring and the subscriber fix.
- `make check` green; all 17 integration packages pass (full logs saved).
- Smoke run of the real `api` and `ws` binaries against Compose with Bun WebSocket clients: readiness 200, host watch, two "tabs" joined, pong, `invalid_option`, a third tab with tab A's identity closed tab A with 4000, `errors_total` exported. The two-browser-tab check waits for the client (task-24).

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
