# task-03: Data stores in Docker Compose

- **Phase:** P0 Foundation
- **Size:** S · **TDD:** —
- **Status:** done
- **Implements:** TRD §11.2 (partial)
- **Depends on:** [task-01](task-01-repository-scaffold.md)
- **Unblocks:** [task-04](task-04-migrations-and-seed-data.md)

## Steps

- [x] `docker-compose.yml`: `redis:7.4` with `appendonly yes` and a `redis-cli ping` healthcheck, bound to `127.0.0.1:6379`
- [x] `postgres:16` with a named volume and a `pg_isready` healthcheck, bound to `127.0.0.1:5432`
- [x] Makefile: `up` (copies `.env.example` to `.env` if missing), `down`, `reset`, `ps`, `logs S=…`, `redis-cli`, `psql`
- [x] Verify an `up` → `down` → `reset` cycle; data survives `down`, not `reset`

## Done when

- [x] `docker-compose.yml` with `redis` (AOF on) and `postgres` (named volume), both with healthchecks, bound to localhost
- [x] `make up` / `down` / `reset` / `ps` / `logs` / `redis-cli` / `psql` work for these services
- [ ] `make check` green (blocked on Go install; nothing in this task affects it)

## Notes and decisions

- **Host ports 16379 and 15432**, configurable in `.env`. Port 6379 was already in use on the development machine, and non-standard defaults avoid that clash for anyone.
- Both ports bind to `127.0.0.1` only.
- `make up` uses `docker compose up -d --wait`, which returns only when the healthchecks pass.

## Verification

- `make up`: both containers healthy on `127.0.0.1:16379` and `127.0.0.1:15432`.
- Redis `CONFIG GET appendonly` → `yes`; PostgreSQL 16.15 answers queries.
- Persistence: a key written before `make down` was still there after `make up`, and gone after `make reset`.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
