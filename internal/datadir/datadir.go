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

// Probe checks that dataDir exists, is a directory and is writable, and
// that Foreman shares a user or group ID with the game (SPEC 3.6).
func Probe(dataDir string, gameUID, gameGID int) error {
	return probe(dataDir, os.Getuid(), os.Getgid(), gameUID, gameGID)
}

func probe(dataDir string, uid, gid, gameUID, gameGID int) error {
	info, err := os.Stat(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the data directory %s does not exist. Mount a directory there", dataDir)
	}
	if err != nil {
		return fmt.Errorf("foreman cannot read the data directory %s: %w", dataDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("the data directory %s is a file, not a directory", dataDir)
	}
	f, err := os.CreateTemp(dataDir, ".foreman-probe-*")
	if err != nil {
		return fmt.Errorf("foreman (user %d, group %d) cannot write to the data directory %s. Give that user or group write access", uid, gid, dataDir)
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("foreman cannot remove files in the data directory %s: %w", dataDir, err)
	}
	if uid != gameUID && gid != gameGID {
		return fmt.Errorf("foreman runs as user %d and group %d, but the game runs as %d:%d. "+
			"Run Foreman with the game's user or group ID so the game can use the files Foreman writes",
			uid, gid, gameUID, gameGID)
	}
	return nil
}
