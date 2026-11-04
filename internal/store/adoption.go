package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrAdoptionExists is returned when an adoption is already recorded: one
// runs at a time (SPEC 4.8).
var ErrAdoptionExists = errors.New("another adoption of the game folder is already in progress")

// SetAdoption records that a first adoption is building world id (SPEC 4.8).
// It returns ErrAdoptionExists, changing nothing, when one is recorded.
func (s *Store) SetAdoption(ctx context.Context, id string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO adoption (one, world_id, started_at) VALUES (1, ?, ?) ON CONFLICT DO NOTHING", id, now.Unix())
	if err != nil {
		return fmt.Errorf("record adoption: %w", err)
	}
	return requireOneRow(res, "record adoption", ErrAdoptionExists)
}

// AdoptionRecord is a first adoption in progress.
type AdoptionRecord struct {
	WorldID string
	// StartedAt is when it was recorded, to the second.
	StartedAt time.Time
}

// Adoption returns the recorded adoption. found is false when none is
// recorded.
func (s *Store) Adoption(ctx context.Context) (rec AdoptionRecord, found bool, err error) {
	var started int64
	err = s.db.QueryRowContext(ctx, "SELECT world_id, started_at FROM adoption WHERE one = 1").Scan(&rec.WorldID, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return AdoptionRecord{}, false, nil
	}
	if err != nil {
		return AdoptionRecord{}, false, fmt.Errorf("read adoption: %w", err)
	}
	rec.StartedAt = time.Unix(started, 0)
	return rec, true, nil
}

// ClearAdoption forgets the recorded adoption. None recorded is not an error.
func (s *Store) ClearAdoption(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM adoption"); err != nil {
		return fmt.Errorf("clear adoption: %w", err)
	}
	return nil
}
