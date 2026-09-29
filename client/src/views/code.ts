export const codePattern = /^[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{6}$/ // D16: no look-alike characters

// People type codes in any case and with stray spaces; the wire format is uppercase (asyncapi.yaml).
export function normalizeCode(raw: string): string {
  return raw.replace(/\s+/g, '').toUpperCase()
}
