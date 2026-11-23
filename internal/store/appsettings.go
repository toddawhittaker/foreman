package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AppSetting returns the value stored for name. found is false when the
// setting was never set, so the caller applies its default.
func (s *Store) AppSetting(ctx context.Context, name string) (value string, found bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE name = ?", name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read setting %s: %w", name, err)
	}
	return value, true, nil
}

// SetAppSetting stores value for name, replacing any earlier value, and
// records who changed it and when.
func (s *Store) SetAppSetting(ctx context.Context, name, value, by string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_settings (name, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		name, value, now.Unix(), by)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", name, err)
	}
	return nil
}

// AppSettingWrite is one setting for SetAppSettings.
type AppSettingWrite struct{ Name, Value string }

// SetAppSettings stores every write in one transaction, so a failure stores
// none of them.
func (s *Store) SetAppSettings(ctx context.Context, writes []AppSettingWrite, by string, now time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set settings: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, w := range writes {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO app_settings (name, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
			 ON CONFLICT (name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
			w.Name, w.Value, now.Unix(), by); err != nil {
			return fmt.Errorf("set setting %s: %w", w.Name, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("set settings: %w", err)
	}
	return nil
}
