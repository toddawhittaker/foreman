package world

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// RCONPasswordFile is the world's RCON password, relative to the world's
// folder. The game's start wrapper reads it inside the container.
const RCONPasswordFile = ConfigDir + "/rconpw"

// rconPasswordMode lets the game's group read the file and nobody else.
const rconPasswordMode fs.FileMode = 0o640

// maxRCONPassword caps how much of the file is read.
const maxRCONPassword = 1024

// errRCONPasswordEmpty is returned when the file holds no password.
var errRCONPasswordEmpty = errors.New("the world's RCON password file is empty")

// RCONPassword returns the world's RCON password, creating the file with a
// random password when it is missing (SPEC 14.8). The value leaves this
// package only as a secret.Secret.
func (m *Manager) RCONPassword(id string) (secret.Secret, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return secret.Secret{}, err
	}
	defer func() { _ = root.Close() }()
	if err := createRCONPassword(root); err != nil {
		return secret.Secret{}, err
	}
	return readRCONPassword(root)
}

// createRCONPassword writes a new password file unless one exists.
func createRCONPassword(root *os.Root) error {
	if err := Mkdir(root, ConfigDir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	// Write a finished temp file, then link it into place: the password
	// file only ever appears complete, and a file made meanwhile by another
	// process is never replaced (link fails if the target exists).
	f, temp, err := CreateTemp(root, ConfigDir)
	if err != nil {
		return fmt.Errorf("create the RCON password file: %w", err)
	}
	defer func() { _ = root.Remove(temp) }()
	_, err = f.WriteString(hex.EncodeToString(buf) + "\n")
	if err == nil {
		err = f.Chmod(rconPasswordMode)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Link(temp, RCONPasswordFile)
	}
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create the RCON password file: %w", err)
	}
	return syncDir(root, ConfigDir)
}

// readRCONPassword reads the password file, refusing anything but a
// regular file.
func readRCONPassword(root *os.Root) (secret.Secret, error) {
	info, err := root.Lstat(RCONPasswordFile)
	if err != nil {
		return secret.Secret{}, err
	}
	if !info.Mode().IsRegular() {
		return secret.Secret{}, fmt.Errorf("%s is not a regular file", RCONPasswordFile)
	}
	f, err := root.Open(RCONPasswordFile)
	if err != nil {
		return secret.Secret{}, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxRCONPassword))
	if err != nil {
		return secret.Secret{}, err
	}
	pw := strings.TrimSpace(string(b))
	if pw == "" {
		return secret.Secret{}, errRCONPasswordEmpty
	}
	return secret.New(pw), nil
}
