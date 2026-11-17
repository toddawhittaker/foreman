package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// APIToken is one API token row (SPEC 12.10). The secret's hash is never
// read back out: lookups take it, and nothing returns it.
type APIToken struct {
	ID          int64
	UserID      int64
	Owner       string // the owner's username
	Name        string
	Scope       string // "read", "operate" or "admin"
	CreatedAt   time.Time
	PeriodStart time.Time // creation or the last renewal
	RevokedAt   time.Time // zero when not revoked
	LastUsedAt  time.Time // zero when never used
	// Reminded is how many of this period's reminders were sent: 0, 1 (the
	// 5-day one) or 2 (the 1-day one).
	Reminded int
}

var (
	// ErrTokenNameTaken is returned when the owner already has a token of
	// that name, ignoring ASCII case, that is not revoked.
	ErrTokenNameTaken = errors.New("store: token name taken")
	// ErrTokenNotFound is returned when no such token exists, or, for a
	// renewal, when it belongs to someone else.
	ErrTokenNotFound = errors.New("store: token not found")
	// ErrTokenRevoked is returned when renewing or revoking a revoked token.
	ErrTokenRevoked = errors.New("store: token revoked")
)

// apiTokenSelect never reads secret_hash, so nothing returns it.
//
//nolint:gosec // a column list, not a credential
const apiTokenSelect = `t.id, t.user_id, u.username, t.name, t.scope, t.created_at,
	t.period_start, t.revoked_at, t.last_used_at, t.reminded
	FROM api_tokens t JOIN users u ON u.id = t.user_id`

type rowScanner interface{ Scan(dest ...any) error }

func scanToken(r rowScanner) (APIToken, error) {
	var t APIToken
	var created, start int64
	var revoked, used *int64
	if err := r.Scan(&t.ID, &t.UserID, &t.Owner, &t.Name, &t.Scope, &created, &start, &revoked, &used, &t.Reminded); err != nil {
		return APIToken{}, err
	}
	t.CreatedAt = time.Unix(created, 0).UTC()
	t.PeriodStart = time.Unix(start, 0).UTC()
	if revoked != nil {
		t.RevokedAt = time.Unix(*revoked, 0).UTC()
	}
	if used != nil {
		t.LastUsedAt = time.Unix(*used, 0).UTC()
	}
	return t, nil
}

// CreateAPIToken stores a new token. secretHash is the SHA-256 of the
// secret in lowercase hex; the secret itself is never stored.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name, scope, secretHash string, now time.Time) (APIToken, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, name, scope, secret_hash, created_at, period_start)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, name, scope, secretHash, now.Unix(), now.Unix())
	if isUniqueViolation(err) {
		// The secret hash is unique too, but 32 random bytes never repeat.
		return APIToken{}, ErrTokenNameTaken
	}
	if err != nil {
		return APIToken{}, fmt.Errorf("create API token: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return APIToken{}, fmt.Errorf("create API token: %w", err)
	}
	return s.APIToken(ctx, id)
}

// APIToken returns one token by ID, or ErrTokenNotFound.
func (s *Store) APIToken(ctx context.Context, id int64) (APIToken, error) {
	t, err := scanToken(s.db.QueryRowContext(ctx, "SELECT "+apiTokenSelect+" WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrTokenNotFound
	}
	if err != nil {
		return APIToken{}, fmt.Errorf("look up API token: %w", err)
	}
	return t, nil
}

// APITokenByHash returns the token whose secret has this hash. found is
// false when there is none.
func (s *Store) APITokenByHash(ctx context.Context, secretHash string) (_ APIToken, found bool, _ error) {
	t, err := scanToken(s.db.QueryRowContext(ctx, "SELECT "+apiTokenSelect+" WHERE t.secret_hash = ?", secretHash))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, false, nil
	}
	if err != nil {
		return APIToken{}, false, fmt.Errorf("look up API token: %w", err)
	}
	return t, true, nil
}

// RenewAPIToken starts a new period from now and clears the reminders.
// Only the owner renews: a token of another user is ErrTokenNotFound. A
// revoked token is ErrTokenRevoked and stays revoked.
func (s *Store) RenewAPIToken(ctx context.Context, id, ownerID int64, now time.Time) (APIToken, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET period_start = ?, reminded = 0
		 WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		now.Unix(), id, ownerID)
	if err != nil {
		return APIToken{}, fmt.Errorf("renew API token: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return APIToken{}, fmt.Errorf("renew API token: %w", err)
	} else if n == 1 {
		return s.APIToken(ctx, id)
	}
	t, err := s.APIToken(ctx, id)
	switch {
	case err != nil:
		return APIToken{}, err
	case t.UserID != ownerID:
		return APIToken{}, ErrTokenNotFound
	default:
		return APIToken{}, ErrTokenRevoked
	}
}

// RevokeAPIToken ends a token for good and returns it. Revoking a revoked
// token is ErrTokenRevoked; its first revoke time stays.
func (s *Store) RevokeAPIToken(ctx context.Context, id int64, now time.Time) (APIToken, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", now.Unix(), id)
	if err != nil {
		return APIToken{}, fmt.Errorf("revoke API token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return APIToken{}, fmt.Errorf("revoke API token: %w", err)
	}
	t, err := s.APIToken(ctx, id)
	if err != nil {
		return APIToken{}, err
	}
	if n == 0 {
		return APIToken{}, ErrTokenRevoked
	}
	return t, nil
}

// ListAPITokens returns every user's tokens, by owner, then name, then ID.
func (s *Store) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	return s.queryTokens(ctx, "list API tokens",
		"SELECT "+apiTokenSelect+" ORDER BY u.username, t.name, t.id")
}

// APITokensStartedBy returns the owner's tokens that are not revoked and
// whose period started at or before cutoff, oldest period first. With
// cutoff set to 25 days ago these are the ones within 5 days of expiring
// or already expired (SPEC 12.10, the banner).
func (s *Store) APITokensStartedBy(ctx context.Context, userID int64, cutoff time.Time) ([]APIToken, error) {
	return s.queryTokens(ctx, "list expiring API tokens",
		"SELECT "+apiTokenSelect+` WHERE t.user_id = ? AND t.revoked_at IS NULL AND t.period_start <= ?
		 ORDER BY t.period_start, t.id`, userID, cutoff.Unix())
}

// APITokensDueReminder returns tokens that are not revoked, whose period
// started after notBefore, and that are owed a reminder: the first when the
// period started at or before first, the second at or before second.
func (s *Store) APITokensDueReminder(ctx context.Context, notBefore, first, second time.Time) ([]APIToken, error) {
	return s.queryTokens(ctx, "list API tokens due a reminder",
		"SELECT "+apiTokenSelect+` WHERE t.revoked_at IS NULL AND t.period_start > ?
		 AND ((t.reminded < 1 AND t.period_start <= ?) OR (t.reminded < 2 AND t.period_start <= ?))
		 ORDER BY t.id`, notBefore.Unix(), first.Unix(), second.Unix())
}

// SetAPITokenReminded records that reminder n (1 or 2) was sent. It changes
// nothing if the token was renewed since periodStart was read, so a renewal
// racing the sweep keeps its fresh reminders.
func (s *Store) SetAPITokenReminded(ctx context.Context, id int64, periodStart time.Time, n int) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE api_tokens SET reminded = ? WHERE id = ? AND period_start = ?", n, id, periodStart.Unix())
	if err != nil {
		return fmt.Errorf("mark API token reminded: %w", err)
	}
	return nil
}

// TouchAPIToken sets the token's last use to now, unless it was set less
// than a minute ago, which keeps a busy client from writing on every call.
func (s *Store) TouchAPIToken(ctx context.Context, id int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = ?
		 WHERE id = ? AND (last_used_at IS NULL OR last_used_at <= ?)`,
		now.Unix(), id, now.Add(-time.Minute).Unix())
	if err != nil {
		return fmt.Errorf("touch API token: %w", err)
	}
	return nil
}

func (s *Store) queryTokens(ctx context.Context, what, query string, args ...any) (_ []APIToken, err error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer func() {
		if cerr := rows.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("%s: %w", what, cerr)
		}
	}()
	var out []APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}
