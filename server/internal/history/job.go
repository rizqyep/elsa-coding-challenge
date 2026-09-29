package history

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// JobKind is the kind of persistence job (TRD §4.5).
type JobKind string

// Job kinds; the prefix of the job ID in sched:flush.
const (
	FlushAnswers JobKind = "q"
	Finalize     JobKind = "final"
)

// Job is a persistence job: "q|<code>|<question>" or "final|<code>".
type Job struct {
	ID         string
	Kind       JobKind
	Code       quiz.Code
	QuestionID quiz.QuestionID
}

// ErrMalformedJob is returned for job IDs that don't follow the format.
var ErrMalformedJob = errors.New("malformed job id")

// ParseJob parses a job ID.
func ParseJob(id string) (Job, error) {
	parts := strings.Split(id, "|")
	switch {
	case len(parts) == 3 && parts[0] == string(FlushAnswers) && parts[1] != "" && parts[2] != "":
		return Job{ID: id, Kind: FlushAnswers, Code: quiz.Code(parts[1]), QuestionID: quiz.QuestionID(parts[2])}, nil
	case len(parts) == 2 && parts[0] == string(Finalize) && parts[1] != "":
		return Job{ID: id, Kind: Finalize, Code: quiz.Code(parts[1])}, nil
	}
	return Job{}, fmt.Errorf("%w: %q", ErrMalformedJob, id)
}

// StoredAnswer is one accepted answer as the answer script stored it.
type StoredAnswer struct {
	ParticipantID quiz.ParticipantID
	OptionID      quiz.OptionID
	Correct       bool
	Points        int
	ReceivedAt    int64
}

// ParseStoredAnswer parses "option|correct|points|received_ms" (TRD §4.2).
func ParseStoredAnswer(participant, v string) (StoredAnswer, error) {
	f := strings.Split(v, "|")
	if len(f) != 4 || (f[1] != "0" && f[1] != "1") {
		return StoredAnswer{}, fmt.Errorf("stored answer %q: bad format", v)
	}
	points, err1 := strconv.Atoi(f[2])
	at, err2 := strconv.ParseInt(f[3], 10, 64)
	if err := errors.Join(err1, err2); err != nil {
		return StoredAnswer{}, fmt.Errorf("stored answer %q: %w", v, err)
	}
	return StoredAnswer{ParticipantID: quiz.ParticipantID(participant), OptionID: quiz.OptionID(f[0]),
		Correct: f[1] == "1", Points: points, ReceivedAt: at}, nil
}

// FinalData is what the final-results job reads from the live store.
type FinalData struct {
	Status  quiz.Status
	Entries []leaderboard.Entry // whole leaderboard, highest first
}

// Result is one participant's final standing.
type Result struct {
	ParticipantID quiz.ParticipantID
	DisplayName   string
	Score         int
	Rank          int
}

// RankResults turns leaderboard entries (highest first) into ranked results (FR-24).
func RankResults(entries []leaderboard.Entry) []Result {
	ranked := append([]leaderboard.Entry(nil), entries...)
	leaderboard.Rank(ranked)
	out := make([]Result, len(ranked))
	for i, e := range ranked {
		out[i] = Result{ParticipantID: e.ParticipantID, DisplayName: e.DisplayName, Score: e.Score, Rank: e.Rank}
	}
	return out
}

// FinalizeInput is written in one transaction (TRD §4.5).
type FinalizeInput struct {
	Code    quiz.Code
	Status  quiz.Status // finished or expired
	Results []Result
}

// Mismatch is a participant whose live total differs from the total recomputed from stored answers (FR-35).
type Mismatch struct {
	ParticipantID quiz.ParticipantID
	Live          int
	Recomputed    int
}
