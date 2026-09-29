# task-29: Load runs and results

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** —
- **Status:** done
- **Implements:** TRD §10.7, non-functional §1–2 · NFR-1–NFR-11
- **Depends on:** [task-28](task-28-simulator-and-k6.md)
- **Unblocks:** [task-30](../p6-submission/task-30-root-readme-and-run-instructions.md), [task-31](../p6-submission/task-31-ai-collaboration-docs.md)

## Steps

- [x] Run all five scenarios; `big-room` at 5,000 then 10,000 participants
- [x] Profile the hottest path (pprof on the gateway, the simulator's CPU and mutex profiles, `/proc/stat`, Redis `INFO commandstats`); fix or document at least one bottleneck
- [x] Write `docs/testing.md`: machine spec, config, results per scenario, what was fixed
- [x] Replace or confirm every estimate in `non-functional.md` with the measured value (script cost, memory per connection, ceiling)

## Done when

- [x] All five scenarios run; `big-room` at 5,000 and then 10,000 participants
- [x] `docs/testing.md` records the results with the machine spec
- [x] The **estimates in `non-functional.md` are replaced or confirmed** with measured values: per-script Redis cost, memory per connection, the ceiling
- [x] At least one bottleneck found and fixed or documented (four fixed, one documented)
- [x] `make check` green

## Notes and decisions

- **Results** (details in `docs/testing.md`): `big-room` 5,000, `many-rooms` 200 × 50, `gateway-crash`, `slow-clients`, and `redis-latency` pass every target. `big-room` 10,000 is scored correctly with server-side times in target, but misses client-observed NFR-6/7/8 because the one test machine hit ~95% CPU on all 12 threads.
- **Bottlenecks, in order:** the simulator's schema validation (fixed by sampling; 240 → 48 CPU-seconds), its shared measurement lock (fixed: per-client buffers), nginx's 1,024-descriptor limit (fixed), Docker's userland port proxy (a local artifact; the reported runs connect to nginx's container address), and the machine itself at 10,000 in one room (documented).
- **Ruled out by measurement, not assumed:** gateways (4 instead of 2 barely helped, each at ~40% CPU), Redis (~15% CPU), and nginx worker imbalance (~1,650 descriptors on each of 12 workers).
- **Security fix:** the descriptor-exhaustion alert logged the request line with its token. nginx's error log is now `emerg`. The lasting fix, moving the token out of the URL, reopens D15 and is the owner's call.
- **Estimates corrected:** the answer script costs ≈ 62 µs, not 20–25 µs, so one Redis core handles ≈ 16,000 answers/s (not 40,000), and the single-room Redis ceiling is ≈ 32,000 (not 80,000). Memory per connection is ≈ 25 KB heap and ≈ 41 KB resident (NFR-4 ≤ 50 KB). The largest leaderboard frame is 1,035 bytes (NFR-11 < 2 KB).
- **Harness fixes found by running at scale:** the late-answer check allowed only `receivedAt < closeAt`, but the answer that closes a question early is accepted and then moves `close_at` to its own millisecond; equality is now allowed (tested). The report directory now follows the scenario file, not the working directory. The container fallback runs as the host user.
- **Added for diagnosis:** `pprof` on the gateway when `APP_ENV=local` (its port isn't published), and `-cpuprofile`, `-mutexprofile`, and `-origin` flags on the simulator.

## Verification

- Reports for every cited run are committed in `loadtest/results/`; every number in `docs/testing.md` comes from a report, a profile, or a metric read during the run.
- Each claimed cause was checked before and after its fix, and each ruled-out cause was checked by an experiment (4 gateways, per-worker descriptor counts, Redis CPU and `commandstats`).
- `make check` green; integration and end-to-end suites unchanged and passing.

## AI collaboration

See [AI-040](../../../ai-collaboration/log.md).
