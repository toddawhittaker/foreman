package dockertest

import (
	"context"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// A container's mounts include its HostConfig.Mounts, not only its Binds,
// so a fake game finds a bind given as fields.
func TestCreateRecordsMountsGivenAsFields(t *testing.T) {
	d := New(t)
	d.AddImage("factoriotools/factorio:2.1.24")
	dc, err := docker.New(d.Host())
	if err != nil {
		t.Fatal(err)
	}
	spec := docker.ContainerSpec{Image: "factoriotools/factorio:2.1.24"}
	spec.HostConfig.Binds = []string{"/host/a:/in/a"}
	spec.HostConfig.Mounts = []docker.BindMount{{Type: "bind", Source: "/host/save.zip", Target: "/out/save.zip", ReadOnly: true}}
	if _, err := dc.CreateContainer(context.Background(), "foreman-mounts", spec); err != nil {
		t.Fatal(err)
	}
	c, ok := d.Container("foreman-mounts")
	if !ok {
		t.Fatal("no container")
	}
	want := []docker.Mount{{Source: "/host/a", Destination: "/in/a"}, {Source: "/host/save.zip", Destination: "/out/save.zip"}}
	if len(c.Mounts) != 2 || c.Mounts[0] != want[0] || c.Mounts[1] != want[1] {
		t.Fatalf("mounts %+v, want %+v", c.Mounts, want)
	}
	if !c.Spec.HostConfig.Mounts[0].ReadOnly {
		t.Fatal("the read-only flag was lost from the spec")
	}
}
