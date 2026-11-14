package game

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// sidecarSecret is in the sidecar's environment; no error or status may
// show it (SPEC 14.16).
const sidecarSecret = "tskey-auth-never-shown" //nolint:gosec // a test value to look for

// sharedNet is an additional network the sidecar is on, with an unrelated
// service on it too: allowed (SPEC 4.13).
const sharedNet = "10.9.0.7/16"

// sidecarOpts shapes a seeded sidecar.
type sidecarOpts struct {
	ip       string            // its IPv4 address on the game network
	networks map[string]string // replaces its networks when set
	status   string            // replaces "running" when set
	mode     string            // replaces its network mode when set
}

// seedSidecar adds a sidecar named name on the game network, with IPv6, and
// on a shared network, as Docker shows a Tailscale container.
func seedSidecar(d *dockertest.Daemon, name string, o sidecarOpts) dockertest.Container {
	nets := o.networks
	if nets == nil {
		nets = map[string]string{"games": o.ip + "/24,fd00:30::5/64", "shared": sharedNet}
	}
	status, mode := "running", "games"
	if o.status != "" {
		status = o.status
	}
	if o.mode != "" {
		mode = o.mode
	}
	spec := docker.ContainerSpec{
		Image: "tailscale/tailscale:v1.80.0",
		Env:   []string{"TS_AUTHKEY=" + sidecarSecret, "TS_USERSPACE=false"},
		HostConfig: docker.HostConfig{
			NetworkMode: mode,
		},
	}
	spec.ExposedPorts = map[string]struct{}{gamePort: {}}
	spec.HostConfig.PortBindings = map[string][]docker.PortBinding{gamePort: {{HostPort: "34197"}}}
	d.Seed(dockertest.Container{Name: name, Status: status, Networks: nets, Spec: spec})
	got, _ := d.Container(name)
	return got
}

// liveSidecarRig is a rig whose fake seeds the sidecar "tailscale" itself
// (fakegame.Config.Sidecar), on the game network at an address of its
// own. A game that joins that namespace binds RCON there and nowhere else,
// so the tests that run a game use this rig; a game in any other
// container's namespace cannot bind the address and exits. published makes
// the sidecar publish UDP 34197 on a host port. inContainer makes the
// fake's self container Foreman's own. It returns the sidecar's address on
// the game network.
func liveSidecarRig(t *testing.T, inContainer, published bool) (*rig, rigOptions, string) {
	t.Helper()
	fake := fakegame.Config{SelfNetwork: "games", Sidecar: "tailscale"}
	if published {
		fake.SidecarPort = "34197"
	}
	r := newRig(t, rigOptions{noRun: true, fake: fake})
	opts := rigOptions{network: "games", sidecar: "tailscale"}
	if inContainer {
		// The fake names its self container after this host.
		hostname, _ := os.Hostname()
		self, ok := r.game.Daemon().Container(hostname)
		if !ok {
			t.Fatal("the fake seeded no self container")
		}
		opts.selfID = self.ID
	}
	r.c = r.newController(opts)
	return r, opts, r.game.SidecarAddress()
}

// rconLogins is how many RCON logins the world's game received, over all
// its containers: the connections on which Foreman sent anything, since
// its first packet is the login with the world's password.
func (r *rig) rconLogins(id string) int {
	r.t.Helper()
	return r.container(id).RCONLogins
}

// dropRCON closes Foreman's open RCON connections to the world's game, as a
// broken network would, and leaves the game's RCON listening.
func (r *rig) dropRCON(id string) {
	r.t.Helper()
	if err := r.game.DropRCON(ContainerName(id)); err != nil {
		r.t.Fatal(err)
	}
}

// sidecarRig is a rig with a sidecar named "tailscale" on the game network.
// inContainer seeds Foreman's own container on the game network.
func sidecarRig(t *testing.T, inContainer bool) (*rig, rigOptions) {
	t.Helper()
	r := newRig(t, rigOptions{noRun: true})
	opts := rigOptions{network: "games", sidecar: "tailscale"}
	if inContainer {
		d := r.game.Daemon()
		d.Seed(dockertest.Container{Name: "foreman-self", Status: "running",
			Networks: map[string]string{"games": "172.30.0.9/24", "proxy-net": "172.31.0.2/24"}})
		self, _ := d.Container("foreman-self")
		opts.selfID = self.ID
	}
	r.c = r.newController(opts)
	return r, opts
}

// created returns the spec Foreman created the world's container with.
func (r *rig) created(id string) docker.ContainerSpec {
	r.t.Helper()
	c, ok := r.game.Daemon().Container(ContainerName(id))
	if !ok {
		r.t.Fatalf("no container for %s", id)
	}
	return c.Spec
}

// SPEC 4.13: the game joins the sidecar's network by the inspected ID,
// declares no port, binds RCON to the sidecar's game-network address, and
// Foreman reaches it there, whether Foreman runs in a container or on the
// host. (The sidecar's other networks are TestSidecarRangesChecked's.)
func TestSidecarStartJoinsCheckedSidecar(t *testing.T) {
	for _, inContainer := range []bool{true, false} {
		t.Run(fmt.Sprintf("in container %v", inContainer), func(t *testing.T) {
			r, _, ip := liveSidecarRig(t, inContainer, true)
			id := r.newWorld("Base")
			sc := r.game.State().Sidecar
			r.stop = r.run(r.c)

			r.start(id)
			spec := r.created(id)
			if spec.HostConfig.NetworkMode != "container:"+sc.ID {
				t.Errorf("network mode %q, want the sidecar's ID", spec.HostConfig.NetworkMode)
			}
			if len(spec.ExposedPorts) != 0 || len(spec.HostConfig.PortBindings) != 0 {
				t.Errorf("the game declares ports: %v %v", spec.ExposedPorts, spec.HostConfig.PortBindings)
			}
			if want := gameenv.RCONBind + "=" + ip + ":27015"; !slices.Contains(spec.Env, want) {
				t.Errorf("env %v lacks %s", spec.Env, want)
			}
			if r.rconLogins(id) == 0 {
				t.Error("Foreman never logged in to RCON at the sidecar's address")
			}
			st := r.c.Status(id)
			if st.State != StateRunning || st.LANUnpublished != "" {
				t.Errorf("state %s, LAN warning %q", st.State, st.LANUnpublished)
			}
			if _, _, err := r.c.Console(context.Background(), id, admin, "/players", ""); err != nil {
				t.Errorf("console: %v", err)
			}
			subnets, known := r.c.GameSubnets()
			for _, want := range prefixes(ip + "/24") {
				want = want.Masked()
				if !known || !slices.Contains(subnets, want) {
					t.Errorf("ranges %v (known %v) lack %s", subnets, known, want)
				}
			}
			if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
				t.Errorf("graceful stop: %v", err)
			}
		})
	}
}

// SPEC 4.13, 14.16: each sidecar the game cannot share is refused with
// its own kind, as an ErrNetwork, without the sidecar's environment.
func TestSidecarRefusals(t *testing.T) {
	cases := []struct {
		name string
		seed *sidecarOpts
		kind string
	}{
		{"missing", nil, NetworkSidecarMissing},
		{"stopped", &sidecarOpts{ip: "172.30.0.5", status: "exited"}, NetworkSidecarStopped},
		{"host network", &sidecarOpts{mode: "host", networks: map[string]string{"host": ""}}, NetworkSidecarHost},
		{"another container's network", &sidecarOpts{mode: "container:abc", networks: map[string]string{}}, NetworkSidecarNested},
		{"off the game network", &sidecarOpts{networks: map[string]string{"shared": sharedNet}}, NetworkSidecarOffNetwork},
		{"unreadable range", &sidecarOpts{networks: map[string]string{"games": "172.30.0.5/24", "odd": ""}}, NetworkSidecarNoRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := sidecarRig(t, false)
			if tc.seed != nil {
				seedSidecar(r.game.Daemon(), "tailscale", *tc.seed)
			}
			_, err := r.c.gameNetwork(context.Background())
			checkNetworkKind(t, err, tc.kind)
		})
	}
}

func checkNetworkKind(t *testing.T, err error, kind string) {
	t.Helper()
	ne, ok := errors.AsType[*NetworkError](err)
	if !ok || ne.Kind != kind || !errors.Is(err, ErrNetwork) {
		t.Fatalf("got %v, want a %s refusal", err, kind)
	}
	if strings.Contains(err.Error(), sidecarSecret) || strings.Contains(err.Error(), "TS_AUTHKEY") {
		t.Errorf("the refusal shows the sidecar's environment: %s", err)
	}
	if ne.Container != "" && ne.Container != "tailscale" {
		t.Errorf("Container = %q", ne.Container)
	}
}

// SPEC 4.13, 12.6: every network the sidecar is on, IPv4 and IPv6, is
// checked against FOREMAN_TRUSTED_PROXIES, and against the address
// DOCKER_HOST reaches; additional networks alone pass, with an unrelated
// service on them.
func TestSidecarRangesChecked(t *testing.T) {
	cases := []struct {
		name    string
		proxies string
		tcp     string // the address DOCKER_HOST resolves to; "" for a socket
		kind    string
	}{
		{"apart", "192.168.50.2/32", "172.31.0.20", ""},
		{"proxy in the shared network", "10.9.4.4/32", "", NetworkSidecarProxyOverlap},
		{"proxy in the game network's IPv6", "fd00:30::77/128", "", NetworkProxyOverlap},
		{"socket proxy on the shared network", "", "10.9.0.20", NetworkSidecarHostOnNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := sidecarRig(t, false)
			d := r.game.Daemon()
			// Foreman shares the proxy network and the shared one, so DOCKER_HOST
			// is reachable away from the game network.
			d.Seed(dockertest.Container{Name: "foreman-self", Status: "running",
				Networks: map[string]string{"games": "172.30.0.9/24,fd00:30::9/64", "proxy-net": "172.31.0.2/24", "shared": "10.9.0.3/16"}})
			self, _ := d.Container("foreman-self")
			r.c.selfID = self.ID
			seedSidecar(d, "tailscale", sidecarOpts{ip: "172.30.0.5"})
			d.Seed(dockertest.Container{Name: "unrelated-db", Status: "running", Networks: map[string]string{"shared": "10.9.0.8/16"}})
			if tc.proxies != "" {
				r.c.proxies = prefixes(tc.proxies)
			}
			if tc.tcp != "" {
				addr := tc.tcp
				r.c.dockerTCP, r.c.dockerHost = true, "socket-proxy"
				r.c.lookupHost = func(context.Context, string) ([]string, error) { return []string{addr}, nil }
			}
			got, err := r.c.gameNetwork(context.Background())
			if tc.kind == "" {
				if err != nil || !strings.HasPrefix(got.mode, "container:") {
					t.Fatalf("got %+v, %v; want the sidecar", got.mode, err)
				}
				return
			}
			checkNetworkKind(t, err, tc.kind)
		})
	}
}

// SPEC 4.4, 4.13: in a container the game network still admits no other
// container than Foreman, its games and the sidecar.
func TestSidecarGameNetworkMembers(t *testing.T) {
	r, _ := sidecarRig(t, true)
	d := r.game.Daemon()
	seedSidecar(d, "tailscale", sidecarOpts{ip: "172.30.0.5"})
	if _, err := r.c.gameNetwork(context.Background()); err != nil {
		t.Fatalf("Foreman and the sidecar alone: %v", err)
	}
	d.Seed(dockertest.Container{Name: "intruder", Status: "running", Networks: map[string]string{"games": "172.30.0.30/24"}})
	_, err := r.c.gameNetwork(context.Background())
	checkNetworkKind(t, err, NetworkShared)
	if !strings.Contains(err.Error(), "intruder") || strings.Contains(err.Error(), "tailscale (") {
		t.Errorf("members named: %s", err)
	}
}

// SPEC 4.13, 7.4: a refused sidecar refuses a start and a restart before
// anything is written to the world or stopped.
func TestSidecarRefusalChangesNothing(t *testing.T) {
	r, _, _ := liveSidecarRig(t, false, true)
	d := r.game.Daemon()
	id := r.newWorld("Base")
	r.stop = r.run(r.c)
	r.start(id)

	// The sidecar is replaced by one off the game network while the game runs.
	removeSidecar(t, r, "tailscale")
	seedSidecar(d, "tailscale", sidecarOpts{networks: map[string]string{"shared": sharedNet}})
	if err := os.Remove(filepath.Join(r.worldDir(id), world.ModsDir, world.ModListFile)); err != nil {
		t.Fatal(err)
	}
	before := len(r.calls("POST"))
	err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{})
	checkNetworkKind(t, err, NetworkSidecarOffNetwork)
	if calls := r.calls("POST")[before:]; len(calls) != 0 {
		t.Errorf("the refused restart called %v", calls)
	}
	if r.container(id).Status != "running" {
		t.Error("the refused restart stopped the game")
	}
	if _, err := os.Stat(filepath.Join(r.worldDir(id), world.ModsDir, world.ModListFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused restart wrote the mod list: %v", err)
	}

	// Stopped, a start is refused the same way.
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	r.waitState(id, StateStopped)
	before = len(r.calls("POST"))
	err = r.c.Start(context.Background(), id, admin, StartOptions{})
	checkNetworkKind(t, err, NetworkSidecarOffNetwork)
	if calls := r.calls("POST")[before:]; len(calls) != 0 {
		t.Errorf("the refused start called %v", calls)
	}
	if _, err := os.Stat(filepath.Join(r.worldDir(id), world.ModsDir, world.ModListFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused start wrote the mod list: %v", err)
	}
	snaps, err := r.worlds.Snapshots(id)
	if err != nil || len(snaps) != 0 {
		t.Errorf("snapshots %v, %v", snaps, err)
	}
}

func removeSidecar(t *testing.T, r *rig, name string) {
	t.Helper()
	if err := r.docker.RemoveContainer(context.Background(), name, true); err != nil {
		t.Fatal(err)
	}
}

// SPEC 4.13: the sidecar is looked up at each start, and the game joins
// the ID that was checked. A container made under the same name after the
// check is never joined: the start fails instead.
func TestSidecarReplacementIsNotFollowed(t *testing.T) {
	r, _ := sidecarRig(t, false)
	d := r.game.Daemon()
	id := r.newWorld("Base")
	old := seedSidecar(d, "tailscale", sidecarOpts{ip: "172.30.0.5"})
	network, err := r.c.gameNetwork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if network.mode != "container:"+old.ID {
		t.Fatalf("mode %q", network.mode)
	}
	removeSidecar(t, r, "tailscale")
	replacement := seedSidecar(d, "tailscale", sidecarOpts{ip: "172.30.0.5"})
	meta, err := r.c.readMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cid, err := r.docker.CreateContainer(ctx, ContainerName(id), r.c.containerSpec(id, image21, network, meta))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, cid); err == nil {
		t.Error("a game joined a sidecar that is gone")
	}
	if c, _ := d.Container(cid); strings.Contains(c.Spec.HostConfig.NetworkMode, replacement.ID) {
		t.Error("the game was given the replacement")
	}
	// The next start checks the replacement and joins it.
	again, err := r.c.gameNetwork(ctx)
	if err != nil || again.mode != "container:"+replacement.ID {
		t.Errorf("next check %q, %v; want the replacement's ID", again.mode, err)
	}
}

// SPEC 3.8, 4.13: after a Foreman restart the running game is reached at
// the sidecar it joined, and the sidecar's ranges are known again before
// trusted headers are accepted.
func TestSidecarForemanRestart(t *testing.T) {
	r, opts, ip := liveSidecarRig(t, false, false)
	id := r.newWorld("Base")
	r.stop = r.run(r.c)
	r.start(id)
	r.stop()

	c2 := r.newController(opts)
	if _, known := c2.GameSubnets(); known {
		t.Error("ranges known before the first look")
	}
	logins := r.rconLogins(id)
	r.run(c2)
	eventually(t, func() bool { return c2.Status(id).State == StateRunning }, "re-attach")
	eventually(t, func() bool { return r.rconLogins(id) > logins }, "RCON login after re-attach")
	if st := c2.Status(id); st.LANUnpublished != "tailscale" {
		t.Errorf("LAN warning %q after re-attach", st.LANUnpublished)
	}
	subnets, known := c2.GameSubnets()
	if !known || !slices.Contains(subnets, netip.MustParsePrefix(ip+"/24").Masked()) {
		t.Errorf("ranges %v, known %v", subnets, known)
	}
	if err := c2.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Errorf("graceful stop after re-attach: %v", err)
	}
}

// SPEC 4.13: a game left running in a sidecar since replaced under the same
// name, at the same address, is never reached through the replacement.
// Without RCON its stop needs Force stop (SPEC 7.6).
func TestSidecarStaleGameDoesNotFollowName(t *testing.T) {
	r, opts, ip := liveSidecarRig(t, false, true)
	d := r.game.Daemon()
	id := r.newWorld("Base")
	r.stop = r.run(r.c)
	r.start(id)
	r.stop()

	// The game keeps listening at the address, so a login sent there would
	// reach it.
	removeSidecar(t, r, "tailscale")
	seedSidecar(d, "tailscale", sidecarOpts{ip: ip})
	logins := r.rconLogins(id)
	c2 := r.newController(opts)
	r.run(c2)
	eventually(t, func() bool { return c2.Status(id).State == StateRunning }, "re-attach")
	err := c2.Stop(context.Background(), id, admin, StopOptions{})
	if !errors.Is(err, ErrRCONUnreachable) {
		t.Errorf("stop: %v, want ErrRCONUnreachable", err)
	}
	if r.rconLogins(id) != logins {
		t.Error("Foreman logged in through the replacement sidecar")
	}
	if err := c2.ForceStop(context.Background(), id, admin); err != nil {
		t.Errorf("force stop: %v", err)
	}
}

// SPEC 4.13: the RCON endpoint read back from the game is dialed only when
// it is the address, on the game network, of the very container the game
// joined.
func TestSharedRCONRefusesForeignEndpoints(t *testing.T) {
	ns := docker.Container{ID: "abc", Networks: map[string]docker.Network{
		"games":  {IPAddress: "172.30.0.5", IPPrefixLen: 24},
		"shared": {IPAddress: "10.9.0.7", IPPrefixLen: 16},
	}}
	game := func(bind string) docker.Container {
		return docker.Container{NetworkMode: "container:abc", Env: []string{gameenv.RCONBind + "=" + bind}}
	}
	if got, err := sharedRCON(game("172.30.0.5:27015"), ns, "games"); err != nil || got != "172.30.0.5:27015" {
		t.Errorf("own address: %q, %v", got, err)
	}
	for _, bind := range []string{
		"10.9.0.7:27015",  // the sidecar's other network
		"192.0.2.1:27015", // anywhere else
		"172.30.0.5:22",   // another port
		"172.30.0.5",      // no port
		"evil.example:27015",
		"",
	} {
		if _, err := sharedRCON(game(bind), ns, "games"); !errors.Is(err, errBindMismatch) {
			t.Errorf("%q: %v, want a refusal", bind, err)
		}
	}
	if _, err := sharedRCON(docker.Container{}, ns, "games"); !errors.Is(err, errBindMismatch) {
		t.Errorf("no endpoint: %v", err)
	}
}

// SPEC 4.13: a game whose network mode names a container rather than the
// ID Foreman joined is not reached, since the name may now be another's.
func TestGameEndpointNeedsTheJoinedID(t *testing.T) {
	r, _ := sidecarRig(t, false)
	sc := seedSidecar(r.game.Daemon(), "tailscale", sidecarOpts{ip: "172.30.0.5"})
	bind := []string{gameenv.RCONBind + "=172.30.0.5:27015"}
	if ep, err := r.c.gameEndpoint(context.Background(), docker.Container{NetworkMode: "container:" + sc.ID, Env: bind}); err != nil || ep.rcon != "172.30.0.5:27015" {
		t.Errorf("by ID: %+v, %v", ep, err)
	}
	if _, err := r.c.gameEndpoint(context.Background(), docker.Container{NetworkMode: "container:tailscale", Env: bind}); !errors.Is(err, errBindMismatch) {
		t.Errorf("by name: %v, want a refusal", err)
	}
	if _, err := r.c.gameEndpoint(context.Background(), docker.Container{NetworkMode: "container:" + strings.Repeat("f", 64), Env: bind}); !errors.Is(err, errNamespaceGone) {
		t.Errorf("gone: %v", err)
	}
}

// SPEC 4.13: a sidecar that publishes no host port for UDP 34197 warns
// without refusing the start, and a fresh start after it publishes one
// clears the warning.
func TestSidecarLANWarning(t *testing.T) {
	r, _, _ := liveSidecarRig(t, false, false)
	id := r.newWorld("Base")
	r.stop = r.run(r.c)
	r.start(id)
	if st := r.c.Status(id); st.LANUnpublished != "tailscale" {
		t.Errorf("LAN warning %q, want the sidecar's name", st.LANUnpublished)
	}
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	if st := r.c.Status(id); st.LANUnpublished != "" {
		t.Errorf("LAN warning %q on a stopped game", st.LANUnpublished)
	}
	// The operator recreates the sidecar with the port published.
	if err := r.game.SetSidecarPort("34197"); err != nil {
		t.Fatal(err)
	}
	r.start(id)
	if st := r.c.Status(id); st.LANUnpublished != "" {
		t.Errorf("LAN warning %q after publishing", st.LANUnpublished)
	}
}

// SPEC 12.6: with a sidecar, its ranges are known before any game starts,
// even with Foreman on the host; a sidecar Docker cannot show keeps them
// unknown, and once known they survive a failed read.
func TestSidecarSubnets(t *testing.T) {
	r, _ := sidecarRig(t, false)
	d := r.game.Daemon()
	ctx := context.Background()
	r.c.tick(ctx)
	if got, known := r.c.GameSubnets(); known {
		t.Errorf("known with no sidecar: %v", got)
	}
	seedSidecar(d, "tailscale", sidecarOpts{ip: "172.30.0.5", status: "exited"})
	r.c.tick(ctx)
	if got, known := r.c.GameSubnets(); known {
		t.Errorf("known from a stopped sidecar: %v", got)
	}
	removeSidecar(t, r, "tailscale")
	seedSidecar(d, "tailscale", sidecarOpts{ip: "172.30.0.5"})
	d.SetCallFailure("GET", "json", 500, "daemon busy")
	r.c.tick(ctx)
	if _, known := r.c.GameSubnets(); known {
		t.Error("known after a failed inspect")
	}
	d.SetCallFailure("GET", "json", 0, "")
	r.c.tick(ctx)
	want := prefixes("10.9.0.0/16", "172.30.0.0/24", "fd00:30::/64", "fe80::/10")
	if got, known := r.c.GameSubnets(); !known || !slices.Equal(got, want) {
		t.Errorf("ranges %v, known %v; want %v", got, known, want)
	}
	removeSidecar(t, r, "tailscale")
	r.c.tick(ctx)
	if got, known := r.c.GameSubnets(); !known || !slices.Equal(got, want) {
		t.Errorf("after the sidecar went: %v, known %v; want the last ranges kept", got, known)
	}
}

// SPEC 12.6: a game in a sidecar's network has no network of its own, so
// its inspect alone never makes the ranges known.
func TestSidecarGameInspectIsNotEvidence(t *testing.T) {
	r, _ := sidecarRig(t, false)
	r.c.noteGameSubnets(context.Background(), docker.Container{NetworkMode: "container:" + strings.Repeat("a", 64)})
	if got, known := r.c.GameSubnets(); known {
		t.Errorf("known from a game with no network: %v", got)
	}
}

// SPEC 4.4: without a sidecar the game still publishes its port and
// declares it, and is told no RCON endpoint.
func TestContainerSpecOrdinaryKeepsPorts(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	spec := r.c.containerSpec("w1", "img", gameNet{mode: "games"}, world.Meta{})
	if _, ok := spec.ExposedPorts[gamePort]; !ok || spec.HostConfig.PortBindings[gamePort][0].HostPort != gameHostPort {
		t.Errorf("ports %v %v", spec.ExposedPorts, spec.HostConfig.PortBindings)
	}
	for _, e := range spec.Env {
		if strings.HasPrefix(e, gameenv.RCONBind+"=") {
			t.Errorf("env %v", spec.Env)
		}
	}
}

// SPEC 4.13: the start script binds RCON to the endpoint it is given, and
// otherwise listens on the RCON port as before.
func TestStartScriptRCONBind(t *testing.T) {
	run := runScriptEnv(t, []string{gameenv.RCONBind + "=172.30.0.5:27015"}, nil)
	args := run.calls[len(run.calls)-1]
	if flagValue(args, "--rcon-bind") != "172.30.0.5:27015" || hasArg(args, "--rcon-port") {
		t.Errorf("with a sidecar: %v", args)
	}
	args = runScript(t)
	if flagValue(args, "--rcon-port") != RCONPort || hasArg(args, "--rcon-bind") {
		t.Errorf("without one: %v", args)
	}
}

// SPEC 12.6: an older running namespace must be read completely even
// when the configured replacement has readable ranges.
func TestSidecarOldNamespaceIncompleteRanges(t *testing.T) {
	r, _ := sidecarRig(t, false)
	d := r.game.Daemon()
	seedSidecar(d, "tailscale", sidecarOpts{ip: "172.30.0.5"})
	old := seedSidecar(d, "old-sidecar", sidecarOpts{networks: map[string]string{"games": "172.30.0.6/24", "unreadable": "10.8.0.2/0"}})
	ctx := context.Background()
	complete := r.c.noteGameSubnets(ctx, docker.Container{NetworkMode: "container:" + old.ID})
	r.c.refreshSubnets(ctx, complete)
	if _, known := r.c.GameSubnets(); known {
		t.Fatal("ranges known despite an unreadable old running namespace")
	}
}

// SPEC 4.13, 14.16: a running game keeps its RCON client. When that
// connection drops after the sidecar the game joined was replaced under the
// same name at the same address, neither the next console command nor a
// stop sends the world's RCON login to the replacement: each new
// connection is checked against the container the game joined before the
// login. The game then needs Force stop (SPEC 7.6).
func TestSidecarCachedRCONDoesNotFollowReplacement(t *testing.T) {
	r, _, ip := liveSidecarRig(t, false, true)
	d := r.game.Daemon()
	id := r.newWorld("Base")
	r.stop = r.run(r.c)
	r.start(id)
	ctx := context.Background()
	if _, _, err := r.c.Console(ctx, id, admin, "/players", ""); err != nil {
		t.Fatalf("console before the replacement: %v", err)
	}

	// The game keeps listening at the address, so a login sent there would
	// reach it.
	removeSidecar(t, r, "tailscale")
	seedSidecar(d, "tailscale", sidecarOpts{ip: ip})
	r.dropRCON(id)
	spoke := r.rconLogins(id)
	for range 2 { // the first may only find the dropped connection
		if _, _, err := r.c.Console(ctx, id, admin, "/players", ""); !errors.Is(err, ErrRCONUnreachable) {
			t.Errorf("console: %v, want ErrRCONUnreachable", err)
		}
	}
	if err := r.c.Stop(ctx, id, admin, StopOptions{}); !errors.Is(err, ErrRCONUnreachable) {
		t.Errorf("stop: %v, want ErrRCONUnreachable", err)
	}
	if got := r.rconLogins(id); got != spoke {
		t.Errorf("Foreman sent %d logins through the replacement sidecar", got-spoke)
	}
	if err := r.c.ForceStop(ctx, id, admin); err != nil {
		t.Errorf("force stop: %v", err)
	}
}

// SPEC 4.13: while the sidecar the game joined is unchanged, a dropped
// RCON connection is made again, with a new login, and a stop is graceful.
func TestSidecarCachedRCONReconnects(t *testing.T) {
	r, _, _ := liveSidecarRig(t, false, true)
	id := r.newWorld("Base")
	r.stop = r.run(r.c)
	r.start(id)
	ctx := context.Background()

	r.dropRCON(id)
	spoke := r.rconLogins(id)
	var err error
	for range 2 { // the first may only find the dropped connection
		if _, _, err = r.c.Console(ctx, id, admin, "/players", ""); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("console after the drop: %v", err)
	}
	if r.rconLogins(id) <= spoke {
		t.Error("the console answered without a new login")
	}
	if err := r.c.Stop(ctx, id, admin, StopOptions{}); err != nil {
		t.Errorf("graceful stop: %v", err)
	}
}

// SPEC 4.13: when the check before a login cannot be made, as when Docker
// does not answer, no login is sent, and the next connection checks again
// and logs in. Foreman's own run is stopped so nothing else connects.
func TestSidecarRCONCheckFailureIsRetried(t *testing.T) {
	r, _, ip := liveSidecarRig(t, false, true)
	d := r.game.Daemon()
	id := r.newWorld("Base")
	r.stop = r.run(r.c)
	r.start(id)
	r.stop()
	ctx := context.Background()
	cont, err := r.docker.InspectContainer(ctx, ContainerName(id))
	if err != nil {
		t.Fatal(err)
	}
	pw, err := r.worlds.RCONPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	rc := r.c.rconClient(cont, net.JoinHostPort(ip, RCONPort), pw)
	defer closeRCON(rc)

	spoke := r.rconLogins(id)
	d.SetFailure(500, "the daemon is busy")
	_, err = rc.Exec(ctx, "/players online")
	d.SetFailure(0, "")
	if err == nil {
		t.Fatal("RCON answered while its sidecar could not be checked")
	}
	if strings.Contains(err.Error(), pw.Reveal()) {
		t.Errorf("error %q shows the RCON password", err)
	}
	if r.rconLogins(id) != spoke {
		t.Error("a login was sent before the sidecar was checked")
	}
	if _, err := rc.Exec(ctx, "/players online"); err != nil {
		t.Fatalf("after Docker answers again: %v", err)
	}
	if got := r.rconLogins(id); got != spoke+1 {
		t.Errorf("%d logins after the retry, want 1", got-spoke)
	}
}
