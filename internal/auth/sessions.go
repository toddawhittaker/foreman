package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// CookieName is the session cookie's name.
const CookieName = "foreman_session"

const (
	tokenBytes = 32
	// touchEvery bounds how often a request writes lastSeen.
	touchEvery = time.Minute
)

// Sessions is the storage the session manager needs. Token hashes are
// lowercase hex SHA-256 strings; the token itself is never stored.
type Sessions interface {
	CreateSession(ctx context.Context, tokenHash string, userID int64, now time.Time) error
	SessionUser(ctx context.Context, tokenHash string) (userID int64, lastSeen time.Time, found bool, err error)
	TouchSession(ctx context.Context, tokenHash string, now time.Time) error
	DeleteSession(ctx context.Context, tokenHash string) error
}

// SessionManager issues, checks and ends sessions (SPEC 12.8).
type SessionManager struct {
	store  Sessions
	idle   time.Duration
	secure bool
	now    func() time.Time
}

// NewSessionManager builds a manager. secure sets the cookie's Secure flag.
// A session unused for longer than idle is treated as expired.
func NewSessionManager(store Sessions, idle time.Duration, secure bool, now func() time.Time) *SessionManager {
	return &SessionManager{store: store, idle: idle, secure: secure, now: now}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (m *SessionManager) cookie(value string, maxAge int) *http.Cookie {
	//nolint:gosec // Secure follows the public URL scheme (SPEC 12.8), so it is not always set.
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		MaxAge:   maxAge,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// Start creates a session for the user and sets the cookie on w. The
// cookie has no Expires, so the browser drops it when it closes.
func (m *SessionManager) Start(ctx context.Context, w http.ResponseWriter, userID int64) error {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("auth: read random session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := m.store.CreateSession(ctx, hashToken(token), userID, m.now()); err != nil {
		return fmt.Errorf("auth: create session: %w", err)
	}
	http.SetCookie(w, m.cookie(token, 0))
	return nil
}

// requestTokenHash returns the stored hash for the request's cookie, or ""
// when there is no well-formed cookie.
func requestTokenHash(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(raw) != tokenBytes {
		return ""
	}
	return hashToken(c.Value)
}

// Lookup returns the user of the request's session. ok is false when there
// is no cookie, the session is unknown, or it has been idle too long (an
// expired session is deleted). A live session has its last-seen time
// refreshed at most once a minute. err means the store failed.
func (m *SessionManager) Lookup(ctx context.Context, r *http.Request) (userID int64, ok bool, err error) {
	return m.lookup(ctx, r, true)
}

// Check is Lookup without the refresh, for a request the user did not
// make, such as a live page's heartbeat (SPEC 12.8).
func (m *SessionManager) Check(ctx context.Context, r *http.Request) (userID int64, ok bool, err error) {
	return m.lookup(ctx, r, false)
}

func (m *SessionManager) lookup(ctx context.Context, r *http.Request, touch bool) (userID int64, ok bool, err error) {
	h := requestTokenHash(r)
	if h == "" {
		return 0, false, nil
	}
	userID, lastSeen, found, err := m.store.SessionUser(ctx, h)
	if err != nil {
		return 0, false, fmt.Errorf("auth: look up session: %w", err)
	}
	if !found {
		return 0, false, nil
	}
	now := m.now()
	idle := now.Sub(lastSeen)
	if idle > m.idle {
		if err := m.store.DeleteSession(ctx, h); err != nil {
			return 0, false, fmt.Errorf("auth: delete expired session: %w", err)
		}
		return 0, false, nil
	}
	if touch && idle >= touchEvery {
		if err := m.store.TouchSession(ctx, h, now); err != nil {
			return 0, false, fmt.Errorf("auth: touch session: %w", err)
		}
	}
	return userID, true, nil
}

// End deletes the request's session, if it has one, and clears the cookie.
func (m *SessionManager) End(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, m.cookie("", -1))
	h := requestTokenHash(r)
	if h == "" {
		return nil
	}
	if err := m.store.DeleteSession(ctx, h); err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}
