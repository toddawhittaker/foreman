package docker

import (
	"bufio"
	"os"
	"regexp"
)

// MountinfoPath is where Foreman finds its own container ID.
const MountinfoPath = "/proc/self/mountinfo"

// DockerEnvPath is a file Docker puts in every container it runs. Its
// presence says Foreman is in a container even when SelfID cannot say which.
const DockerEnvPath = "/.dockerenv"

// PodmanEnvPath is the file Podman puts in every container it runs, as
// DockerEnvPath is for Docker.
const PodmanEnvPath = "/run/.containerenv"

var containerIDPattern = regexp.MustCompile(`/containers/([0-9a-f]{64})/`)

// SelfID returns the ID of the container Foreman runs in: the first container
// ID in the mountinfo file at mountinfoPath, else hostname (which Docker sets
// to the short ID unless the admin changed it). It returns "" if neither helps.
func SelfID(mountinfoPath, hostname string) string {
	if id := idFromMountinfo(mountinfoPath); id != "" {
		return id
	}
	return hostname
}

func idFromMountinfo(path string) string {
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
