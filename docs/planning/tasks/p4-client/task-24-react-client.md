# task-24: React client

- **Phase:** P4 Client
- **Size:** L · **TDD:** partial
- **Status:** todo
- **Implements:** TRD §6.3, §9.5 · FR-8, FR-11, FR-20, FR-26, FR-28, FR-29
- **Depends on:** [task-09](../p1-domain/task-09-protocol-package.md), [task-16](../p3-services/task-16-rest-api-service.md), [task-20](../p3-services/task-20-gateway-message-handlers-and-presence.md)
- **Unblocks:** [task-25](../p5-stack-and-verification/task-25-full-local-stack.md)

## Steps

- [ ] Vitest: the state reducer applies only newer versions, including snapshots
- [ ] Vitest: countdown from `closeAt` + clock offset; reconnect policy per close code; unanswered answers resent with the same `id`
- [ ] Protocol client: WebSocket wrapper with reconnect, request IDs, pending answers, ping/pong offset
- [ ] Participant view: join, lobby, question with countdown, answer, result, live leaderboard with own row, rank after close, final standings
- [ ] Host view: pick a question set, create, share code, start, watch
- [ ] Production build served by nginx; Vite dev server proxies to the stack

## Done when

- [ ] Participant view: join, question with countdown, answer, result, live leaderboard with own row, rank after close, final standings
- [ ] Host view: pick a question set, create, share code, start, watch
- [ ] Types generated from the contracts (task-05); connection status shown
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
