# task-28: Simulator and k6

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** partial
- **Status:** done
- **Implements:** TRD §10.7, §11.4, §11.5
- **Depends on:** [task-26](task-26-test-kit.md)
- **Unblocks:** [task-29](task-29-load-runs-and-results.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- scenario file parsing and command-line overrides
- chaos step scheduling

## Steps

- [x] Tests: scenario YAML parsing (unknown fields rejected); command-line overrides; chaos step parsing (`question:2+1s`, `start+5s`, each action); checks against targets; merging rooms
- [x] `cmd/sim`: scenario runner on the test kit, live terminal summary, JSON report, exit code from `assert`, raised open-file limit, faults cleaned up on exit
- [x] Scenario files: `big-room`, `many-rooms`, `gateway-crash`, `slow-clients`, `redis-latency`
- [x] k6 script for `many-rooms`; `make sim` container fallback (Docker socket only for chaos scenarios)

## Done when

- [x] `cmd/sim` runs YAML scenarios with live output, JSON reports, and exit codes from `assert`
- [x] Scenarios `big-room`, `many-rooms`, `gateway-crash`, `slow-clients`, `redis-latency`
- [x] k6 script for `many-rooms`; `make sim` falls back to a container when Go isn't installed
- [x] `make check` green

## Notes and decisions

- **Asserts are named targets** (`nfr6`, `nfr7`, `nfr8`, `nfr9`, `transition-lag`, `rejoin`, `reconciliation`, `no-errors`), each checked against its non-functional §2 value. A `clean` check always runs, so a run that loses a participant or sees a protocol violation fails whatever it asserts.
- **Percentiles across rooms come from merged raw latencies** (`Result.Raw`, `Recorder.Merge`), not averages of room percentiles.
- **`rejoin`** (dropped connection → next snapshot) was added to the runner for `gateway-crash`'s "all clients back within 15 s".
- **Chaos steps anchor to the first room.** The simulator removes toxics and restarts stopped containers before it exits, pass or fail.
- **k6 is a second, independent load generator** for the many-rooms shape. It checks latency, errors and completion, not scores. In k6 1.3 the WebSocket module is `k6/experimental/websockets`: my first import (`k6/websockets`) failed, and probing the image found the right path.
- **No `questions: N`:** the API has no per-quiz question count, so each scenario picks a question set (`demo-quick` has 3).
- Result JSON files are git-ignored by default; the ones task-29 cites are committed with it.

## Verification

- Unit tests written first and seen failing, then green.
- `make sim SCENARIO=big-room PARTICIPANTS=100 RAMP=2s`: every check PASS (NFR-6 p95 147 ms, NFR-7 p95 1.4 ms, NFR-8 p95 6.2 ms, NFR-9 p95 1.0 ms, reconciliation 0), report written, exit 0.
- **The failure path:** a throwaway scenario with +150 ms Redis latency failed NFR-7 (p95 152 ms, p99 455 ms) and exited 1; Toxiproxy had no toxics left afterwards.
- `make k6 ROOMS=10 PER_ROOM=10`: 100 players finished, 300 answers accepted, answer p95 2 ms, question delivery p95 2 ms, 0 errors; all thresholds met.
- **Container fallback** (`make sim GO=nogo`): ran 20 players in the `golang:1.26` image with every check passing. The first run wrote its report as root with mode 0600, unreadable to the user; it now runs as the host user and group (plus the Docker socket group for chaos scenarios), and the rerun wrote a user-owned report. Report names use UTC, so host and container runs sort together.

## AI collaboration

See [AI-039](../../../ai-collaboration/log.md).
