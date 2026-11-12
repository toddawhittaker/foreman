package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RestartSchedule is the one restart schedule (SPEC 7.12).
type RestartSchedule struct {
	Enabled bool
	// Minute is minutes after midnight in Zone.
	Minute     int
	Zone       string
	LimitHours int
	UpdatedAt  time.Time
	UpdatedBy  string
}

// RestartSchedule returns the schedule; found is false when none was ever
// saved.
func (s *Store) RestartSchedule(ctx context.Context) (_ RestartSchedule, found bool, err error) {
	var r RestartSchedule
	var updated int64
	err = s.db.QueryRowContext(ctx,
		"SELECT enabled, minute, zone, limit_hours, updated_at, updated_by FROM restart_schedule WHERE id = 1").
		Scan(&r.Enabled, &r.Minute, &r.Zone, &r.LimitHours, &updated, &r.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return RestartSchedule{}, false, nil
	}
	if err != nil {
		return RestartSchedule{}, false, fmt.Errorf("read restart schedule: %w", err)
	}
	r.UpdatedAt = time.Unix(updated, 0).UTC()
	return r, true, nil
}

// SetRestartSchedule saves the schedule. r.UpdatedAt is ignored.
func (s *Store) SetRestartSchedule(ctx context.Context, r RestartSchedule, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO restart_schedule (id, enabled, minute, zone, limit_hours, updated_at, updated_by)
		 VALUES (1, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET enabled = excluded.enabled, minute = excluded.minute, zone = excluded.zone,
		   limit_hours = excluded.limit_hours, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		r.Enabled, r.Minute, r.Zone, r.LimitHours, now.Unix(), r.UpdatedBy)
	if err != nil {
		return fmt.Errorf("save restart schedule: %w", err)
	}
	return nil
}
