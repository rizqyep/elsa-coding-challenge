package logging

import (
	"context"
	"io"
	"log/slog"
)

// Field names used across services, so logs can be filtered by quiz, participant, connection, or request (NFR-28).
const (
	QuizID        = "quiz_id"
	ParticipantID = "participant_id"
	ConnID        = "conn_id"
	RequestID     = "request_id"
)

// New returns a JSON logger that adds every field stored in the context (see With) to each record.
func New(w io.Writer, level slog.Level, service string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(contextHandler{h}).With("service", service)
}

type ctxKey struct{}

// With returns a context carrying extra log fields.
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	prev, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	merged := make([]slog.Attr, 0, len(prev)+len(attrs))
	merged = append(merged, prev...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, ctxKey{}, merged)
}

// WithQuiz adds the quiz code to the context's log fields.
func WithQuiz(ctx context.Context, code string) context.Context {
	return With(ctx, slog.String(QuizID, code))
}

// WithParticipant adds the participant ID to the context's log fields.
func WithParticipant(ctx context.Context, id string) context.Context {
	return With(ctx, slog.String(ParticipantID, id))
}

// WithConn adds the WebSocket connection ID to the context's log fields.
func WithConn(ctx context.Context, id string) context.Context {
	return With(ctx, slog.String(ConnID, id))
}

// WithRequest adds the request ID to the context's log fields.
func WithRequest(ctx context.Context, id string) context.Context {
	return With(ctx, slog.String(RequestID, id))
}

type contextHandler struct{ next slog.Handler }

func (h contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs, ok := ctx.Value(ctxKey{}).([]slog.Attr); ok {
		r.AddAttrs(attrs...)
	}
	return h.next.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.next.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.next.WithGroup(name)}
}
