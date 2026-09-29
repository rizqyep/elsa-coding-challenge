import { describe, expect, it } from 'vitest'
import { renderToString } from 'react-dom/server'
import App from './App'

describe('App', () => {
  it('renders the placeholder heading', () => {
    expect(renderToString(<App />)).toContain('Real-Time Vocabulary Quiz')
  })
})
