package game

import "github.com/toddawhittaker/foreman/internal/docker"

// GameFolder says which folder adoption is about, for the pages (SPEC 4.7,
// 4.8): the game container's name and where /factorio lives on the host.
type GameFolder struct {
	// Container is the game container's name.
	Container string
	// HostPath is the host folder mounted at /factorio; empty when the
	// mount is not found or is a Docker volume.
	HostPath string
	// Volume is the name of the Docker volume mounted at /factorio; empty
	// otherwise.
	Volume string
}

// gameFolderOf finds the mount at /factorio among a container's mounts.
func gameFolderOf(name string, mounts []docker.Mount) GameFolder {
	f := GameFolder{Container: name}
	for _, m := range mounts {
		if m.Destination != gameMount {
			continue
		}
		if m.Type == "volume" && m.Name != "" {
			f.Volume = m.Name
		} else {
			f.HostPath = m.Source
		}
	}
	return f
}
