package game

import (
	"context"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

func TestGameFolderOf(t *testing.T) {
	cases := []struct {
		name   string
		mounts []docker.Mount
		want   GameFolder
	}{
		{"a bind mount", []docker.Mount{{Type: "bind", Source: "/srv/factorio", Destination: "/factorio"}},
			GameFolder{Container: "game", HostPath: "/srv/factorio"}},
		{"no type is a path", []docker.Mount{{Source: "/srv/factorio", Destination: "/factorio"}},
			GameFolder{Container: "game", HostPath: "/srv/factorio"}},
		{"a named volume", []docker.Mount{{Type: "volume", Name: "factorio-data", Source: "/var/lib/docker/volumes/factorio-data/_data", Destination: "/factorio"}},
			GameFolder{Container: "game", Volume: "factorio-data"}},
		{"the right mount of several", []docker.Mount{
			{Type: "bind", Source: "/etc/tz", Destination: "/etc/localtime"},
			{Type: "bind", Source: "/srv/factorio", Destination: "/factorio"}},
			GameFolder{Container: "game", HostPath: "/srv/factorio"}},
		{"no mount at /factorio", []docker.Mount{{Source: "/srv", Destination: "/data"}}, GameFolder{Container: "game"}},
		{"no mounts", nil, GameFolder{Container: "game"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gameFolderOf("game", c.mounts); got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestAdoptedStatusAndDefaultsNameTheGameFolder(t *testing.T) {
	r, dir := newAdoptedRig(t, adoptedOptions{})
	want := GameFolder{Container: adoptedName, HostPath: dir}
	eventually(t, func() bool { return r.c.Status("").GameFolder == want }, "the monitor's first look")
	d, err := r.c.AdoptionDefaults(context.Background())
	if err != nil || d.Folder != want {
		t.Errorf("defaults %+v, %v; want folder %+v", d.Folder, err, want)
	}
}
