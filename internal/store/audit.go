package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AuditEntry is one row of the audit log (SPEC 12.9).
type AuditEntry struct {
	ID       int64
	Time     time.Time
	UserID   int64 // 0 when there is no user: a system action, or the user was deleted
	Username string
	Action   string
	WorldID  string // "" when the action is not about one world
	Detail   string // must not hold a secret (SPEC 14.8)
}

// AddAudit records an entry. The ID of e is ignored.
func (s *Store) AddAudit(ctx context.Context, e AuditEntry) error {
	var userID any
	if e.UserID != 0 {
		userID = e.UserID
	}
	var worldID any
	if e.WorldID != "" {
		worldID = e.WorldID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (time, user_id, username, action, world_id, detail)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.Time.Unix(), userID, e.Username, e.Action, worldID, e.Detail)
	if err != nil {
		return fmt.Errorf("record audit entry: %w", err)
	}
	return nil
}

// ListAudit returns up to limit entries, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int) (_ []AuditEntry, err error) {
	if limit < 1 {
		return nil, errors.New("list audit log: limit must be at least 1")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, time, user_id, username, action, world_id, detail
		 FROM audit_log ORDER BY time DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("list audit log: %w", cerr)
		}
	}()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var at int64
		var userID *int64
		var worldID *string
		if err := rows.Scan(&e.ID, &at, &userID, &e.Username, &e.Action, &worldID, &e.Detail); err != nil {
			return nil, fmt.Errorf("list audit log: %w", err)
		}
		e.Time = time.Unix(at, 0).UTC()
		if userID != nil {
			e.UserID = *userID
		}
		if worldID != nil {
			e.WorldID = *worldID
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	return out, nil
}
