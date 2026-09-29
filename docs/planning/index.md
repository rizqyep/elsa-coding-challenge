# Planning

Last updated: 2026-09-28 · Deadline: _TBD_

Working plan for the Real-Time Vocabulary Quiz challenge. The original brief is in [`../assignment.md`](../assignment.md).

## Documents

Read them in this order. Each one builds on the one before it.

| Doc | Purpose | Status |
|---|---|---|
| [`context.md`](context.md) | Problem, scope, and the decisions everything else builds on (D1–D16) | current |
| [`requirements.md`](requirements.md) | Functional and non-functional requirements, assumptions, resolved questions | agreed |
| [`../system-design/`](../system-design/) | Part 1 deliverable: architecture, components, data flow, technology choices, non-functional design | complete, pending final review |
| [`trd.md`](trd.md) | Technical requirements for the Go backend: layout, config, domain, Redis scripts, schema, contracts, internals, test plan | complete |
| [`tasks.md`](tasks.md) + [`tasks/`](tasks/) | Index of 32 implementation tasks derived from the TRD, with one file per task in phase folders: tests first, granular steps, done criteria, notes, verification, AI collaboration | ready to start |

**How the docs relate:** the system design describes the **whole** feature for reviewers, including the mocked parts, at the level the brief asks for. The TRD covers only the component we build, down to the implementation detail. It links to the system design instead of repeating it.

## Brief traceability

Every requirement in the brief, mapped to the place that covers it. Before submitting, check every row.

| # | Brief requirement | Covered by |
|---|---|---|
| B1 | Join a quiz via unique quiz ID (AC 1, Part 2.2) | FR-1, FR-8 to FR-13 |
| B2 | Many users in one session at once (AC 1) | FR-9, NFR-1, load test |
| B3 | Real-time, accurate, consistent scoring (AC 2, Part 2.2) | FR-16 to FR-22, FR-32 to FR-35, NFR-12, NFR-13, NFR-13a |
| B4 | Leaderboard of all participants, updated promptly (AC 3, Part 2.2) | FR-23 to FR-28, NFR-6 |
| B5 | Architecture diagram and component descriptions (Part 1) | [`architecture.md`](../system-design/architecture.md) |
| B6 | Data flow, join → leaderboard (Part 1) | [`data-flow.md`](../system-design/data-flow.md) |
| B7 | Technologies with justification (Part 1) | [`tech-choices.md`](../system-design/tech-choices.md), decisions in `context.md` |
| B8 | AI Collaboration in Design (Submission 1) | [`docs/ai-collaboration/`](../ai-collaboration/index.md) (AI-001 to AI-016 so far) |
| B9 | AI-assisted code marked, with verification (Part 2.3) | `docs/ai-collaboration/` + code pointers (D7) |
| B10 | Scalability and trade-offs (Part 2.4) | NFR-1 to NFR-5e, NFR-24 to NFR-26, D10, D11; [`non-functional.md`](../system-design/non-functional.md) §1, §7 |
| B11 | Performance under load (Part 2.4) | NFR-6 to NFR-11; `non-functional.md` §2; load test results |
| B12 | Reliability (Part 2.4) | NFR-12 to NFR-18; `non-functional.md` §3 |
| B13 | Maintainability (Part 2.4) | NFR-31 to NFR-35, D9, D13 (contracts), D14 (TDD and test levels) |
| B14 | Monitoring and observability (Part 2.4) | NFR-27 to NFR-30; `non-functional.md` §4 |
| B15 | Run and test instructions (Submission 2) | root `README.md` |
| B16 | Video, 5–10 min (Submission 3) | outside this repo |

## Working rhythm

For each piece of work:

1. **Define.** Write the goal and acceptance criteria before building.
2. **Explore.** List options and trade-offs when a real choice exists.
3. **Decide.** Pick one and record why (in `context.md` or an ADR).
4. **Build** against the acceptance criteria.
5. **Verify.** Test, measure, review, and record what was checked and what it showed.
6. **Record** the AI collaboration notes (D7).
