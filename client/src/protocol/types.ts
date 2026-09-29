import type {
  AnswerResultMessage,
  ErrorMessage,
  JoinMessage,
  LeaderboardMessage,
  PingMessage,
  PongMessage,
  QuestionClosedMessage,
  QuestionMessage,
  QuizFinishedMessage,
  QuizStateMessage,
  RankMessage,
  SnapshotMessage,
  SubmitAnswerMessage,
  WatchMessage,
} from './messages'

// Every message the gateway sends (docs/api/asyncapi.yaml).
export type ServerMessage =
  | SnapshotMessage
  | QuestionMessage
  | QuestionClosedMessage
  | AnswerResultMessage
  | RankMessage
  | LeaderboardMessage
  | QuizFinishedMessage
  | QuizStateMessage
  | ErrorMessage
  | PongMessage

export type ClientMessage = JoinMessage | WatchMessage | SubmitAnswerMessage | PingMessage

export type ErrorCode = ErrorMessage['data']['code']
