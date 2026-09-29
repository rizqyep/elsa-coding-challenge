# task-20: Gateway: message handlers and presence

- **Phase:** P3 Services
- **Size:** M · **TDD:** yes
- **Status:** todo
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

- [ ] Test: join registers and subscribes **before** reading the snapshot
- [ ] Tests: allowed messages per connection state; watch requires the host; one quiz per connection
- [ ] Tests: `invalid_option` and `wrong_question` resolved from the cache without calling Redis
- [ ] Tests: error mapping table (TRD §9.6); `errors_total{code}` incremented once per error
- [ ] Implement the dispatcher and handlers for join, watch, submit_answer, ping (server time aligned to Redis `TIME`)
- [ ] Implement the presence refresher (one script call per room, Redis `TIME`)
- [ ] `cmd/ws` wiring; run end to end against Redis with two browser tabs

## Done when

- [ ] `cmd/ws` runs end to end against Redis
- [ ] tests pass
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
