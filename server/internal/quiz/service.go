package quiz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// Service errors.
var (
	ErrInvalidSettings = errors.New("invalid quiz settings")
	ErrNoFreeCode      = errors.New("no free quiz code after the maximum attempts")
)

// Settings configure quiz creation (TRD §2.2).
type Settings struct {
	DefaultWindow time.Duration
	DefaultReveal time.Duration
	LobbyTimeout  time.Duration
	DataTTL       time.Duration
	CodeAttempts  int
	CodeSource    io.Reader // nil means crypto/rand
}

// Service creates, starts, and reads quizzes.
type Service struct {
	live     Repository
	store    Store
	settings Settings
	log      *slog.Logger
}

// NewService returns a service over the live and persistent stores.
func NewService(live Repository, store Store, s Settings, log *slog.Logger) *Service {
	return &Service{live: live, store: store, settings: s, log: log}
}

// CreateInput is a host's request for a new quiz. Zero durations mean the defaults.
type CreateInput struct {
	HostID        ParticipantID
	QuestionSetID QuestionSetID
	Window        time.Duration
	Reveal        time.Duration
}

// Summary is a quiz's status and settings (openapi.yaml Quiz).
type Summary struct {
	Code             Code
	QuestionSetID    QuestionSetID
	Status           Status
	QuestionCount    int
	ParticipantCount int
	WindowMs         int64
	RevealMs         int64
	CreatedAt        time.Time
	LobbyExpiresAt   *time.Time
	FinishedAt       *time.Time
}

// QuestionSets lists the sets a host can choose from.
func (s *Service) QuestionSets(ctx context.Context) ([]QuestionSetSummary, error) {
	return s.store.QuestionSets(ctx)
}

// Create reserves a unique code and creates the room in one transaction, retrying collisions (TRD §5.2, D16).
func (s *Service) Create(ctx context.Context, in CreateInput) (Summary, error) {
	window, reveal := orDefault(in.Window, s.settings.DefaultWindow), orDefault(in.Reveal, s.settings.DefaultReveal)
	if err := ValidateTiming(window.Milliseconds(), reveal.Milliseconds()); err != nil {
		return Summary{}, fmt.Errorf("%w: %w", ErrInvalidSettings, err)
	}
	ids, err := s.store.QuestionIDs(ctx, in.QuestionSetID)
	if err != nil {
		return Summary{}, err
	}
	for range s.settings.CodeAttempts {
		code, err := NewCode(s.settings.CodeSource)
		if err != nil {
			return Summary{}, err
		}
		row := NewQuiz{Code: code, QuestionSetID: in.QuestionSetID, HostID: in.HostID, WindowMs: window.Milliseconds(), RevealMs: reveal.Milliseconds()}
		var createdAt int64
		err = s.store.CreateQuiz(ctx, row, func(ctx context.Context) (int64, error) {
			at, roomErr := s.live.CreateRoom(ctx, CreateRoomInput{
				Code: code, QuestionSetID: in.QuestionSetID, HostID: in.HostID, QuestionIDs: ids,
				WindowMs: row.WindowMs, RevealMs: row.RevealMs,
				LobbyTimeoutMs: s.settings.LobbyTimeout.Milliseconds(), TTL: s.settings.DataTTL,
			})
			createdAt = at
			return at, roomErr
		})
		if errors.Is(err, ErrCodeTaken) || errors.Is(err, ErrCodeInUse) {
			continue
		}
		if err != nil {
			return Summary{}, err
		}
		created := time.UnixMilli(createdAt).UTC()
		expires := created.Add(s.settings.LobbyTimeout)
		return Summary{
			Code: code, QuestionSetID: in.QuestionSetID, Status: StatusLobby, QuestionCount: len(ids),
			WindowMs: row.WindowMs, RevealMs: row.RevealMs, CreatedAt: created, LobbyExpiresAt: &expires,
		}, nil
	}
	return Summary{}, ErrNoFreeCode
}

// Start accepts the host's start (FR-3). Redis decides; the archive status is best effort.
func (s *Service) Start(ctx context.Context, code Code, caller ParticipantID) error {
	if err := s.live.Start(ctx, code, caller); err != nil {
		return err
	}
	if err := s.store.MarkRunning(ctx, code); err != nil {
		s.log.WarnContext(ctx, "mark quiz running in the archive failed", "error", err)
	}
	return nil
}

// Get reads the quiz from Redis while live, from PostgreSQL once released.
func (s *Service) Get(ctx context.Context, code Code) (Summary, error) {
	live, err := s.live.Live(ctx, code)
	if err == nil {
		return liveSummary(live), nil
	}
	if !errors.Is(err, ErrUnknownQuiz) {
		return Summary{}, err
	}
	q, err := s.store.Quiz(ctx, code)
	if err != nil {
		return Summary{}, err
	}
	return Summary{
		Code: q.Code, QuestionSetID: q.QuestionSetID, Status: ArchivedStatus(q.Status), QuestionCount: q.QuestionCount,
		ParticipantCount: q.ParticipantCount, WindowMs: q.WindowMs, RevealMs: q.RevealMs, CreatedAt: q.CreatedAt, FinishedAt: q.FinishedAt,
	}, nil
}

func liveSummary(l LiveRoom) Summary {
	s := Summary{
		Code: l.Code, QuestionSetID: l.QuestionSetID, Status: l.Status, QuestionCount: l.QuestionCount,
		ParticipantCount: l.Participants, WindowMs: l.WindowMs, RevealMs: l.RevealMs, CreatedAt: time.UnixMilli(l.CreatedAt).UTC(),
	}
	if l.Status == StatusLobby {
		t := time.UnixMilli(l.LobbyExpiresAt).UTC()
		s.LobbyExpiresAt = &t
	}
	return s
}

// ArchivedStatus maps an archive row to a quiz status. A lobby or running row whose room is gone
// never reached the final job, so it ended without results.
func ArchivedStatus(s string) Status {
	if s == ArchiveFinished {
		return StatusFinished
	}
	return StatusExpired
}

func orDefault(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}
