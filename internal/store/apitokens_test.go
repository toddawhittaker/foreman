package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func tokenUser(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	u, err := s.CreateUser(context.Background(), name, "h", t0)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestAPITokenCreateAndLookUp(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ann := tokenUser(t, s, "ann")
	made, err := s.CreateAPIToken(ctx, ann, "Claude", "operate", "hash1", t0)
	if err != nil {
		t.Fatal(err)
	}
	want := APIToken{ID: made.ID, UserID: ann, Owner: "ann", Name: "Claude", Scope: "operate", CreatedAt: t0, PeriodStart: t0}
	if made != want {
		t.Errorf("created %+v, want %+v", made, want)
	}
	got, found, err := s.APITokenByHash(ctx, "hash1")
	if err != nil || !found || got != want {
		t.Errorf("by hash: %+v found=%v err=%v", got, found, err)
	}
	if _, found, err := s.APITokenByHash(ctx, "nope"); found || err != nil {
		t.Errorf("unknown hash: found=%v err=%v", found, err)
	}
}

func TestAPITokenScopeChecked(t *testing.T) {
	s, _ := openTemp(t)
	ann := tokenUser(t, s, "ann")
	if _, err := s.CreateAPIToken(context.Background(), ann, "x", "root", "h", t0); err == nil {
		t.Error("a scope outside read, operate and admin was stored")
	}
}

func TestAPITokenNameUniqueAmongLiveTokens(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ann, bob := tokenUser(t, s, "ann"), tokenUser(t, s, "bob")
	first, err := s.CreateAPIToken(ctx, ann, "Claude", "read", "h1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAPIToken(ctx, ann, "claude", "read", "h2", t0); !errors.Is(err, ErrTokenNameTaken) {
		t.Errorf("same name, other case: err = %v, want ErrTokenNameTaken", err)
	}
	if _, err := s.CreateAPIToken(ctx, bob, "Claude", "read", "h3", t0); err != nil {
		t.Errorf("another owner may use the name: %v", err)
	}
	if _, err := s.RevokeAPIToken(ctx, first.ID, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAPIToken(ctx, ann, "Claude", "read", "h4", t0); err != nil {
		t.Errorf("a revoked token's name is free again: %v", err)
	}
}

func TestAPITokenRenew(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ann, bob := tokenUser(t, s, "ann"), tokenUser(t, s, "bob")
	tok, _ := s.CreateAPIToken(ctx, ann, "c", "read", "h", t0)
	if err := s.SetAPITokenReminded(ctx, tok.ID, t0, 2); err != nil {
		t.Fatal(err)
	}
	later := t0.Add(40 * 24 * time.Hour)
	if _, err := s.RenewAPIToken(ctx, tok.ID, bob, later); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("renew by another user: err = %v, want ErrTokenNotFound", err)
	}
	got, err := s.RenewAPIToken(ctx, tok.ID, ann, later)
	if err != nil {
		t.Fatal(err)
	}
	if !got.PeriodStart.Equal(later) || got.Reminded != 0 || !got.CreatedAt.Equal(t0) {
		t.Errorf("renewed %+v", got)
	}
	if _, err := s.RenewAPIToken(ctx, 999, ann, later); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("renew missing: err = %v", err)
	}
}

func TestAPITokenRevokeIsFinal(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ann := tokenUser(t, s, "ann")
	tok, _ := s.CreateAPIToken(ctx, ann, "c", "admin", "h", t0)
	t1 := t0.Add(time.Hour)
	got, err := s.RevokeAPIToken(ctx, tok.ID, t1)
	if err != nil || !got.RevokedAt.Equal(t1) || got.Name != "c" || got.Owner != "ann" {
		t.Fatalf("revoke: %+v %v", got, err)
	}
	if _, err := s.RevokeAPIToken(ctx, tok.ID, t1.Add(time.Hour)); !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("second revoke: err = %v, want ErrTokenRevoked", err)
	}
	if _, err := s.RenewAPIToken(ctx, tok.ID, ann, t1); !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("renew revoked: err = %v, want ErrTokenRevoked", err)
	}
	again, _ := s.APIToken(ctx, tok.ID)
	if !again.RevokedAt.Equal(t1) || !again.PeriodStart.Equal(t0) {
		t.Errorf("after refused calls: %+v", again)
	}
	if _, err := s.RevokeAPIToken(ctx, 999, t1); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("revoke missing: err = %v", err)
	}
}

func TestListAPITokensAndCascade(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ann, bob := tokenUser(t, s, "ann"), tokenUser(t, s, "bob")
	_, _ = s.CreateAPIToken(ctx, bob, "b", "read", "h1", t0)
	_, _ = s.CreateAPIToken(ctx, ann, "z", "read", "h2", t0)
	_, _ = s.CreateAPIToken(ctx, ann, "a", "read", "h3", t0)
	list, err := s.ListAPITokens(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if list[0].Name != "a" || list[1].Name != "z" || list[2].Owner != "bob" {
		t.Errorf("order: %+v", list)
	}
	if _, err := s.db.Exec("DELETE FROM users WHERE id = ?", ann); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListAPITokens(ctx)
	if len(list) != 1 {
		t.Errorf("a deleted user's tokens stay: %+v", list)
	}
}

func TestAPITokensStartedBy(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ann, bob := tokenUser(t, s, "ann"), tokenUser(t, s, "bob")
	day := 24 * time.Hour
	old, _ := s.CreateAPIToken(ctx, ann, "old", "read", "h1", t0)
	edge, _ := s.CreateAPIToken(ctx, ann, "edge", "read", "h2", t0.Add(day))
	_, _ = s.CreateAPIToken(ctx, ann, "fresh", "read", "h3", t0.Add(2*day))
	_, _ = s.CreateAPIToken(ctx, bob, "bobs", "read", "h4", t0)
	gone, _ := s.CreateAPIToken(ctx, ann, "gone", "read", "h5", t0)
	_, _ = s.RevokeAPIToken(ctx, gone.ID, t0)
	got, err := s.APITokensStartedBy(ctx, ann, t0.Add(day))
	if err != nil || len(got) != 2 || got[0].ID != old.ID || got[1].ID != edge.ID {
		t.Errorf("got %+v err=%v", got, err)
	}
}

func TestAPITokensDueReminder(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	ann := tokenUser(t, s, "ann")
	day := 24 * time.Hour
	now := t0.Add(100 * day)
	start := func(daysAgo int) time.Time { return now.Add(-time.Duration(daysAgo) * day) }
	first, second, notBefore := start(25), start(29), start(30)
	at26, _ := s.CreateAPIToken(ctx, ann, "a26", "read", "h1", start(26))
	at29, _ := s.CreateAPIToken(ctx, ann, "a29", "read", "h2", start(29))
	_, _ = s.CreateAPIToken(ctx, ann, "a20", "read", "h3", start(20))
	_, _ = s.CreateAPIToken(ctx, ann, "a31", "read", "h4", start(31))
	rev, _ := s.CreateAPIToken(ctx, ann, "rev", "read", "h5", start(26))
	_, _ = s.RevokeAPIToken(ctx, rev.ID, now)

	due, err := s.APITokensDueReminder(ctx, notBefore, first, second)
	if err != nil || len(due) != 2 || due[0].ID != at26.ID || due[1].ID != at29.ID {
		t.Fatalf("due: %+v err=%v", due, err)
	}
	_ = s.SetAPITokenReminded(ctx, at26.ID, at26.PeriodStart, 1)
	_ = s.SetAPITokenReminded(ctx, at29.ID, at29.PeriodStart, 1)
	due, _ = s.APITokensDueReminder(ctx, notBefore, first, second)
	if len(due) != 1 || due[0].ID != at29.ID {
		t.Errorf("after the first reminders: %+v", due)
	}
	// A stale period start leaves the row alone.
	_ = s.SetAPITokenReminded(ctx, at29.ID, t0, 2)
	got, _ := s.APIToken(ctx, at29.ID)
	if got.Reminded != 1 {
		t.Errorf("stale mark changed reminded to %d", got.Reminded)
	}
}

func TestTouchAPITokenAtMostOnceAMinute(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	tok, _ := s.CreateAPIToken(ctx, tokenUser(t, s, "ann"), "c", "read", "h", t0)
	for _, tc := range []struct {
		at, want time.Time
	}{
		{t0, t0},
		{t0.Add(30 * time.Second), t0},
		{t0.Add(time.Minute), t0.Add(time.Minute)},
	} {
		if err := s.TouchAPIToken(ctx, tok.ID, tc.at); err != nil {
			t.Fatal(err)
		}
		got, _ := s.APIToken(ctx, tok.ID)
		if !got.LastUsedAt.Equal(tc.want) {
			t.Errorf("touch at %v: last used %v, want %v", tc.at, got.LastUsedAt, tc.want)
		}
	}
}

func TestTokenMigrationTurnsOffEventOnExistingWebhooks(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", dsn(filepath.Join(t.TempDir(), "f.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ms, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(ctx, db, ms[:9]); err != nil {
		t.Fatal(err)
	}
	for _, off := range []string{"", "server-start"} {
		if _, err := db.Exec(`INSERT INTO webhooks (preset, host, url, events_off, lang, created_at, created_by, updated_at, updated_by)
			VALUES ('generic', 'h', x'', ?, 'en', 0, 'a', 0, 'a')`, off); err != nil {
			t.Fatal(err)
		}
	}
	if err := applyMigrations(ctx, db, ms); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("SELECT events_off FROM webhooks ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		got = append(got, v)
	}
	if len(got) != 2 || got[0] != "token-expiring" || got[1] != "server-start,token-expiring" {
		t.Errorf("events_off = %q", got)
	}
}
