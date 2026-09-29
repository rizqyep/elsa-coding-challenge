import { useEffect, useReducer, useRef, useState } from 'react'
import { token, wsUrl } from '../api/identity'
import { QuizSocket, type SocketStatus } from '../protocol/socket'
import { emptyRoom, reduce } from './room'

export type Hello =
  | { type: 'join'; data: { quizCode: string; displayName: string } }
  | { type: 'watch'; data: { quizCode: string } }

// useRoom connects for the lifetime of the component and folds server messages into room state.
export function useRoom(hello: Hello) {
  const [state, dispatch] = useReducer(reduce, emptyRoom)
  const [status, setStatus] = useState<SocketStatus>('connecting')
  const socket = useRef<QuizSocket | null>(null)
  const role = hello.type === 'join' ? 'participant' : 'host'
  const key = JSON.stringify(hello)

  useEffect(() => {
    const s = new QuizSocket({
      url: async () => wsUrl(await token(role)),
      hello: JSON.parse(key) as Hello,
      onMessage: dispatch,
      onStatus: setStatus,
    })
    socket.current = s
    s.start()
    return () => s.close()
  }, [key, role])

  return {
    state,
    status,
    submitAnswer: (questionId: string, optionId: string) => socket.current?.submitAnswer(questionId, optionId),
    offsetMs: () => socket.current?.offsetMs ?? 0,
  }
}

// useNow re-renders every interval while active, for countdowns.
export function useNow(active: boolean, intervalMs = 250): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    const t = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(t)
  }, [active, intervalMs])
  return now
}
