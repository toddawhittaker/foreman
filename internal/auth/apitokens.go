package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/store"
)

// API token rules (SPEC 12.10, 14.19).
const (
	// TokenPrefix starts every secret, so a leaked one is easy to spot.
	TokenPrefix = "fmn_"
	// TokenPeriod is how long a token works after creation or renewal.
	TokenPeriod = 30 * 24 * time.Hour
	// TokenWarnBefore is when the banner and the first reminder start.
	TokenWarnBefore = 5 * 24 * time.Hour
	// TokenLastWarnBefore is when the second reminder is due.
	TokenLastWarnBefore = 24 * time.Hour
	// TokenNameMax is the longest name, in characters.
	TokenNameMax = 32

	tokenSecretLen = len(TokenPrefix) + 43 // 32 bytes in unpadded base64url
)

// Token scopes, each including the one before (SPEC 12.10).
const (
	ScopeRead    = "read"
	ScopeOperate = "operate"
	ScopeAdmin   = "admin"
)

var scopeRank = map[string]int{ScopeRead: 1, ScopeOperate: 2, ScopeAdmin: 3}

// ScopeIncludes reports whether a token of scope have may do what scope
// need allows. An unknown scope includes nothing.
func ScopeIncludes(have, need string) bool {
	h, n := scopeRank[have], scopeRank[need]
	return h > 0 && n > 0 && h >= n
}

// TokenState is worked out from a token's fields, never stored.
type TokenState int

// The states of SPEC 12.10.
const (
	TokenWorking TokenState = iota
	TokenExpired
	TokenRevoked
)

// Errors from Check. Each names why the token was refused (SPEC 14.19).
var (
	ErrTokenUnknown = errors.New("auth: unknown API token")
	ErrTokenExpired = errors.New("auth: API token expired")
	ErrTokenRevoked = errors.New("auth: API token revoked")
)

// Errors from Create.
var (
	ErrTokenName  = errors.New("auth: token name must be 1 to 32 letters, digits, spaces, dots, underscores or hyphens")
	ErrTokenScope = errors.New("auth: unknown token scope")
)

// APITokenStore is the storage the token manager needs. *store.Store
// satisfies it.
type APITokenStore interface {
	CreateAPIToken(ctx context.Context, userID int64, name, scope, secretHash string, now time.Time) (store.APIToken, error)
	APITokenByHash(ctx context.Context, secretHash string) (store.APIToken, bool, error)
	RenewAPIToken(ctx context.Context, id, ownerID int64, now time.Time) (store.APIToken, error)
	RevokeAPIToken(ctx context.Context, id int64, now time.Time) (store.APIToken, error)
	ListAPITokens(ctx context.Context) ([]store.APIToken, error)
	APITokensStartedBy(ctx context.Context, userID int64, cutoff time.Time) ([]store.APIToken, error)
	APITokensDueReminder(ctx context.Context, notBefore, first, second time.Time) ([]store.APIToken, error)
	SetAPITokenReminded(ctx context.Context, id int64, periodStart time.Time, n int) error
	TouchAPIToken(ctx context.Context, id int64, now time.Time) error
}

// APITokens creates, checks, renews and revokes API tokens (SPEC 12.10).
// Only a secret's SHA-256 is stored.
type APITokens struct {
	store APITokenStore
	now   func() time.Time
}

// NewAPITokens builds the token manager.
func NewAPITokens(s APITokenStore, now func() time.Time) *APITokens {
	return &APITokens{store: s, now: now}
}

// ExpiresAt is when the token's current period ends.
func ExpiresAt(t store.APIToken) time.Time { return t.PeriodStart.Add(TokenPeriod) }

// State works out the token's state at now.
func State(t store.APIToken, now time.Time) TokenState {
	switch {
	case !t.RevokedAt.IsZero():
		return TokenRevoked
	case !now.Before(ExpiresAt(t)):
		return TokenExpired
	default:
		return TokenWorking
	}
}

// State is the package State at the manager's current time.
func (m *APITokens) State(t store.APIToken) TokenState { return State(t, m.now()) }

// CleanTokenName trims surrounding spaces and checks the name rule. The
// rule refines SPEC 12.10's "letters, digits" to Unicode letters and
// digits, so brackets stay out of the chat prefix "[name]".
func CleanTokenName(name string) (string, error) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if !utf8.ValidString(name) || n < 1 || n > TokenNameMax {
		return "", ErrTokenName
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" ._-", r) {
			return "", ErrTokenName
		}
	}
	return name, nil
}

// Create makes a token for the user and returns its secret, which is never
// available again. A name the owner already uses is store.ErrTokenNameTaken.
func (m *APITokens) Create(ctx context.Context, userID int64, name, scope string) (secret string, _ store.APIToken, _ error) {
	name, err := CleanTokenName(name)
	if err != nil {
		return "", store.APIToken{}, err
	}
	if scopeRank[scope] == 0 {
		return "", store.APIToken{}, ErrTokenScope
	}
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", store.APIToken{}, fmt.Errorf("auth: read random token secret: %w", err)
	}
	secret = TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	t, err := m.store.CreateAPIToken(ctx, userID, name, scope, hashToken(secret), m.now())
	if err != nil {
		return "", store.APIToken{}, err
	}
	return secret, t, nil
}

// parseSecret reports whether s has the secret's exact shape, so malformed
// input is refused without a database lookup.
func parseSecret(s string) bool {
	if len(s) != tokenSecretLen || !strings.HasPrefix(s, TokenPrefix) {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s[len(TokenPrefix):])
	return err == nil && len(raw) == tokenBytes
}

// Check returns the token for a secret, with its owner's name in Owner. A
// malformed or unknown secret is ErrTokenUnknown; an expired or revoked
// token is ErrTokenExpired or ErrTokenRevoked. A working token's last use
// is recorded, at most once a minute. Any other error means the store
// failed.
func (m *APITokens) Check(ctx context.Context, secret string) (store.APIToken, error) {
	if !parseSecret(secret) {
		return store.APIToken{}, ErrTokenUnknown
	}
	t, found, err := m.store.APITokenByHash(ctx, hashToken(secret))
	if err != nil {
		return store.APIToken{}, fmt.Errorf("auth: look up API token: %w", err)
	}
	if !found {
		return store.APIToken{}, ErrTokenUnknown
	}
	now := m.now()
	switch State(t, now) {
	case TokenRevoked:
		return store.APIToken{}, ErrTokenRevoked
	case TokenExpired:
		return store.APIToken{}, ErrTokenExpired
	}
	if t.LastUsedAt.IsZero() || now.Sub(t.LastUsedAt) >= touchEvery {
		if err := m.store.TouchAPIToken(ctx, t.ID, now); err != nil {
			return store.APIToken{}, fmt.Errorf("auth: touch API token: %w", err)
		}
		t.LastUsedAt = now.UTC().Truncate(time.Second)
	}
	return t, nil
}

// Renew starts a new 30-day period for the owner's token, keeping its
// secret. Another user's token is store.ErrTokenNotFound and a revoked one
// store.ErrTokenRevoked. The caller must be signed in through a browser,
// never through a token (SPEC 12.10).
func (m *APITokens) Renew(ctx context.Context, id, ownerID int64) (store.APIToken, error) {
	return m.store.RenewAPIToken(ctx, id, ownerID, m.now())
}

// Revoke ends a token for good and returns it. The caller checks that the
// user is its owner or an admin.
func (m *APITokens) Revoke(ctx context.Context, id int64) (store.APIToken, error) {
	return m.store.RevokeAPIToken(ctx, id, m.now())
}

// List returns every token, never with its secret or hash.
func (m *APITokens) List(ctx context.Context) ([]store.APIToken, error) {
	return m.store.ListAPITokens(ctx)
}

// Expiring returns the user's tokens that expire within 5 days or have
// expired and are not revoked, soonest first: the banner's lines.
func (m *APITokens) Expiring(ctx context.Context, userID int64) ([]store.APIToken, error) {
	return m.store.APITokensStartedBy(ctx, userID, m.now().Add(TokenWarnBefore-TokenPeriod))
}

// Reminder is a token-expiring reminder that is due: N is 1 for the 5-day
// reminder and 2 for the 1-day one.
type Reminder struct {
	Token store.APIToken
	N     int
}

// DueReminders returns the reminders to send now. A working token owed both
// gets only the later one; an expired or revoked token gets none.
func (m *APITokens) DueReminders(ctx context.Context) ([]Reminder, error) {
	now := m.now()
	ts, err := m.store.APITokensDueReminder(ctx,
		now.Add(-TokenPeriod), now.Add(TokenWarnBefore-TokenPeriod), now.Add(TokenLastWarnBefore-TokenPeriod))
	if err != nil {
		return nil, err
	}
	out := make([]Reminder, 0, len(ts))
	for _, t := range ts {
		n := 1
		if !now.Before(ExpiresAt(t).Add(-TokenLastWarnBefore)) {
			n = 2
		}
		out = append(out, Reminder{Token: t, N: n})
	}
	return out, nil
}

// MarkReminded records that r was sent. A token renewed since r was read
// is left alone.
func (m *APITokens) MarkReminded(ctx context.Context, r Reminder) error {
	return m.store.SetAPITokenReminded(ctx, r.Token.ID, r.Token.PeriodStart, r.N)
}
