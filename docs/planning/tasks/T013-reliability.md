# T013: Reliability hardening

- **Phase:** P3
- **Status:** todo
- **Depends on:** T012

## Goal

The server degrades gracefully instead of failing silently (R13).

## Acceptance criteria

- [ ] Payload validation on every inbound message
- [ ] Per-connection rate limiting
- [ ] Heartbeats; dead connections cleaned up
- [ ] Backpressure: slow consumers are dropped or skipped, not buffered forever
- [ ] Graceful shutdown: stop accepting, notify clients, drain, exit
- [ ] Redis unavailable: clear errors, readiness fails, recovers when Redis is back
- [ ] Client reconnect path tested end to end

## Notes and decisions

## Verification

## AI collaboration
