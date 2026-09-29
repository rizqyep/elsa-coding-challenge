// many-rooms with k6: an independent load generator for the same shape the Go simulator runs (TRD §10.7).
// It measures latency and errors only; score correctness is checked by cmd/sim.
import http from 'k6/http'
import { WebSocket } from 'k6/experimental/websockets'
import { setTimeout } from 'k6/timers'
import { Counter, Trend } from 'k6/metrics'

const BASE = __ENV.BASE || 'http://localhost:8080'
const WS = BASE.replace(/^http/, 'ws') + '/ws'
const ROOMS = Number(__ENV.ROOMS || 50)
const PER_ROOM = Number(__ENV.PER_ROOM || 20)
const json = { headers: { 'Content-Type': 'application/json' } }

const answerMs = new Trend('answer_result_ms', true)
const questionMs = new Trend('question_delivery_ms', true)
const accepted = new Counter('answers_accepted')
const wsErrors = new Counter('ws_errors')
const finished = new Counter('players_finished')

export const options = {
  scenarios: { players: { executor: 'per-vu-iterations', vus: ROOMS * PER_ROOM, iterations: 1, maxDuration: '3m' } },
  thresholds: {
    answer_result_ms: ['p(95)<100'],
    question_delivery_ms: ['p(95)<200'],
    ws_errors: ['count==0'],
    players_finished: [`count==${ROOMS * PER_ROOM}`],
  },
}

export function setup() {
  const host = http.post(`${BASE}/api/v1/dev/tokens`, JSON.stringify({ role: 'host' }), json).json('token')
  const auth = { headers: { ...json.headers, Authorization: `Bearer ${host}` } }
  const codes = []
  for (let i = 0; i < ROOMS; i++) {
    const body = JSON.stringify({ questionSetId: 'demo-quick', questionWindowSeconds: 8, revealSeconds: 2 })
    codes.push(http.post(`${BASE}/api/v1/quizzes`, body, auth).json('code'))
  }
  return { host, codes }
}

export default function (data) {
  const room = (__VU - 1) % ROOMS
  const first = __VU <= ROOMS // one player per room starts it once everyone has had time to join
  const code = data.codes[room]
  const token = http.post(`${BASE}/api/v1/dev/tokens`, JSON.stringify({ role: 'participant' }), json).json('token')
  const ws = new WebSocket(`${WS}?token=${token}`, null, { headers: { Origin: BASE } })
  const seen = {}
  const sent = {}
  let seq = 0
  const send = (type, payload) => {
    const id = `${type}-${++seq}`
    ws.send(JSON.stringify({ v: 1, type, id, data: payload }))
    return id
  }
  ws.onopen = () => send('join', { quizCode: code, displayName: `K${__VU}` })
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data)
    if (m.type === 'snapshot' && first && m.data.quiz.status === 'lobby') {
      setTimeout(() => {
        const auth = { headers: { ...json.headers, Authorization: `Bearer ${data.host}` } }
        http.post(`${BASE}/api/v1/quizzes/${code}/start`, '{}', auth)
      }, 5000)
    } else if (m.type === 'question') {
      const q = m.data.question
      if (seen[q.questionId]) return // an early close re-sends it
      seen[q.questionId] = true
      questionMs.add(Date.now() - q.openedAt)
      setTimeout(() => {
        const opt = q.options[Math.floor(Math.random() * q.options.length)].id
        sent[send('submit_answer', { questionId: q.questionId, optionId: opt })] = Date.now()
      }, Math.random() * 3000)
    } else if (m.type === 'answer_result' && sent[m.id]) {
      answerMs.add(Date.now() - sent[m.id])
      if (m.data.status === 'accepted') accepted.add(1)
    } else if (m.type === 'error') {
      wsErrors.add(1, { code: m.data.code })
    } else if (m.type === 'quiz_finished') {
      finished.add(1)
      ws.close()
    }
  }
}
