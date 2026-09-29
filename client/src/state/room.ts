import type { Leaderboard, LeaderboardEntry, PublicQuestion, QuizState } from '../protocol/messages'
import type { ErrorCode, ServerMessage } from '../protocol/types'

export interface You {
  participantId: string
  displayName: string
  score: number
  rank: number
}

export interface YourAnswer {
  questionId: string
  optionId: string
  correct: boolean
  points: number
}

// What one connection knows about its room, built only from server messages.
export interface RoomState {
  quizCode: string | null
  role: 'participant' | 'host' | null
  quiz: QuizState | null
  question: PublicQuestion | null
  correctOptionId: string | null
  nextTransitionAt: number | null
  leaderboard: Leaderboard | null
  you: You | null
  yourAnswer: YourAnswer | null
  finalTop: LeaderboardEntry[] | null
  lastError: { code: ErrorCode; message: string } | null
}

export const emptyRoom: RoomState = {
  quizCode: null,
  role: null,
  quiz: null,
  question: null,
  correctOptionId: null,
  nextTransitionAt: null,
  leaderboard: null,
  you: null,
  yourAnswer: null,
  finalTop: null,
  lastError: null,
}

const finished = (s: RoomState) => s.quiz?.status === 'finished'

// Quiz state and the leaderboard version separately; finished is terminal (asyncapi.yaml, FR-28).
const newerState = (s: RoomState, v: number) => !finished(s) && (s.quiz === null || v > s.quiz.stateVersion)
const newerBoard = (s: RoomState, v: number) => s.leaderboard === null || v > s.leaderboard.version

export function reduce(s: RoomState, m: ServerMessage): RoomState {
  switch (m.type) {
    case 'snapshot': {
      const d = m.data
      let n: RoomState = { ...s, quizCode: d.quizCode, role: d.role }
      const endsQuiz = d.quiz.status === 'finished' && !finished(s)
      if (endsQuiz || newerState(s, d.quiz.stateVersion)) {
        n = {
          ...n,
          quiz: d.quiz,
          question: d.question,
          correctOptionId: d.correctOptionId,
          yourAnswer: d.yourAnswer,
          you: d.you ?? n.you,
          nextTransitionAt: null,
          lastError: null,
        }
        if (endsQuiz) return { ...n, leaderboard: d.leaderboard, finalTop: d.leaderboard.top }
      } else if (n.you === null) {
        n = { ...n, you: d.you }
      }
      return newerBoard(n, d.leaderboard.version) ? { ...n, leaderboard: d.leaderboard } : n
    }
    case 'question': {
      const { question, stateVersion } = m.data
      if (!newerState(s, stateVersion)) return s
      const sameQuestion = s.yourAnswer?.questionId === question.questionId
      return {
        ...s,
        quiz: { status: 'question_open', questionIndex: question.index, questionCount: question.count, stateVersion },
        question,
        correctOptionId: null,
        nextTransitionAt: null,
        yourAnswer: sameQuestion ? s.yourAnswer : null,
        lastError: null,
      }
    }
    case 'question_closed': {
      const d = m.data
      if (!newerState(s, d.stateVersion)) return s
      return {
        ...s,
        quiz: {
          status: 'question_closed',
          questionIndex: s.quiz?.questionIndex ?? s.question?.index ?? 0,
          questionCount: s.quiz?.questionCount ?? s.question?.count ?? 1,
          stateVersion: d.stateVersion,
        },
        correctOptionId: d.correctOptionId,
        nextTransitionAt: d.nextTransitionAt,
      }
    }
    case 'quiz_state':
      if (!newerState(s, m.data.stateVersion)) return s
      return { ...s, quiz: m.data, question: m.data.status === 'lobby' ? null : s.question }
    case 'quiz_finished': {
      if (finished(s)) return s
      const d = m.data
      return {
        ...s,
        quiz: {
          status: 'finished',
          questionIndex: s.quiz?.questionIndex ?? 0,
          questionCount: s.quiz?.questionCount ?? 1,
          stateVersion: d.stateVersion,
        },
        question: null,
        correctOptionId: null,
        nextTransitionAt: null,
        finalTop: d.finalTop,
        leaderboard: { version: s.leaderboard?.version ?? 0, participantCount: d.participantCount, top: d.finalTop },
      }
    }
    case 'leaderboard':
      return newerBoard(s, m.data.version) ? { ...s, leaderboard: m.data } : s
    case 'answer_result': {
      const d = m.data
      return {
        ...s,
        yourAnswer: { questionId: d.questionId, optionId: d.optionId, correct: d.correct, points: d.points },
        you: s.you && { ...s.you, score: d.totalScore },
        lastError: null,
      }
    }
    case 'rank':
      return { ...s, you: s.you && { ...s.you, score: m.data.score, rank: m.data.rank } }
    case 'error':
      return { ...s, lastError: { code: m.data.code, message: m.data.message } }
    case 'pong':
      return s
  }
}
