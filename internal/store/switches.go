package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// The steps of a world switch, in order (SPEC 7.9, 7.10). The database
// refuses any other value.
const (
	StepBegun       = "begun"
	StepStopped     = "stopped"
	StepSnapshotted = "snapshotted"
	StepChecked     = "checked"
	StepPresented   = "presented"
	StepStarted     = "started"
)

// ErrNoSwitch is returned when no switch record exists for the world named.
var ErrNoSwitch = errors.New("no switch in progress from that world")

// Switch is a switch in progress or cut short: From is the world being left.
type Switch struct {
	From string
	To   string
	Step string
}

// BeginSwitch records a new switch at step begun. It fails when a switch
// from that world already exists.
func (s *Store) BeginSwitch(ctx context.Context, from, to, byUser string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO world_switches (from_world, to_world, step, started_at, by_user)
		 VALUES (?, ?, ?, ?, ?)`, from, to, StepBegun, now.Unix(), byUser)
	if err != nil {
		return fmt.Errorf("begin switch: %w", err)
	}
	return nil
}

// SetSwitchStep records how far the switch from the world has got. It
// returns ErrNoSwitch when there is no such record.
func (s *Store) SetSwitchStep(ctx context.Context, from, step string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE world_switches SET step = ? WHERE from_world = ?", step, from)
	if err != nil {
		return fmt.Errorf("set switch step: %w", err)
	}
	return requireOneRow(res, "set switch step", ErrNoSwitch)
}

// ListSwitches returns every switch record, oldest first, for startup
// recovery (SPEC 3.8).
func (s *Store) ListSwitches(ctx context.Context) (_ []Switch, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_world, to_world, step
		 FROM world_switches ORDER BY started_at, from_world`)
	if err != nil {
		return nil, fmt.Errorf("list switches: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("list switches: %w", cerr)
		}
	}()
	var out []Switch
	for rows.Next() {
		var sw Switch
		if err := rows.Scan(&sw.From, &sw.To, &sw.Step); err != nil {
			return nil, fmt.Errorf("list switches: %w", err)
		}
		out = append(out, sw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list switches: %w", err)
	}
	return out, nil
}

// EndSwitch deletes the record of the switch from the world. A missing
// record is not an error.
func (s *Store) EndSwitch(ctx context.Context, from string) error {
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM world_switches WHERE from_world = ?", from); err != nil {
		return fmt.Errorf("end switch: %w", err)
	}
	return nil
}
