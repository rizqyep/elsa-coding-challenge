import type { components } from './schema'

type Schemas = components['schemas']
export type Role = Schemas['Role']
export type DevToken = Schemas['DevToken']
export type Quiz = Schemas['Quiz']
export type QuestionSetSummary = Schemas['QuestionSetSummary']
// Defaulted fields are optional on the wire; the server fills them in.
export type CreateQuiz = Pick<Schemas['CreateQuizRequest'], 'questionSetId'> & Partial<Schemas['CreateQuizRequest']>

const base = '/api/v1'

// A failed REST call: the problem details' machine-readable code, and Retry-After when the server sent one.
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly retryAfterSeconds?: number
  constructor(status: number, code: string, message: string, retryAfterSeconds?: number) {
    super(message)
    this.status = status
    this.code = code
    this.retryAfterSeconds = retryAfterSeconds
  }
}

async function call<T>(path: string, init: RequestInit & { token?: string } = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body !== undefined) headers.set('Content-Type', 'application/json')
  if (init.token) headers.set('Authorization', `Bearer ${init.token}`)
  let res: Response
  try {
    res = await fetch(base + path, { ...init, headers })
  } catch {
    throw new ApiError(0, 'network', 'Cannot reach the quiz server.')
  }
  if (res.ok) return (await res.json()) as T
  let code = 'internal'
  let message = `Request failed (${res.status}).`
  try {
    const p = (await res.json()) as Schemas['Problem']
    code = p.code
    message = p.detail ?? p.title
  } catch {
    // not a problem-details body; keep the generic message
  }
  const retry = Number(res.headers.get('Retry-After'))
  throw new ApiError(res.status, code, message, Number.isFinite(retry) && retry > 0 ? retry : undefined)
}

export const api = {
  devToken: (role: Role, participantId?: string) =>
    call<DevToken>('/dev/tokens', { method: 'POST', body: JSON.stringify(participantId ? { role, participantId } : { role }) }),
  questionSets: (token: string) => call<{ items: QuestionSetSummary[] }>('/question-sets', { token }),
  createQuiz: (token: string, body: CreateQuiz) =>
    call<Quiz>('/quizzes', { method: 'POST', token, body: JSON.stringify(body) }),
  startQuiz: (token: string, code: string) =>
    call<Schemas['StartAccepted']>(`/quizzes/${encodeURIComponent(code)}/start`, { method: 'POST', token }),
}
