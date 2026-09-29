# task-04: Migrations and seed data

- **Phase:** P0 Foundation
- **Size:** S · **TDD:** test
- **Status:** done (the Go seed-validation test moves to task-10, which provides the test containers)
- **Implements:** TRD §5.1, §5.5 · FR-32, FR-27a
- **Depends on:** [task-03](task-03-data-stores-in-docker-compose.md)
- **Unblocks:** [task-10](../p2-redis-scripts/task-10-integration-test-harness.md), [task-17](../p3-services/task-17-question-cache.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- seed-validation test: 2–4 options per question, the correct option belongs to its question, positions contiguous

## Steps

- [ ] Test first (integration tag): after migrating, every seeded set has 2–4 options per question, the correct option belongs to its question, positions are contiguous
- [x] goose migration `0001_schema.sql` with every table and constraint from TRD §5.1
- [x] Write the seed sets (vocabulary reviewed and approved by me): `demo-quick` (3), `synonyms-everyday` (10), `business-english` (10). **Review the vocabulary content by hand** (correct answers, one unambiguous right option)
- [x] Seed applied only when `APP_ENV=local` (separate seed directory run by the migrate entrypoint)
- [x] One-shot `migrate` service in Compose; later services depend on it completing successfully
- [x] Verify: migrations are idempotent on a second run; row counts match

## Done when

- [x] goose migrations for all tables in §5.1
- [x] Seed sets `demo-quick` (3), `synonyms-everyday` (10), `business-english` (10), applied only when `APP_ENV=local`
- [x] One-shot `migrate` service in Compose; `make up` runs it before anything else
- [x] `make check` green

## Notes and decisions

- Files: `server/migrations/00001_schema.sql`, `server/migrations/seed/00001_question_sets.sql`. The seed lives in its own directory with its own goose version table, so it can be skipped outside `APP_ENV=local`.
- **Migrate runner:** instead of a third-party goose image, a small `cmd/migrate` using goose as a library with embedded migrations (built once Go is installed).
- The Go seed-validation test needs the integration harness, so it lands with task-10. Until then, the same checks run as SQL (below).
- **`migrate` is in a `tools` Compose profile**, run by `make up` after the data stores are healthy. `docker compose up --wait` treats any exited container as a failure, even a successful one-shot, so it can't be part of the plain `up`.
- `cmd/migrate` uses goose as a library with `go:embed`ed SQL; schema and seed have separate version tables (`goose_db_version`, `goose_seed_version`).

## Verification

- Applied both "Up" sections to the running PostgreSQL inside a transaction in a throwaway schema, ran the seed-validation checks, and rolled back:
  - 3 sets; 3 / 10 / 10 questions
  - 0 questions with fewer than 2 or more than 4 options
  - 0 correct options outside their question
  - 0 gaps in question or option positions
- `make up` → migrate applied schema v1 and seed v1; a second `make migrate` applied nothing (idempotent).
- Up/Down on a scratch database with the goose CLI: schema up → 6 tables; seed up → 23 questions; seed down → 0 questions; schema down → 0 tables; re-up → 6 tables. Scratch database dropped afterwards.
- An earlier attempt at this check printed "tables left: 0" without showing goose's output. Because that result is ambiguous (it would also be 0 if nothing ran), it was redone as a script with every step's output visible.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
