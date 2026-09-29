import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QuizSocket, type SocketStatus } from './socket'
import type { ServerMessage } from './types'

class FakeWebSocket {
  static all: FakeWebSocket[] = []
  sent: Record<string, unknown>[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: ((e: { code: number }) => void) | null = null
  closed = false
  readonly url: string
  constructor(url: string) {
    this.url = url
    FakeWebSocket.all.push(this)
  }
  send(data: string) {
    this.sent.push(JSON.parse(data))
  }
  close() {
    this.closed = true
  }
  open() {
    this.onopen?.()
  }
  receive(m: ServerMessage) {
    this.onmessage?.({ data: JSON.stringify(m) })
  }
  drop(code: number) {
    this.onclose?.({ code })
  }
  types() {
    return this.sent.map((f) => `${f.type}:${f.id ?? ''}`)
  }
}

const snapshotReply = (id: string): ServerMessage => ({
  v: 1,
  type: 'snapshot',
  id,
  data: {
    quizCode: 'K7Q2MX',
    role: 'participant',
    quiz: { status: 'question_open', questionIndex: 0, questionCount: 3, stateVersion: 3 },
    question: null,
    you: { participantId: 'u_1', displayName: 'Rina', score: 0, rank: 1 },
    yourAnswer: null,
    correctOptionId: null,
    leaderboard: { version: 1, participantCount: 1, top: [] },
    serverTime: 1,
  },
})

let statuses: SocketStatus[]
let received: ServerMessage[]

function socket(clock = { now: 0 }) {
  statuses = []
  received = []
  const s = new QuizSocket({
    url: async () => 'ws://test/ws?token=t',
    hello: { type: 'join', data: { quizCode: 'K7Q2MX', displayName: 'Rina' } },
    onMessage: (m) => received.push(m),
    onStatus: (st) => statuses.push(st),
    WebSocketImpl: FakeWebSocket as unknown as typeof WebSocket,
    random: () => 0.5,
    now: () => clock.now,
  })
  return s
}

async function connected(s: QuizSocket): Promise<FakeWebSocket> {
  s.start()
  await vi.waitFor(() => expect(FakeWebSocket.all.length).toBeGreaterThan(0))
  const ws = FakeWebSocket.all.at(-1)!
  ws.open()
  return ws
}

beforeEach(() => {
  FakeWebSocket.all = []
})
afterEach(() => {
  vi.useRealTimers()
})

describe('QuizSocket', () => {
  it('sends the join first, with a request id, and reports open once joined', async () => {
    const s = socket()
    const ws = await connected(s)
    expect(ws.sent[0]).toMatchObject({ v: 1, type: 'join', data: { quizCode: 'K7Q2MX', displayName: 'Rina' } })
    expect(typeof ws.sent[0].id).toBe('string')
    ws.receive(snapshotReply(ws.sent[0].id as string))
    expect(statuses.at(-1)).toBe('open')
    expect(received[0].type).toBe('snapshot')
  })

  it('resends an unanswered answer with its original id after reconnecting (FR-30)', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const s = socket()
    const first = await connected(s)
    first.receive(snapshotReply(first.sent[0].id as string))
    const answerId = s.submitAnswer('q1', 'q1-b')
    first.drop(1006)
    expect(statuses.at(-1)).toBe('reconnecting')

    await vi.runOnlyPendingTimersAsync()
    await vi.waitFor(() => expect(FakeWebSocket.all.length).toBe(2))
    const second = FakeWebSocket.all[1]
    second.open()
    expect(second.types()).toEqual([`join:${second.sent[0].id}`]) // nothing before the join reply
    second.receive(snapshotReply(second.sent[0].id as string))
    expect(second.sent[1]).toMatchObject({ type: 'submit_answer', id: answerId, data: { questionId: 'q1', optionId: 'q1-b' } })
  })

  it('does not resend an answer that already got its result', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const s = socket()
    const first = await connected(s)
    first.receive(snapshotReply(first.sent[0].id as string))
    const id = s.submitAnswer('q1', 'q1-b')
    first.receive({ v: 1, type: 'answer_result', id, data: { questionId: 'q1', status: 'accepted', optionId: 'q1-b', correct: true, points: 186, totalScore: 186, receivedAt: 2 } })
    first.drop(1006)
    await vi.runOnlyPendingTimersAsync()
    await vi.waitFor(() => expect(FakeWebSocket.all.length).toBe(2))
    const second = FakeWebSocket.all[1]
    second.open()
    second.receive(snapshotReply(second.sent[0].id as string))
    expect(second.sent.filter((f) => f.type === 'submit_answer')).toHaveLength(0)
  })

  it('stops after being replaced by a newer connection (4000)', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const s = socket()
    const ws = await connected(s)
    ws.drop(4000)
    await vi.runAllTimersAsync()
    expect(statuses.at(-1)).toBe('replaced')
    expect(FakeWebSocket.all).toHaveLength(1)
  })

  it('updates the clock offset from pong', async () => {
    const clock = { now: 1_000 }
    const s = socket(clock)
    const ws = await connected(s)
    ws.receive(snapshotReply(ws.sent[0].id as string))
    const pingId = s.ping()
    const sentAt = ws.sent.find((f) => f.id === pingId)!.data as { clientTime: number }
    expect(sentAt.clientTime).toBe(1_000)
    clock.now = 1_100
    ws.receive({ v: 1, type: 'pong', id: pingId, data: { clientTime: 1_000, serverTime: 5_050 } })
    expect(s.offsetMs).toBe(4_000)
  })

  it('close() stops for good', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const s = socket()
    const ws = await connected(s)
    s.close()
    ws.drop(1000)
    await vi.runAllTimersAsync()
    expect(FakeWebSocket.all).toHaveLength(1)
    expect(statuses.at(-1)).toBe('closed')
  })
})
