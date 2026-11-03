package web

import (
	"context"
	"log/slog"
	"time"

	"github.com/toddawhittaker/foreman/internal/store"
)

const sweepEvery = time.Hour

// SweepSessions deletes sessions idle for longer than idle, once an hour,
// until ctx ends. Lookup already refuses expired sessions; this only keeps
// the table small.
func SweepSessions(ctx context.Context, st *store.Store, idle time.Duration, log *slog.Logger) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweepOnce(ctx, st, idle, time.Now(), log)
		}
	}
}

func sweepOnce(ctx context.Context, st *store.Store, idle time.Duration, now time.Time, log *slog.Logger) {
	n, err := st.DeleteSessionsIdleSince(ctx, now.Add(-idle))
	if err != nil {
		log.Error("sweep idle sessions", "error", err)
		return
	}
	log.Debug("swept idle sessions", "count", n)
}
