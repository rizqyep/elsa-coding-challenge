# T012: Multi-instance fan-out

- **Phase:** P2
- **Status:** todo
- **Depends on:** T011

## Goal

Two or more server instances behind a load balancer. Users of the same quiz on different instances see the same leaderboard (R11).

## Acceptance criteria

- [ ] Per-quiz pub/sub channel; each instance subscribes only to quizzes that have local sockets
- [ ] Only one instance emits each coalesced leaderboard update (or duplicates are harmless because of `version`)
- [ ] Quiz clock (D10): no owner. Any instance can apply a due transition, and a Lua version check makes it exactly-once
- [ ] `docker compose` runs 2 instances + a load balancer
- [ ] Demonstrated: clients on instance A and instance B see identical leaderboards

## Notes and decisions

## Verification

## AI collaboration
