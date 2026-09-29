package redisx

// Key names: TRD §4.2. Room keys share the {code} hash tag (one cluster slot).

// Global schedule keys.
const (
	SchedTransitions      = "sched:transitions"
	SchedLeaderboardDirty = "sched:lbdirty"
	SchedFlush            = "sched:flush"
)

func roomPrefix(code string) string { return "quiz:{" + code + "}:" }

// RoomKey is the room control record (hash).
func RoomKey(code string) string { return roomPrefix(code) + "room" }

// QuestionIDsKey lists the quiz's question IDs in order (list).
func QuestionIDsKey(code string) string { return roomPrefix(code) + "qids" }

// RosterKey maps participant ID to display name (hash).
func RosterKey(code string) string { return roomPrefix(code) + "roster" }

// OnlineKey maps participant ID to last-seen milliseconds (sorted set).
func OnlineKey(code string) string { return roomPrefix(code) + "online" }

// LeaderboardKey maps participant ID to total score (sorted set).
func LeaderboardKey(code string) string { return roomPrefix(code) + "lb" }

// AnswersKey holds one question's accepted answers until they are flushed (hash).
func AnswersKey(code, questionID string) string { return roomPrefix(code) + "ans:" + questionID }

// RoomChannel is the pub/sub channel for the room's events.
func RoomChannel(code string) string { return "room:{" + code + "}" }
