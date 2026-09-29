# task-06: Identifiers, quiz codes, validation

- **Phase:** P1 Domain (TDD)
- **Size:** S · **TDD:** yes
- **Status:** todo
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

- [ ] Tests: alphabet and length; no look-alike ever generated (large sample); chi-square uniformity; lowercase codes normalised
- [ ] Tests: display-name table (trimming, 20 **characters** not bytes, allowed punctuation, control characters rejected, emoji)
- [ ] Implement `QuizCode` generation with `crypto/rand` and rejection sampling
- [ ] Implement ID types and validation functions

## Done when

- [ ] tests pass
- [ ] generator uses `crypto/rand` with rejection sampling
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
