package world

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRCONPasswordCreatedOnceAndKept(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	p := filepath.Join(dir, RCONPasswordFile)

	first, err := m.RCONPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Reveal()) < 32 {
		t.Fatalf("password too short: %d characters", len(first.Reveal()))
	}
	if got := mode(t, p); got != 0o640 {
		t.Fatalf("mode %v, want 0640", got)
	}
	second, err := m.RCONPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	if second.Reveal() != first.Reveal() {
		t.Fatal("an existing password was replaced")
	}
	if s := fmt.Sprintf("%v %s %+v", first, first, first); s != "[redacted] [redacted] [redacted]" {
		t.Fatalf("the password printed as %q", s)
	}

	// Another world gets its own password.
	other, err := m.Create(t.Context(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	third, err := m.RCONPassword(other)
	if err != nil {
		t.Fatal(err)
	}
	if third.Reveal() == first.Reveal() {
		t.Fatal("two worlds share a password")
	}
}

func TestRCONPasswordReadsExistingFile(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	writeFile(t, filepath.Join(dir, RCONPasswordFile), "  admin-chosen \n")
	pw, err := m.RCONPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	if pw.Reveal() != "admin-chosen" {
		t.Fatal("the existing password was not read as written")
	}
}

func TestRCONPasswordRefusesSymlinkAndEmpty(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	p := filepath.Join(dir, RCONPasswordFile)
	if err := os.Symlink("../world.json", p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RCONPassword(id); err == nil {
		t.Fatal("a symlinked password file was read")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, "\n")
	if _, err := m.RCONPassword(id); err == nil {
		t.Fatal("an empty password was accepted")
	}
}

func TestRCONPasswordMakesMissingConfigFolder(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	if err := os.Remove(filepath.Join(dir, ConfigDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RCONPassword(id); err != nil {
		t.Fatal(err)
	}
}
