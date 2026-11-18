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
	// TokenName names the API token the action came through, "" for none (SPEC 12.9).
	TokenName string
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
		`INSERT INTO audit_log (time, user_id, username, action, world_id, detail, token_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Time.Unix(), userID, e.Username, e.Action, worldID, e.Detail, e.TokenName)
	if err != nil {
		return fmt.Errorf("record audit entry: %w", err)
	}
	return nil
}

// AuditCursor marks a place in the log: entries older than the one with this
// time and id. The zero value means the newest entry (SPEC 12.9).
type AuditCursor struct {
	Time time.Time
	ID   int64
}

// AuditFilter selects entries. An empty field matches every entry; the others
// must match exactly. Limit is the most entries to return.
type AuditFilter struct {
	Username string
	Action   string
	WorldID  string
	Before   AuditCursor
	Limit    int
}

// ListAudit returns up to f.Limit entries that match f, newest first.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) (_ []AuditEntry, err error) {
	if f.Limit < 1 {
		return nil, errors.New("list audit log: limit must be at least 1")
	}
	// A filter that is not set matches every row, so the statement is fixed
	// text and the values are only ever parameters.
	var beforeTime int64
	if !f.Before.Time.IsZero() {
		beforeTime = f.Before.Time.Unix()
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, time, user_id, username, action, world_id, detail, token_name FROM audit_log
		 WHERE (? = '' OR username = ?) AND (? = '' OR action = ?) AND (? = '' OR world_id = ?)
		   AND (? = 0 OR time < ? OR (time = ? AND id < ?))
		 ORDER BY time DESC, id DESC LIMIT ?`,
		f.Username, f.Username, f.Action, f.Action, f.WorldID, f.WorldID,
		beforeTime, beforeTime, beforeTime, f.Before.ID, f.Limit)
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
		if err := rows.Scan(&e.ID, &at, &userID, &e.Username, &e.Action, &worldID, &e.Detail, &e.TokenName); err != nil {
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

// AuditValues are the distinct values the log holds, for the filter menus.
// Each list is sorted.
type AuditValues struct {
	Users   []string
	Actions []string
	Worlds  []string
}

// AuditFilterValues returns the distinct users, actions and worlds in the log.
func (s *Store) AuditFilterValues(ctx context.Context) (AuditValues, error) {
	var v AuditValues
	var err error
	if v.Users, err = s.distinctAudit(ctx, "users",
		`SELECT DISTINCT username FROM audit_log WHERE username <> '' ORDER BY username`); err != nil {
		return AuditValues{}, err
	}
	if v.Actions, err = s.distinctAudit(ctx, "actions",
		`SELECT DISTINCT action FROM audit_log WHERE action <> '' ORDER BY action`); err != nil {
		return AuditValues{}, err
	}
	if v.Worlds, err = s.distinctAudit(ctx, "worlds",
		`SELECT DISTINCT world_id FROM audit_log WHERE world_id IS NOT NULL AND world_id <> '' ORDER BY world_id`); err != nil {
		return AuditValues{}, err
	}
	return v, nil
}

// distinctAudit runs a query that returns one text column. what names the
// column in an error.
func (s *Store) distinctAudit(ctx context.Context, what, query string) (_ []string, err error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list audit %s: %w", what, err)
	}
	defer func() {
		if cerr := rows.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("list audit %s: %w", what, cerr)
		}
	}()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("list audit %s: %w", what, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit %s: %w", what, err)
	}
	return out, nil
}

// DeleteAuditBefore deletes the entries older than t and returns how many.
func (s *Store) DeleteAuditBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM audit_log WHERE time < ?", t.Unix())
	if err != nil {
		return 0, fmt.Errorf("delete old audit entries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete old audit entries: %w", err)
	}
	return n, nil
}
