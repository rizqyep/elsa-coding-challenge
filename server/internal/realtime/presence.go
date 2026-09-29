package realtime

import (
	"context"
	"log/slog"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// PresenceStore marks participants online (session.RedisRepository).
type PresenceStore interface {
	RefreshPresence(ctx context.Context, code quiz.Code, ids []quiz.ParticipantID) error
}

// RunPresence refreshes presence for every local room each interval until ctx ends (TRD §7.7).
func (h *Hub) RunPresence(ctx context.Context, store PresenceStore, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.refreshPresence(ctx, store)
		}
	}
}

// refreshPresence makes one call per room that has local participants; watchers aren't participants.
func (h *Hub) refreshPresence(ctx context.Context, store PresenceStore) {
	for _, code := range h.reg.codes() {
		local := h.reg.participants(code)
		if len(local) == 0 {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, h.readTimeout)
		err := store.RefreshPresence(rctx, code, ids(local))
		cancel()
		if err != nil && ctx.Err() == nil {
			h.log.Warn("presence refresh failed", slog.String("quiz_code", string(code)), slog.Any("error", err))
		}
	}
}
