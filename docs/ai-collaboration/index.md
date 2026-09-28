# AI collaboration

How I used generative AI on this challenge, and how I checked its output. Individual interactions are logged in [`log.md`](log.md).

## Tools

| Tool | Used for |
|---|---|
| Claude Code (Claude Opus 5.5), in the terminal against this repo | Reading and analysing the brief, drafting planning and requirements documents, proposing options and trade-offs, challenging design decisions |

This section will be extended as implementation starts (code generation, tests, load scripts, review).

## How I split the work

I drive and the AI navigates.

| Me | AI |
|---|---|
| Frame each problem and set the acceptance criteria | Draft documents and structure from my direction |
| Make every decision and record why | Offer options with trade-offs, recommend one |
| Simplify when the AI over-builds | Point out gaps and failure modes in my proposals and its own |
| Verify output against the brief, and later against tests and measurements | Keep the documents consistent after each decision |

**Rule:** nothing is marked `decided` until I have read it and can explain the reasoning without the AI.

## How AI output is verified

**Design phase:**
- **Traceability.** Every brief requirement maps to numbered FR/NFR items ([`../planning/index.md`](../planning/index.md#brief-traceability)). Anything the AI added that isn't in the brief is marked as a proposal or assumption, and I accepted or rejected each one explicitly.
- **Re-reading the brief** whenever a recommendation claimed support from it. See AI-004, where this changed the outcome.
- **Challenging recommendations for scale and correctness** before accepting them. See AI-005 and AI-006.
- **Measurable targets.** Performance and scale claims are written as numbers (NFR-1 to NFR-11) so the load test can confirm or refute them later.

**Implementation phase** (to come): tests written or approved by me before accepting AI-generated logic, concurrency tests for scoring, load-test measurements, and a code review pass.

## Conventions

- Detailed entries live in [`log.md`](log.md) as `AI-NNN`.
- Code that was significantly AI-assisted carries a one-line pointer, e.g. `// AI-assisted: AI-012`. Longer explanations belong in the log, not in the code.
- Commit messages and PRs contain no AI attribution. This folder is where it is disclosed.

## Limitations observed so far

- **Over-building by default.** The AI's first plan had 20 task files before any requirements existed, and a richer scoring formula and host feature set than needed. I trimmed these (AI-002, AI-004, AI-006).
- **Recommendations stated more firmly than the evidence allowed.** Its first quiz-mode recommendation was presented as following from the brief, but the brief doesn't specify pacing at all (AI-004).
- **Useful at spotting second-order effects** when asked to challenge a design: timers with no owner, check-then-set races, and the cost of per-recipient broadcast payloads (AI-003, AI-005, AI-006).
