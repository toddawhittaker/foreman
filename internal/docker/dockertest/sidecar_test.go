package dockertest

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

const sidecarImage = "tailscale/tailscale:v1.80.0"

func sidecarClient(t *testing.T) (*docker.Client, *Daemon) {
	t.Helper()
	d := New(t)
	d.AddImage(sidecarImage)
	d.AddImage("factoriotools/factorio:2.0.55")
	c, err := docker.New(d.Host())
	if err != nil {
		t.Fatal(err)
	}
	return c, d
}

// seedSidecar adds a running sidecar on two networks, the game network with
// IPv6 and a shared one, publishing UDP 34197 and holding a credential.
func seedSidecar(d *Daemon, name string) Container {
	c := Container{
		Name: name, Status: "running",
		Networks: map[string]string{"games": "172.30.0.5/24,fd00:30::5/64", "shared": "10.9.0.7/16"},
		Spec: docker.ContainerSpec{
			Image:        sidecarImage,
			Env:          []string{"TS_AUTHKEY=tskey-secret-value"},
			ExposedPorts: map[string]struct{}{"34197/udp": {}, "9/tcp": {}},
			HostConfig: docker.HostConfig{
				NetworkMode:  "games",
				PortBindings: map[string][]docker.PortBinding{"34197/udp": {{HostPort: "34197"}}},
			},
		},
	}
	d.Seed(c)
	got, _ := d.Container(name)
	return got
}

func gameSpec(mode string) docker.ContainerSpec {
	return docker.ContainerSpec{
		Image: "factoriotools/factorio:2.0.55",
		Env:   []string{"FOREMAN_RCON_BIND=172.30.0.5:27015"},
		HostConfig: docker.HostConfig{
			NetworkMode: mode,
		},
	}
}

func apiStatus(t *testing.T, err error) int {
	t.Helper()
	var ae *docker.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want a Docker API error, got %v", err)
	}
	return ae.Status
}

// Inspect shows a multi-network sidecar as Docker does: each network with its
// own address, IPv6 included, and its published and merely exposed ports.
func TestInspectOfMultiNetworkSidecar(t *testing.T) {
	c, d := sidecarClient(t)
	seedSidecar(d, "tailscale")
	got, err := c.InspectContainer(context.Background(), "tailscale")
	if err != nil {
		t.Fatal(err)
	}
	wantNets := map[string]docker.Network{
		"games":  {IPAddress: "172.30.0.5", IPPrefixLen: 24, GlobalIPv6Address: "fd00:30::5", GlobalIPv6PrefixLen: 64},
		"shared": {IPAddress: "10.9.0.7", IPPrefixLen: 16},
	}
	if !reflect.DeepEqual(got.Networks, wantNets) {
		t.Errorf("Networks = %+v, want %+v", got.Networks, wantNets)
	}
	wantPorts := map[string][]docker.PortBinding{
		"34197/udp": {{HostIP: "0.0.0.0", HostPort: "34197"}, {HostIP: "::", HostPort: "34197"}},
		"9/tcp":     nil,
	}
	if !reflect.DeepEqual(got.Ports, wantPorts) {
		t.Errorf("Ports = %+v, want %+v", got.Ports, wantPorts)
	}
	if v, _ := got.EnvValue("TS_AUTHKEY"); v != "tskey-secret-value" || got.NetworkMode != "games" {
		t.Errorf("env or mode lost: mode %q", got.NetworkMode)
	}
}

// A stopped container reports no addresses and no ports, and a restart brings
// back the addresses it was seeded with, not a made-up one.
func TestSeededAddressesSurviveRestart(t *testing.T) {
	c, d := sidecarClient(t)
	seedSidecar(d, "tailscale")
	ctx := context.Background()
	d.Exit("tailscale", 0, false)
	got, _ := c.InspectContainer(ctx, "tailscale")
	if got.Networks["games"].IPAddress != "" || len(got.Ports) != 0 {
		t.Errorf("stopped: networks %+v, ports %v", got.Networks, got.Ports)
	}
	d.Restart("tailscale")
	got, _ = c.InspectContainer(ctx, "tailscale")
	if got.Networks["games"].IPAddress != "172.30.0.5" || got.Networks["games"].GlobalIPv6PrefixLen != 64 || got.Networks["shared"].IPPrefixLen != 16 {
		t.Errorf("restarted networks = %+v", got.Networks)
	}
	if len(got.Ports["34197/udp"]) != 2 {
		t.Errorf("restarted ports = %+v", got.Ports)
	}
}

// The sidecar is found by name, by full ID and by a unique ID prefix, and
// every way reports the same full ID, which is what a game must join.
func TestSidecarResolvesByNameIDAndPrefix(t *testing.T) {
	c, d := sidecarClient(t)
	want := seedSidecar(d, "tailscale")
	seedSidecarWithID(d, "feed-a", "feed01"+strings.Repeat("0", 58))
	seedSidecarWithID(d, "feed-b", "feed02"+strings.Repeat("0", 58))
	ctx := context.Background()
	for _, ref := range []string{"tailscale", want.ID, want.ID[:63]} {
		got, err := c.InspectContainer(ctx, ref)
		if err != nil || got.ID != want.ID {
			t.Errorf("%q: ID %q, error %v; want %q", ref, got.ID, err, want.ID)
		}
	}
	if got, err := c.InspectContainer(ctx, "feed01"); err != nil || got.Name != "feed-a" {
		t.Errorf("unique prefix: %q, %v", got.Name, err)
	}
	// Two IDs start with "feed", so that prefix is ambiguous and Docker finds
	// nothing.
	if _, err := c.InspectContainer(ctx, "feed"); !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("ambiguous prefix: error %v, want not found", err)
	}
	if _, err := c.InspectContainer(ctx, "missing"); !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

// A game in the sidecar's namespace has no networks or ports of its own, so
// the game network's member list does not show it, and it keeps its env.
func TestSharedNamespaceGameHasNoNetworksOfItsOwn(t *testing.T) {
	c, d := sidecarClient(t)
	sc := seedSidecar(d, "tailscale")
	ctx := context.Background()
	id, err := c.CreateContainer(ctx, "game", gameSpec("container:"+sc.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, _ := c.InspectContainer(ctx, id)
	if !got.State.Running || got.NetworkMode != "container:"+sc.ID {
		t.Errorf("state %+v, mode %q", got.State, got.NetworkMode)
	}
	if len(got.Networks) != 0 || len(got.Ports) != 0 {
		t.Errorf("networks %+v, ports %+v; a shared game has neither", got.Networks, got.Ports)
	}
	if v, ok := got.EnvValue("FOREMAN_RCON_BIND"); !ok || v != "172.30.0.5:27015" {
		t.Errorf("RCON bind env = %q, %v", v, ok)
	}
	for _, network := range []string{"games", "shared"} {
		members, err := c.ListContainersOnNetwork(ctx, network)
		if err != nil || len(members) != 1 || members[0].ID != sc.ID {
			t.Errorf("%s members = %+v, %v; want only the sidecar", network, members, err)
		}
	}
	// Stopping and starting again keeps it off the networks.
	_ = c.StopContainer(ctx, id, 1)
	_ = c.StartContainer(ctx, id)
	got, _ = c.InspectContainer(ctx, id)
	if len(got.Networks) != 0 {
		t.Errorf("after restart, networks = %+v", got.Networks)
	}
}

// Docker refuses, at create, port options that cannot apply to a shared
// namespace, and leaves no container behind.
func TestSharedNamespaceRejectsPortOptions(t *testing.T) {
	c, d := sidecarClient(t)
	sc := seedSidecar(d, "tailscale")
	ctx := context.Background()
	publish := gameSpec("container:" + sc.ID)
	publish.HostConfig.PortBindings = map[string][]docker.PortBinding{"34197/udp": {{HostPort: "34197"}}}
	expose := gameSpec("container:" + sc.ID)
	expose.ExposedPorts = map[string]struct{}{"34197/udp": {}}
	empty := gameSpec("container:")
	cases := map[string]struct {
		spec docker.ContainerSpec
		want string
	}{
		"publish": {publish, "port publishing"},
		"expose":  {expose, "port exposing"},
		"no id":   {empty, "container:<name|id>"},
	}
	for name, tc := range cases {
		_, err := c.CreateContainer(ctx, "game-"+strings.ReplaceAll(name, " ", ""), tc.spec)
		if status := apiStatus(t, err); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, status)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not say %q", name, err, tc.want)
		}
	}
	if len(d.Containers()) != 1 {
		t.Errorf("a refused create left containers behind: %d", len(d.Containers()))
	}
	// Ordinary mode keeps its ports.
	if _, err := c.CreateContainer(ctx, "ordinary", fullPortSpec()); err != nil {
		t.Errorf("a game on a named network with ports: %v", err)
	}
}

func fullPortSpec() docker.ContainerSpec {
	s := gameSpec("games")
	s.ExposedPorts = map[string]struct{}{"34197/udp": {}}
	s.HostConfig.PortBindings = map[string][]docker.PortBinding{"34197/udp": {{HostPort: "34197"}}}
	return s
}

// Docker checks the target when the game starts. A target that is missing,
// stopped or restarting fails the start, and the game stays created with the
// reason in its state.
func TestSharedNamespaceStartNeedsARunningSidecar(t *testing.T) {
	c, d := sidecarClient(t)
	ctx := context.Background()
	sc := seedSidecar(d, "tailscale")
	id, err := c.CreateContainer(ctx, "game", gameSpec("container:"+sc.ID))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		setup  func()
		status int
	}{
		{"exited", func() { d.Exit("tailscale", 1, false) }, http.StatusConflict},
		{"restarting", func() { d.WillRestart("tailscale") }, http.StatusConflict},
		{"removed", func() { _ = c.RemoveContainer(ctx, sc.ID, true) }, http.StatusNotFound},
	}
	for _, tc := range cases {
		tc.setup()
		err := c.StartContainer(ctx, id)
		if got := apiStatus(t, err); got != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, got, tc.status)
		}
		after, _ := c.InspectContainer(ctx, id)
		if after.State.Running || after.State.Status != "created" || after.State.Error == "" || after.State.ExitCode != 128 {
			t.Errorf("%s: state after refused start = %+v", tc.name, after.State)
		}
		if strings.Contains(err.Error(), "tskey-secret-value") {
			t.Errorf("%s: error carries the sidecar's environment: %v", tc.name, err)
		}
	}
	// Once a sidecar with that ID runs again, the same game starts.
	seedSidecarWithID(d, "tailscale", sc.ID)
	if err := c.StartContainer(ctx, id); err != nil {
		t.Errorf("start with the sidecar running: %v", err)
	}
}

func seedSidecarWithID(d *Daemon, name, id string) {
	d.Seed(Container{ID: id, Name: name, Status: "running", Networks: map[string]string{"games": "172.30.0.5/24"},
		Spec: docker.ContainerSpec{Image: sidecarImage}})
}

// A game created with the verified ID never follows a replacement that took
// the old name: the start fails, while a game created by name would have
// joined the unchecked container.
func TestSharedNamespaceByIDIgnoresAReplacement(t *testing.T) {
	c, d := sidecarClient(t)
	ctx := context.Background()
	old := seedSidecar(d, "tailscale")
	byID, err := c.CreateContainer(ctx, "game-id", gameSpec("container:"+old.ID))
	if err != nil {
		t.Fatal(err)
	}
	byName, err := c.CreateContainer(ctx, "game-name", gameSpec("container:tailscale"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveContainer(ctx, old.ID, true); err != nil {
		t.Fatal(err)
	}
	replacement := seedSidecar(d, "tailscale")
	if replacement.ID == old.ID {
		t.Fatal("the replacement reused the old ID")
	}
	if err := c.StartContainer(ctx, byID); !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("start by old ID: error %v, want not found", err)
	}
	if err := c.StartContainer(ctx, byName); err != nil {
		t.Errorf("start by name: %v", err)
	}
}
