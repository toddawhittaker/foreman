package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoSecret is returned when no stored secret has the name given.
var ErrNoSecret = errors.New("no stored secret with that name")

// PutSecret stores an already sealed value under name, replacing any earlier
// one. The store never sees the plain value (SPEC 9.11).
func (s *Store) PutSecret(ctx context.Context, name string, sealed []byte, byUser string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO stored_secrets (name, value, updated_at, updated_by)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (name) DO UPDATE SET
		     value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		name, sealed, now.Unix(), byUser)
	if err != nil {
		return fmt.Errorf("store secret: %w", err)
	}
	return nil
}

// GetSecret returns the sealed value, when it was stored and by whom. It
// returns ErrNoSecret when there is none.
func (s *Store) GetSecret(ctx context.Context, name string) (sealed []byte, updatedAt time.Time, updatedBy string, err error) {
	var at int64
	err = s.db.QueryRowContext(ctx,
		"SELECT value, updated_at, updated_by FROM stored_secrets WHERE name = ?", name).Scan(&sealed, &at, &updatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, "", ErrNoSecret
	}
	if err != nil {
		return nil, time.Time{}, "", fmt.Errorf("read secret: %w", err)
	}
	return sealed, time.Unix(at, 0).UTC(), updatedBy, nil
}

// DeleteSecret removes the secret. A missing one is not an error.
func (s *Store) DeleteSecret(ctx context.Context, name string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM stored_secrets WHERE name = ?", name); err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	return nil
}
