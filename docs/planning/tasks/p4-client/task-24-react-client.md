# task-24: React client

- **Phase:** P4 Client
- **Size:** L · **TDD:** partial
- **Status:** done
- **Implements:** TRD §6.3, §9.5 · FR-8, FR-11, FR-20, FR-26, FR-28, FR-29
- **Depends on:** [task-09](../p1-domain/task-09-protocol-package.md), [task-16](../p3-services/task-16-rest-api-service.md), [task-20](../p3-services/task-20-gateway-message-handlers-and-presence.md)
- **Unblocks:** [task-25](../p5-stack-and-verification/task-25-full-local-stack.md)

## Steps

- [x] Vitest: the state reducer applies only newer versions, including snapshots
- [x] Vitest: countdown from `closeAt` + clock offset; reconnect policy per close code; unanswered answers resent with the same `id`
- [x] Protocol client: WebSocket wrapper with reconnect, request IDs, pending answers, ping/pong offset
- [x] Participant view: join, lobby, question with countdown, answer, result, live leaderboard with own row, rank after close, final standings
- [x] Host view: pick a question set, create, share code, start, watch
- [ ] Production build served by nginx (moves to task-25, with the rest of the stack); Vite dev server proxies to the stack: done

## Done when

- [x] Participant view: join, question with countdown, answer, result, live leaderboard with own row, rank after close, final standings
- [x] Host view: pick a question set, create, share code, start, watch
- [x] Types generated from the contracts (task-05); connection status shown
- [x] `make check` green

## Notes and decisions

- **Design: "draft without direction"** (your call: a working client, no styling). The UI rules were applied during the build at the honest default dials ENERGY 1 / RHYTHM 1 / MOTION 1, and the page says so in its footer. System colours follow the OS theme, so light and dark both work without a toggle; one accent marks your pick, and green marks the correct answer.
- **State:** a pure reducer over server messages. Quiz state and the leaderboard have separate versions and apply only when newer, snapshots included, so an event that beats the join snapshot on a new connection isn't undone. `finished` is terminal, which also covers the archived snapshot at version 0.
- **Socket:** joins (or watches) on every connect, and sends nothing else before the join reply. Unanswered answers are kept and resent after a reconnect **with their original ids**, so the server's duplicate check returns the original result (FR-30). Close codes follow TRD §9.5: 4000 means another tab took over, so it doesn't reconnect; everything else reconnects with full-jitter backoff (base 500 ms, cap 15 s). A retryable join error waits at least the server's `retryAfterMs`; a refused join (unknown quiz, expired) stops. A ping every 30 s keeps the clock offset for countdowns.
- **Identity:** one participant per browser tab (`sessionStorage`), so two tabs are two players and a reload rejoins as the same player with the same score. Tokens are renewed a minute before they expire, because a browser can't see a 401 on a WebSocket handshake.
- **Codes:** typed in any case and with spaces, normalised to the uppercase wire format; the share link `/?code=…` prefills the join form.
- **REST types** come from the generated OpenAPI schema. Defaulted request fields are optional on the wire, so the client leaves them to the server.
- **Moved to task-25:** serving the production build from nginx, with the rest of the stack.
- **Known small gap:** a reload drops the app's local screen state, so the player retypes their name; the server restores their identity and score.

## Verification

- 31 Vitest tests: the reducer (versions, the subscribe-then-read race, finished is terminal, own answer and rank), the socket over a fake WebSocket (join first, resend with the original id, no resend after a result, stop on 4000, pong offset, close), policy (offset, countdown, close codes, backoff). 6 deliberate breaks, all caught; the first miss exposed a weak finished-is-terminal test, fixed.
- `tsc`, oxlint and the production build are clean; `make check` green.
- **Click-through in a real browser (Playwright)** against the real `api`, `ws` and `worker` binaries on Compose, through the Vite proxy:
  - Home: empty name shows "Enter a name of 1 to 20 characters."; the share link prefills the code, including a lowercase one.
  - Host: question sets load from PostgreSQL; pick a set and window; Create shows the code, link and Copy link; Start stays disabled until a player joins, and the count went 0 → 2 live.
  - Two players in separate tabs: question 1 arrived with a live countdown; a correct answer showed "+171 points" and locked the options; once both answered the question closed early and showed the reveal, "Not this one: 0 points", rank 2, and the leaderboard.
  - Reload mid-quiz: the player came back with the same score, straight into the open question.
  - Question 2 with one player not answering: no early close, the full window ran, and the other tab's leaderboard updated live.
  - After the quiz: a late join showed the archived final standings (442 and 123), matching the answers saved in PostgreSQL.
  - Leave quiz returns to the start screen. Phone width (375 px): no horizontal scroll, every control at least 44 px. Dark theme and Tab order checked, focus ring visible.
  - Console: the only error was a missing favicon, fixed.
- During the click-through the user also answered in the visible browser window. Those extra answers first looked like a client bug and were traced to the user before any code was changed.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
