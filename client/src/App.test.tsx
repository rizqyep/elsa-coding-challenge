import { describe, expect, it } from 'vitest'
import { renderToString } from 'react-dom/server'
import App from './App'
import { normalizeCode } from './views/code'

describe('App', () => {
  it('starts on the join screen', () => {
    const html = renderToString(<App />)
    expect(html).toContain('Join quiz')
    expect(html).toContain('Host a quiz')
  })

  it('normalizes typed codes to the uppercase wire format', () => {
    expect(normalizeCode(' k7q2 mx ')).toBe('K7Q2MX')
  })
})
