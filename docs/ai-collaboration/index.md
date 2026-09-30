# AI collaboration

How I used generative AI on this challenge, and how I checked what it produced. Every significant interaction is logged in [`log.md`](log.md) as `AI-001` to `AI-041`, with what I asked, what the AI produced, what I decided, and how it was verified.

## Tools

| Tool | Used for |
|---|---|
| Claude Code (Claude Opus 5.5), in the terminal against this repo | Everything below: reading the brief, planning, design, contracts, code, tests, load tooling, docs |
| Playwright, driven through Claude Code | Clicking through the React client in a real browser against the running stack (AI-033, AI-035) |

Checks the AI ran on its own output used ordinary tools, not AI: `go test -race`, golangci-lint (including gosec), JSON Schema validation, Redocly and the AsyncAPI CLI, `mermaid-cli`, testcontainers and Toxiproxy, `pprof`, and the load simulator.

## How I split the work

I drive; the AI navigates and types.

| Me | AI |
|---|---|
| Frame each problem and set the acceptance criteria | Draft documents, tests, and code from my direction |
| Make every decision, and say why | Offer options with trade-offs and recommend one |
| Cut scope when it over-builds | Point out gaps and failure modes, in my proposals and its own |
| Review tests and code, re-run them myself | Prove its own work: failing test first, deliberate breaks, measurements |

**Rule:** nothing is marked `decided` or `done` until I've read it and can explain it without the AI.

**Decisions that were mine** (full list in [`../planning/context.md`](../planning/context.md#decisions)):
- Go and React, because I know Go best and can review it responsibly (D1, D3).
- The host only starts the quiz; the server runs it after that (D2).
- Live state in Redis with virtual rooms, so no server owns a quiz (D10).
- Three services from one codebase: REST API, WebSocket gateway, worker (D11).
- Answers leave Redis only once saved to PostgreSQL (AI-007).
- Simple scoring (100 + speed bonus), and scale measured as concurrency inside one room (AI-006).
- Keep the build focused on the real-time path; observability is a scaffold (D17).

## Where AI was used

| Phase | What the AI did | Log |
|---|---|---|
| Brief and planning | Read the brief section by section, proposed the repo and planning structure | AI-001, AI-002, AI-015, AI-016 |
| Requirements | Drafted FRs and NFRs, proposed answers to open questions, priced the options | AI-006, AI-007 |
| System design | Architecture, data flow, technology choices, capacity estimates, failure modes | AI-003 to AI-005, AI-008, AI-009 |
| Technical spec | TRD: Redis scripts, schema, contracts, gateway and worker internals, test plan | AI-010 to AI-014 |
| Implementation | Every server package, the Lua scripts, the React client, Docker Compose and nginx | AI-017 to AI-036 |
| Verification tooling | The test kit, end-to-end and fault tests, the simulator, k6, the load runs | AI-037 to AI-040 |

Each server package and the main client files carry a one-line pointer to the entries that built them, e.g. `// AI-assisted: AI-023 (docs/ai-collaboration/log.md).`

## How AI output was verified

This is the part I cared most about. Generated code that passes its own happy-path tests proves very little, so each layer below exists to catch a different way the AI could be wrong.

1. **Tests before code, and proven red.** For the critical paths (scoring, the state machine, the Redis scripts, the gateway, the worker), tests were written from the requirement IDs first and run to confirm they failed. I reviewed the test cases for the domain core before implementation (task-02, task-06 to 08).

2. **Deliberate breaks (mutation checks).** After tests passed, the AI broke the code on purpose and confirmed the right test failed. This is what makes a green suite mean something. Two examples:
   - Removing the duplicate-answer check made 100 concurrent submits all score: 100 accepted, 19,300 points instead of 193 (AI-023).
   - It also caught weak tests, several times: breaks that were only detected as 60-second hangs or not at all. Those tests were fixed until each break failed fast (AI-028, AI-029, AI-030).

3. **Independent references.** Scoring and quiz transitions exist in both Go and Lua. Both run the same test-vector files, and the vectors themselves were checked against a separate Python model before any Go was written (AI-020). The simulator recomputes every player's expected score from what it sent and compares it with the server's answer results and the saved totals (AI-037).

4. **Contracts as validators.** The REST API validates every request and response against `openapi.yaml`; every WebSocket message is checked against its JSON Schema, on the server and in the tests. A schema was changed on purpose to confirm the contract tests catch a leaked answer key (AI-013, AI-017).

5. **Real infrastructure and real faults.** Integration tests run against real Redis and PostgreSQL, with Toxiproxy cutting or slowing connections mid-operation, and end-to-end tests kill containers mid-quiz (AI-022, AI-026, AI-038).

6. **Running the actual product.** Every stack claim was checked on the running system: scaling spread connections 10/10/10/10 across four gateways; a killed gateway's clients all reconnected elsewhere; a full quiz was clicked through as host and two players in a browser, and the final scores matched the database (AI-033, AI-034).

7. **Load runs against numeric targets.** The simulator checks each NFR and exits non-zero on a miss. 5,000 players in one room passed every target; the 10,000 run failed on client-observed latency, and the report says so and why (AI-040).

8. **Every commit builds on its own.** Each commit was checked out alone and built, vetted, and tested, so the history can be reviewed commit by commit (AI-019 onwards).

**Security checks:**
- The answer key can't reach a client by construction: the protocol package only accepts a question type with no correct option, and a test fails if that package ever references the full type (AI-021).
- The server rejects client messages with unknown fields, so malformed input fails loudly with a named reason (AI-021).
- Origin, token, and admission checks run before the WebSocket upgrade; per-connection rate limits run before parsing (AI-029).
- Tokens were checked to be absent from logs, and when the load runs showed nginx's error log could still quote one, that was fixed and reported, not hidden (AI-040, AI-041).

## Bugs verification caught

Not everything was right the first time. These were found by the checks above, not by luck:

| Found | Bug | How |
|---|---|---|
| Transition tests (AI-025) | Without the "is it due" check, a question closed and the next opened instantly, skipping the reveal | Deliberate break |
| End-to-end test (AI-030) | An early close was published with an unchanged version, so every client ignored it | Raw events logged: two events both `"v":2` |
| Fault reproduction (AI-030) | After a Redis restart, every answer would fail when replica acks were enabled | Emptied Redis's script cache on purpose |
| REST flow test (AI-027) | Tied players could reorder between the live and archived leaderboards on locale-collated PostgreSQL | Forced a locale collation in the test |
| REST flow test (AI-027) | Start wasn't idempotent once question 1 opened | Retried start in the test |
| Gateway tests (AI-031) | A stopping gateway took 15 s instead of 0.2 s | Noticed the tests' own run time |
| Browser check (AI-035) | Players who left the lobby stayed in the host's list | Found by me in the browser |
| Load runs (AI-040) | The capacity estimates were off by about 2.5× | Measured with `pprof` and Redis stats |

## Limitations I saw

- **It over-builds by default.** 20 task files before requirements existed, a heavier scoring formula, a version check that mutation testing later showed did nothing (AI-002, AI-024). I cut these.
- **It states things more firmly than the evidence allows.** It said the brief implied host-paced quizzes (it doesn't, AI-004), and it claimed tokens never reach logs before the load runs proved otherwise (AI-040).
- **It guesses library APIs.** A go-redis `Wait` method that doesn't exist, the wrong k6 import path, schema column names it hadn't read. Each was caught by the compiler or a failing run, then checked against the library source (AI-023, AI-037, AI-039).
- **Its checks can be hollow.** A log check that couldn't fail because a variable was empty, a mutation that didn't compile, a shell quirk that ran no tests. The fix was a habit: before trusting a pass, confirm the check could have failed (AI-028, AI-032, AI-034).
- **Tests that wait need deadlines.** It repeated this mistake three times before it became a standing rule (AI-026, AI-028, AI-029).
- **It is very good at second-order effects** when asked to challenge a design: races between check and write, the cost of per-recipient payloads, missed events on reconnect, stampedes after an outage (AI-006, AI-012, AI-029, AI-030).

## Conventions

- Detailed entries live in [`log.md`](log.md), one per decision round or task.
- Code pointers are one line, at package level. Explanations live in the log, not in comments (D7).
- Commit messages and PRs carry no AI attribution. This folder is where it is disclosed.
