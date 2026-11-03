package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CreateSession records a session. tokenHash is the SHA-256 of the cookie
// token, in lowercase hex; the token itself is never stored.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at) VALUES (?, ?, ?, ?)",
		tokenHash, userID, now.Unix(), now.Unix())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// SessionUser returns the session's user and last-seen time. found is false
// when no such session exists.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (userID int64, lastSeen time.Time, found bool, err error) {
	var seen int64
	err = s.db.QueryRowContext(ctx,
		"SELECT user_id, last_seen_at FROM sessions WHERE token_hash = ?", tokenHash).Scan(&userID, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, fmt.Errorf("look up session: %w", err)
	}
	return userID, time.Unix(seen, 0).UTC(), true, nil
}

// TouchSession sets the session's last-seen time.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?", now.Unix(), tokenHash)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// DeleteSession ends a session. A missing session is not an error.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteSessionsIdleSince removes sessions last seen before cutoff and
// returns how many it removed.
func (s *Store) DeleteSessionsIdleSince(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE last_seen_at < ?", cutoff.Unix())
	if err != nil {
		return 0, fmt.Errorf("delete idle sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete idle sessions: %w", err)
	}
	return n, nil
}
