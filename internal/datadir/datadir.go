// Package datadir knows the data directory's layout (SPEC 5.2), finds its
// path on the host (SPEC 3.5) and checks it is usable at startup (SPEC 3.6).
package datadir

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// MountinfoPath is where Foreman finds its own container ID.
const MountinfoPath = docker.MountinfoPath

// Database returns the path of the app database inside dataDir.
func Database(dataDir string) string { return filepath.Join(dataDir, "foreman.db") }

// Worlds returns the folder that holds managed mode's worlds (SPEC 5.2).
func Worlds(dataDir string) string { return filepath.Join(dataDir, "worlds") }

// Snapshots returns the folder that holds every world's snapshots (SPEC 5.2).
func Snapshots(dataDir string) string { return filepath.Join(dataDir, "snapshots") }

// Imports returns the inbox that worlds are imported from.
func Imports(dataDir string) string { return filepath.Join(dataDir, "import") }

// Inspector reads a container's mounts. *docker.Client satisfies it.
type Inspector interface {
	InspectContainer(ctx context.Context, id string) (docker.Container, error)
}

// HostDir returns the host path of dataDir for managed mode. It uses
// configured when set; otherwise it finds Foreman's own container (from
// mountinfoPath, then hostname) and reads the source of the mount at dataDir.
func HostDir(ctx context.Context, dataDir, configured string, d Inspector, mountinfoPath, hostname string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	id := docker.SelfID(mountinfoPath, hostname)
	if id == "" {
		return "", errHostDir("foreman could not tell which container it is running in")
	}
	c, err := d.InspectContainer(ctx, id)
	if err != nil {
		return "", errHostDir(fmt.Sprintf("foreman could not inspect its own container (%v)", err))
	}
	want := filepath.Clean(dataDir)
	for _, m := range c.Mounts {
		if filepath.Clean(m.Destination) == want && m.Source != "" {
			return m.Source, nil
		}
	}
	return "", errHostDir(fmt.Sprintf("Foreman's container has no mount at %s", want))
}

func errHostDir(why string) error {
	return fmt.Errorf("%s, so it cannot find the host path of its data directory. Set FOREMAN_HOST_DATA_DIR to that host path", why)
}

// Probe checks that dir exists, is a directory and is writable, and that
// Foreman shares a user or group ID with the game (SPEC 3.6). what names
// the folder in the messages, such as "the data directory".
func Probe(what, dir string, gameUID, gameGID int) error {
	return probe(what, dir, os.Getuid(), os.Getgid(), gameUID, gameGID)
}

func probe(what, dir string, uid, gid, gameUID, gameGID int) error {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s %s does not exist. Mount a directory there", what, dir)
	}
	if err != nil {
		return fmt.Errorf("foreman cannot read %s %s: %w", what, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s %s is a file, not a directory", what, dir)
	}
	f, err := os.CreateTemp(dir, ".foreman-probe-*")
	if err != nil {
		return fmt.Errorf("foreman (user %d, group %d) cannot write to %s %s. Give that user or group write access", uid, gid, what, dir)
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("foreman cannot remove files in %s %s: %w", what, dir, err)
	}
	if uid != gameUID && gid != gameGID {
		return fmt.Errorf("foreman runs as user %d and group %d, but the game runs as %d:%d. "+
			"Run Foreman with the game's user or group ID so the game can use the files Foreman writes",
			uid, gid, gameUID, gameGID)
	}
	return nil
}
