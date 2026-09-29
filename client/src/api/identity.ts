import { api, type DevToken, type Role } from './client'

// Mocked identity (D15): one participant per browser tab, kept across reloads so a rejoin
// restores the same score (FR-12). sessionStorage can be unavailable; then identity lasts the page.
const renewBeforeMs = 60_000
const cache = new Map<Role, DevToken>()

function stored(role: Role): string | undefined {
  try {
    return sessionStorage.getItem(`quiz.id.${role}`) ?? undefined
  } catch {
    return undefined
  }
}

function remember(role: Role, id: string) {
  try {
    sessionStorage.setItem(`quiz.id.${role}`, id)
  } catch {
    // identity then lasts only as long as this page
  }
}

// token returns a valid token, renewing it before it expires (TRD §9.5: browsers can't see a 401 on connect).
export async function token(role: Role): Promise<string> {
  const current = cache.get(role)
  if (current && Date.parse(current.expiresAt) - Date.now() > renewBeforeMs) return current.token
  const t = await api.devToken(role, current?.participantId ?? stored(role))
  cache.set(role, t)
  remember(role, t.participantId)
  return t.token
}

export function wsUrl(tok: string): string {
  const scheme = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${scheme}://${location.host}/ws?token=${encodeURIComponent(tok)}`
}
