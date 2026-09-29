import { useEffect, useState } from 'react'
import { ApiError, api, type QuestionSetSummary, type Quiz } from '../api/client'
import { token } from '../api/identity'
import { useNow, useRoom } from '../state/useRoom'
import { Ended } from './Play'
import { ConnectionStatus, Countdown, Standings } from './shared'

const windows = [10, 15, 20, 30]

// The host creates a quiz, shares its code, starts it, then watches; the server runs the clock (D2).
export function Host({ onLeave }: { onLeave: () => void }) {
  const [quiz, setQuiz] = useState<Quiz | null>(null)
  return quiz ? <HostRoom quiz={quiz} onLeave={onLeave} /> : <CreateQuiz onCreated={setQuiz} onLeave={onLeave} />
}

type Load = { kind: 'loading' } | { kind: 'error'; message: string } | { kind: 'ready'; sets: QuestionSetSummary[] }

function CreateQuiz({ onCreated, onLeave }: { onCreated: (q: Quiz) => void; onLeave: () => void }) {
  const [load, setLoad] = useState<Load>({ kind: 'loading' })
  const [attempt, setAttempt] = useState(0)
  const [setId, setSetId] = useState('')
  const [windowSecs, setWindowSecs] = useState(15)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    token('host')
      .then((t) => api.questionSets(t))
      .then((r) => {
        if (!live) return
        setLoad({ kind: 'ready', sets: r.items })
        if (r.items.length > 0) setSetId((cur) => cur || r.items[0].id)
      })
      .catch((e: unknown) => live && setLoad({ kind: 'error', message: e instanceof ApiError ? e.message : 'Could not load question sets.' }))
    return () => {
      live = false
    }
  }, [attempt])

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setCreating(true)
    setError(null)
    try {
      onCreated(await api.createQuiz(await token('host'), { questionSetId: setId, questionWindowSeconds: windowSecs }))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not create the quiz.')
      setCreating(false)
    }
  }

  return (
    <main className="screen">
      <section className="stage">
        <h1>Host a quiz</h1>
        {load.kind === 'loading' && <p className="muted">Loading question sets.</p>}
        {load.kind === 'error' && (
          <>
            <p className="error">{load.message}</p>
            <button type="button" onClick={() => { setLoad({ kind: 'loading' }); setAttempt((a) => a + 1) }}>
              Try loading again
            </button>
          </>
        )}
        {load.kind === 'ready' && load.sets.length === 0 && (
          <p className="error">No question sets exist yet. Run the database seed (make up) and reload.</p>
        )}
        {load.kind === 'ready' && load.sets.length > 0 && (
          <form onSubmit={create} className="form">
            <fieldset>
              <legend>Question set</legend>
              {load.sets.map((s) => (
                <label key={s.id} className="choice">
                  <input type="radio" name="set" value={s.id} checked={setId === s.id} onChange={() => setSetId(s.id)} />
                  {s.title} <span className="muted">({s.questionCount} questions)</span>
                </label>
              ))}
            </fieldset>
            <label>
              Seconds per question
              <select value={windowSecs} onChange={(e) => setWindowSecs(Number(e.target.value))}>
                {windows.map((w) => (
                  <option key={w} value={w}>
                    {w}
                  </option>
                ))}
              </select>
            </label>
            {error && <p className="error">{error}</p>}
            <button type="submit" disabled={creating || !setId}>
              {creating ? 'Creating' : 'Create quiz'}
            </button>
          </form>
        )}
      </section>
      <button type="button" className="secondary" onClick={onLeave}>
        Back to start
      </button>
    </main>
  )
}

function HostRoom({ quiz, onLeave }: { quiz: Quiz; onLeave: () => void }) {
  const { state, status, offsetMs } = useRoom({ type: 'watch', data: { quizCode: quiz.code } })
  const [starting, setStarting] = useState(false)
  const [startError, setStartError] = useState<string | null>(null)
  const [copied, setCopied] = useState<'idle' | 'done' | 'failed'>('idle')
  const phase = state.quiz?.status
  const now = useNow(phase === 'question_open' || phase === 'question_closed')
  const players = state.leaderboard?.participantCount ?? 0
  const link = `${location.origin}/?code=${quiz.code}`

  const start = async () => {
    setStarting(true)
    setStartError(null)
    try {
      await api.startQuiz(await token('host'), quiz.code)
    } catch (err) {
      setStartError(
        err instanceof ApiError && err.code === 'no_participants'
          ? 'Wait for at least one player to join.'
          : err instanceof ApiError
            ? err.message
            : 'Could not start the quiz.',
      )
      setStarting(false)
    }
  }

  if (status === 'closed' && state.lastError && !state.quiz) {
    return (
      <Ended onLeave={onLeave} title="Cannot watch this quiz">
        {state.lastError.message}
      </Ended>
    )
  }

  const q = state.question
  return (
    <main className="screen">
      <header className="bar">
        <p>
          Hosting <strong className="code">{quiz.code}</strong>
        </p>
        <ConnectionStatus status={status} />
      </header>
      {status === 'reconnecting' && <p className="notice">Connection lost. Reconnecting; the quiz keeps running.</p>}

      {!state.quiz && <p className="muted">Opening the room.</p>}

      {phase === 'lobby' && (
        <section className="stage">
          <h1>
            Code <span className="code big">{quiz.code}</span>
          </h1>
          <p>Players join with this code, or with the link:</p>
          <p className="link-row">
            <a href={link} target="_blank" rel="noreferrer">
              {link}
            </a>
            <button
              type="button"
              className="secondary"
              onClick={() =>
                navigator.clipboard
                  .writeText(link)
                  .then(() => setCopied('done'))
                  .catch(() => setCopied('failed'))
              }
            >
              {copied === 'done' ? 'Link copied' : 'Copy link'}
            </button>
          </p>
          {copied === 'failed' && <p className="error">Could not copy. Select the link and copy it by hand.</p>}
          <p>
            {players === 0 ? 'No players yet.' : `${players} ${players === 1 ? 'player has' : 'players have'} joined.`}
          </p>
          {startError && <p className="error">{startError}</p>}
          <button type="button" onClick={start} disabled={starting || players === 0}>
            {starting ? 'Starting' : 'Start quiz'}
          </button>
        </section>
      )}

      {(phase === 'question_open' || phase === 'question_closed') && q && (
        <section className="stage">
          <p className="muted">
            Question {q.index + 1} of {q.count}
          </p>
          <h1>{q.prompt}</h1>
          {phase === 'question_open' ? (
            <Countdown until={q.closeAt} now={now} offsetMs={offsetMs()} label="Closes in" />
          ) : state.nextTransitionAt ? (
            <Countdown until={state.nextTransitionAt} now={now} offsetMs={offsetMs()} label="Next step in" />
          ) : null}
          <ul className="options static">
            {q.options.map((o) => (
              <li key={o.id} className={phase === 'question_closed' && o.id === state.correctOptionId ? 'correct' : undefined}>
                {o.text}
                {phase === 'question_closed' && o.id === state.correctOptionId && <span className="tag"> correct answer</span>}
              </li>
            ))}
          </ul>
        </section>
      )}

      {phase === 'finished' && (
        <section className="stage">
          <h1>Quiz finished</h1>
          <p>The final standings are saved. Players who join with this code now see them.</p>
        </section>
      )}

      {state.quiz && <Standings board={state.leaderboard} you={null} title={phase === 'finished' ? 'Final leaderboard' : 'Leaderboard'} />}

      <button type="button" className="secondary" onClick={onLeave}>
        Back to start
      </button>
    </main>
  )
}
