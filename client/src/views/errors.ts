import type { ErrorCode } from '../protocol/types'

// Shown to people, not logged: what went wrong and what to do next.
const errorText: Partial<Record<ErrorCode, string>> = {
  unknown_quiz: 'No quiz uses this code. Check the code with your host.',
  quiz_expired: 'This quiz has expired. Ask your host for a new code.',
  invalid_display_name: "That name isn't allowed. Use 1 to 20 letters, digits, spaces, or - _ . '",
  forbidden: 'This account cannot do that here.',
  question_closed: 'Too late: the question closed before your answer arrived.',
  wrong_question: 'That question is no longer open.',
  server_busy: 'The server is busy. Retrying.',
  rate_limited: 'Too many requests. Slow down a little.',
}

export function describeError(code: ErrorCode, fallback: string): string {
  return errorText[code] ?? fallback
}
