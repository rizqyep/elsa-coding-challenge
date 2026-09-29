package httpapi

import (
	"context"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi/gen"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

var _ gen.StrictServerInterface = (*api)(nil)

// CreateDevToken issues a token from the mocked identity provider (task-15).
func (a *api) CreateDevToken(_ context.Context, req gen.CreateDevTokenRequestObject) (gen.CreateDevTokenResponseObject, error) {
	var id string
	if req.Body.ParticipantId != nil {
		id = *req.Body.ParticipantId
	} else {
		var err error
		if id, err = auth.NewParticipantID(); err != nil {
			return nil, err
		}
	}
	tok, c, err := a.cfg.Tokens.Issue(id, auth.Role(req.Body.Role))
	if err != nil {
		return nil, invalid(err)
	}
	return gen.CreateDevToken201JSONResponse{Token: tok, ParticipantId: c.ParticipantID, Role: gen.Role(c.Role), ExpiresAt: c.ExpiresAt}, nil
}

// ListQuestionSets lists the sets a host can use.
func (a *api) ListQuestionSets(ctx context.Context, _ gen.ListQuestionSetsRequestObject) (gen.ListQuestionSetsResponseObject, error) {
	sets, err := a.cfg.Quizzes.QuestionSets(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]gen.QuestionSetSummary, len(sets))
	for i, s := range sets {
		items[i] = gen.QuestionSetSummary{Id: string(s.ID), Title: s.Title, QuestionCount: s.QuestionCount}
	}
	return gen.ListQuestionSets200JSONResponse{Items: items}, nil
}

// CreateQuiz creates a quiz in the lobby; the caller becomes its host.
func (a *api) CreateQuiz(ctx context.Context, req gen.CreateQuizRequestObject) (gen.CreateQuizResponseObject, error) {
	claims, _ := auth.ClaimsFrom(ctx)
	in := quiz.CreateInput{HostID: quiz.ParticipantID(claims.ParticipantID), QuestionSetID: quiz.QuestionSetID(req.Body.QuestionSetId)}
	if s := req.Body.QuestionWindowSeconds; s != nil {
		in.Window = time.Duration(*s) * time.Second
	}
	if s := req.Body.RevealSeconds; s != nil {
		in.Reveal = time.Duration(*s) * time.Second
	}
	q, err := a.cfg.Quizzes.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	location := "/api/v1/quizzes/" + string(q.Code)
	return gen.CreateQuiz201JSONResponse{Body: toQuiz(q), Headers: gen.CreateQuiz201ResponseHeaders{Location: &location}}, nil
}

// GetQuiz returns the quiz's status and settings.
func (a *api) GetQuiz(ctx context.Context, req gen.GetQuizRequestObject) (gen.GetQuizResponseObject, error) {
	q, err := a.cfg.Quizzes.Get(ctx, quiz.Code(req.Code))
	if err != nil {
		return nil, err
	}
	return gen.GetQuiz200JSONResponse(toQuiz(q)), nil
}

// StartQuiz accepts the host's start (FR-3); a worker opens the first question.
func (a *api) StartQuiz(ctx context.Context, req gen.StartQuizRequestObject) (gen.StartQuizResponseObject, error) {
	claims, _ := auth.ClaimsFrom(ctx)
	if err := a.cfg.Quizzes.Start(ctx, quiz.Code(req.Code), quiz.ParticipantID(claims.ParticipantID)); err != nil {
		return nil, err
	}
	return gen.StartQuiz202JSONResponse{Code: req.Code, StartRequested: true}, nil
}

// GetLeaderboard returns a page of the leaderboard (FR-27, FR-13).
func (a *api) GetLeaderboard(ctx context.Context, req gen.GetLeaderboardRequestObject) (gen.GetLeaderboardResponseObject, error) {
	offset, limit := 0, 100
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	p, err := a.cfg.Leaderboards.Page(ctx, quiz.Code(req.Code), offset, limit)
	if err != nil {
		return nil, err
	}
	return gen.GetLeaderboard200JSONResponse(toPage(p)), nil
}

// Healthz reports that the process is alive.
func (a *api) Healthz(context.Context, gen.HealthzRequestObject) (gen.HealthzResponseObject, error) {
	return gen.Healthz200Response{}, nil
}

// Readyz reports whether Redis and PostgreSQL are reachable.
func (a *api) Readyz(ctx context.Context, _ gen.ReadyzRequestObject) (gen.ReadyzResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := a.cfg.Ready(ctx); err != nil {
		a.cfg.Log.WarnContext(ctx, "not ready", "error", err)
		return gen.Readyz503Response{}, nil
	}
	return gen.Readyz200Response{}, nil
}

func toQuiz(q quiz.Summary) gen.Quiz {
	return gen.Quiz{
		Code: gen.QuizCode(q.Code), QuestionSetId: string(q.QuestionSetID), Status: gen.QuizStatus(q.Status),
		QuestionCount: q.QuestionCount, ParticipantCount: q.ParticipantCount,
		QuestionWindowSeconds: int(q.WindowMs / 1000), RevealSeconds: int(q.RevealMs / 1000),
		CreatedAt: q.CreatedAt, LobbyExpiresAt: q.LobbyExpiresAt, FinishedAt: q.FinishedAt,
	}
}

func toPage(p leaderboard.Page) gen.LeaderboardPage {
	entries := make([]gen.LeaderboardEntry, len(p.Entries))
	for i, e := range p.Entries {
		entries[i] = gen.LeaderboardEntry{Rank: e.Rank, ParticipantId: string(e.ParticipantID), DisplayName: e.DisplayName, Score: e.Score}
	}
	return gen.LeaderboardPage{
		Code: gen.QuizCode(p.Code), Status: gen.QuizStatus(p.Status), ParticipantCount: p.ParticipantCount,
		Offset: p.Offset, Limit: p.Limit, Entries: entries,
	}
}
