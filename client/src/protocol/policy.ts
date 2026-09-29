// Client timing and reconnect rules (TRD §9.5).

export type CloseAction = 'reconnect' | 'replaced' | 'stop'

const backoffBaseMs = 500
const backoffCapMs = 15_000

// A browser sees every rejected handshake as 1006, so unknown codes reconnect with backoff.
export function closeAction(code: number): CloseAction {
  switch (code) {
    case 4000:
      return 'replaced' // another tab or device owns the session
    case 1000:
    case 1009:
      return 'stop'
    default:
      return 'reconnect'
  }
}

// Full jitter: uniform in [0, min(cap, base·2^attempt)), but never less than the server asked for.
export function backoffMs(attempt: number, random: () => number, atLeastMs = 0): number {
  const ceiling = Math.min(backoffCapMs, backoffBaseMs * 2 ** Math.min(attempt, 30))
  return Math.max(atLeastMs, Math.floor(random() * ceiling))
}

// Server clock minus local clock, measured at the round trip's midpoint.
export function offsetFromPong(sentAt: number, serverTime: number, receivedAt: number): number {
  return serverTime - (sentAt + (receivedAt - sentAt) / 2)
}

// Countdowns use server time; the server alone decides lateness.
export function remainingMs(closeAt: number, localNow: number, offsetMs: number): number {
  return Math.max(0, closeAt - (localNow + offsetMs))
}
