package scoring_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring/mocks"
)

var in = scoring.AnswerInput{Code: "K7Q2MX", ParticipantID: "u_1", QuestionID: "dq-01", OptionID: "dq-01-b", Correct: true}

func newService(t *testing.T) (*scoring.Service, *mocks.MockRepository) {
	t.Helper()
	repo := mocks.NewMockRepository(gomock.NewController(t))
	return scoring.NewService(repo, 250*time.Millisecond), repo
}

func TestSubmitAnswer_ReturnsTheRecordedResult(t *testing.T) {
	svc, repo := newService(t)
	want := scoring.AnswerResult{Status: scoring.Accepted, OptionID: "dq-01-b", Correct: true, Points: 186, Total: 574, ReceivedAt: 42}
	repo.EXPECT().RecordAnswer(gomock.Any(), in).Return(want, nil).Times(1)
	got, err := svc.SubmitAnswer(context.Background(), in)
	if err != nil || got != want {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestSubmitAnswer_AppliesTheTimeout(t *testing.T) {
	svc, repo := newService(t)
	repo.EXPECT().RecordAnswer(gomock.Any(), in).DoAndReturn(func(ctx context.Context, _ scoring.AnswerInput) (scoring.AnswerResult, error) {
		dl, ok := ctx.Deadline()
		if !ok || time.Until(dl) > 250*time.Millisecond {
			t.Errorf("deadline %v (set: %v), want within 250 ms (TRD §9.2)", time.Until(dl), ok)
		}
		return scoring.AnswerResult{}, nil
	})
	_, _ = svc.SubmitAnswer(context.Background(), in)
}

func TestSubmitAnswer_RejectionsPassThrough(t *testing.T) {
	for _, rejection := range []error{scoring.ErrQuestionClosed, scoring.ErrWrongQuestion, scoring.ErrNotJoined} {
		svc, repo := newService(t)
		repo.EXPECT().RecordAnswer(gomock.Any(), in).Return(scoring.AnswerResult{}, rejection).Times(1)
		if _, err := svc.SubmitAnswer(context.Background(), in); !errors.Is(err, rejection) {
			t.Errorf("got %v, want %v", err, rejection)
		}
	}
}

// TRD §9.3: the server never retries an answer script; the client resends with the same request ID.
func TestSubmitAnswer_InfrastructureFailureIsServerBusyWithoutRetry(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, errors.New("dial tcp: connection refused")} {
		svc, repo := newService(t)
		repo.EXPECT().RecordAnswer(gomock.Any(), in).Return(scoring.AnswerResult{}, cause).Times(1)
		_, err := svc.SubmitAnswer(context.Background(), in)
		if !errors.Is(err, scoring.ErrServerBusy) {
			t.Errorf("cause %v: got %v, want ErrServerBusy", cause, err)
		}
	}
}

func TestSubmitAnswer_UnexpectedReplyIsInternal(t *testing.T) {
	svc, repo := newService(t)
	repo.EXPECT().RecordAnswer(gomock.Any(), in).Return(scoring.AnswerResult{}, scoring.ErrUnexpectedReply).Times(1)
	if _, err := svc.SubmitAnswer(context.Background(), in); !errors.Is(err, scoring.ErrInternal) {
		t.Errorf("got %v, want ErrInternal", err)
	}
}
