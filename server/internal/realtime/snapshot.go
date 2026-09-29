package realtime

import (
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

// BuildSnapshot combines a room's shared view with one member's own standing (FR-11, FR-29).
func BuildSnapshot(v session.RoomView, set *quiz.QuestionSet, m Member, st session.Standing) protocol.Snapshot {
	r := v.Room
	snap := protocol.Snapshot{
		QuizCode: string(r.Code),
		Role:     "host",
		Quiz: protocol.QuizState{Status: r.Status, QuestionIndex: r.QuestionIndex,
			QuestionCount: r.QuestionCount, StateVersion: r.StateVersion},
		Leaderboard: protocol.Leaderboard{Version: r.LeaderboardVersion, ParticipantCount: v.ParticipantCount, Top: wireEntries(v.Top)},
		ServerTime:  v.ServerTime,
	}
	if r.Status == quiz.StatusQuestionOpen || r.Status == quiz.StatusQuestionClosed {
		if q, ok := lookup(set, r.QuestionID); ok {
			pq := protocol.QuestionFrom(q.Public(), r.QuestionIndex, r.QuestionCount, r.OpenedAt, r.Deadline, r.CloseAt)
			snap.Question = &pq
			if r.Status == quiz.StatusQuestionClosed {
				id := string(q.CorrectOptionID)
				snap.CorrectOptionID = &id
			}
		}
	}
	if m.ParticipantID == "" {
		return snap
	}
	snap.Role = "participant"
	rank := st.Rank
	if !st.Found {
		rank = v.ParticipantCount + 1
	}
	snap.You = &protocol.You{ParticipantID: string(m.ParticipantID), DisplayName: m.DisplayName, Score: st.Score, Rank: rank}
	if st.Answer != nil && snap.Question != nil {
		snap.YourAnswer = &protocol.YourAnswer{QuestionID: string(r.QuestionID), OptionID: string(st.Answer.OptionID),
			Correct: st.Answer.Correct, Points: st.Answer.Points}
	}
	return snap
}

func lookup(set *quiz.QuestionSet, id quiz.QuestionID) (quiz.Question, bool) {
	if set == nil || id == "" {
		return quiz.Question{}, false
	}
	return set.Question(id)
}
