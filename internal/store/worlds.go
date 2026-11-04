package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotActive is returned when a world that must be active is not.
var ErrNotActive = errors.New("world is not the active world")

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

// ReplaceActiveWorldForSwitch makes to active in place of from, leaving any
// other active row alone, and in the same transaction sets the step of the
// switch record for from to StepPresented (SPEC 7.10), so the active row and
// the record cannot disagree. It changes nothing and returns ErrNotActive
// when from is not active, or ErrNoSwitch when there is no record.
func (s *Store) ReplaceActiveWorldForSwitch(ctx context.Context, from, to string, now time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace active world: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx, "DELETE FROM active_worlds WHERE world_id = ?", from)
	if err != nil {
		return fmt.Errorf("replace active world: %w", err)
	}
	if err = requireOneRow(res, "replace active world", ErrNotActive); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		"INSERT INTO active_worlds (world_id, since) VALUES (?, ?)", to, now.Unix()); err != nil {
		return fmt.Errorf("replace active world: %w", err)
	}
	res, err = tx.ExecContext(ctx,
		"UPDATE world_switches SET step = ? WHERE from_world = ?", StepPresented, from)
	if err != nil {
		return fmt.Errorf("replace active world: %w", err)
	}
	if err = requireOneRow(res, "replace active world", ErrNoSwitch); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("replace active world: %w", err)
	}
	return nil
}

// requireOneRow returns missing when the statement changed no row.
func requireOneRow(res sql.Result, what string, missing error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, missing)
	}
	return nil
}
