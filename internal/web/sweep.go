package web

import (
	"context"
	"log/slog"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/notify"
)

const sweepEvery = time.Hour

// remindEvery is how often due token reminders are looked for. SPEC 8.10
// asks for at least once an hour; a quarter hour keeps a reminder close to
// its time.
const remindEvery = 15 * time.Minute

// emitter is the part of the notifier the reminders use.
type emitter interface{ Emit(notify.Message) }

// SweepSessions deletes sessions idle for longer than idle, and audit
// entries older than the retention setting, once an hour until ctx ends.
// Lookup already refuses expired sessions; this only keeps the tables small.
func (h *Handler) SweepSessions(ctx context.Context, idle time.Duration) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.srv.sweepOnce(ctx, idle)
		}
	}
}

func (s *server) sweepOnce(ctx context.Context, idle time.Duration) {
	n, err := s.store.DeleteSessionsIdleSince(ctx, s.now().Add(-idle))
	if err != nil {
		s.log.Error("sweep idle sessions", "error", err)
	} else {
		s.log.Debug("swept idle sessions", "count", n)
	}
	s.sweepAudit(ctx)
}

// sweepAudit deletes audit entries older than the retention setting. The
// sweep is not itself audited (SPEC 12.9). When the setting cannot be read
// nothing is deleted.
func (s *server) sweepAudit(ctx context.Context) {
	days, ok := s.readInt(ctx, settingAuditRetention)
	if !ok {
		s.log.Error("the audit retention could not be read, so no audit entry was deleted")
		return
	}
	n, err := s.store.DeleteAuditBefore(ctx, s.now().AddDate(0, 0, -days))
	if err != nil {
		s.log.Error("sweep old audit entries", "error", err)
		return
	}
	s.log.Debug("swept old audit entries", "count", n)
}

// RemindTokens sends the token-expiring event for each reminder that is
// due (SPEC 8.10), once at start and then every quarter hour until ctx
// ends. The first run also sends a reminder that came due while Foreman was
// down.
func RemindTokens(ctx context.Context, tokens *auth.APITokens, n *notify.Notifier, log *slog.Logger) {
	remindOnce(ctx, tokens, n, log)
	t := time.NewTicker(remindEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			remindOnce(ctx, tokens, n, log)
		}
	}
}

// remindOnce queues each due reminder and then records it as sent, so a
// crash or failed write in between sends it twice rather than never.
// Delivery itself is best effort, as for every event (SPEC 8.12).
// The message names the token and its owner, never the secret.
func remindOnce(ctx context.Context, tokens *auth.APITokens, n emitter, log *slog.Logger) {
	due, err := tokens.DueReminders(ctx)
	if err != nil {
		log.Error("find due token reminders", "error", err)
		return
	}
	for _, r := range due {
		n.Emit(notify.Message{Event: notify.TokenExpiring, Token: notify.Token{
			Name: r.Token.Name, Owner: r.Token.Owner, Expires: auth.ExpiresAt(r.Token),
		}})
		if err := tokens.MarkReminded(ctx, r); err != nil {
			log.Error("record a token reminder", "token", r.Token.Name, "error", err)
		}
	}
}
