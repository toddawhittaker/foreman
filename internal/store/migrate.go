package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the embedded files, which are named NNNN_name.sql,
// and returns them in order. The numbers must run 1, 2, 3 with no gaps.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		n, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil {
			return nil, fmt.Errorf("migration %q is not named NNNN_name.sql", e.Name())
		}
		body, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: n, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration numbers must run 1, 2, 3 without gaps; found %q at position %d", m.name, i+1)
		}
	}
	return out, nil
}

// migrate applies every migration newer than the database's user_version.
// Each one runs in its own transaction, so a failure leaves the database at
// the last good version.
func migrate(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	return applyMigrations(ctx, db, migrations)
}

func applyMigrations(ctx context.Context, db *sql.DB, migrations []migration) error {
	// One connection, so BEGIN and COMMIT share a session.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open app database: %w", err)
	}
	defer func() { _ = conn.Close() }()

	for _, m := range migrations {
		if err := applyOne(ctx, conn, m, len(migrations)); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, conn *sql.Conn, m migration, known int) (err error) {
	// IMMEDIATE takes the write lock first, so two processes starting at once
	// cannot both apply the same migration.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("start migration %s: %w", m.name, err)
	}
	defer func() {
		if err != nil {
			// The context may be cancelled, so roll back without it.
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	var current int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > known {
		return fmt.Errorf("the app database is at schema version %d, but this Foreman only knows up to %d; run a newer Foreman or restore an older database", current, known)
	}
	if current < m.version {
		if _, err := conn.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		// PRAGMA takes no bound parameters; the version is our own integer.
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(m.version)); err != nil {
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migration %s: %w", m.name, err)
	}
	return nil
}
