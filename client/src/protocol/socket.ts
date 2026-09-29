import { backoffMs, closeAction, offsetFromPong } from './policy'
import type { ServerMessage } from './types'

export type SocketStatus = 'connecting' | 'open' | 'reconnecting' | 'replaced' | 'closed'

type Hello =
  | { type: 'join'; data: { quizCode: string; displayName: string } }
  | { type: 'watch'; data: { quizCode: string } }

export interface SocketOptions {
  url: () => Promise<string> // called per connection, so a renewed token is used
  hello: Hello
  onMessage: (m: ServerMessage) => void
  onStatus: (s: SocketStatus) => void
  WebSocketImpl?: typeof WebSocket
  random?: () => number
  now?: () => number
}

const pingEveryMs = 30_000

// One quiz connection: joins (or watches) on every connect, keeps unanswered answers and resends
// them with their original ids after a reconnect, and tracks the server clock offset (TRD §9.5).
export class QuizSocket {
  offsetMs = 0
  private ws: WebSocket | null = null
  private joined = false
  private stopped = false
  private attempt = 0
  private seq = 0
  private helloId: string | null = null
  private readonly pending = new Map<string, string>()
  private readonly pings = new Map<string, number>()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private pingTimer: ReturnType<typeof setTimeout> | null = null
  private readonly Impl: typeof WebSocket
  private readonly random: () => number
  private readonly now: () => number

  private readonly o: SocketOptions

  constructor(o: SocketOptions) {
    this.o = o
    this.Impl = o.WebSocketImpl ?? WebSocket
    this.random = o.random ?? Math.random
    this.now = o.now ?? Date.now
  }

  start() {
    this.o.onStatus('connecting')
    void this.connect()
  }

  submitAnswer(questionId: string, optionId: string): string {
    const id = this.nextId('a')
    const frame = JSON.stringify({ v: 1, type: 'submit_answer', id, data: { questionId, optionId } })
    this.pending.set(id, frame)
    if (this.joined) this.ws?.send(frame)
    return id
  }

  ping(): string {
    const id = this.nextId('p')
    const sentAt = this.now()
    this.pings.set(id, sentAt)
    if (this.joined) this.ws?.send(JSON.stringify({ v: 1, type: 'ping', id, data: { clientTime: sentAt } }))
    return id
  }

  // leave tells the server this is on purpose (removed in the lobby, score kept after start), then stops.
  leave() {
    if (this.joined && this.o.hello.type === 'join') {
      this.ws?.send(JSON.stringify({ v: 1, type: 'leave', id: this.nextId('l'), data: {} }))
    }
    this.close()
  }

  close() {
    this.stopped = true
    this.clearTimers()
    this.ws?.close()
    this.o.onStatus('closed')
  }

  private nextId(prefix: string) {
    this.seq += 1
    return `${prefix}${this.seq}-${Math.floor(this.random() * 1e9).toString(36)}`
  }

  private async connect() {
    let url: string
    try {
      url = await this.o.url()
    } catch {
      this.scheduleReconnect()
      return
    }
    if (this.stopped) return
    const ws = new this.Impl(url)
    this.ws = ws
    this.joined = false
    ws.onopen = () => {
      this.helloId = this.nextId('h')
      ws.send(JSON.stringify({ v: 1, type: this.o.hello.type, id: this.helloId, data: this.o.hello.data }))
    }
    ws.onmessage = (e: MessageEvent) => this.handle(JSON.parse(e.data as string) as ServerMessage)
    ws.onclose = (e: CloseEvent) => this.onClose(e.code)
  }

  private handle(m: ServerMessage) {
    const id = 'id' in m ? m.id : undefined
    if (id !== undefined && id === this.helloId) {
      if (m.type === 'snapshot') {
        this.joined = true
        this.attempt = 0
        this.o.onStatus('open')
        for (const frame of this.pending.values()) this.ws?.send(frame) // same ids: the server returns the original result
        this.ping()
      } else if (m.type === 'error') {
        this.helloRejected(m.data.retryable, m.data.retryAfterMs ?? 0)
      }
    }
    if (m.type === 'pong' && id !== undefined) {
      const sentAt = this.pings.get(id)
      if (sentAt !== undefined) {
        this.pings.delete(id)
        this.offsetMs = offsetFromPong(sentAt, m.data.serverTime, this.now())
        this.schedulePing()
      }
    }
    if ((m.type === 'answer_result' || m.type === 'error') && id !== undefined && this.pending.has(id)) {
      if (m.type === 'error' && m.data.retryable) {
        const frame = this.pending.get(id)!
        setTimeout(() => this.joined && this.ws?.send(frame), m.data.retryAfterMs ?? 1_000)
      } else {
        this.pending.delete(id)
      }
    }
    this.o.onMessage(m)
  }

  // A busy server gets retried after its delay; a refused join (unknown quiz, forbidden) is final.
  private helloRejected(retryable: boolean, retryAfterMs: number) {
    const ws = this.ws
    this.ws = null
    if (ws) {
      ws.onclose = null
      ws.close()
    }
    if (retryable) {
      this.scheduleReconnect(retryAfterMs)
    } else {
      this.stopped = true
      this.o.onStatus('closed')
    }
  }

  private onClose(code: number) {
    this.ws = null
    this.joined = false
    this.clearTimers()
    if (this.stopped) {
      this.o.onStatus('closed')
      return
    }
    const action = closeAction(code)
    if (action === 'reconnect') {
      this.scheduleReconnect()
      return
    }
    this.stopped = true
    this.o.onStatus(action === 'replaced' ? 'replaced' : 'closed')
  }

  private scheduleReconnect(atLeastMs = 0) {
    this.o.onStatus('reconnecting')
    const delay = backoffMs(this.attempt, this.random, atLeastMs)
    this.attempt += 1
    this.reconnectTimer = setTimeout(() => void this.connect(), delay)
  }

  private schedulePing() {
    if (this.pingTimer) clearTimeout(this.pingTimer)
    this.pingTimer = setTimeout(() => this.ping(), pingEveryMs)
  }

  private clearTimers() {
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    if (this.pingTimer) clearTimeout(this.pingTimer)
    this.reconnectTimer = this.pingTimer = null
  }
}
