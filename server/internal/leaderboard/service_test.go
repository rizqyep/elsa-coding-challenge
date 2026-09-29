package leaderboard_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard/mocks"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

func newService(t *testing.T) (*mocks.MockLivePages, *mocks.MockFinalPages, *leaderboard.Service) {
	ctrl := gomock.NewController(t)
	live, final := mocks.NewMockLivePages(ctrl), mocks.NewMockFinalPages(ctrl)
	return live, final, leaderboard.NewService(live, final)
}

var page = leaderboard.Page{Code: "K7Q2MX", Status: quiz.StatusQuestionOpen, ParticipantCount: 1, Limit: 100,
	Entries: []leaderboard.Entry{{Rank: 1, ParticipantID: "u_1", DisplayName: "Rina", Score: 186}}}

func TestPage_Live(t *testing.T) {
	live, _, svc := newService(t)
	live.EXPECT().LivePage(gomock.Any(), quiz.Code("K7Q2MX"), 0, 100).Return(page, nil)
	got, err := svc.Page(context.Background(), "K7Q2MX", 0, 100)
	if err != nil || !reflect.DeepEqual(got, page) {
		t.Errorf("got %+v, %v", got, err)
	}
}

// Once released from Redis, the results come from PostgreSQL (FR-13).
func TestPage_FallsBackToFinalResults(t *testing.T) {
	live, final, svc := newService(t)
	gomock.InOrder(
		live.EXPECT().LivePage(gomock.Any(), gomock.Any(), 20, 10).Return(leaderboard.Page{}, quiz.ErrUnknownQuiz),
		final.EXPECT().FinalPage(gomock.Any(), quiz.Code("K7Q2MX"), 20, 10).Return(page, nil),
	)
	if got, err := svc.Page(context.Background(), "K7Q2MX", 20, 10); err != nil || !reflect.DeepEqual(got, page) {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestPage_Errors(t *testing.T) {
	t.Run("unknown everywhere", func(t *testing.T) {
		live, final, svc := newService(t)
		live.EXPECT().LivePage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaderboard.Page{}, quiz.ErrUnknownQuiz)
		final.EXPECT().FinalPage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaderboard.Page{}, quiz.ErrUnknownQuiz)
		if _, err := svc.Page(context.Background(), "K7Q2MX", 0, 10); !errors.Is(err, quiz.ErrUnknownQuiz) {
			t.Errorf("got %v, want ErrUnknownQuiz", err)
		}
	})
	// A Redis outage must not serve PostgreSQL, which has no results for a running quiz yet.
	t.Run("redis down does not fall back", func(t *testing.T) {
		live, _, svc := newService(t)
		down := errors.New("connection refused")
		live.EXPECT().LivePage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(leaderboard.Page{}, down)
		if _, err := svc.Page(context.Background(), "K7Q2MX", 0, 10); !errors.Is(err, down) {
			t.Errorf("got %v, want the Redis error", err)
		}
	})
}
