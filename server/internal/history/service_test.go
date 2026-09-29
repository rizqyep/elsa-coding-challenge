package history_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history/mocks"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

type fixture struct {
	svc        *history.Service
	live       *mocks.MockLiveStore
	store      *mocks.MockStore
	mismatches int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{live: mocks.NewMockLiveStore(ctrl), store: mocks.NewMockStore(ctrl)}
	f.svc = history.NewService(f.live, f.store, history.Options{
		TTL: time.Hour, FinalRetryDelay: time.Second,
		OnMismatch: func(quiz.Code, []history.Mismatch) { f.mismatches++ },
	})
	return f
}

var (
	flushJob = history.Job{ID: "q|K7Q2MX|dq-01", Kind: history.FlushAnswers, Code: "K7Q2MX", QuestionID: "dq-01"}
	finalJob = history.Job{ID: "final|K7Q2MX", Kind: history.Finalize, Code: "K7Q2MX"}
	answers  = []history.StoredAnswer{{ParticipantID: "u_1", OptionID: "dq-01-b", Correct: true, Points: 186, ReceivedAt: 1}}
)

func TestProcessFlush_SavesThenAcknowledges(t *testing.T) {
	f := newFixture(t)
	gomock.InOrder(
		f.live.EXPECT().ExtendTTL(gomock.Any(), flushJob.Code, flushJob.QuestionID, time.Hour).Return(nil),
		f.live.EXPECT().ReadAnswers(gomock.Any(), flushJob.Code, flushJob.QuestionID).Return(answers, nil),
		f.store.EXPECT().SaveAnswers(gomock.Any(), flushJob.Code, flushJob.QuestionID, answers).Return(nil),
		f.live.EXPECT().AckFlush(gomock.Any(), flushJob).Return(nil),
	)
	if err := f.svc.Process(context.Background(), flushJob); err != nil {
		t.Fatal(err)
	}
}

// FR-34: answers leave Redis only after the database commit.
func TestProcessFlush_FailedSaveIsNeverAcknowledged(t *testing.T) {
	f := newFixture(t)
	f.live.EXPECT().ExtendTTL(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.live.EXPECT().ReadAnswers(gomock.Any(), gomock.Any(), gomock.Any()).Return(answers, nil)
	f.store.EXPECT().SaveAnswers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("commit failed"))
	f.live.EXPECT().AckFlush(gomock.Any(), gomock.Any()).Times(0)
	if err := f.svc.Process(context.Background(), flushJob); err == nil {
		t.Error("expected the save error")
	}
}

func TestProcessFlush_NothingToSaveIsAcknowledged(t *testing.T) {
	f := newFixture(t) // already flushed by an earlier attempt, or nobody answered
	f.live.EXPECT().ExtendTTL(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.live.EXPECT().ReadAnswers(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	f.store.EXPECT().SaveAnswers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	f.live.EXPECT().AckFlush(gomock.Any(), flushJob).Return(nil)
	if err := f.svc.Process(context.Background(), flushJob); err != nil {
		t.Fatal(err)
	}
}

func TestProcessFinal_WaitsForPendingFlushes(t *testing.T) {
	f := newFixture(t)
	f.live.EXPECT().PendingFlushes(gomock.Any(), finalJob.Code).Return(int64(2), nil)
	f.live.EXPECT().Defer(gomock.Any(), finalJob, time.Second).Return(nil)
	f.store.EXPECT().Finalize(gomock.Any(), gomock.Any()).Times(0)
	f.live.EXPECT().Release(gomock.Any(), gomock.Any()).Times(0)
	if err := f.svc.Process(context.Background(), finalJob); err != nil {
		t.Fatal(err)
	}
}

func TestProcessFinal_WritesRankedResultsThenReleases(t *testing.T) {
	f := newFixture(t)
	final := history.FinalData{Status: quiz.StatusFinished, Entries: []leaderboard.Entry{
		{ParticipantID: "u_a", DisplayName: "Rina", Score: 574},
		{ParticipantID: "u_b", DisplayName: "Tomás", Score: 551},
		{ParticipantID: "u_c", DisplayName: "Aiko", Score: 551},
	}}
	gomock.InOrder(
		f.live.EXPECT().PendingFlushes(gomock.Any(), finalJob.Code).Return(int64(0), nil),
		f.live.EXPECT().ReadFinal(gomock.Any(), finalJob.Code).Return(final, nil),
		f.store.EXPECT().Finalize(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in history.FinalizeInput) ([]history.Mismatch, error) {
			var ranks []int
			for _, r := range in.Results {
				ranks = append(ranks, r.Rank)
			}
			if in.Code != "K7Q2MX" || in.Status != quiz.StatusFinished || fmt.Sprint(ranks) != "[1 2 2]" {
				t.Errorf("finalize input %+v (ranks %v, want [1 2 2])", in, ranks)
			}
			return nil, nil
		}),
		f.live.EXPECT().Release(gomock.Any(), finalJob).Return(nil),
	)
	if err := f.svc.Process(context.Background(), finalJob); err != nil {
		t.Fatal(err)
	}
	if f.mismatches != 0 {
		t.Errorf("mismatch hook called %d times", f.mismatches)
	}
}

// FR-35: a mismatch is reported but doesn't block finishing the quiz.
func TestProcessFinal_ReportsMismatchesAndStillReleases(t *testing.T) {
	f := newFixture(t)
	f.live.EXPECT().PendingFlushes(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	f.live.EXPECT().ReadFinal(gomock.Any(), gomock.Any()).Return(history.FinalData{Status: quiz.StatusFinished}, nil)
	f.store.EXPECT().Finalize(gomock.Any(), gomock.Any()).Return([]history.Mismatch{{ParticipantID: "u_1", Live: 200, Recomputed: 186}}, nil)
	f.live.EXPECT().Release(gomock.Any(), finalJob).Return(nil)
	if err := f.svc.Process(context.Background(), finalJob); err != nil {
		t.Fatal(err)
	}
	if f.mismatches != 1 {
		t.Errorf("mismatch hook called %d times, want 1", f.mismatches)
	}
}

func TestProcessFinal_FailedFinalizeKeepsTheRoom(t *testing.T) {
	f := newFixture(t)
	f.live.EXPECT().PendingFlushes(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	f.live.EXPECT().ReadFinal(gomock.Any(), gomock.Any()).Return(history.FinalData{Status: quiz.StatusFinished}, nil)
	f.store.EXPECT().Finalize(gomock.Any(), gomock.Any()).Return(nil, errors.New("tx aborted"))
	f.live.EXPECT().Release(gomock.Any(), gomock.Any()).Times(0)
	if err := f.svc.Process(context.Background(), finalJob); err == nil {
		t.Error("expected the finalize error")
	}
}

func TestProcessFinal_RoomAlreadyGoneDropsTheJob(t *testing.T) {
	f := newFixture(t)
	f.live.EXPECT().PendingFlushes(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	f.live.EXPECT().ReadFinal(gomock.Any(), gomock.Any()).Return(history.FinalData{}, quiz.ErrUnknownQuiz)
	f.store.EXPECT().Finalize(gomock.Any(), gomock.Any()).Times(0)
	f.live.EXPECT().Release(gomock.Any(), finalJob).Return(nil) // removes the orphaned job instead of retrying forever
	if err := f.svc.Process(context.Background(), finalJob); err != nil {
		t.Fatal(err)
	}
}
