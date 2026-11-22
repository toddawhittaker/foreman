package world

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The players record is a plain file in the world folder: it reads back what
// was written, a missing one is fs.ErrNotExist, and removing one is no error
// when there is none (SPEC 5.1, 8.14).
func TestPlayersRecordFile(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if _, err := m.ReadPlayersRecord(id); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read of none: %v", err)
	}
	if err := m.RemovePlayersRecord(id); err != nil {
		t.Fatalf("remove of none: %v", err)
	}
	want := []byte(`{"format_version":1}`)
	if err := m.WritePlayersRecord(id, want); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ReadPlayersRecord(id); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("read %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, PlayersFile)); err != nil {
		t.Fatal(err)
	}
	if err := m.RemovePlayersRecord(id); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadPlayersRecord(id); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read after remove: %v", err)
	}
}

// A link in the record's place is not followed, and a file over the limit is
// not read: the game container can write the world folder (SPEC 14.6).
func TestPlayersRecordIsUntrusted(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if err := os.Symlink("config/rconpw", filepath.Join(dir, PlayersFile)); err != nil {
		t.Fatal(err)
	}
	if b, err := m.ReadPlayersRecord(id); err == nil {
		t.Fatalf("followed a link: %q", b)
	}
	if err := os.Remove(filepath.Join(dir, PlayersFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PlayersFile), make([]byte, maxPlayersRecord+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadPlayersRecord(id); err == nil {
		t.Fatal("read a file over the limit")
	}
}
