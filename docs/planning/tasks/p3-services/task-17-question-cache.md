# task-17: Question cache

- **Phase:** P3 Services
- **Size:** M · **TDD:** yes
- **Status:** done
- **Implements:** TRD §7.8 · NFR-9, D12
- **Depends on:** [task-02](../p0-foundation/task-02-platform-packages.md), [task-04](../p0-foundation/task-04-migrations-and-seed-data.md)
- **Unblocks:** [task-18](task-18-gateway-connections.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- single-flight (1,000 concurrent gets → 1 load)
- PostgreSQL fails 3× then succeeds → loaded with backoff (gomock)
- budget exhausted → `server_busy`
- malformed set refused
- reference counting and LRU eviction

## Steps

- [x] Tests with gomock repository and a fake clock: 1,000 concurrent gets → 1 load (single-flight)
- [x] Tests: PostgreSQL fails 3× then succeeds → loaded with backoff; budget exhausted → `server_busy`
- [x] Tests: malformed set refused and logged; reference counting; LRU eviction beyond the cap
- [x] Implement the cache and the PostgreSQL question-set repository

## Done when

- [ ] cache used by the gateway (moved to task-18: the gateway doesn't exist yet)
- [x] tests pass
- [x] `make check` green

## Notes and decisions

- **Place:** `quiz` module (TRD §1.2): `Cache`, `SetLoader` (implemented by `PostgresStore.QuestionSet`), `ValidateSet`, and lookups on `QuestionSet`/`Question` for the answer path (`Question`, `HasOption`, `IsCorrect`).
- **API:** `Acquire(ctx, set)` loads or reuses the set and holds a reference (a gateway calls it when a room gets its first local connection). `Release` drops the reference. `Get` is the answer path's lookup and never does I/O.
- **Single-flight** (`x/sync/singleflight`). The load runs on a context detached from the first caller, bounded by the retry budget, so a caller that gives up doesn't fail everyone waiting on the same load. The load caches the set *before* returning, so a caller arriving just after the flight finds it instead of starting a second load.
- **Retries** go through `retry.Retrier` with `retry.Transient` as the filter: base 100 ms, cap 5 s, budget 10 s, 2 s per attempt (TRD §9.2). An exhausted budget returns an error that is still transient, so callers map it to `server_busy` / 503. Unknown or malformed sets are permanent: no retry, no caching.
- **Validation:** 1–50 questions, 2–4 options, unique non-empty IDs and text, and a correct option that belongs to its own question. Refused sets are logged at error level with the set ID. The loader left-joins options, so a question with none reaches validation instead of silently disappearing.
- **Eviction:** once over `QUESTION_CACHE_MAX_SETS`, the least-recently-used *unreferenced* set goes. Sets in use are never evicted, so the cap can be exceeded while every cached set is live. `Get` counts as use.

## Verification

- Tests were written first and confirmed failing; all pass under `-race`, including 3 back-to-back runs and 2 shuffled integration runs. `make check` is green.
- **Unit (gomock loader, fake clock):**
  - 1,000 concurrent `Acquire`s → 1 load, one shared pointer, 1,000 references;
  - a cancelled caller doesn't cancel the load;
  - 3 transient failures then success, with the exact backoff sleeps;
  - budget exhausted → transient error, nothing cached, a later load recovers;
  - unknown and malformed sets are not retried or cached, and the malformed one is logged;
  - reference counting and LRU order, including over-release;
  - referenced sets are never evicted;
  - 64 goroutines acquire, get and release concurrently.
- **Integration (real PostgreSQL):** the seeded set loads in position order with its answer key; every seeded set passes validation; a malformed set written into the database is refused by the real cache while a good one loads.
- **Mutations:** 7, all caught (no single-flight, load bound to the first caller, retrying every error, evicting sets in use, evicting the most recently used, `Get` not counting as use, the correct-option check removed).
- **Test weakness found and fixed:** in the first mutation run, 3 mutants were caught only as **hangs**. A gomock over-call inside the single-flight goroutine exits that goroutine, so its callers wait forever. Every `Acquire` in the tests now has a 5 s deadline, and the single-flight test counts loads itself, so the same mutants now fail in about 5 s.
- **Isolation fix:** the malformed-set test deletes its rows afterwards, because question sets survive `Reset` and another test expects exactly the seeded three.

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
