// Package store is Foreman's app database: SQLite in WAL mode with
// embedded, numbered migrations (SPEC 3.4, STACK 4).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// dbFileMode is the mode of a newly created database file.
const dbFileMode = 0o660

// Store is the app database.
type Store struct {
	db *sql.DB
}

// Open opens the database at path, creating the file if it is missing, and
// brings its schema up to date.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := createFile(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open app database: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// dsn sets the pragmas through the driver so every pooled connection gets them.
func dsn(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)",
		"foreign_keys(ON)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
	} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// createFile makes a missing database file with a fixed mode, whatever the
// process umask is.
func createFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, dbFileMode) //nolint:gosec // path is the app database in the data directory
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create app database file: %w", err)
	}
	if err := f.Chmod(dbFileMode); err != nil {
		_ = f.Close()
		return fmt.Errorf("set app database file mode: %w", err)
	}
	return f.Close()
}
