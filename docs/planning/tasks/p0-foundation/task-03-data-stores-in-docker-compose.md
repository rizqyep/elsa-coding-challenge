# task-03: Data stores in Docker Compose

- **Phase:** P0 Foundation
- **Size:** S · **TDD:** —
- **Status:** todo
- **Implements:** TRD §11.2 (partial)
- **Depends on:** [task-01](task-01-repository-scaffold.md)
- **Unblocks:** [task-04](task-04-migrations-and-seed-data.md)

## Steps

- [ ] `docker-compose.yml`: `redis:7.4` with `appendonly yes` and a `redis-cli ping` healthcheck, bound to `127.0.0.1:6379`
- [ ] `postgres:16` with a named volume and a `pg_isready` healthcheck, bound to `127.0.0.1:5432`
- [ ] Makefile: `up` (copies `.env.example` to `.env` if missing), `down`, `reset`, `ps`, `logs S=…`, `redis-cli`, `psql`
- [ ] Verify an `up` → `down` → `reset` cycle; data survives `down`, not `reset`

## Done when

- [ ] `docker-compose.yml` with `redis` (AOF on) and `postgres` (named volume), both with healthchecks, bound to localhost
- [ ] `make up` / `down` / `reset` / `ps` / `logs` / `redis-cli` / `psql` work for these services
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
