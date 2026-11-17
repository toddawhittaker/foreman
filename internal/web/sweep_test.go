package web

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/notify"
)

// emitted collects what the sweep sends.
type emitted struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (e *emitted) Emit(m notify.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.msgs = append(e.msgs, m)
}

func (e *emitted) all() []notify.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]notify.Message(nil), e.msgs...)
}

// tokenAt makes a token whose period started age ago.
func tokenAt(t *testing.T, hn *harness, tokens *auth.APITokens, userID int64, name string, age time.Duration) {
	t.Helper()
	hn.clock.Advance(-age)
	if _, _, err := tokens.Create(context.Background(), userID, name, auth.ScopeRead); err != nil {
		t.Fatal(err)
	}
	hn.clock.Advance(age)
}

// Each reminder goes out once per period, at 5 days and at 1 day before
// expiry, naming the token, owner and expiry (SPEC 8.10).
func TestRemindTokensSendsEachReminderOnce(t *testing.T) {
	hn := newHarness(t, nil)
	u := hn.addUser("alice", testPassword)
	tokens := auth.NewAPITokens(hn.store, hn.clock.Now)
	ctx := context.Background()
	var out emitted
	created := hn.clock.Now()

	remindOnce(ctx, tokens, &out, hn.slog)
	if got := out.all(); len(got) != 0 {
		t.Fatalf("a token with no reminder due sent %v", got)
	}
	if _, _, err := tokens.Create(ctx, u.ID, "Claude", auth.ScopeRead); err != nil {
		t.Fatal(err)
	}
	expires := created.Add(auth.TokenPeriod)

	hn.clock.Advance(auth.TokenPeriod - auth.TokenWarnBefore - time.Minute)
	remindOnce(ctx, tokens, &out, hn.slog)
	if got := out.all(); len(got) != 0 {
		t.Fatalf("a reminder was sent before 5 days out: %v", got)
	}

	hn.clock.Advance(2 * time.Minute)
	remindOnce(ctx, tokens, &out, hn.slog)
	remindOnce(ctx, tokens, &out, hn.slog)
	got := out.all()
	if len(got) != 1 {
		t.Fatalf("5-day reminder sent %d times: %v", len(got), got)
	}
	m := got[0]
	if m.Event != notify.TokenExpiring || m.Token.Name != "Claude" || m.Token.Owner != "alice" || !m.Token.Expires.Equal(expires) || m.World != (notify.World{}) {
		t.Errorf("message %+v", m)
	}

	hn.clock.Advance(auth.TokenWarnBefore - auth.TokenLastWarnBefore)
	remindOnce(ctx, tokens, &out, hn.slog)
	remindOnce(ctx, tokens, &out, hn.slog)
	if got := out.all(); len(got) != 2 {
		t.Fatalf("after the 1-day reminder: %d messages", len(got))
	}

	// Renewing starts the period over, so both come again.
	toks, _ := tokens.List(ctx)
	if _, err := tokens.Renew(ctx, toks[0].ID, u.ID); err != nil {
		t.Fatal(err)
	}
	hn.clock.Advance(auth.TokenPeriod - auth.TokenWarnBefore)
	remindOnce(ctx, tokens, &out, hn.slog)
	if got := out.all(); len(got) != 3 {
		t.Errorf("a renewed token got %d messages in all, want 3", len(got))
	}
}

// A reminder that came due while Foreman was down goes out at the next
// check if the token still works; when both came due, only the later one
// does; an expired or revoked token gets none (SPEC 8.10).
func TestRemindTokensAfterDowntime(t *testing.T) {
	hn := newHarness(t, nil)
	u := hn.addUser("alice", testPassword)
	tokens := auth.NewAPITokens(hn.store, hn.clock.Now)
	ctx := context.Background()
	tokenAt(t, hn, tokens, u.ID, "missed-one", auth.TokenPeriod-auth.TokenWarnBefore+time.Hour)
	tokenAt(t, hn, tokens, u.ID, "missed-both", auth.TokenPeriod-time.Hour)
	tokenAt(t, hn, tokens, u.ID, "expired", auth.TokenPeriod+time.Hour)
	tokenAt(t, hn, tokens, u.ID, "revoked", auth.TokenPeriod-time.Hour)
	toks, _ := tokens.List(ctx)
	for _, tk := range toks {
		if tk.Name == "revoked" {
			if _, err := tokens.Revoke(ctx, tk.ID); err != nil {
				t.Fatal(err)
			}
		}
	}

	var out emitted
	remindOnce(ctx, tokens, &out, hn.slog)
	remindOnce(ctx, tokens, &out, hn.slog)
	var names []string
	for _, m := range out.all() {
		names = append(names, m.Token.Name)
	}
	if got := strings.Join(names, ","); got != "missed-one,missed-both" && got != "missed-both,missed-one" {
		t.Errorf("reminders for %q, want one each for missed-one and missed-both", got)
	}
}

// A failing database is logged and nothing is sent or marked.
func TestRemindTokensLogsAStoreFailure(t *testing.T) {
	hn := newHarness(t, nil)
	tokens := auth.NewAPITokens(hn.store, hn.clock.Now)
	if err := hn.store.Close(); err != nil {
		t.Fatal(err)
	}
	var out emitted
	remindOnce(context.Background(), tokens, &out, hn.slog)
	if len(out.all()) != 0 || !strings.Contains(hn.logs.String(), "token reminders") {
		t.Errorf("sent %v, logs %s", out.all(), hn.logs.String())
	}
}
