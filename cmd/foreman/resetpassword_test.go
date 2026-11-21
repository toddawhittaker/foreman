package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/web"
)

func resetStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// SPEC 12.12: reset-password sets the hash, ends the user's sessions and
// records password-change by system, with no detail.
func TestResetPassword(t *testing.T) {
	ctx := context.Background()
	st := resetStore(t)
	now := time.Unix(1_700_000_000, 0)
	hash, _ := auth.Hash("the old passphrase")
	u, err := st.CreateUser(ctx, "alice", hash, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "s1", u.ID, now); err != nil {
		t.Fatal(err)
	}
	// Only the first line counts: later lines, valid or not, are ignored.
	if err := resetPassword(ctx, st, "alice", strings.NewReader("a new passphrase\r\nshort\nanother long line here\n"), now); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.UserByUsername(ctx, "alice")
	if ok, _, err := auth.Verify("a new passphrase", got.PasswordHash); err != nil || !ok {
		t.Errorf("the new password does not verify: %v", err)
	}
	if _, _, ok, _ := st.SessionUser(ctx, "s1"); ok {
		t.Error("the session survived")
	}
	entries, _ := st.ListAudit(ctx, store.AuditFilter{Limit: 10})
	if len(entries) != 1 || entries[0].Action != web.AuditPasswordChange || entries[0].Username != "system" || entries[0].Detail != "alice" || entries[0].UserID != 0 {
		t.Errorf("audit %+v", entries)
	}
}

func TestResetPasswordRefusals(t *testing.T) {
	ctx := context.Background()
	st := resetStore(t)
	now := time.Unix(1_700_000_000, 0)
	hash, _ := auth.Hash("the old passphrase")
	if _, err := st.CreateUser(ctx, "alice", hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, "bob", "", now); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ user, in string }{
		"unknown user":  {"nobody", "a new passphrase\n"},
		"proxy user":    {"bob", "a new passphrase\n"},
		"short":         {"alice", "short\n"},
		"empty":         {"alice", ""},
		"too long":      {"alice", strings.Repeat("x", auth.MaxPasswordBytes+1)},
		"only the line": {"alice", "\nsecond line is ignored\n"},
	} {
		err := resetPassword(ctx, st, c.user, strings.NewReader(c.in), now)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if c.in != "" && strings.Contains(err.Error(), strings.TrimSpace(c.in)) {
			t.Errorf("%s: the error repeats the password", name)
		}
	}
	if u, _, _ := st.UserByUsername(ctx, "alice"); u.PasswordHash != hash {
		t.Error("a refused reset changed the password")
	}
	if u, _, _ := st.UserByUsername(ctx, "bob"); u.PasswordHash != "" {
		t.Error("a proxy user got a password")
	}
	if entries, _ := st.ListAudit(ctx, store.AuditFilter{Limit: 10}); len(entries) != 0 {
		t.Errorf("a refused reset was recorded: %+v", entries)
	}
}

// The password change and the end of sessions commit together; when the
// audit entry then cannot be written, the command still fails, but its error
// says the password was changed so the admin does not repeat the reset.
func TestResetPasswordSaysItChangedThePasswordWhenTheAuditWriteFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "foreman.db")
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Unix(1_700_000_000, 0)
	hash, _ := auth.Hash("the old passphrase")
	if _, err := st.CreateUser(ctx, "alice", hash, now); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "DROP TABLE audit_log"); err != nil {
		t.Fatal(err)
	}

	err = resetPassword(ctx, st, "alice", strings.NewReader("a new passphrase\n"), now)
	if err == nil {
		t.Fatal("the failed audit write was not reported")
	}
	if want := "the password was changed, but the audit entry was not recorded"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not say %q", err, want)
	}
	got, _, _ := st.UserByUsername(ctx, "alice")
	if ok, _, err := auth.Verify("a new passphrase", got.PasswordHash); err != nil || !ok {
		t.Errorf("the new password does not verify: %v", err)
	}
}
