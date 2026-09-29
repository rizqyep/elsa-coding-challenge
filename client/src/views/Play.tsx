import { useState } from 'react'
import { useNow, useRoom } from '../state/useRoom'
import { describeError } from './errors'
import { ConnectionStatus, Countdown, Standings } from './shared'

// The participant's screen: lobby, question with countdown, result, reveal and rank, final standings.
export function Play({ code, name, onLeave }: { code: string; name: string; onLeave: () => void }) {
  const { state, status, submitAnswer, offsetMs } = useRoom({ type: 'join', data: { quizCode: code, displayName: name } })
  const [chosen, setChosen] = useState<{ questionId: string; optionId: string } | null>(null)
  const phase = state.quiz?.status
  const now = useNow(phase === 'question_open' || phase === 'question_closed')
  const q = state.question
  const picked = state.yourAnswer?.questionId === q?.questionId ? state.yourAnswer?.optionId : chosen?.questionId === q?.questionId ? chosen?.optionId : undefined

  if (status === 'replaced') {
    return (
      <Ended onLeave={onLeave} title="This tab stopped">
        You joined as the same player from another tab or window. Keep playing there.
      </Ended>
    )
  }
  if (status === 'closed' && state.lastError && !state.quiz) {
    return (
      <Ended onLeave={onLeave} title="Could not join">
        {describeError(state.lastError.code, state.lastError.message)}
      </Ended>
    )
  }

  return (
    <main className="screen">
      <header className="bar">
        <p>
          Quiz <strong className="code">{code}</strong> as <strong>{name}</strong>
        </p>
        <ConnectionStatus status={status} />
      </header>
      {status === 'reconnecting' && <p className="notice">Connection lost. Reconnecting; your answers are kept.</p>}

      {!state.quiz && <p className="muted">Joining quiz {code}.</p>}

      {phase === 'lobby' && (
        <section className="stage">
          <h1>You're in</h1>
          <p>The host starts the quiz. Questions appear here for everyone at the same time.</p>
        </section>
      )}

      {(phase === 'question_open' || phase === 'question_closed') && q && (
        <section className="stage">
          <p className="muted">
            Question {q.index + 1} of {q.count}
          </p>
          <h1>{q.prompt}</h1>
          {phase === 'question_open' ? (
            <Countdown until={q.closeAt} now={now} offsetMs={offsetMs()} label="Time left" />
          ) : state.nextTransitionAt ? (
            <Countdown
              until={state.nextTransitionAt}
              now={now}
              offsetMs={offsetMs()}
              label={q.index + 1 === q.count ? 'Final results in' : 'Next question in'}
            />
          ) : null}
          <div className="options" role="group" aria-label="Answers">
            {q.options.map((o) => {
              const isCorrect = phase === 'question_closed' && o.id === state.correctOptionId
              const cls = [picked === o.id ? 'picked' : '', isCorrect ? 'correct' : ''].join(' ').trim()
              return (
                <button
                  key={o.id}
                  type="button"
                  className={cls || undefined}
                  disabled={phase !== 'question_open' || picked !== undefined}
                  aria-pressed={picked === o.id}
                  onClick={() => {
                    setChosen({ questionId: q.questionId, optionId: o.id })
                    submitAnswer(q.questionId, o.id)
                  }}
                >
                  {o.text}
                  {isCorrect && <span className="tag"> correct answer</span>}
                </button>
              )
            })}
          </div>
          <AnswerLine
            phase={phase}
            answered={state.yourAnswer?.questionId === q.questionId ? state.yourAnswer : null}
            waiting={picked !== undefined && state.yourAnswer?.questionId !== q.questionId}
          />
          {phase === 'question_closed' && state.you && (
            <p>
              Your rank: <strong>{state.you.rank}</strong> with <strong>{state.you.score}</strong> points
            </p>
          )}
          {state.lastError && <p className="error">{describeError(state.lastError.code, state.lastError.message)}</p>}
        </section>
      )}

      {phase === 'finished' && (
        <section className="stage">
          <h1>Final standings</h1>
          {state.you ? (
            <p>
              You finished <strong>#{state.you.rank}</strong> with <strong>{state.you.score}</strong> points.
            </p>
          ) : (
            <p>This quiz has finished.</p>
          )}
        </section>
      )}

      {phase === 'expired' && (
        <Ended onLeave={onLeave} title="Quiz expired">
          The host never started this quiz.
        </Ended>
      )}

      {state.quiz && (
        <Standings board={state.leaderboard} you={state.you} title={phase === 'finished' ? 'Final leaderboard' : 'Leaderboard'} />
      )}

      <button type="button" className="secondary" onClick={onLeave}>
        Leave quiz
      </button>
    </main>
  )
}

function AnswerLine({
  phase,
  answered,
  waiting,
}: {
  phase: string
  answered: { correct: boolean; points: number } | null
  waiting: boolean
}) {
  if (answered) {
    return answered.correct ? (
      <p className="result ok">Correct: +{answered.points} points</p>
    ) : (
      <p className="result">Not this one: 0 points</p>
    )
  }
  if (waiting) return <p className="muted">Answer sent. Waiting for the server.</p>
  if (phase === 'question_closed') return <p className="result">No answer this round: 0 points</p>
  return <p className="muted">Pick one answer. Faster correct answers earn more points.</p>
}

export function Ended({ title, children, onLeave }: { title: string; children: React.ReactNode; onLeave: () => void }) {
  return (
    <main className="screen">
      <section className="stage">
        <h1>{title}</h1>
        <p>{children}</p>
        <button type="button" onClick={onLeave}>
          Back to start
        </button>
      </section>
    </main>
  )
}
