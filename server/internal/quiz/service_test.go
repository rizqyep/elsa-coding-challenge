package quiz_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz/mocks"
)

var errDown = errors.New("connection refused")

// codes returns a source that yields the given codes in turn; byte i maps to alphabet[i].
func codes(cs ...string) io.Reader {
	var b bytes.Buffer
	for _, c := range cs {
		for _, ch := range c {
			b.WriteByte(byte(bytes.IndexRune([]byte(quiz.CodeAlphabet), ch)))
		}
	}
	return &b
}

type fixture struct {
	live  *mocks.MockRepository
	store *mocks.MockStore
	svc   *quiz.Service
}

func newFixture(t *testing.T, src io.Reader) fixture {
	ctrl := gomock.NewController(t)
	f := fixture{live: mocks.NewMockRepository(ctrl), store: mocks.NewMockStore(ctrl)}
	f.svc = quiz.NewService(f.live, f.store, quiz.Settings{
		DefaultWindow: 15 * time.Second, DefaultReveal: 5 * time.Second,
		LobbyTimeout: 30 * time.Minute, DataTTL: 24 * time.Hour, CodeAttempts: 3, CodeSource: src,
	}, slog.New(slog.DiscardHandler))
	return f
}

// runLive makes CreateQuiz behave like the real store: it calls live inside the "transaction".
func runLive(ctx context.Context, _ quiz.NewQuiz, live func(context.Context) (int64, error)) error {
	_, err := live(ctx)
	return err
}

var ids = []quiz.QuestionID{"dq-01", "dq-02", "dq-03"}

const createdMs = int64(1_790_000_000_000)

func TestCreate_Success(t *testing.T) {
	f := newFixture(t, codes("K7Q2MX"))
	ctx := context.Background()
	f.store.EXPECT().QuestionIDs(ctx, quiz.QuestionSetID("demo-quick")).Return(ids, nil)
	f.store.EXPECT().CreateQuiz(ctx, quiz.NewQuiz{Code: "K7Q2MX", QuestionSetID: "demo-quick", HostID: "host_1", WindowMs: 10_000, RevealMs: 3_000}, gomock.Any()).DoAndReturn(runLive)
	f.live.EXPECT().CreateRoom(ctx, quiz.CreateRoomInput{
		Code: "K7Q2MX", QuestionSetID: "demo-quick", HostID: "host_1", QuestionIDs: ids,
		WindowMs: 10_000, RevealMs: 3_000, LobbyTimeoutMs: 1_800_000, TTL: 24 * time.Hour,
	}).Return(createdMs, nil)

	got, err := f.svc.Create(ctx, quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick", Window: 10 * time.Second, Reveal: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	created := time.UnixMilli(createdMs).UTC()
	expires := created.Add(30 * time.Minute)
	want := quiz.Summary{
		Code: "K7Q2MX", QuestionSetID: "demo-quick", Status: quiz.StatusLobby, QuestionCount: 3,
		WindowMs: 10_000, RevealMs: 3_000, CreatedAt: created, LobbyExpiresAt: &expires,
	}
	assertSummary(t, got, want)
}

func TestCreate_DefaultsTiming(t *testing.T) {
	f := newFixture(t, codes("K7Q2MX"))
	f.store.EXPECT().QuestionIDs(gomock.Any(), gomock.Any()).Return(ids, nil)
	f.store.EXPECT().CreateQuiz(gomock.Any(), gomock.Cond(func(q quiz.NewQuiz) bool {
		return q.WindowMs == 15_000 && q.RevealMs == 5_000
	}), gomock.Any()).DoAndReturn(runLive)
	f.live.EXPECT().CreateRoom(gomock.Any(), gomock.Any()).Return(createdMs, nil)
	if _, err := f.svc.Create(context.Background(), quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick"}); err != nil {
		t.Fatal(err)
	}
}

// D16: a taken code (in PostgreSQL, or an orphan room left in Redis) is retried with a new code.
func TestCreate_RetriesOnCodeCollision(t *testing.T) {
	f := newFixture(t, codes("AAAAAA", "BBBBBB", "CCCCCC"))
	f.store.EXPECT().QuestionIDs(gomock.Any(), gomock.Any()).Return(ids, nil)
	gomock.InOrder(
		f.store.EXPECT().CreateQuiz(gomock.Any(), codeIs("AAAAAA"), gomock.Any()).Return(quiz.ErrCodeTaken),
		f.store.EXPECT().CreateQuiz(gomock.Any(), codeIs("BBBBBB"), gomock.Any()).DoAndReturn(runLive),
		f.live.EXPECT().CreateRoom(gomock.Any(), gomock.Any()).Return(int64(0), quiz.ErrCodeInUse),
		f.store.EXPECT().CreateQuiz(gomock.Any(), codeIs("CCCCCC"), gomock.Any()).DoAndReturn(runLive),
		f.live.EXPECT().CreateRoom(gomock.Any(), gomock.Any()).Return(createdMs, nil),
	)
	got, err := f.svc.Create(context.Background(), quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "CCCCCC" {
		t.Errorf("code = %s, want CCCCCC", got.Code)
	}
}

func TestCreate_GivesUpAfterMaxAttempts(t *testing.T) {
	f := newFixture(t, codes("AAAAAA", "BBBBBB", "CCCCCC", "DDDDDD"))
	f.store.EXPECT().QuestionIDs(gomock.Any(), gomock.Any()).Return(ids, nil)
	f.store.EXPECT().CreateQuiz(gomock.Any(), gomock.Any(), gomock.Any()).Return(quiz.ErrCodeTaken).Times(3)
	_, err := f.svc.Create(context.Background(), quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick"})
	if !errors.Is(err, quiz.ErrNoFreeCode) {
		t.Errorf("got %v, want ErrNoFreeCode", err)
	}
}

// TRD §5.2: a Redis failure inside the transaction is returned (so the store rolls back) and not retried.
func TestCreate_RedisFailureIsReturnedNotRetried(t *testing.T) {
	f := newFixture(t, codes("AAAAAA", "BBBBBB"))
	f.store.EXPECT().QuestionIDs(gomock.Any(), gomock.Any()).Return(ids, nil)
	f.store.EXPECT().CreateQuiz(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(runLive).Times(1)
	f.live.EXPECT().CreateRoom(gomock.Any(), gomock.Any()).Return(int64(0), errDown)
	if _, err := f.svc.Create(context.Background(), quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick"}); !errors.Is(err, errDown) {
		t.Errorf("got %v, want the Redis error", err)
	}
}

func TestCreate_Rejects(t *testing.T) {
	cases := map[string]struct {
		in      quiz.CreateInput
		setErr  error
		wantErr error
	}{
		"window too short":      {quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick", Window: 4 * time.Second}, nil, quiz.ErrInvalidSettings},
		"reveal too long":       {quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick", Reveal: 31 * time.Second}, nil, quiz.ErrInvalidSettings},
		"unknown question set":  {quiz.CreateInput{HostID: "host_1", QuestionSetID: "nope"}, quiz.ErrQuestionSetNotFound, quiz.ErrQuestionSetNotFound},
		"question set DB error": {quiz.CreateInput{HostID: "host_1", QuestionSetID: "demo-quick"}, errDown, errDown},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, codes("AAAAAA"))
			if tc.setErr != nil {
				f.store.EXPECT().QuestionIDs(gomock.Any(), gomock.Any()).Return(nil, tc.setErr)
			}
			if _, err := f.svc.Create(context.Background(), tc.in); !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestServiceStart(t *testing.T) {
	ctx := context.Background()
	t.Run("marks the archive running", func(t *testing.T) {
		f := newFixture(t, nil)
		gomock.InOrder(
			f.live.EXPECT().Start(ctx, quiz.Code("K7Q2MX"), quiz.ParticipantID("host_1")).Return(nil),
			f.store.EXPECT().MarkRunning(ctx, quiz.Code("K7Q2MX")).Return(nil),
		)
		if err := f.svc.Start(ctx, "K7Q2MX", "host_1"); err != nil {
			t.Fatal(err)
		}
	})
	// Redis is the authority on the running quiz; the archive status is informational.
	t.Run("archive failure does not fail the start", func(t *testing.T) {
		f := newFixture(t, nil)
		f.live.EXPECT().Start(ctx, gomock.Any(), gomock.Any()).Return(nil)
		f.store.EXPECT().MarkRunning(ctx, gomock.Any()).Return(errDown)
		if err := f.svc.Start(ctx, "K7Q2MX", "host_1"); err != nil {
			t.Errorf("got %v, want nil", err)
		}
	})
	for _, want := range []error{quiz.ErrNotHost, quiz.ErrNotInLobby, quiz.ErrNoParticipants, quiz.ErrUnknownQuiz, errDown} {
		t.Run("live error "+want.Error(), func(t *testing.T) {
			f := newFixture(t, nil)
			f.live.EXPECT().Start(ctx, gomock.Any(), gomock.Any()).Return(want)
			if err := f.svc.Start(ctx, "K7Q2MX", "host_1"); !errors.Is(err, want) {
				t.Errorf("got %v, want %v", err, want)
			}
		})
	}
}

func TestGet_Live(t *testing.T) {
	f := newFixture(t, nil)
	rec := quiz.RoomRecord{Code: "K7Q2MX", QuestionSetID: "demo-quick", CreatedAt: createdMs, Room: quiz.Room{
		HostID: "host_1", Status: quiz.StatusLobby, QuestionIndex: -1, QuestionCount: 3,
		WindowMs: 10_000, RevealMs: 3_000, LobbyExpiresAt: createdMs + 1_800_000,
	}}
	f.live.EXPECT().Live(gomock.Any(), quiz.Code("K7Q2MX")).Return(quiz.LiveRoom{RoomRecord: rec, Participants: 4}, nil)
	got, err := f.svc.Get(context.Background(), "K7Q2MX")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.UnixMilli(createdMs + 1_800_000).UTC()
	assertSummary(t, got, quiz.Summary{
		Code: "K7Q2MX", QuestionSetID: "demo-quick", Status: quiz.StatusLobby, QuestionCount: 3, ParticipantCount: 4,
		WindowMs: 10_000, RevealMs: 3_000, CreatedAt: time.UnixMilli(createdMs).UTC(), LobbyExpiresAt: &expires,
	})

	f.live.EXPECT().Live(gomock.Any(), gomock.Any()).Return(quiz.LiveRoom{RoomRecord: func() quiz.RoomRecord {
		r := rec
		r.Status = quiz.StatusQuestionOpen
		return r
	}()}, nil)
	if got, _ := f.svc.Get(context.Background(), "K7Q2MX"); got.LobbyExpiresAt != nil {
		t.Error("lobbyExpiresAt present after the lobby")
	}
}

func TestGet_FallsBackToTheArchive(t *testing.T) {
	finished := time.Date(2026, 9, 29, 10, 5, 0, 0, time.UTC)
	created := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		archived string
		want     quiz.Status
	}{
		"finished":                            {quiz.ArchiveFinished, quiz.StatusFinished},
		"expired":                             {quiz.ArchiveExpired, quiz.StatusExpired},
		"running but the room is gone":        {quiz.ArchiveRunning, quiz.StatusExpired},
		"lobby but the room is gone (orphan)": {quiz.ArchiveLobby, quiz.StatusExpired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			f.live.EXPECT().Live(gomock.Any(), gomock.Any()).Return(quiz.LiveRoom{}, quiz.ErrUnknownQuiz)
			f.store.EXPECT().Quiz(gomock.Any(), quiz.Code("K7Q2MX")).Return(quiz.StoredQuiz{
				Code: "K7Q2MX", QuestionSetID: "demo-quick", Status: tc.archived, WindowMs: 10_000, RevealMs: 3_000,
				QuestionCount: 3, ParticipantCount: 7, CreatedAt: created, FinishedAt: &finished,
			}, nil)
			got, err := f.svc.Get(context.Background(), "K7Q2MX")
			if err != nil {
				t.Fatal(err)
			}
			assertSummary(t, got, quiz.Summary{
				Code: "K7Q2MX", QuestionSetID: "demo-quick", Status: tc.want, QuestionCount: 3, ParticipantCount: 7,
				WindowMs: 10_000, RevealMs: 3_000, CreatedAt: created, FinishedAt: &finished,
			})
		})
	}
}

func TestGet_Errors(t *testing.T) {
	t.Run("unknown everywhere", func(t *testing.T) {
		f := newFixture(t, nil)
		f.live.EXPECT().Live(gomock.Any(), gomock.Any()).Return(quiz.LiveRoom{}, quiz.ErrUnknownQuiz)
		f.store.EXPECT().Quiz(gomock.Any(), gomock.Any()).Return(quiz.StoredQuiz{}, quiz.ErrUnknownQuiz)
		if _, err := f.svc.Get(context.Background(), "K7Q2MX"); !errors.Is(err, quiz.ErrUnknownQuiz) {
			t.Errorf("got %v, want ErrUnknownQuiz", err)
		}
	})
	// A Redis outage must surface, not fall through to the archive (which would report a live quiz as expired).
	t.Run("redis down does not fall back", func(t *testing.T) {
		f := newFixture(t, nil)
		f.live.EXPECT().Live(gomock.Any(), gomock.Any()).Return(quiz.LiveRoom{}, errDown)
		if _, err := f.svc.Get(context.Background(), "K7Q2MX"); !errors.Is(err, errDown) {
			t.Errorf("got %v, want the Redis error", err)
		}
	})
}

func codeIs(c string) gomock.Matcher {
	return gomock.Cond(func(q quiz.NewQuiz) bool { return q.Code == quiz.Code(c) })
}

func assertSummary(t *testing.T, got, want quiz.Summary) {
	t.Helper()
	g, w := got, want
	g.LobbyExpiresAt, w.LobbyExpiresAt, g.FinishedAt, w.FinishedAt = nil, nil, nil, nil
	if g != w {
		t.Errorf("summary =\n  %+v\nwant\n  %+v", got, want)
	}
	if !sameTime(got.LobbyExpiresAt, want.LobbyExpiresAt) || !sameTime(got.FinishedAt, want.FinishedAt) {
		t.Errorf("times: lobbyExpiresAt %v finishedAt %v, want %v %v", got.LobbyExpiresAt, got.FinishedAt, want.LobbyExpiresAt, want.FinishedAt)
	}
}

func sameTime(a, b *time.Time) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Equal(*b))
}
