import type { Leaderboard } from '../protocol/messages'
import type { SocketStatus } from '../protocol/socket'
import { remainingMs } from '../protocol/policy'
import type { You } from '../state/room'

const statusText: Record<SocketStatus, string> = {
  connecting: 'Connecting',
  open: 'Live',
  reconnecting: 'Reconnecting',
  replaced: 'Stopped: opened elsewhere',
  closed: 'Disconnected',
}

export function ConnectionStatus({ status }: { status: SocketStatus }) {
  return (
    <p className={`conn conn-${status}`} role="status" aria-live="polite">
      Connection: <strong>{statusText[status]}</strong>
    </p>
  )
}

export function Countdown({ until, now, offsetMs, label }: { until: number; now: number; offsetMs: number; label: string }) {
  const secs = Math.ceil(remainingMs(until, now, offsetMs) / 1000)
  return (
    <p className="countdown" aria-live="off">
      {label} <strong>{secs}s</strong>
    </p>
  )
}

export function Standings({ board, you, title }: { board: Leaderboard | null; you: You | null; title: string }) {
  if (!board) return <p className="muted">Loading the leaderboard.</p>
  const inTop = you !== null && board.top.some((e) => e.participantId === you.participantId)
  return (
    <section className="standings" aria-label={title}>
      <h2>{title}</h2>
      <p className="muted">
        {board.participantCount} {board.participantCount === 1 ? 'player' : 'players'}
      </p>
      {board.top.length === 0 ? (
        <p className="muted">No scores yet. Points appear after the first answers.</p>
      ) : (
        <ol className="board">
          {board.top.map((e) => (
            <li key={e.participantId} className={e.participantId === you?.participantId ? 'me' : undefined}>
              <span className="rank">{e.rank}</span>
              <span className="name">{e.displayName}</span>
              <span className="score">{e.score}</span>
            </li>
          ))}
          {you && !inTop && (
            <li className="me own-row" aria-label="Your position">
              <span className="rank">{you.rank}</span>
              <span className="name">{you.displayName} (you)</span>
              <span className="score">{you.score}</span>
            </li>
          )}
        </ol>
      )}
    </section>
  )
}
