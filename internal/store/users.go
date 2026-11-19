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
	PasswordHash string // "" for header-created and OIDC users
	Role         string
	CreatedAt    time.Time
	// OIDCIssuer and OIDCSubject name the user at their identity provider;
	// both are "" for a user with no OIDC link (SPEC 12.7).
	OIDCIssuer, OIDCSubject string
}

// ErrAdminExists is returned by CreateFirstAdmin when any user already exists.
var ErrAdminExists = errors.New("store: an admin already exists")

// ErrUsernameTaken is returned when a username is already in use, ignoring case.
var ErrUsernameTaken = errors.New("store: username taken")

// ErrNotLinkable is returned by LinkOIDC when the user has a password or
// is already linked (SPEC 12.7).
var ErrNotLinkable = errors.New("store: user cannot be linked to an OIDC subject")

const userColumns = "id, username, password_hash, role, created_at, oidc_issuer, oidc_subject"

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created int64
	var issuer, subject sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created, &issuer, &subject); err != nil {
		return User{}, err
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	u.OIDCIssuer, u.OIDCSubject = issuer.String, subject.String
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

// CreateOIDCUser adds an admin user with no password, linked to an identity
// provider's subject in the same insert, so no other sign-in can see the
// user unlinked (SPEC 12.7). It returns ErrUsernameTaken when the name, or
// the subject, is already in use.
func (s *Store) CreateOIDCUser(ctx context.Context, username, issuer, subject string, now time.Time) (User, error) {
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at, oidc_issuer, oidc_subject)
		 VALUES (?, '', 'admin', ?, ?, ?) RETURNING `+userColumns,
		username, now.Unix(), issuer, subject)
	u, err := scanUser(row)
	if isUniqueViolation(err) {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("create oidc user: %w", err)
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

// UserByOIDC finds the user linked to an identity provider's subject. Both
// are compared exactly.
func (s *Store) UserByOIDC(ctx context.Context, issuer, subject string) (User, bool, error) {
	return s.queryUser(ctx, "SELECT "+userColumns+" FROM users WHERE oidc_issuer = ? AND oidc_subject = ?", issuer, subject)
}

// LinkOIDC links a user to an identity provider's subject. Only a user with
// no password and no link may be linked, and the check is part of the
// update, so a racing link cannot slip past it (SPEC 12.7). It returns
// ErrNotLinkable otherwise, and when the subject is already linked to
// another user.
func (s *Store) LinkOIDC(ctx context.Context, id int64, issuer, subject string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET oidc_issuer = ?, oidc_subject = ?
		 WHERE id = ? AND password_hash = '' AND oidc_issuer IS NULL`,
		issuer, subject, id)
	if isUniqueViolation(err) {
		return ErrNotLinkable
	}
	if err != nil {
		return fmt.Errorf("link oidc subject: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("link oidc subject: %w", err)
	}
	if n == 0 {
		return ErrNotLinkable
	}
	return nil
}

// ListUsers returns every user, oldest first.
func (s *Store) ListUsers(ctx context.Context) (_ []User, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY created_at, id")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("list users: %w", cerr)
		}
	}()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// DeleteUser removes user id on behalf of user by. Their sessions and API
// tokens go with them by the tables' foreign key cascades, and their audit
// entries keep the name (SPEC 12.9). One statement also requires by to
// still exist and differ from id, so two users deleting each other at once
// can never leave the table empty (SPEC 12.12). found is false when
// nothing was deleted.
func (s *Store) DeleteUser(ctx context.Context, id, by int64) (found bool, err error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM users WHERE id = ? AND id <> ? AND EXISTS (SELECT 1 FROM users WHERE id = ?)", id, by, by)
	if err != nil {
		return false, fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete user: %w", err)
	}
	return n > 0, nil
}

func (s *Store) queryUser(ctx context.Context, query string, args ...any) (User, bool, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, query, args...))
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

// SetPasswordEndSessions replaces a user's password hash and ends all of
// their sessions in one transaction, so a failure never leaves old
// sessions alive under a new password (SPEC 12.12).
func (s *Store) SetPasswordEndSessions(ctx context.Context, id int64, hash string) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, id)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("set password: no user with id %d", id)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("set password: end sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
