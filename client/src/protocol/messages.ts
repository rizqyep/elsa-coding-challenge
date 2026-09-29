// Generated from docs/api/schemas by tools/contracts/gen-protocol.mjs. Do not edit; run `make generate`.

export interface Protocol {
  client_join?: JoinMessage
  client_ping?: PingMessage
  client_submit_answer?: SubmitAnswerMessage
  client_watch?: WatchMessage
  server_answer_result?: AnswerResultMessage
  server_error?: ErrorMessage
  server_leaderboard?: LeaderboardMessage
  server_pong?: PongMessage
  server_question?: QuestionMessage
  server_question_closed?: QuestionClosedMessage
  server_quiz_finished?: QuizFinishedMessage
  server_quiz_state?: QuizStateMessage
  server_rank?: RankMessage
  server_snapshot?: SnapshotMessage
}
/**
 * Participant joins a quiz. One quiz per connection. Reply: snapshot (or error), with the same id.
 */
export interface JoinMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'join'
  data: {
    /**
     * 6 characters, no look-alikes (D16).
     */
    quizCode: string
    /**
     * Trimmed; letters, digits, spaces, - _ . ' (FR-15). Full rules are enforced server-side.
     */
    displayName: string
  }
  /**
   * Client-generated; echoed in the reply.
   */
  id: string
}
/**
 * Application-level ping, used by the client to estimate its clock offset from the server.
 */
export interface PingMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'ping'
  data: {
    clientTime: number
  }
  /**
   * Client-generated; echoed in the reply.
   */
  id?: string
}
/**
 * Answer the open question. Reply: answer_result or error, with the same id. Safe to resend (FR-18).
 */
export interface SubmitAnswerMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'submit_answer'
  data: {
    questionId: string
    optionId: string
  }
  /**
   * Client-generated; echoed in the reply.
   */
  id: string
}
/**
 * Host watches their quiz without playing. Host token only. Reply: snapshot.
 */
export interface WatchMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'watch'
  data: {
    /**
     * 6 characters, no look-alikes (D16).
     */
    quizCode: string
  }
  /**
   * Client-generated; echoed in the reply.
   */
  id: string
}
/**
 * Reply to submit_answer. status=duplicate returns the originally stored result (FR-18).
 */
export interface AnswerResultMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'answer_result'
  data: {
    questionId: string
    status: 'accepted' | 'duplicate'
    optionId: string
    correct: boolean
    points: number
    totalScore: number
    receivedAt: number
  }
  /**
   * Client-generated; echoed in the reply.
   */
  id: string
}
/**
 * A request failed, or a connection-level problem. Carries the request id when it answers a request.
 */
export interface ErrorMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'error'
  data: {
    code:
      | 'invalid_message'
      | 'unsupported_version'
      | 'unknown_type'
      | 'rate_limited'
      | 'forbidden'
      | 'unknown_quiz'
      | 'quiz_expired'
      | 'already_joined'
      | 'not_joined'
      | 'invalid_display_name'
      | 'wrong_question'
      | 'question_closed'
      | 'invalid_option'
      | 'server_busy'
      | 'internal'
    message: string
    retryable: boolean
    retryAfterMs?: number
  }
  /**
   * Client-generated; echoed in the reply.
   */
  id?: string
}
/**
 * Live leaderboard; identical bytes for everyone in the room (FR-26, NFR-5c).
 */
export interface LeaderboardMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'leaderboard'
  data: Leaderboard
}
export interface Leaderboard {
  /**
   * Monotonic; clients ignore anything older than what they have (FR-28).
   */
  version: number
  participantCount: number
  /**
   * @maxItems 100
   */
  top: LeaderboardEntry[]
}
export interface LeaderboardEntry {
  /**
   * Equal scores share a rank (FR-24).
   */
  rank: number
  participantId: string
  /**
   * Trimmed; letters, digits, spaces, - _ . ' (FR-15). Full rules are enforced server-side.
   */
  displayName: string
  score: number
}
/**
 * Reply to ping; serverTime lets the client estimate its clock offset for countdowns.
 */
export interface PongMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'pong'
  data: {
    clientTime: number
    serverTime: number
  }
}
/**
 * A question opened, or its close time moved earlier (early close). Idempotent by questionId: replace what is shown.
 */
export interface QuestionMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'question'
  data: {
    question: PublicQuestion
    /**
     * Monotonic; clients ignore anything older than what they have (FR-28).
     */
    stateVersion: number
  }
}
/**
 * A question as clients see it. Never contains the correct option (FR-21).
 */
export interface PublicQuestion {
  questionId: string
  index: number
  count: number
  prompt: string
  /**
   * @minItems 2
   * @maxItems 4
   */
  options:
    | [PublicOption, PublicOption]
    | [PublicOption, PublicOption, PublicOption]
    | [PublicOption, PublicOption, PublicOption, PublicOption]
  openedAt: number
  /**
   * Original deadline; the speed bonus is computed against it.
   */
  deadline: number
  /**
   * When answers stop being accepted; earlier than deadline after an early close. Countdowns use this.
   */
  closeAt: number
}
export interface PublicOption {
  id: string
  text: string
}
/**
 * The question closed; reveals the correct option (FR-21).
 */
export interface QuestionClosedMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'question_closed'
  data: {
    questionId: string
    correctOptionId: string
    nextTransitionAt: number
    /**
     * Monotonic; clients ignore anything older than what they have (FR-28).
     */
    stateVersion: number
  }
}
/**
 * The quiz finished. The full leaderboard is available from the REST API (FR-27).
 */
export interface QuizFinishedMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'quiz_finished'
  data: {
    /**
     * @maxItems 100
     */
    finalTop: LeaderboardEntry[]
    participantCount: number
    /**
     * Monotonic; clients ignore anything older than what they have (FR-28).
     */
    stateVersion: number
  }
}
/**
 * A lifecycle change with no question attached, e.g. the lobby expired.
 */
export interface QuizStateMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'quiz_state'
  data: QuizState
}
export interface QuizState {
  status: 'lobby' | 'question_open' | 'question_closed' | 'finished' | 'expired'
  questionIndex: number
  questionCount: number
  /**
   * Monotonic; clients ignore anything older than what they have (FR-28).
   */
  stateVersion: number
}
/**
 * The participant's own rank, sent once after each question closes (FR-26).
 */
export interface RankMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'rank'
  data: {
    questionId: string
    score: number
    rank: number
    participantCount: number
  }
}
/**
 * Full view of the room. Sent in reply to join/watch, and to resync a client (reconnect, slow client). Apply field by field, only where versions are newer (FR-28).
 */
export interface SnapshotMessage {
  /**
   * Protocol version.
   */
  v: 1
  type: 'snapshot'
  /**
   * question is null in lobby and after the quiz. correctOptionId is set only while the current question is closed (reveal). you and yourAnswer are null for the host.
   */
  data: {
    /**
     * 6 characters, no look-alikes (D16).
     */
    quizCode: string
    role: 'participant' | 'host'
    quiz: QuizState
    question: PublicQuestion | null
    you: {
      participantId: string
      /**
       * Trimmed; letters, digits, spaces, - _ . ' (FR-15). Full rules are enforced server-side.
       */
      displayName: string
      score: number
      rank: number
    } | null
    yourAnswer: {
      questionId: string
      optionId: string
      correct: boolean
      points: number
    } | null
    correctOptionId: string | null
    leaderboard: Leaderboard
    serverTime: number
  }
  /**
   * Client-generated; echoed in the reply.
   */
  id?: string
}
