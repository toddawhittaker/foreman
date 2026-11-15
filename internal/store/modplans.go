package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MaxModPlanNames is how many mods one world's saved plan may name (SPEC 9.14).
const MaxModPlanNames = 50

// ErrModPlanFull is returned when a plan already names MaxModPlanNames mods.
var ErrModPlanFull = errors.New("store: the mod plan is full")

// ModPlanMod is one picked mod in a world's saved plan.
type ModPlanMod struct {
	Name    string
	AddedAt time.Time
	AddedBy string
}

// ModPlan returns the mods picked for a world, oldest first. Names are not
// checked here; callers check them before adding.
func (s *Store) ModPlan(ctx context.Context, worldID string) ([]ModPlanMod, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, added_at, added_by FROM mod_plan_mods WHERE world_id = ? ORDER BY added_at, rowid", worldID)
	if err != nil {
		return nil, fmt.Errorf("read mod plan: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ModPlanMod
	for rows.Next() {
		var m ModPlanMod
		var added int64
		if err := rows.Scan(&m.Name, &added, &m.AddedBy); err != nil {
			return nil, fmt.Errorf("read mod plan: %w", err)
		}
		m.AddedAt = time.Unix(added, 0).UTC()
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read mod plan: %w", err)
	}
	return out, nil
}

// AddModPlanName adds a mod to a world's plan. A name already there is left
// as it is. It returns ErrModPlanFull when the plan is at its limit.
func (s *Store) AddModPlanName(ctx context.Context, worldID, name, by string, now time.Time) error {
	// The insert takes the write lock even when it adds no row, so the check
	// after it sees the same plan: no remove or add can slip in between.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add to mod plan: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO mod_plan_mods (world_id, name, added_at, added_by)
		 SELECT ?1, ?2, ?3, ?4
		 WHERE NOT EXISTS (SELECT 1 FROM mod_plan_mods WHERE world_id = ?1 AND name = ?2)
		   AND (SELECT COUNT(*) FROM mod_plan_mods WHERE world_id = ?1) < ?5`,
		worldID, name, now.Unix(), by, MaxModPlanNames)
	if err != nil {
		return fmt.Errorf("add to mod plan: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("add to mod plan: %w", err)
	}
	if n == 0 {
		var there int
		err = tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM mod_plan_mods WHERE world_id = ? AND name = ?", worldID, name).Scan(&there)
		if err != nil {
			return fmt.Errorf("add to mod plan: %w", err)
		}
		if there == 0 {
			return ErrModPlanFull
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add to mod plan: %w", err)
	}
	return nil
}

// RemoveModPlanName removes a mod from a world's plan. A name that is not
// there is not an error.
func (s *Store) RemoveModPlanName(ctx context.Context, worldID, name string) error {
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM mod_plan_mods WHERE world_id = ? AND name = ?", worldID, name); err != nil {
		return fmt.Errorf("remove from mod plan: %w", err)
	}
	return nil
}

// ClearModPlan empties a world's plan. Callers use it when the plan is
// installed and when the world is deleted.
func (s *Store) ClearModPlan(ctx context.Context, worldID string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM mod_plan_mods WHERE world_id = ?", worldID); err != nil {
		return fmt.Errorf("clear mod plan: %w", err)
	}
	return nil
}
