package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "foreman.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func pragma(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v string
	if err := db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

func TestOpenCreatesFileWithMode(t *testing.T) {
	_, path := openTemp(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o660 {
		t.Errorf("file mode = %o, want 660", got)
	}
}

func TestOpenKeepsExistingFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreman.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	info, _ := os.Stat(path)
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("existing file mode changed to %o", got)
	}
}

func TestOpenFailsWhenDirectoryMissing(t *testing.T) {
	_, err := Open(context.Background(), filepath.Join(t.TempDir(), "nope", "foreman.db"))
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestOpenPathWithSpecialCharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my data?x#y")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "foreman.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database is not at the requested path: %v", err)
	}
}

// Every pooled connection must carry the pragmas, not only the first.
func TestPragmasOnEveryConnection(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	var conns []*sql.Conn
	for range 3 {
		c, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	want := map[string]string{
		"journal_mode": "wal",
		"foreign_keys": "1",
		"busy_timeout": "5000",
		"synchronous":  "1", // NORMAL
	}
	for i, c := range conns {
		for name, w := range want {
			var got string
			if err := c.QueryRowContext(ctx, "PRAGMA "+name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != w {
				t.Errorf("connection %d: %s = %q, want %q", i, name, got, w)
			}
		}
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

// latestVersion is the number of the newest embedded migration, as the
// string PRAGMA user_version returns.
func latestVersion(t *testing.T) string {
	t.Helper()
	ms, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(len(ms))
}

func TestMigrateIsIdempotent(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateFirstAdmin(ctx, "ann", "h", t0); err != nil {
		t.Fatal(err)
	}
	if v := pragma(t, s.db, "user_version"); v != latestVersion(t) {
		t.Fatalf("user_version = %s, want %s", v, latestVersion(t))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = s2.Close() }()
	if v := pragma(t, s2.db, "user_version"); v != latestVersion(t) {
		t.Errorf("user_version after reopen = %s, want %s", v, latestVersion(t))
	}
	if _, found, err := s2.UserByUsername(ctx, "ann"); err != nil || !found {
		t.Errorf("data lost on reopen: found=%v err=%v", found, err)
	}
}

func TestOpenRefusesNewerDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreman.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	_, err = Open(context.Background(), path)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "99") || !strings.Contains(err.Error(), "newer Foreman") {
		t.Errorf("error should say what is wrong and what to do: %v", err)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	// The real schema is already at the latest version, so only the bad one runs.
	real, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	bad := migration{version: len(real) + 1, name: "9999_bad.sql", sql: "CREATE TABLE half (id INTEGER); THIS IS NOT SQL;"}
	err = applyMigrations(ctx, s.db, append(real, bad))
	if err == nil || !strings.Contains(err.Error(), "9999_bad.sql") {
		t.Fatalf("err = %v, want a failure naming 9999_bad.sql", err)
	}
	if v := pragma(t, s.db, "user_version"); v != latestVersion(t) {
		t.Errorf("user_version = %s after failure, want %s", v, latestVersion(t))
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'half'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("table from the failed migration survived")
	}
	// The connection must be usable afterwards.
	if _, err := s.HasUsers(ctx); err != nil {
		t.Errorf("store unusable after failed migration: %v", err)
	}
}

func TestLoadMigrations(t *testing.T) {
	real, err := loadMigrations(migrationFiles)
	if err != nil || len(real) == 0 {
		t.Fatalf("embedded migrations: %v (%d)", err, len(real))
	}

	file := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	tests := []struct {
		name  string
		files fstest.MapFS
		bad   string
	}{
		{"gap", fstest.MapFS{"migrations/0001_a.sql": file("x"), "migrations/0003_c.sql": file("x")}, "without gaps"},
		{"not starting at one", fstest.MapFS{"migrations/0002_b.sql": file("x")}, "without gaps"},
		{"no number", fstest.MapFS{"migrations/init.sql": file("x")}, "NNNN_name.sql"},
		{"no name", fstest.MapFS{"migrations/0001.sql": file("x")}, "NNNN_name.sql"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadMigrations(tt.files)
			if err == nil || !strings.Contains(err.Error(), tt.bad) {
				t.Errorf("err = %v, want one containing %q", err, tt.bad)
			}
		})
	}

	ordered, err := loadMigrations(fstest.MapFS{
		"migrations/0002_b.sql": file("b"),
		"migrations/0001_a.sql": file("a"),
	})
	if err != nil || ordered[0].sql != "a" || ordered[1].sql != "b" {
		t.Errorf("not applied in order: %v %v", ordered, err)
	}
}

func TestHasUsers(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	has, err := s.HasUsers(ctx)
	if err != nil || has {
		t.Fatalf("empty: has=%v err=%v", has, err)
	}
	if _, err := s.CreateUser(ctx, "ann", "h", t0); err != nil {
		t.Fatal(err)
	}
	has, err = s.HasUsers(ctx)
	if err != nil || !has {
		t.Fatalf("after create: has=%v err=%v", has, err)
	}
}

func TestCreateFirstAdmin(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, err := s.CreateFirstAdmin(ctx, "Ann", "hash1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.Username != "Ann" || u.PasswordHash != "hash1" || u.Role != "admin" || !u.CreatedAt.Equal(t0) {
		t.Errorf("unexpected user %+v", u)
	}
	if u.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt is not UTC: %v", u.CreatedAt.Location())
	}
	if _, err := s.CreateFirstAdmin(ctx, "bob", "h", t0); !errors.Is(err, ErrAdminExists) {
		t.Errorf("second call err = %v, want ErrAdminExists", err)
	}
}

// A user created some other way (a header sign-in) also closes setup.
func TestCreateFirstAdminRefusedAfterCreateUser(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "viaheader", "", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFirstAdmin(ctx, "ann", "h", t0); !errors.Is(err, ErrAdminExists) {
		t.Errorf("err = %v, want ErrAdminExists", err)
	}
}

func TestCreateFirstAdminRace(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	const n = 20
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = s.CreateFirstAdmin(ctx, fmt.Sprintf("admin%d", i), "h", t0)
		}()
	}
	close(start)
	wg.Wait()

	ok := 0
	for i, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrAdminExists):
			t.Errorf("goroutine %d: unexpected error %v", i, err)
		}
	}
	if ok != 1 {
		t.Errorf("%d calls succeeded, want exactly 1", ok)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 1 {
		t.Errorf("users = %d (err %v), want 1", count, err)
	}
}

func TestCreateUserUsernameTaken(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "Ann", "h", t0); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Ann", "ann", "ANN"} {
		if _, err := s.CreateUser(ctx, name, "h", t0); !errors.Is(err, ErrUsernameTaken) {
			t.Errorf("CreateUser(%q) err = %v, want ErrUsernameTaken", name, err)
		}
	}
	if _, err := s.CreateUser(ctx, "bob", "h", t0); err != nil {
		t.Errorf("distinct name refused: %v", err)
	}
}

func TestUserLookups(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	made, err := s.CreateUser(ctx, "Ann", "h", t0)
	if err != nil {
		t.Fatal(err)
	}

	byName, found, err := s.UserByUsername(ctx, "aNN")
	if err != nil || !found || byName != made {
		t.Errorf("UserByUsername = %+v %v %v, want %+v", byName, found, err, made)
	}
	byID, found, err := s.UserByID(ctx, made.ID)
	if err != nil || !found || byID != made {
		t.Errorf("UserByID = %+v %v %v", byID, found, err)
	}
	if _, found, err := s.UserByUsername(ctx, "nobody"); found || err != nil {
		t.Errorf("missing name: found=%v err=%v", found, err)
	}
	if _, found, err := s.UserByID(ctx, made.ID+100); found || err != nil {
		t.Errorf("missing id: found=%v err=%v", found, err)
	}
}

func TestSetPasswordHash(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "ann", "old", t0)
	if err := s.SetPasswordHash(ctx, u.ID, "new"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.UserByID(ctx, u.ID)
	if got.PasswordHash != "new" {
		t.Errorf("hash = %q, want new", got.PasswordHash)
	}
	if err := s.SetPasswordHash(ctx, u.ID+100, "x"); err == nil {
		t.Error("expected an error for a missing user")
	}
}

func TestRoleIsChecked(t *testing.T) {
	s, _ := openTemp(t)
	_, err := s.db.Exec("INSERT INTO users (username, role, created_at) VALUES ('x', 'viewer', 0)")
	if err == nil {
		t.Error("a role outside the allowed list was accepted")
	}
}

func TestSessions(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "ann", "h", t0)

	if _, _, found, err := s.SessionUser(ctx, "aa"); found || err != nil {
		t.Fatalf("missing session: found=%v err=%v", found, err)
	}
	if err := s.CreateSession(ctx, "aa", u.ID, t0); err != nil {
		t.Fatal(err)
	}
	id, seen, found, err := s.SessionUser(ctx, "aa")
	if err != nil || !found || id != u.ID || !seen.Equal(t0) || seen.Location() != time.UTC {
		t.Fatalf("SessionUser = %v %v %v %v", id, seen, found, err)
	}

	later := t0.Add(time.Hour)
	if err := s.TouchSession(ctx, "aa", later); err != nil {
		t.Fatal(err)
	}
	_, seen, _, _ = s.SessionUser(ctx, "aa")
	if !seen.Equal(later) {
		t.Errorf("last seen = %v, want %v", seen, later)
	}

	if err := s.DeleteSession(ctx, "aa"); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := s.SessionUser(ctx, "aa"); found {
		t.Error("session survived DeleteSession")
	}
	if err := s.DeleteSession(ctx, "aa"); err != nil {
		t.Errorf("deleting a missing session: %v", err)
	}
}

func TestCreateSessionNeedsRealUserAndUniqueToken(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "aa", 42, t0); err == nil {
		t.Error("session for a missing user was accepted; foreign keys are off")
	}
	u, _ := s.CreateUser(ctx, "ann", "h", t0)
	if err := s.CreateSession(ctx, "bb", u.ID, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "bb", u.ID, t0); err == nil {
		t.Error("duplicate token hash accepted")
	}
}

func TestDeleteSessionsIdleSince(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "ann", "h", t0)
	for name, at := range map[string]time.Time{
		"old":   t0,
		"edge":  t0.Add(time.Hour),
		"fresh": t0.Add(2 * time.Hour),
	} {
		if err := s.CreateSession(ctx, name, u.ID, at); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DeleteSessionsIdleSince(ctx, t0.Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("removed %d (err %v), want 1", n, err)
	}
	if _, _, found, _ := s.SessionUser(ctx, "old"); found {
		t.Error("old session survived")
	}
	for _, name := range []string{"edge", "fresh"} {
		if _, _, found, _ := s.SessionUser(ctx, name); !found {
			t.Errorf("session %q was removed", name)
		}
	}
}

func TestDeletingUserEndsTheirSessions(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "ann", "h", t0)
	_ = s.CreateSession(ctx, "aa", u.ID, t0)
	if _, err := s.db.Exec("DELETE FROM users WHERE id = ?", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := s.SessionUser(ctx, "aa"); found {
		t.Error("session outlived its user")
	}
}

func TestClosedStoreReturnsErrors(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	_ = s.Close()
	if _, err := s.HasUsers(ctx); err == nil {
		t.Error("HasUsers: expected an error")
	}
	if _, err := s.CreateFirstAdmin(ctx, "a", "h", t0); err == nil || errors.Is(err, ErrAdminExists) {
		t.Errorf("CreateFirstAdmin err = %v", err)
	}
	if _, err := s.CreateUser(ctx, "a", "h", t0); err == nil || errors.Is(err, ErrUsernameTaken) {
		t.Errorf("CreateUser err = %v", err)
	}
	if _, _, err := s.UserByID(ctx, 1); err == nil {
		t.Error("UserByID: expected an error")
	}
	if err := s.SetPasswordHash(ctx, 1, "x"); err == nil {
		t.Error("SetPasswordHash: expected an error")
	}
	if err := s.CreateSession(ctx, "a", 1, t0); err == nil {
		t.Error("CreateSession: expected an error")
	}
	if _, _, _, err := s.SessionUser(ctx, "a"); err == nil {
		t.Error("SessionUser: expected an error")
	}
	if err := s.TouchSession(ctx, "a", t0); err == nil {
		t.Error("TouchSession: expected an error")
	}
	if err := s.DeleteSession(ctx, "a"); err == nil {
		t.Error("DeleteSession: expected an error")
	}
	if _, err := s.DeleteSessionsIdleSince(ctx, t0); err == nil {
		t.Error("DeleteSessionsIdleSince: expected an error")
	}
}
