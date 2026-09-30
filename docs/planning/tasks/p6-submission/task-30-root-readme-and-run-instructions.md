# task-30: Root README and run instructions

- **Phase:** P6 Submission
- **Size:** S · **TDD:** —
- **Status:** done
- **Implements:** brief "Submission 2" · NFR-35
- **Depends on:** [task-29](../p5-stack-and-verification/task-29-load-runs-and-results.md)
- **Unblocks:** [task-32](task-32-submission-review.md)

## Steps

- [x] Write the README: overview, repo map, quick start, tests, simulations, known limitations, links to all docs
- [x] Follow it from a fresh clone on a clean machine (or container) and fix every gap found

## Done when

- [x] overview, repo map, quick start, test and simulation commands, known limitations
- [x] **tested from a fresh clone**
- [x] `make check` green

## Notes and decisions

- The README leads with what was built in full (the Go backend) and what's thin or mocked (the client, auth), then the design in five points, so a reviewer knows the scope before running anything.
- Load results are summarised with the 10,000 miss stated plainly, pointing to `docs/testing.md` for the cause.
- Known limitations are collected from the docs: 10,000 in one room unproven end to end, the token in the URL (D15), mocked auth, asynchronous replication, crashed tabs staying in the lobby, reload asking for the name, the WebSocket `invalid_message` detail quoting the schema path, and observability as a scaffold (D17).

## Verification

From a fresh `git clone` of `7769b06`, under a separate Compose project name (the committed `name: quiz` would otherwise reuse the existing stack's volumes):

- `make up`: healthy in 24 s with fresh volumes; migrate and seed ran. The image build reused Docker's layer cache, so the cold 4-minute first build wasn't re-measured.
- The README's three steps in a real browser (Playwright): host created a Quick demo quiz, two players joined through the link, the host saw 2 players and started; all 3 questions closed early and advanced on their own; final standings saved and shown to host and players.
- `make check`: green in 31 s, including fresh `npm ci` for the client and contract tools.
- `make test-integration`: 22 packages ok, 29 s, not cached (`-count=1`). The README said ~2 min; corrected to ~30 s.
- `make sim SCENARIO=big-room PARTICIPANTS=100`: every check PASS, 0 reconciliation mismatches.
- The clone's stack, volumes, and images were removed afterwards; the main stack was restarted.
- Every path, directory, and make target the README names was checked to exist.

## AI collaboration

AI-042.
