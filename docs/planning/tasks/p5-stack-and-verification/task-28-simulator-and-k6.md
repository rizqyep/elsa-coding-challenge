# task-28: Simulator and k6

- **Phase:** P5 Stack and verification
- **Size:** M · **TDD:** partial
- **Status:** todo
- **Implements:** TRD §10.7, §11.4, §11.5
- **Depends on:** [task-26](task-26-test-kit.md)
- **Unblocks:** [task-29](task-29-load-runs-and-results.md)

## Tests first

Written before the implementation and reviewed by me before any code is written or generated (D14).

- scenario file parsing and command-line overrides
- chaos step scheduling

## Steps

- [ ] Tests: scenario YAML parsing; command-line overrides; chaos step scheduling (`at: question:2+1s`)
- [ ] `cmd/sim`: scenario runner on the test kit, live terminal summary, JSON report, exit code from `assert`, raised open-file limit
- [ ] Scenario files: `big-room`, `many-rooms`, `gateway-crash`, `slow-clients`, `redis-latency`
- [ ] k6 script for `many-rooms`; `make sim` container fallback (Docker socket only for chaos scenarios)

## Done when

- [ ] `cmd/sim` runs YAML scenarios with live output, JSON reports, and exit codes from `assert`
- [ ] Scenarios `big-room`, `many-rooms`, `gateway-crash`, `slow-clients`, `redis-latency`
- [ ] k6 script for `many-rooms`; `make sim` falls back to a container when Go isn't installed
- [ ] `make check` green

## Notes and decisions

_Filled in while working._

## Verification

_What was run or checked, and what it showed._

## AI collaboration

_Tool, task, key prompts, what was kept, changed, or rejected, and how it was verified. Significant entries go to `docs/ai-collaboration/log.md` (D7)._
