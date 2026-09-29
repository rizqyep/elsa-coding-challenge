# task-04: Migrations and seed data

- **Phase:** P0 Foundation
- **Size:** S · **TDD:** test
- **Status:** todo
- **Implements:** TRD §5.1, §5.5 · FR-32, FR-27a
- **Depends on:** [task-03](task-03-data-stores-in-docker-compose.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md), [task-17](../p3-services/task-17-question-cache.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- seed-validation test: 2–4 options per question, the correct option belongs to its question, positions contiguous

## Steps

- [ ] Test first (integration tag): after migrating, every seeded set has 2–4 options per question, the correct option belongs to its question, positions are contiguous
- [ ] goose migration `0001_schema.sql` with every table and constraint from TRD §5.1
- [ ] Write the seed sets: `demo-quick` (3), `synonyms-everyday` (10), `business-english` (10). **Review the vocabulary content by hand** (correct answers, one unambiguous right option)
- [ ] Seed applied only when `APP_ENV=local` (separate seed directory run by the migrate entrypoint)
- [ ] One-shot `migrate` service in Compose; later services depend on it completing successfully
- [ ] Verify: migrations are idempotent on a second run; row counts match

## Done when

- [ ] goose migrations for all tables in §5.1
- [ ] Seed sets `demo-quick` (3), `synonyms-everyday` (10), `business-english` (10), applied only when `APP_ENV=local`
- [ ] One-shot `migrate` service in Compose; `make up` runs it before anything else
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
