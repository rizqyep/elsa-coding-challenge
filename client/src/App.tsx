import { useState } from 'react'
import { normalizeCode } from './views/code'
import { Home } from './views/Home'
import { Host } from './views/Host'
import { Play } from './views/Play'

type View = { kind: 'home' } | { kind: 'play'; code: string; name: string } | { kind: 'host' }

function codeFromUrl(): string {
  if (typeof location === 'undefined') return ''
  return normalizeCode(new URLSearchParams(location.search).get('code') ?? '')
}

export default function App() {
  const [view, setView] = useState<View>({ kind: 'home' })
  const home = () => setView({ kind: 'home' })
  return (
    <>
      {view.kind === 'home' && (
        <Home initialCode={codeFromUrl()} onJoin={(code, name) => setView({ kind: 'play', code, name })} onHost={() => setView({ kind: 'host' })} />
      )}
      {view.kind === 'play' && <Play code={view.code} name={view.name} onLeave={home} />}
      {view.kind === 'host' && <Host onLeave={home} />}
      <footer className="draft">Draft without direction: a working client for the demo, not a styled product.</footer>
    </>
  )
}
