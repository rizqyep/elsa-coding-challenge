import { useState } from 'react'
import { codePattern, normalizeCode } from './code'

export function Home({ initialCode, onJoin, onHost }: { initialCode: string; onJoin: (code: string, name: string) => void; onHost: () => void }) {
  const [code, setCode] = useState(initialCode)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    const c = normalizeCode(code)
    const n = name.trim()
    if (!codePattern.test(c)) {
      setError('A quiz code is 6 characters, like K7Q2MX.')
      return
    }
    if (n.length === 0 || [...n].length > 20) {
      setError('Enter a name of 1 to 20 characters.')
      return
    }
    onJoin(c, n)
  }

  return (
    <main className="screen">
      <section className="stage">
        <h1>Vocabulary Quiz</h1>
        <p>Join with the code your host shared. Everyone answers the same question at the same time.</p>
        <form onSubmit={submit} className="form" noValidate>
          <label>
            Quiz code
            <input
              value={code}
              onChange={(e) => setCode(e.target.value)}
              autoCapitalize="characters"
              autoComplete="off"
              spellCheck={false}
              maxLength={12}
              placeholder="K7Q2MX"
              required
            />
          </label>
          <label>
            Your name
            <input value={name} onChange={(e) => setName(e.target.value)} maxLength={40} placeholder="Your name" required />
          </label>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <button type="submit">Join quiz</button>
        </form>
      </section>
      <section className="stage aside">
        <h2>Running a session?</h2>
        <p>Create a quiz from a question set and share its code.</p>
        <button type="button" className="secondary" onClick={onHost}>
          Host a quiz
        </button>
      </section>
    </main>
  )
}
