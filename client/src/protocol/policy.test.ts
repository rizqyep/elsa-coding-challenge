import { describe, expect, it } from 'vitest'
import { backoffMs, closeAction, offsetFromPong, remainingMs } from './policy'

describe('clock offset and countdown', () => {
  it('estimates the offset from the round-trip midpoint', () => {
    // sent at 1000 local, pong back at 1100 local, server stamped 5050: midpoint 1050, so +4000.
    expect(offsetFromPong(1000, 5050, 1100)).toBe(4000)
  })

  it('counts down to closeAt in server time and stops at zero', () => {
    expect(remainingMs(20_000, 11_000, 4_000)).toBe(5_000)
    expect(remainingMs(20_000, 17_000, 4_000)).toBe(0)
  })
})

describe('reconnect policy (TRD §9.5)', () => {
  it.each([
    [1001, 'reconnect'],
    [1006, 'reconnect'],
    [1012, 'reconnect'],
    [4003, 'reconnect'],
    [4002, 'reconnect'],
    [4000, 'replaced'],
    [1000, 'stop'],
    [1009, 'stop'],
  ] as const)('close %i → %s', (code, want) => {
    expect(closeAction(code)).toBe(want)
  })

  it('backs off with full jitter, base 500 ms, capped at 15 s', () => {
    expect(backoffMs(0, () => 0.999)).toBeLessThan(500)
    expect(backoffMs(3, () => 0.999)).toBeLessThan(4_000)
    expect(backoffMs(3, () => 0.999)).toBeGreaterThan(3_900)
    expect(backoffMs(20, () => 0.999)).toBeLessThanOrEqual(15_000)
    expect(backoffMs(5, () => 0)).toBe(0)
  })

  it('waits at least the server-given delay', () => {
    expect(backoffMs(0, () => 0, 2_000)).toBe(2_000)
  })
})
