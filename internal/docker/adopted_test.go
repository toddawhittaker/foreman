package docker_test

import (
	"context"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

func TestInspectReadsWhatAdoptedModeNeeds(t *testing.T) {
	c, d := newClient(t)
	d.Seed(dockertest.Container{
		ID: "game", Name: "factorio", Status: "running",
		Spec: docker.ContainerSpec{
			Image: testImage,
			Env:   []string{"PUID=845", "RCON_PORT=27016", "PUID=846", "NOVALUE"},
			User:  "845:845",
			HostConfig: docker.HostConfig{
				PortBindings: map[string][]docker.PortBinding{
					"34197/udp": {{HostPort: "34197"}},
					"27015/tcp": {{HostIP: "127.0.0.1", HostPort: "27015"}},
					"9999/tcp":  {},
				},
				NetworkMode: "game",
			},
		},
		Mounts:   []docker.Mount{{Source: "/srv/foreman/game", Destination: "/factorio"}},
		Networks: map[string]string{"game": "172.18.0.9/24"},
	})
	got, err := c.InspectContainer(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "845:845" || got.NetworkMode != "game" {
		t.Errorf("user %q, network mode %q", got.User, got.NetworkMode)
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Source != "/srv/foreman/game" || got.Mounts[0].Destination != "/factorio" {
		t.Errorf("mounts = %+v", got.Mounts)
	}
	if n := got.Networks["game"]; n.IPAddress != "172.18.0.9" || n.IPPrefixLen != 24 {
		t.Errorf("network = %+v", n)
	}
	if v, ok := got.EnvValue("PUID"); !ok || v != "846" {
		t.Errorf("PUID = %q, %v; want the last one, 846", v, ok)
	}
	if v, ok := got.EnvValue("RCON_PORT"); !ok || v != "27016" {
		t.Errorf("RCON_PORT = %q, %v", v, ok)
	}
	for _, name := range []string{"PGID", "NOVALUE", "PUI", ""} {
		if v, ok := got.EnvValue(name); ok {
			t.Errorf("EnvValue(%q) = %q, true; want not set", name, v)
		}
	}
	if !got.PublishesTCP(27015) {
		t.Error("27015/tcp is published, PublishesTCP said no")
	}
	if got.PublishesTCP(34197) || got.PublishesTCP(9999) || got.PublishesTCP(1) {
		t.Error("PublishesTCP counted a UDP port, an empty binding, or an unknown port")
	}
}

func TestEnvValueKeepsAnEqualsSignInTheValue(t *testing.T) {
	c := docker.Container{Env: []string{"TOKEN=a=b", "EMPTY="}}
	if v, ok := c.EnvValue("TOKEN"); !ok || v != "a=b" {
		t.Errorf("TOKEN = %q, %v", v, ok)
	}
	if v, ok := c.EnvValue("EMPTY"); !ok || v != "" {
		t.Errorf("EMPTY = %q, %v; want set and empty", v, ok)
	}
}

func TestInspectOfAContainerWithNothingSet(t *testing.T) {
	c, d := newClient(t)
	d.Seed(dockertest.Container{ID: "bare", Name: "bare", Status: "exited"})
	got, err := c.InspectContainer(context.Background(), "bare")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Env) != 0 || got.User != "" || got.PublishesTCP(27015) || got.NetworkMode != "default" {
		t.Errorf("got %+v", got)
	}
}

func TestAdoptedClientKeepsTheCallsAdoptedModeUses(t *testing.T) {
	d := dockertest.New(t)
	d.ForbidManagedCalls()
	d.Seed(dockertest.Container{ID: "game", Name: "factorio", Status: "exited"})
	c, err := docker.NewAdopted(d.Host())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	steps := map[string]func() error{
		"ping":        func() error { return c.Ping(ctx) },
		"inspect":     func() error { _, err := c.InspectContainer(ctx, "game"); return err },
		"list":        func() error { _, err := c.ListContainers(ctx, true, nil); return err },
		"start":       func() error { return c.StartContainer(ctx, "game") },
		"stats":       func() error { _, err := c.ContainerStats(ctx, "game"); return err },
		"kill":        func() error { return c.KillContainer(ctx, "game", "") },
		"start again": func() error { return c.StartContainer(ctx, "game") },
		"stop":        func() error { return c.StopContainer(ctx, "game", 10) },
	}
	for _, name := range []string{"ping", "inspect", "list", "start", "stats", "kill", "start again", "stop"} {
		if err := steps[name](); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestAdoptedClientRefusesWithoutContactingTheDaemon(t *testing.T) {
	d := dockertest.New(t)
	d.AddImage(testImage)
	d.Seed(dockertest.Container{ID: "game", Name: "factorio", Status: "running"})
	c, err := docker.NewAdopted(d.Host())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	refused := map[string]error{}
	_, refused["create"] = c.CreateContainer(ctx, "x", fullSpec())
	refused["remove"] = c.RemoveContainer(ctx, "game", true)
	refused["pull"] = c.PullImage(ctx, testImage)
	_, refused["image inspect"] = c.InspectImage(ctx, testImage)
	for name, err := range refused {
		if err == nil || !strings.Contains(err.Error(), "adopted mode") {
			t.Errorf("%s: err = %v, want a refusal that names adopted mode", name, err)
		}
	}
	if calls := d.Calls(); len(calls) != 0 {
		t.Errorf("refused calls reached the daemon: %+v", calls)
	}
	if _, ok := d.Container("factorio"); !ok {
		t.Error("the container was removed")
	}
}

func TestManagedClientStillCreatesAndRemoves(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx := context.Background()
	id, err := c.CreateContainer(ctx, "x", fullSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveContainer(ctx, id, false); err != nil {
		t.Fatal(err)
	}
}

func TestNewAdoptedRejectsBadHosts(t *testing.T) {
	if _, err := docker.NewAdopted("ftp://x"); err == nil {
		t.Error("want an error for a bad scheme")
	}
}

func TestAdoptedAllowlist(t *testing.T) {
	kept := []struct{ method, path string }{
		{"GET", "/_ping"}, {"GET", "/version"}, {"GET", "/containers/json"},
		{"GET", "/containers/abc/json"}, {"POST", "/containers/abc/start"},
		{"POST", "/containers/abc/stop"}, {"POST", "/containers/abc/kill"},
		{"GET", "/containers/abc/logs"}, {"GET", "/containers/abc/stats"},
		{"GET", "/events"},
	}
	for _, tc := range kept {
		if !docker.AllowedAdopted(tc.method, tc.path) {
			t.Errorf("%s %s should stay allowed in adopted mode", tc.method, tc.path)
		}
	}
	dropped := []struct{ method, path string }{
		{"POST", "/containers/create"}, {"DELETE", "/containers/abc"},
		{"POST", "/images/create"}, {"GET", "/images/factoriotools/factorio:2.0.55/json"},
		// Never on the list in either mode.
		{"POST", "/containers/abc/exec"}, {"POST", "/containers/abc/restart"},
		{"POST", "/networks/create"}, {"POST", "/networks/x/connect"},
	}
	for _, tc := range dropped {
		if docker.AllowedAdopted(tc.method, tc.path) {
			t.Errorf("%s %s should be refused in adopted mode", tc.method, tc.path)
		}
	}
}
