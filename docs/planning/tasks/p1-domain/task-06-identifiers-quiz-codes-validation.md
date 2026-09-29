# task-06: Identifiers, quiz codes, validation

- **Phase:** P1 Domain (TDD)
- **Size:** S · **TDD:** yes
- **Status:** done (awaiting my code review)
- **Implements:** TRD §3.1, §3.6 · FR-1, FR-15, D16
- **Depends on:** [task-01](../p0-foundation/task-01-repository-scaffold.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- code alphabet and length
- the generator never emits a look-alike
- uniform distribution (chi-square over a large sample)
- display-name rules (table)
- quiz-code normalisation (lowercase accepted)

## Steps

- [x] Tests: alphabet and length; no look-alike ever generated (large sample); chi-square uniformity; lowercase codes normalised
- [x] Tests: display-name table (trimming, 20 **characters** not bytes, allowed punctuation, control characters rejected, emoji)
- [x] Implement `QuizCode` generation with `crypto/rand` and rejection sampling
- [x] Implement ID types and validation functions

## Done when

- [x] tests pass
- [x] generator uses `crypto/rand` with rejection sampling
- [x] `make check` green

## Notes and decisions

- **Review timing:** for the domain tasks I chose to review tests and code together *after* implementation, instead of reviewing tests first. TDD order was still followed: tests written and confirmed failing before implementation.
- **Where things live:** ID types, quiz codes, timing limits, and request IDs are in `internal/quiz`; display names are in `internal/session`, which owns joining (FR-15).
- `NewCode` reads its random source one byte at a time and rejects bytes ≥ 248 (= 31·8), so all 31 symbols are equally likely. A nil source means `crypto/rand`.
- `ParseCode` trims and upper-cases, so participants can type codes in lowercase.
- Display names are measured in characters (runes), not bytes; emoji and punctuation outside `- _ . '` are rejected.

## Verification

- 9 tests pass under `-race`; golangci-lint 0 issues.
- **Bias test redesigned before implementing:** the first draft depended on exactly which partial code was discarded at the end. Replaced with six full rounds of all 256 byte values → exactly 248 codes → every symbol exactly 48 times.
- **Mutation check:** disabling the rejection branch made both the bias test (256 codes instead of 248) and the skip test fail; restored and green.
- An earlier mutation attempt (setting the limit to 256) didn't compile, because 256 doesn't fit in a byte. Discarded as proving nothing, and replaced by the mutation above.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
