package docker_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

const testImage = "factoriotools/factorio:2.0.55"

func newClient(t *testing.T) (*docker.Client, *dockertest.Daemon) {
	t.Helper()
	d := dockertest.New(t)
	c, err := docker.New(d.Host())
	if err != nil {
		t.Fatal(err)
	}
	return c, d
}

// fullSpec uses every field the controller will need to set (SPEC 4.3 to 4.6).
func fullSpec() docker.ContainerSpec {
	grace := 60
	return docker.ContainerSpec{
		Image:        testImage,
		Labels:       map[string]string{"io.github.toddawhittaker.foreman.world": "w1"},
		Env:          []string{"PUID=845", "PGID=845"},
		Cmd:          []string{"--start-server-load-latest"},
		Entrypoint:   []string{"/opt/factorio/bin/x64/factorio"},
		User:         "845:845",
		OpenStdin:    true,
		StopSignal:   "SIGTERM",
		StopTimeout:  &grace,
		ExposedPorts: map[string]struct{}{"34197/udp": {}},
		HostConfig: docker.HostConfig{
			Binds:         []string{"/srv/foreman/worlds/w1:/factorio"},
			PortBindings:  map[string][]docker.PortBinding{"34197/udp": {{HostPort: "34197"}}},
			CapDrop:       []string{"ALL"},
			SecurityOpt:   []string{"no-new-privileges"},
			RestartPolicy: docker.RestartPolicy{Name: "unless-stopped"},
			LogConfig:     docker.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}},
			NetworkMode:   "game",
		},
	}
}

func TestPingAndVersion(t *testing.T) {
	c, d := newClient(t)
	d.SetAPIVersion("1.47")
	ctx := context.Background()
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := c.Version(ctx)
	if err != nil || v.APIVersion != "1.47" {
		t.Fatalf("Version = %+v, %v", v, err)
	}
}

func TestCreateSendsEveryField(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	id, err := c.CreateContainer(context.Background(), "foreman-w1", fullSpec())
	if err != nil || id == "" {
		t.Fatalf("CreateContainer = %q, %v", id, err)
	}
	got, ok := d.Container("foreman-w1")
	if !ok || got.ID != id {
		t.Fatalf("daemon has %+v", got)
	}
	if !reflect.DeepEqual(got.Spec, fullSpec()) {
		t.Errorf("daemon got spec %+v, want %+v", got.Spec, fullSpec())
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Source != "/srv/foreman/worlds/w1" || got.Mounts[0].Destination != "/factorio" {
		t.Errorf("mounts = %+v", got.Mounts)
	}
}

func TestCreateErrors(t *testing.T) {
	c, d := newClient(t)
	ctx := context.Background()
	_, err := c.CreateContainer(ctx, "foreman-w1", fullSpec())
	if !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("missing image: want ErrNotFound, got %v", err)
	}
	d.AddImage(testImage)
	if _, err := c.CreateContainer(ctx, "foreman-w1", fullSpec()); err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateContainer(ctx, "foreman-w1", fullSpec())
	if !errors.Is(err, docker.ErrConflict) {
		t.Errorf("taken name: want ErrConflict, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("the daemon's reason was lost: %v", err)
	}
}

func TestStartStopKillRemoveLifecycle(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx := context.Background()
	id, _ := c.CreateContainer(ctx, "foreman-w1", fullSpec())

	// Stop and kill on a container that never ran.
	if err := c.StopContainer(ctx, id, 60); !errors.Is(err, docker.ErrNotModified) {
		t.Errorf("stop of a created container: want ErrNotModified, got %v", err)
	}
	if err := c.KillContainer(ctx, id, ""); !errors.Is(err, docker.ErrConflict) {
		t.Errorf("kill of a created container: want ErrConflict, got %v", err)
	}

	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := c.StartContainer(ctx, id); !errors.Is(err, docker.ErrNotModified) {
		t.Errorf("second start: want ErrNotModified, got %v", err)
	}
	if err := c.RemoveContainer(ctx, id, false); !errors.Is(err, docker.ErrConflict) {
		t.Errorf("remove of a running container: want ErrConflict, got %v", err)
	}

	if err := c.StopContainer(ctx, id, 60); err != nil {
		t.Fatal(err)
	}
	got, _ := d.Container(id)
	if got.Status != "exited" || got.ExitCode != 0 {
		t.Errorf("after stop: %s, exit %d", got.Status, got.ExitCode)
	}
	last := d.Calls()[len(d.Calls())-1]
	if last.Path != "/containers/"+id+"/stop" || last.Query.Get("t") != "60" {
		t.Errorf("stop call = %+v", last)
	}

	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := c.KillContainer(ctx, id, "SIGKILL"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.Container(id)
	if got.Status != "exited" || got.ExitCode != 137 {
		t.Errorf("after kill: %s, exit %d", got.Status, got.ExitCode)
	}
	last = d.Calls()[len(d.Calls())-1]
	if last.Query.Get("signal") != "SIGKILL" {
		t.Errorf("kill call = %+v", last)
	}

	if err := c.RemoveContainer(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Container(id); ok {
		t.Error("container still there after remove")
	}
	for name, err := range map[string]error{
		"start":   c.StartContainer(ctx, id),
		"stop":    c.StopContainer(ctx, id, 1),
		"kill":    c.KillContainer(ctx, id, ""),
		"remove":  c.RemoveContainer(ctx, id, true),
		"inspect": func() error { _, err := c.InspectContainer(ctx, id); return err }(),
	} {
		if !errors.Is(err, docker.ErrNotFound) {
			t.Errorf("%s of a missing container: want ErrNotFound, got %v", name, err)
		}
	}
}

func TestForceRemoveOfRunningContainer(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx := context.Background()
	id, _ := c.CreateContainer(ctx, "foreman-w1", fullSpec())
	_ = c.StartContainer(ctx, id)
	if err := c.RemoveContainer(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if len(d.Containers()) != 0 {
		t.Error("container still there")
	}
}

func TestInspectContainer(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx := context.Background()
	id, _ := c.CreateContainer(ctx, "foreman-w1", fullSpec())

	got, err := c.InspectContainer(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Name != "foreman-w1" || got.Image != testImage {
		t.Errorf("identity = %+v", got)
	}
	if got.Labels["io.github.toddawhittaker.foreman.world"] != "w1" {
		t.Errorf("labels = %v", got.Labels)
	}
	if got.State.Status != "created" || got.State.Running {
		t.Errorf("state = %+v", got.State)
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Destination != "/factorio" {
		t.Errorf("mounts = %+v", got.Mounts)
	}

	_ = c.StartContainer(ctx, id)
	got, _ = c.InspectContainer(ctx, id)
	if !got.State.Running || got.State.StartedAt.IsZero() || got.Networks["game"].IPAddress == "" {
		t.Errorf("running state = %+v, networks %v", got.State, got.Networks)
	}

	d.Exit(id, 1, true)
	got, _ = c.InspectContainer(ctx, id)
	if got.State.Running || got.State.ExitCode != 1 || !got.State.OOMKilled || got.State.FinishedAt.IsZero() {
		t.Errorf("exited state = %+v", got.State)
	}

	d.Restart(id)
	got, _ = c.InspectContainer(ctx, id)
	if !got.State.Running || got.RestartCount != 1 || got.State.OOMKilled {
		t.Errorf("restarted: running %v, count %d", got.State.Running, got.RestartCount)
	}
	if got.Privileged {
		t.Error("a container Foreman made reads as privileged")
	}
	d.Seed(dockertest.Container{Name: "privileged", Privileged: true})
	if got, err := c.InspectContainer(ctx, "privileged"); err != nil || !got.Privileged {
		t.Errorf("privileged container read as %+v, %v", got, err)
	}
	d.Seed(dockertest.Container{Name: "caps", CapAdd: []string{"NET_ADMIN"}, PidMode: "host"})
	if got, err := c.InspectContainer(ctx, "caps"); err != nil || len(got.CapAdd) != 1 || got.CapAdd[0] != "NET_ADMIN" || got.PidMode != "host" {
		t.Errorf("container with capabilities read as %+v, %v", got, err)
	}
	d.Seed(dockertest.Container{
		Name: "loose", Devices: []docker.Device{{PathOnHost: "/dev/kvm", PathInContainer: "/dev/kvm"}},
		SecurityOpt: []string{"seccomp=unconfined"}, UsernsMode: "host", IpcMode: "host", UTSMode: "host",
	})
	got, err = c.InspectContainer(ctx, "loose")
	if err != nil || len(got.Devices) != 1 || got.Devices[0].PathOnHost != "/dev/kvm" ||
		len(got.SecurityOpt) != 1 || got.SecurityOpt[0] != "seccomp=unconfined" ||
		got.UsernsMode != "host" || got.IpcMode != "host" || got.UTSMode != "host" {
		t.Errorf("container with loose host settings read as %+v, %v", got, err)
	}
}

func TestInspectRejectsBadIDWithoutSending(t *testing.T) {
	c, d := newClient(t)
	for _, id := range []string{"", "a/b", "..", "a?b", "a%2Fb"} {
		if _, err := c.InspectContainer(context.Background(), id); err == nil {
			t.Errorf("id %q was accepted", id)
		}
	}
	if len(d.Calls()) != 0 {
		t.Errorf("bad ids reached the daemon: %v", d.Calls())
	}
}

func TestListContainersByLabel(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx := context.Background()
	mine := fullSpec()
	other := fullSpec()
	other.Labels = map[string]string{"io.github.toddawhittaker.foreman.world": "w2"}
	plain := docker.ContainerSpec{Image: testImage}
	idMine, _ := c.CreateContainer(ctx, "foreman-w1", mine)
	_, _ = c.CreateContainer(ctx, "foreman-w2", other)
	_, _ = c.CreateContainer(ctx, "stranger", plain)
	_ = c.StartContainer(ctx, idMine)

	const key = "io.github.toddawhittaker.foreman.world"
	cases := []struct {
		name   string
		all    bool
		labels []string
		want   []string
	}{
		{"key only, all", true, []string{key}, []string{"/foreman-w1", "/foreman-w2"}},
		{"key and value", true, []string{key + "=w2"}, []string{"/foreman-w2"}},
		{"running only", false, []string{key}, []string{"/foreman-w1"}},
		{"no filter", true, nil, []string{"/foreman-w1", "/foreman-w2", "/stranger"}},
	}
	for _, tc := range cases {
		got, err := c.ListContainers(ctx, tc.all, tc.labels)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var names []string
		for _, s := range got {
			names = append(names, s.Names[0])
		}
		if !reflect.DeepEqual(names, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, names, tc.want)
		}
	}
}

func TestContainerStats(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	id, _ := c.CreateContainer(context.Background(), "foreman-w1", fullSpec())
	got, err := c.ContainerStats(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got != dockertest.SampleStats {
		t.Errorf("stats = %+v, want %+v", got, dockertest.SampleStats)
	}
	last := d.Calls()[len(d.Calls())-1]
	if last.Query.Get("stream") != "false" {
		t.Errorf("stats call = %+v", last)
	}
}

func TestSeededContainerIsInspectable(t *testing.T) {
	c, d := newClient(t)
	d.Seed(dockertest.Container{
		ID: "self", Name: "foreman", Status: "running",
		Mounts:   []docker.Mount{{Source: "/srv/foreman", Destination: "/data"}},
		Networks: map[string]string{"game": "172.18.0.9"},
	})
	got, err := c.InspectContainer(context.Background(), "self")
	if err != nil {
		t.Fatal(err)
	}
	if got.Networks["game"].IPAddress != "172.18.0.9" || got.Mounts[0].Source != "/srv/foreman" {
		t.Errorf("got %+v", got)
	}
}

func TestDaemonDownAndFailing(t *testing.T) {
	c, d := newClient(t)
	ctx := context.Background()
	d.SetFailure(500, "boom")
	var ae *docker.APIError
	if err := c.Ping(ctx); !errors.As(err, &ae) || ae.Status != 500 || ae.Message != "boom" {
		t.Errorf("want a 500 APIError saying boom, got %v", err)
	}
	d.SetFailure(0, "")
	if err := c.Ping(ctx); err != nil {
		t.Errorf("after healing: %v", err)
	}
	d.Close()
	if err := c.Ping(ctx); err == nil || !strings.Contains(err.Error(), "could not reach the Docker daemon") {
		t.Errorf("closed daemon: %v", err)
	}
}

// SPEC 4.4: Foreman lists what else shares the game network.
func TestListContainersOnNetwork(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx := context.Background()
	games := docker.ContainerSpec{Image: testImage, HostConfig: docker.HostConfig{NetworkMode: "games"}}
	idRun, _ := c.CreateContainer(ctx, "running-on-games", games)
	_ = c.StartContainer(ctx, idRun)
	_, _ = c.CreateContainer(ctx, "created-on-games", games)
	_, _ = c.CreateContainer(ctx, "elsewhere", docker.ContainerSpec{Image: testImage})
	got, err := c.ListContainersOnNetwork(ctx, "games")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Names[0])
	}
	sort.Strings(names)
	if want := []string{"/created-on-games", "/running-on-games"}; !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
	last := d.Calls()[len(d.Calls())-1]
	if last.Query.Get("all") != "true" || !strings.Contains(last.Query.Get("filters"), `"network":["games"]`) {
		t.Errorf("list call = %+v", last.Query)
	}
}
