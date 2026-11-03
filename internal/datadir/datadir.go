// Package datadir knows the data directory's layout (SPEC 5.2), finds its
// path on the host (SPEC 3.5) and checks it is usable at startup (SPEC 3.6).
package datadir

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/toddawhittaker/foreman/internal/docker"
)

const (
	// MountinfoPath is where Foreman finds its own container ID.
	MountinfoPath = "/proc/self/mountinfo"
)

// Database returns the path of the app database inside dataDir.
func Database(dataDir string) string { return filepath.Join(dataDir, "foreman.db") }

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
	id := containerIDFromMountinfo(mountinfoPath)
	if id == "" {
		id = hostname
	}
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

var containerIDPattern = regexp.MustCompile(`/containers/([0-9a-f]{64})/`)

// containerIDFromMountinfo returns the first container ID found in a
// mountinfo file, or "" if the file is missing or holds none.
func containerIDFromMountinfo(path string) string {
	f, err := os.Open(path) // #nosec G304 -- a fixed system path, injectable only for tests
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		if m := containerIDPattern.FindStringSubmatch(s.Text()); m != nil {
			return m[1]
		}
	}
	return ""
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
