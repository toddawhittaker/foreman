package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/store"
)

const day = 24 * time.Hour

func newTokens(t *testing.T) (*APITokens, *store.Store, *fakeClock, int64) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clk := newClock()
	u, err := s.CreateUser(ctx, "ann", "h", clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	return NewAPITokens(s, clk.Now), s, clk, u.ID
}

var secretShape = regexp.MustCompile(`^fmn_[A-Za-z0-9_-]{43}$`)

func TestCreateReturnsSecretOnceAndStoresHash(t *testing.T) {
	m, _, _, ann := newTokens(t)
	ctx := context.Background()
	secret, tok, err := m.Create(ctx, ann, "  Claude Code ", ScopeOperate)
	if err != nil {
		t.Fatal(err)
	}
	if !secretShape.MatchString(secret) {
		t.Errorf("secret %q has the wrong shape", secret)
	}
	if tok.Name != "Claude Code" || tok.Scope != ScopeOperate || tok.Owner != "ann" {
		t.Errorf("token %+v", tok)
	}
	got, err := m.Check(ctx, secret)
	if err != nil || got.ID != tok.ID {
		t.Fatalf("check: %+v %v", got, err)
	}
	other, _, _ := m.Create(ctx, ann, "two", ScopeRead)
	if other == secret {
		t.Error("two tokens share a secret")
	}
}

func TestCreateRefusesBadNameAndScope(t *testing.T) {
	m, _, _, ann := newTokens(t)
	ctx := context.Background()
	for _, name := range []string{"", "   ", strings.Repeat("a", 33), "[x]", "a=b", "tab\tname", "a/b"} {
		if _, _, err := m.Create(ctx, ann, name, ScopeRead); !errors.Is(err, ErrTokenName) {
			t.Errorf("name %q: err = %v, want ErrTokenName", name, err)
		}
	}
	for _, name := range []string{"Claude", "my-bot_2.0", "Ünïcødé ボット", strings.Repeat("é", 32)} {
		if _, _, err := m.Create(ctx, ann, name, ScopeRead); err != nil {
			t.Errorf("name %q refused: %v", name, err)
		}
	}
	if _, _, err := m.Create(ctx, ann, "s", "root"); !errors.Is(err, ErrTokenScope) {
		t.Errorf("bad scope: err = %v", err)
	}
	if _, _, err := m.Create(ctx, ann, "claude", ScopeRead); !errors.Is(err, store.ErrTokenNameTaken) {
		t.Errorf("taken name: err = %v", err)
	}
}

// lookupFails is a store whose lookup must never run.
type lookupFails struct {
	APITokenStore
	t *testing.T
}

func (l lookupFails) APITokenByHash(context.Context, string) (store.APIToken, bool, error) {
	l.t.Fatal("a malformed secret reached the database (SPEC 14.19)")
	return store.APIToken{}, false, nil
}

func TestCheckRefusesMalformedWithoutLookup(t *testing.T) {
	m, _, clk, ann := newTokens(t)
	secret, _, _ := m.Create(context.Background(), ann, "c", ScopeRead)
	stub := NewAPITokens(lookupFails{t: t}, clk.Now)
	for _, bad := range []string{
		"", "fmn_", secret[4:], "xyz_" + secret[4:], secret + "A", secret[:len(secret)-1],
		"fmn_" + strings.Repeat("=", 43), "fmn_" + strings.Repeat("A", 42) + "B", // last char has stray bits
	} {
		if _, err := stub.Check(context.Background(), bad); !errors.Is(err, ErrTokenUnknown) {
			t.Errorf("Check(%q) err = %v, want ErrTokenUnknown", bad, err)
		}
	}
}

func TestCheckRefusesWellFormedUnknown(t *testing.T) {
	m, _, _, _ := newTokens(t)
	if _, err := m.Check(context.Background(), "fmn_"+strings.Repeat("A", 43)); !errors.Is(err, ErrTokenUnknown) {
		t.Errorf("err = %v, want ErrTokenUnknown", err)
	}
}

// SPEC 12.10: only the secret's SHA-256, as lowercase hex, is stored.
func TestStoredHashIsTheSecretsSHA256(t *testing.T) {
	m, s, _, ann := newTokens(t)
	ctx := context.Background()
	secret, tok, _ := m.Create(ctx, ann, "c", ScopeRead)
	sum := sha256.Sum256([]byte(secret))
	got, found, err := s.APITokenByHash(ctx, hex.EncodeToString(sum[:]))
	if err != nil || !found || got.ID != tok.ID {
		t.Errorf("lookup by the lowercase hex SHA-256: %+v found=%v err=%v", got, found, err)
	}
	if _, found, _ := s.APITokenByHash(ctx, secret); found {
		t.Error("the secret itself is stored")
	}
}

func TestStateExpiryRenewAndRevoke(t *testing.T) {
	m, _, clk, ann := newTokens(t)
	ctx := context.Background()
	secret, tok, _ := m.Create(ctx, ann, "c", ScopeRead)
	clk.Advance(TokenPeriod - time.Second)
	if _, err := m.Check(ctx, secret); err != nil {
		t.Errorf("a second before expiry: %v", err)
	}
	clk.Advance(time.Second)
	if _, err := m.Check(ctx, secret); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("at expiry: err = %v, want ErrTokenExpired", err)
	}
	// Expired tokens stay renewable however long ago they expired.
	clk.Advance(400 * day)
	if _, err := m.Renew(ctx, tok.ID, ann+1); !errors.Is(err, store.ErrTokenNotFound) {
		t.Errorf("renew by another: err = %v", err)
	}
	renewed, err := m.Renew(ctx, tok.ID, ann)
	if err != nil || State(renewed, clk.Now()) != TokenWorking {
		t.Fatalf("renew: %+v %v", renewed, err)
	}
	if got, want := ExpiresAt(renewed), clk.Now().Add(30*day); !got.Equal(want) {
		t.Errorf("renewed token expires %v, want %v", got, want)
	}
	if _, err := m.Check(ctx, secret); err != nil {
		t.Errorf("the same secret works after renewal: %v", err)
	}
	revoked, err := m.Revoke(ctx, tok.ID)
	if err != nil || State(revoked, clk.Now()) != TokenRevoked {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	if _, err := m.Check(ctx, secret); !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("after revoke: err = %v", err)
	}
	if _, err := m.Renew(ctx, tok.ID, ann); !errors.Is(err, store.ErrTokenRevoked) {
		t.Errorf("renew revoked: err = %v", err)
	}
}

func TestCheckTouchesLastUse(t *testing.T) {
	m, s, clk, ann := newTokens(t)
	ctx := context.Background()
	secret, tok, _ := m.Create(ctx, ann, "c", ScopeRead)
	start := clk.Now()
	_, _ = m.Check(ctx, secret)
	clk.Advance(30 * time.Second)
	_, _ = m.Check(ctx, secret)
	got, _ := s.APIToken(ctx, tok.ID)
	if !got.LastUsedAt.Equal(start) {
		t.Errorf("last used %v, want %v", got.LastUsedAt, start)
	}
}

func TestExpiringForTheBanner(t *testing.T) {
	m, _, clk, ann := newTokens(t)
	ctx := context.Background()
	_, old, _ := m.Create(ctx, ann, "old", ScopeRead)
	clk.Advance(day)
	_, _, _ = m.Create(ctx, ann, "new", ScopeRead)
	clk.Advance(TokenPeriod - TokenWarnBefore - day)
	got, err := m.Expiring(ctx, ann)
	if err != nil || len(got) != 1 || got[0].ID != old.ID {
		t.Errorf("at 25 days: %+v %v", got, err)
	}
	clk.Advance(100 * day)
	if got, _ := m.Expiring(ctx, ann); len(got) != 2 {
		t.Errorf("expired tokens stay on the banner: %+v", got)
	}
}

func TestDueReminders(t *testing.T) {
	m, _, clk, ann := newTokens(t)
	ctx := context.Background()
	_, tok, _ := m.Create(ctx, ann, "c", ScopeRead)
	clk.Advance(TokenPeriod - TokenWarnBefore - time.Second)
	if due, _ := m.DueReminders(ctx); len(due) != 0 {
		t.Errorf("before 5 days: %+v", due)
	}
	clk.Advance(time.Second)
	due, err := m.DueReminders(ctx)
	if err != nil || len(due) != 1 || due[0].N != 1 || due[0].Token.ID != tok.ID {
		t.Fatalf("at 5 days: %+v %v", due, err)
	}
	if err := m.MarkReminded(ctx, due[0]); err != nil {
		t.Fatal(err)
	}
	if due, _ := m.DueReminders(ctx); len(due) != 0 {
		t.Errorf("sent twice: %+v", due)
	}
	clk.Advance(TokenWarnBefore - TokenLastWarnBefore)
	due, _ = m.DueReminders(ctx)
	if len(due) != 1 || due[0].N != 2 {
		t.Fatalf("at 1 day: %+v", due)
	}
	_ = m.MarkReminded(ctx, due[0])
	clk.Advance(TokenLastWarnBefore)
	if due, _ := m.DueReminders(ctx); len(due) != 0 {
		t.Errorf("expired: %+v", due)
	}
}

func TestDueRemindersSkipsToTheLaterOne(t *testing.T) {
	m, _, clk, ann := newTokens(t)
	ctx := context.Background()
	_, _, _ = m.Create(ctx, ann, "c", ScopeRead)
	clk.Advance(TokenPeriod - time.Hour) // Foreman was down for both
	due, _ := m.DueReminders(ctx)
	if len(due) != 1 || due[0].N != 2 {
		t.Fatalf("got %+v, want only the 1-day reminder", due)
	}
	_ = m.MarkReminded(ctx, due[0])
	if due, _ := m.DueReminders(ctx); len(due) != 0 {
		t.Errorf("the 5-day one came after: %+v", due)
	}
}

func TestScopeIncludes(t *testing.T) {
	cases := []struct {
		have, need string
		want       bool
	}{
		{ScopeAdmin, ScopeOperate, true},
		{ScopeOperate, ScopeRead, true},
		{ScopeRead, ScopeRead, true},
		{ScopeRead, ScopeOperate, false},
		{ScopeOperate, ScopeAdmin, false},
		{"", "", false},
		{"root", ScopeRead, false},
	}
	for _, c := range cases {
		if got := ScopeIncludes(c.have, c.need); got != c.want {
			t.Errorf("ScopeIncludes(%q, %q) = %v", c.have, c.need, got)
		}
	}
}

// SPEC 12.10: Recheck follows the token's state by id, without a secret.
func TestRecheckFollowsState(t *testing.T) {
	m, _, clk, ann := newTokens(t)
	ctx := context.Background()
	_, tok, _ := m.Create(ctx, ann, "c", ScopeRead)
	if err := m.Recheck(ctx, tok.ID); err != nil {
		t.Errorf("working: %v", err)
	}
	if err := m.Recheck(ctx, tok.ID+100); !errors.Is(err, ErrTokenUnknown) {
		t.Errorf("unknown id: err = %v", err)
	}
	clk.Advance(TokenPeriod)
	if err := m.Recheck(ctx, tok.ID); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expired: err = %v", err)
	}
	if _, err := m.Renew(ctx, tok.ID, ann); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Revoke(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Recheck(ctx, tok.ID); !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("revoked: err = %v", err)
	}
}

func TestOwnedByListsOnlyTheUsersTokens(t *testing.T) {
	m, s, _, ann := newTokens(t)
	ctx := context.Background()
	bob, err := s.CreateUser(ctx, "bob", "h", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, mine, _ := m.Create(ctx, ann, "mine", ScopeRead)
	_, _, _ = m.Create(ctx, bob.ID, "theirs", ScopeRead)
	got, err := m.OwnedBy(ctx, ann)
	if err != nil || len(got) != 1 || got[0].ID != mine.ID {
		t.Errorf("owned by ann: %+v %v", got, err)
	}
	if one, err := m.Get(ctx, mine.ID); err != nil || one.ID != mine.ID {
		t.Errorf("get: %+v %v", one, err)
	}
	if _, err := m.Get(ctx, mine.ID+100); !errors.Is(err, store.ErrTokenNotFound) {
		t.Errorf("get unknown: err = %v", err)
	}
}
