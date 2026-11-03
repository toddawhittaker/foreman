package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ActiveWorld returns the id of the active world. found is false when no
// world is active.
func (s *Store) ActiveWorld(ctx context.Context) (id string, found bool, err error) {
	err = s.db.QueryRowContext(ctx,
		"SELECT world_id FROM active_worlds ORDER BY since, world_id LIMIT 1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read active world: %w", err)
	}
	return id, true, nil
}

// SetActiveWorld makes id the one active world (SPEC 5.3: one at a time in v1).
func (s *Store) SetActiveWorld(ctx context.Context, id string, now time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set active world: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, "DELETE FROM active_worlds"); err != nil {
		return fmt.Errorf("set active world: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		"INSERT INTO active_worlds (world_id, since) VALUES (?, ?)", id, now.Unix()); err != nil {
		return fmt.Errorf("set active world: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("set active world: %w", err)
	}
	return nil
}

// ClearActiveWorld leaves no world active. Clearing when none is active is
// not an error.
func (s *Store) ClearActiveWorld(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM active_worlds"); err != nil {
		return fmt.Errorf("clear active world: %w", err)
	}
	return nil
}
