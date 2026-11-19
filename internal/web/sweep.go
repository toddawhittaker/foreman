package web

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/store"
)

const sweepEvery = time.Hour

// remindEvery is how often due token reminders are looked for. SPEC 8.10
// asks for at least once an hour; a quarter hour keeps a reminder close to
// its time.
const remindEvery = 15 * time.Minute

// emitter is the part of the notifier the reminders use.
type emitter interface{ Emit(notify.Message) }

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
	} else {
		log.Debug("swept idle sessions", "count", n)
	}
	sweepAudit(ctx, st, now, log)
}

// The audit log's retention setting (SPEC 12.9, 13.1), in days.
const (
	auditRetentionSetting     = "audit_retention_days"
	defaultAuditRetentionDays = 365
	minAuditRetentionDays     = 7
	maxAuditRetentionDays     = 3650
)

// auditRetention reads the retention in days. A missing or out-of-range
// value gives the default, never a shorter time than was set.
func auditRetention(ctx context.Context, st *store.Store) (int, error) {
	v, found, err := st.AppSetting(ctx, auditRetentionSetting)
	if err != nil || !found {
		return defaultAuditRetentionDays, err
	}
	days, err := strconv.Atoi(v)
	if err != nil || days < minAuditRetentionDays || days > maxAuditRetentionDays {
		return defaultAuditRetentionDays, nil
	}
	return days, nil
}

// sweepAudit deletes audit entries older than the retention. The sweep is
// not itself audited (SPEC 12.9). When the setting cannot be read nothing is
// deleted.
func sweepAudit(ctx context.Context, st *store.Store, now time.Time, log *slog.Logger) {
	days, err := auditRetention(ctx, st)
	if err != nil {
		log.Error("read the audit retention", "error", err)
		return
	}
	n, err := st.DeleteAuditBefore(ctx, now.AddDate(0, 0, -days))
	if err != nil {
		log.Error("sweep old audit entries", "error", err)
		return
	}
	log.Debug("swept old audit entries", "count", n)
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
