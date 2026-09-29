import { describe, expect, it } from 'vitest'
import type { PublicQuestion } from '../protocol/messages'
import type { ServerMessage } from '../protocol/types'
import { emptyRoom, reduce, type RoomState } from './room'

const q = (id: string, index: number, closeAt = 20_000): PublicQuestion => ({
  questionId: id,
  index,
  count: 3,
  prompt: `Prompt ${id}`,
  options: [
    { id: `${id}-a`, text: 'a' },
    { id: `${id}-b`, text: 'b' },
  ],
  openedAt: 10_000,
  deadline: 20_000,
  closeAt,
})

const snapshot = (stateVersion: number, lbVersion: number, extra: Partial<Extract<ServerMessage, { type: 'snapshot' }>['data']> = {}): ServerMessage => ({
  v: 1,
  type: 'snapshot',
  id: 'j1',
  data: {
    quizCode: 'K7Q2MX',
    role: 'participant',
    quiz: { status: 'question_open', questionIndex: 0, questionCount: 3, stateVersion },
    question: q('q1', 0),
    you: { participantId: 'u_1', displayName: 'Rina', score: 0, rank: 1 },
    yourAnswer: null,
    correctOptionId: null,
    leaderboard: { version: lbVersion, participantCount: 2, top: [] },
    serverTime: 12_000,
    ...extra,
  },
})

const question = (id: string, index: number, stateVersion: number, closeAt?: number): ServerMessage => ({
  v: 1,
  type: 'question',
  data: { question: q(id, index, closeAt), stateVersion },
})

const leaderboard = (version: number, top = [{ rank: 1, participantId: 'u_2', displayName: 'Budi', score: 190 }]): ServerMessage => ({
  v: 1,
  type: 'leaderboard',
  data: { version, participantCount: 2, top },
})

const apply = (...msgs: ServerMessage[]): RoomState => msgs.reduce(reduce, emptyRoom)

describe('reduce: versions (FR-28)', () => {
  it('applies a snapshot to an empty room', () => {
    const s = apply(snapshot(3, 5))
    expect(s.quiz?.stateVersion).toBe(3)
    expect(s.question?.questionId).toBe('q1')
    expect(s.leaderboard?.version).toBe(5)
    expect(s.you?.displayName).toBe('Rina')
  })

  it('ignores an older question event and applies a newer one', () => {
    const s = apply(snapshot(3, 5), question('q0', 0, 2), question('q2', 1, 4))
    expect(s.question?.questionId).toBe('q2')
    expect(s.quiz?.stateVersion).toBe(4)
    expect(s.quiz?.questionIndex).toBe(1)
  })

  it('applies a snapshot field by field when an event beat it (subscribe-then-read race)', () => {
    // The newer question and leaderboard arrive before the older snapshot on a fresh connection.
    const s = apply(question('q2', 1, 6), leaderboard(9), snapshot(5, 7))
    expect(s.question?.questionId).toBe('q2')
    expect(s.quiz?.stateVersion).toBe(6)
    expect(s.leaderboard?.version).toBe(9)
    expect(s.quizCode).toBe('K7Q2MX')
    expect(s.you?.displayName).toBe('Rina')
  })

  it('ignores an older leaderboard', () => {
    const s = apply(snapshot(3, 5), leaderboard(8), leaderboard(6, []))
    expect(s.leaderboard?.version).toBe(8)
    expect(s.leaderboard?.top).toHaveLength(1)
  })

  it('treats finished as terminal: an archived snapshot with version 0 still applies', () => {
    const archived = snapshot(0, 0, {
      quiz: { status: 'finished', questionIndex: 2, questionCount: 3, stateVersion: 0 },
      question: null,
      leaderboard: { version: 0, participantCount: 2, top: [{ rank: 1, participantId: 'u_1', displayName: 'Rina', score: 390 }] },
      you: { participantId: 'u_1', displayName: 'Rina', score: 390, rank: 1 },
    })
    const s = apply(snapshot(7, 9), archived)
    expect(s.quiz?.status).toBe('finished')
    expect(s.question).toBeNull()
    expect(s.finalTop?.[0].score).toBe(390)
    expect(s.you?.score).toBe(390)
  })

  it('stays finished after an archived snapshot (version 0), even for a later-looking event', () => {
    const archived = snapshot(0, 0, {
      quiz: { status: 'finished', questionIndex: 2, questionCount: 3, stateVersion: 0 },
      question: null,
    })
    const s = apply(archived, question('q3', 2, 9))
    expect(s.quiz?.status).toBe('finished')
    expect(s.question).toBeNull()
  })

  it('never leaves finished for an older event', () => {
    const finished: ServerMessage = { v: 1, type: 'quiz_finished', data: { finalTop: [], participantCount: 2, stateVersion: 10 } }
    const s = apply(snapshot(3, 5), finished, question('q3', 2, 9))
    expect(s.quiz?.status).toBe('finished')
    expect(s.question).toBeNull()
  })
})

describe('reduce: question lifecycle', () => {
  it('reveals on close and keeps when the next question opens', () => {
    const closed: ServerMessage = { v: 1, type: 'question_closed', data: { questionId: 'q1', correctOptionId: 'q1-b', nextTransitionAt: 23_000, stateVersion: 4 } }
    let s = apply(snapshot(3, 5), closed)
    expect(s.quiz?.status).toBe('question_closed')
    expect(s.correctOptionId).toBe('q1-b')
    expect(s.nextTransitionAt).toBe(23_000)
    s = reduce(s, question('q2', 1, 5))
    expect(s.quiz?.status).toBe('question_open')
    expect(s.correctOptionId).toBeNull()
  })

  it('records the own answer and total; a new question clears it, an earlier close time keeps it', () => {
    const result: ServerMessage = {
      v: 1, type: 'answer_result', id: 'a1',
      data: { questionId: 'q1', status: 'accepted', optionId: 'q1-b', correct: true, points: 186, totalScore: 186, receivedAt: 12_100 },
    }
    let s = apply(snapshot(3, 5), result)
    expect(s.yourAnswer).toEqual({ questionId: 'q1', optionId: 'q1-b', correct: true, points: 186 })
    expect(s.you?.score).toBe(186)

    s = reduce(s, question('q1', 0, 4, 15_000)) // early close re-announces q1 with an earlier closeAt
    expect(s.question?.closeAt).toBe(15_000)
    expect(s.yourAnswer?.optionId).toBe('q1-b')

    s = reduce(s, question('q2', 1, 6))
    expect(s.yourAnswer).toBeNull()
  })

  it('updates the own rank after a close', () => {
    const rank: ServerMessage = { v: 1, type: 'rank', data: { questionId: 'q1', score: 186, rank: 1, participantCount: 2 } }
    const s = apply(snapshot(3, 5), rank)
    expect(s.you).toMatchObject({ score: 186, rank: 1 })
  })

  it('keeps the last error until the next state change', () => {
    const err: ServerMessage = { v: 1, type: 'error', id: 'a1', data: { code: 'question_closed', message: 'the question has closed', retryable: false } }
    const s = apply(snapshot(3, 5), err)
    expect(s.lastError?.code).toBe('question_closed')
    expect(reduce(s, question('q2', 1, 6)).lastError).toBeNull()
  })
})
