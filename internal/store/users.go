package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// User is one account. Every user is an admin in v1 (SPEC 12.1).
type User struct {
	ID           int64
	Username     string
	PasswordHash string // "" for header-created users
	Role         string
	CreatedAt    time.Time
}

// ErrAdminExists is returned by CreateFirstAdmin when any user already exists.
var ErrAdminExists = errors.New("store: an admin already exists")

// ErrUsernameTaken is returned when a username is already in use, ignoring case.
var ErrUsernameTaken = errors.New("store: username taken")

const userColumns = "id, username, password_hash, role, created_at"

func scanUser(row *sql.Row) (User, error) {
	var u User
	var created int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created); err != nil {
		return User{}, err
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return u, nil
}

// HasUsers reports whether at least one user exists.
func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var has bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM users)").Scan(&has)
	if err != nil {
		return false, fmt.Errorf("check for users: %w", err)
	}
	return has, nil
}

// CreateFirstAdmin inserts the user only if the table is empty. The check and
// the insert are one statement, so two racing calls cannot both succeed.
func (s *Store) CreateFirstAdmin(ctx context.Context, username, passwordHash string, now time.Time) (User, error) {
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at)
		 SELECT ?, ?, 'admin', ? WHERE NOT EXISTS (SELECT 1 FROM users)
		 RETURNING `+userColumns,
		username, passwordHash, now.Unix())
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrAdminExists
	}
	if err != nil {
		return User{}, fmt.Errorf("create first admin: %w", err)
	}
	return u, nil
}

// CreateUser adds an admin user. It returns ErrUsernameTaken when the name
// is in use, whatever its case.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, now time.Time) (User, error) {
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at)
		 VALUES (?, ?, 'admin', ?) RETURNING `+userColumns,
		username, passwordHash, now.Unix())
	u, err := scanUser(row)
	if isUniqueViolation(err) {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// UserByUsername finds a user by name, ignoring case.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, bool, error) {
	return s.queryUser(ctx, "SELECT "+userColumns+" FROM users WHERE username = ?", username)
}

// UserByID finds a user by id.
func (s *Store) UserByID(ctx context.Context, id int64) (User, bool, error) {
	return s.queryUser(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id)
}

func (s *Store) queryUser(ctx context.Context, query string, arg any) (User, bool, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, query, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("look up user: %w", err)
	}
	return u, true, nil
}

// SetPasswordHash replaces a user's password hash.
func (s *Store) SetPasswordHash(ctx context.Context, id int64, hash string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, id)
	if err != nil {
		return fmt.Errorf("set password hash: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set password hash: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("set password hash: no user with id %d", id)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
