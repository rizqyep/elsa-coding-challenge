# Tasks

Status: **draft (pre-TRD)** · Last updated: 2026-09-28

This board was drafted before the requirements and TRD were written. Once the TRD is agreed, it gets re-derived from it, and each task will link to the FR/NFR IDs it satisfies. Detailed drafts are in [`tasks/`](tasks/).

## Phases

| Phase | Scope | Exit criteria |
|---|---|---|
| P0 Setup | Repo, decisions | Repo initialised; D1–D10 decided |
| P1 Requirements and design | requirements, system design, TRD, protocol | Requirements signed off; system design reviewable; protocol v1 frozen |
| P2 Core server | Join, quiz clock, scoring, leaderboard, fan-out | Full quiz runs across two server instances |
| P3 Hardening | Reliability, observability | Reconnects, bad input, instance restarts, and Redis outages handled; metrics exposed |
| P4 Client demo | React client | Several tabs show one quiz running live |
| P5 Verification | Tests, load test | Tests green; load test numbers recorded against the NFRs |
| P6 Submission | AI docs, README, final review | Brief traceability all checked; fresh-clone run works |

## Board

| ID | Task | Phase | Status |
|---|---|---|---|
| [T001](tasks/T001-repo-setup.md) | Repo setup | P0 | in progress |
| [T002](tasks/T002-requirements-and-assumptions.md) | Requirements (now [`requirements.md`](requirements.md)) | P1 | in review |
| [T003](tasks/T003-architecture.md) | Architecture diagram and components | P1 | todo |
| [T004](tasks/T004-data-flow.md) | Data flow and sequence diagrams | P1 | todo |
| [T005](tasks/T005-tech-choices.md) | Technology choices and ADRs | P1 | todo |
| [T006](tasks/T006-non-functional-design.md) | Scalability, performance, reliability, observability | P1 | todo |
| [T007](tasks/T007-realtime-protocol.md) | Real-time protocol contract v1 | P1 | todo |
| — | TRD ([`trd.md`](trd.md)) | P1 | todo |
| [T008](tasks/T008-server-skeleton.md) | Server skeleton | P2 | todo |
| [T009](tasks/T009-session-join.md) | Session join and presence | P2 | todo |
| [T010](tasks/T010-scoring-engine.md) | Scoring engine | P2 | todo |
| [T011](tasks/T011-leaderboard.md) | Leaderboard and broadcast | P2 | todo |
| [T012](tasks/T012-multi-instance-fanout.md) | Multi-instance fan-out and quiz clock | P2 | todo |
| [T013](tasks/T013-reliability.md) | Reliability hardening | P3 | todo |
| [T014](tasks/T014-observability.md) | Observability | P3 | todo |
| [T015](tasks/T015-client-demo.md) | Client demo | P4 | todo |
| [T016](tasks/T016-tests.md) | Unit and integration tests | P5 | todo |
| [T017](tasks/T017-load-test.md) | Load test and results | P5 | todo |
| [T018](tasks/T018-ai-collaboration-docs.md) | AI collaboration docs | P6 | todo |
| [T019](tasks/T019-readme-and-run.md) | Root README and run instructions | P6 | todo |
| [T020](tasks/T020-submission-review.md) | Submission review | P6 | todo |
