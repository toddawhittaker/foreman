package world

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// RCONPasswordFile is the world's RCON password, relative to the world's
// folder. The game's start wrapper reads it inside the container.
const RCONPasswordFile = ConfigDir + "/rconpw"

// rconPasswordMode lets the game's group read the file and nobody else.
const rconPasswordMode fs.FileMode = 0o640

// maxRCONPassword is the largest password file that is read.
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
	if err := narrowRCONPassword(root); err != nil {
		return secret.Secret{}, fmt.Errorf("narrow the RCON password file to 0640: %w", err)
	}
	return ReadRCONPassword(root)
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
	f, temp, err := createPasswordTemp(root, ConfigDir)
	if err != nil {
		return fmt.Errorf("create the RCON password file: %w", err)
	}
	defer func() { _ = root.Remove(temp) }()
	_, err = f.WriteString(hex.EncodeToString(buf) + "\n")
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
	return SyncDir(root, ConfigDir)
}

// createPasswordTemp is CreateTemp for the password: the file is 0640 from
// the moment it exists, since anyone who opens it while it is wider can
// read what is written later (SPEC 14.8).
func createPasswordTemp(root *os.Root, dir string) (*os.File, string, error) {
	name := path.Join(dir, PrefixPartial+randomSuffix())
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, rconPasswordMode)
	if err != nil {
		return nil, "", err
	}
	// The umask may have narrowed it; the game's group still needs to read.
	if err := f.Chmod(rconPasswordMode); err != nil {
		_ = f.Close()
		_ = root.Remove(name)
		return nil, "", err
	}
	return f, name, nil
}

// copyRCONPassword copies the password file from one world folder to
// another, at 0640 throughout, keeping its modification time.
func copyRCONPassword(src, dst *os.Root) error {
	in, info, err := openRegular(src, RCONPasswordFile)
	if err != nil {
		return fmt.Errorf("copy %s: %w", RCONPasswordFile, err)
	}
	defer func() { _ = in.Close() }()
	out, temp, err := createPasswordTemp(dst, ConfigDir)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = dst.Chtimes(temp, info.ModTime(), info.ModTime())
	}
	if err == nil {
		err = Commit(dst, temp, RCONPasswordFile)
	}
	if err != nil {
		_ = dst.Remove(temp)
	}
	return err
}

// narrowRCONPassword takes away any permission beyond 0640 from an existing
// password file, such as one restored before restores narrowed it.
func narrowRCONPassword(root *os.Root) error {
	info, err := root.Lstat(RCONPasswordFile)
	if err != nil || !info.Mode().IsRegular() {
		return nil // ReadRCONPassword reports it
	}
	perm := info.Mode().Perm()
	if perm&^rconPasswordMode == 0 {
		return nil
	}
	return root.Chmod(RCONPasswordFile, perm&rconPasswordMode)
}

// ReadRCONPassword reads the password file in a world or game folder.
// Anything but a small regular file, a link included, is an error
// (SPEC 14.8).
func ReadRCONPassword(root *os.Root) (secret.Secret, error) {
	b, err := ReadSmallFile(root, RCONPasswordFile, maxRCONPassword)
	if err != nil {
		return secret.Secret{}, err
	}
	pw := strings.TrimSpace(string(b))
	if pw == "" {
		return secret.Secret{}, errRCONPasswordEmpty
	}
	return secret.New(pw), nil
}
