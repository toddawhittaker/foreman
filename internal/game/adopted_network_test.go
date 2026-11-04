package game

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gamedir"
)

// These tests pin down the adopted container's privilege and network
// refusals (SPEC 4.4, 4.6, 14).

// SPEC 4.6, 14: Foreman refuses an adopted container that is privileged,
// adds a capability, shares the host's processes or can reach the Docker
// socket, since a game in it could control the host.
func TestAdoptedContainerPrivilegeRefusals(t *testing.T) {
	cases := []struct {
		name       string
		privileged bool
		capAdd     []string
		pidMode    string
		extra      []docker.Mount
		want       string
	}{
		{"privileged", true, nil, "", nil, "privileged"},
		{"adds a capability", false, []string{"SYS_ADMIN"}, "", nil, "SYS_ADMIN"},
		{"host process namespace", false, nil, "host", nil, "process namespace"},
		{"the host's root", false, nil, "", []docker.Mount{{Source: "/", Destination: "/host"}}, "Docker socket"},
		{"the folder above /var/run", false, nil, "", []docker.Mount{{Source: "/var/", Destination: "/host-var"}}, "Docker socket"},
		{"socket at the usual place", false, nil, "", []docker.Mount{{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"}}, "Docker socket"},
		{"socket under /run", false, nil, "", []docker.Mount{{Source: "/run/docker.sock", Destination: "/sock"}}, "Docker socket"},
		{"socket under another name", false, nil, "", []docker.Mount{{Source: "/srv/docker.sock", Destination: "/tmp/d"}}, "Docker socket"},
		{"the folder that holds the socket", false, nil, "", []docker.Mount{{Source: "/var/run", Destination: "/host-run"}}, "Docker socket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newAdoptedRig(t, adoptedOptions{gameContainer: "other", noRun: true})
			r.game.Daemon().Seed(dockertest.Container{
				Name: "other", Status: "exited", CapDrop: []string{"NET_RAW"}, Privileged: tc.privileged, CapAdd: tc.capAdd, PidMode: tc.pidMode,
				Spec:     docker.ContainerSpec{Image: "factoriotools/factorio:2.1.24", Env: []string{"PUID=845", "PGID=845", "DLC_SPACE_AGE=false", "VERSION=2.1.24"}},
				Mounts:   append([]docker.Mount{{Source: dir, Destination: "/factorio"}}, tc.extra...),
				Networks: map[string]string{testNetwork: ""},
			})
			a := r.newWorld("Alpha")
			err := r.c.Start(context.Background(), a, admin, StartOptions{})
			if !errors.Is(err, ErrContainerRefused) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("start = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}

// SPEC 4.6, 14: Foreman also refuses an adopted container that maps host
// devices, turns off AppArmor, seccomp or SELinux labels, or shares the
// host's user, IPC or UTS namespace.
func TestAdoptedContainerHostSettingRefusals(t *testing.T) {
	cases := []struct {
		name string
		set  func(*dockertest.Container)
		want string
	}{
		{"devices", func(c *dockertest.Container) {
			c.Devices = []docker.Device{{PathOnHost: "/dev/kvm", PathInContainer: "/dev/kvm"}}
		}, "devices"},
		{"apparmor equals", func(c *dockertest.Container) { c.SecurityOpt = []string{"apparmor=unconfined"} }, "apparmor=unconfined"},
		{"apparmor colon", func(c *dockertest.Container) { c.SecurityOpt = []string{"no-new-privileges", "apparmor:unconfined"} }, "apparmor:unconfined"},
		{"seccomp equals", func(c *dockertest.Container) { c.SecurityOpt = []string{"seccomp=unconfined"} }, "seccomp=unconfined"},
		{"seccomp colon", func(c *dockertest.Container) { c.SecurityOpt = []string{"seccomp:unconfined"} }, "seccomp:unconfined"},
		{"label equals", func(c *dockertest.Container) { c.SecurityOpt = []string{"label=disable"} }, "label=disable"},
		{"label colon", func(c *dockertest.Container) { c.SecurityOpt = []string{"label:disable"} }, "label:disable"},
		{"userns host", func(c *dockertest.Container) { c.UsernsMode = "host" }, "userns_mode: host"},
		{"ipc host", func(c *dockertest.Container) { c.IpcMode = "host" }, "ipc: host"},
		{"uts host", func(c *dockertest.Container) { c.UTSMode = "host" }, "uts: host"},
		{"network host", func(c *dockertest.Container) {
			c.Spec.HostConfig.NetworkMode = "host"
			c.Networks = map[string]string{"host": ""}
		}, "network_mode: host"},
		{"NET_RAW kept", func(c *dockertest.Container) { c.CapDrop = nil }, "cap_drop: [NET_RAW]"},
		{"other caps dropped", func(c *dockertest.Container) { c.CapDrop = []string{"NET_ADMIN", "MKNOD"} }, "NET_RAW capability"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newAdoptedRig(t, adoptedOptions{gameContainer: "other", noRun: true})
			cont := dockertest.Container{
				Name: "other", Status: "exited", CapDrop: []string{"NET_RAW"},
				Spec:     docker.ContainerSpec{Image: "factoriotools/factorio:2.1.24", Env: []string{"PUID=845", "PGID=845", "DLC_SPACE_AGE=false", "VERSION=2.1.24"}},
				Mounts:   []docker.Mount{{Source: dir, Destination: "/factorio"}},
				Networks: map[string]string{testNetwork: ""},
			}
			tc.set(&cont)
			r.game.Daemon().Seed(cont)
			a := r.newWorld("Alpha")
			err := r.c.Start(context.Background(), a, admin, StartOptions{})
			if !errors.Is(err, ErrContainerRefused) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("start = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}

// TestDropsNetRaw checks the spellings Docker accepts for dropping NET_RAW.
func TestDropsNetRaw(t *testing.T) {
	for _, tc := range []struct {
		drop []string
		want bool
	}{
		{nil, false},
		{[]string{"MKNOD"}, false},
		{[]string{"NET_RAW"}, true},
		{[]string{"net_raw"}, true},
		{[]string{"CAP_NET_RAW"}, true},
		{[]string{"cap_net_raw"}, true},
		{[]string{"ALL"}, true},
		{[]string{"all"}, true},
		{[]string{"MKNOD", "CAP_ALL"}, true},
	} {
		if got := dropsNetRaw(tc.drop); got != tc.want {
			t.Errorf("dropsNetRaw(%q) = %v, want %v", tc.drop, got, tc.want)
		}
	}
}

// sharedNetworkRig is an adopted rig inside a container: Foreman is
// "foreman-self" on selfNets and the game is "other" on gameNets, both with
// the game folder (SPEC 4.4, 4.8).
func sharedNetworkRig(t *testing.T, selfNets, gameNets map[string]string) *rig {
	t.Helper()
	r, dir := newAdoptedRig(t, adoptedOptions{gameContainer: "other", noRun: true})
	r.game.Daemon().Seed(dockertest.Container{
		Name: "other", Status: "exited", CapDrop: []string{"NET_RAW"},
		Spec:     docker.ContainerSpec{Image: "factoriotools/factorio:2.1.24", Env: []string{"PUID=845", "PGID=845", "DLC_SPACE_AGE=false", "VERSION=2.1.24"}},
		Mounts:   []docker.Mount{{Source: dir, Destination: "/factorio"}},
		Networks: gameNets,
	})
	r.game.Daemon().Seed(dockertest.Container{
		Name: "foreman-self", Status: "running",
		Mounts:   []docker.Mount{{Source: dir, Destination: dir}},
		Networks: selfNets,
	})
	seeded, _ := r.game.Daemon().Container("foreman-self")
	r.c = r.newController(rigOptions{gameDir: dir, selfID: seeded.ID, gameContainer: "other"})
	return r
}

// SPEC 4.4: the network Foreman shares with an adopted container follows
// the managed-mode rules, and FOREMAN_GAME_NETWORK picks it when there are
// several.
func TestAdoptedSharedNetworkRules(t *testing.T) {
	ctx := context.Background()
	// The fake's own container for this process, when it seeds one, is a
	// stranger on the "games" network here, so these cases use networks of
	// their own.
	one := map[string]string{"shared": "10.251.0.9/24"}
	cases := []struct {
		name     string
		selfNets map[string]string
		gameNets map[string]string
		setup    func(r *rig)
		want     string // "" means accepted
	}{
		{"accepted", one, map[string]string{"shared": ""}, func(*rig) {}, ""},
		{"overlaps the trusted proxies", one, map[string]string{"shared": ""}, func(r *rig) {
			r.c.proxies = []netip.Prefix{netip.MustParsePrefix("10.251.0.0/16")}
		}, "FOREMAN_TRUSTED_PROXIES"},
		{"Docker API reachable from it", one, map[string]string{"shared": ""}, func(r *rig) {
			r.c.dockerTCP, r.c.dockerHost = true, "proxy"
			r.c.lookupHost = func(context.Context, string) ([]string, error) { return []string{"10.251.0.50"}, nil }
		}, "DOCKER_HOST"},
		{"a stranger on it", one, map[string]string{"shared": ""}, func(r *rig) {
			r.game.Daemon().Seed(dockertest.Container{Name: "intruder", Status: "running", Networks: map[string]string{"shared": ""}})
		}, "intruder"},
		{"several shared networks, none named", map[string]string{"shared": "10.251.0.9/24", "extra": "10.252.0.9/24"},
			map[string]string{"shared": "", "extra": ""}, func(*rig) {}, "Set FOREMAN_GAME_NETWORK"},
		{"several shared networks, one named", map[string]string{"shared": "10.251.0.9/24", "extra": "10.252.0.9/24"},
			map[string]string{"shared": "", "extra": ""}, func(r *rig) { r.c.network = "extra" }, ""},
		{"the named network is not shared", one, map[string]string{"shared": ""}, func(r *rig) { r.c.network = "elsewhere" }, "do not share it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := sharedNetworkRig(t, tc.selfNets, tc.gameNets)
			tc.setup(r)
			_, err := r.c.checkContainer(ctx)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("check = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrNetwork) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("check = %v, want a network refusal naming %q", err, tc.want)
			}
		})
	}
}

// SPEC 4.4: RCON uses the network FOREMAN_GAME_NETWORK names.
func TestAdoptedRCONUsesTheNamedNetwork(t *testing.T) {
	ctx := context.Background()
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24", "extra": "10.252.0.9/24"}, map[string]string{"shared": "", "extra": ""})
	if err := r.docker.StartContainer(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	cont, err := r.docker.InspectContainer(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	r.c.network = "extra"
	addr, err := r.c.rconAddress(ctx, cont)
	want := cont.Networks["extra"].IPAddress
	if err != nil || want == "" || addr != net.JoinHostPort(want, "27015") {
		t.Errorf("RCON address %q, %v; want %s on extra (networks %+v)", addr, err, want, cont.Networks)
	}
}

// SPEC 4.4, 7.1: when the shared network cannot be chosen, the Server page
// shows that cause as the network warning, not a claim about the RCON
// password.
func TestAdoptedNetworkWarningNamesTheRefusal(t *testing.T) {
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24", "extra": "10.252.0.9/24"}, map[string]string{"shared": "", "extra": ""})
	a := r.newWorld("Alpha")
	if err := gamedir.Present(r.c.adopted.dir, a); err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
	defer r.run(r.c)()
	eventually(t, func() bool { return r.c.Status(a).NetworkWarning != "" }, "the network warning")
	st := r.c.Status(a)
	if !strings.Contains(st.NetworkWarning, "Set FOREMAN_GAME_NETWORK") || strings.Contains(st.NetworkWarning, "RCON password") {
		t.Errorf("warning = %q, want one naming FOREMAN_GAME_NETWORK", st.NetworkWarning)
	}
	if strings.Contains(st.Reason, "RCON password") {
		t.Errorf("reason blames the RCON password: %s", st.Reason)
	}
}

// SPEC 4.4, 7.1: a stranger that joins the shared network while the game
// runs raises the warning after two looks in a row. The game is still
// watched and can be stopped; it cannot be started again.
func TestAdoptedNetworkChangeWarnsAndKeepsStop(t *testing.T) {
	ctx := context.Background()
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24"}, map[string]string{"shared": ""})
	a := r.newWorld("Alpha")
	r.start(a)
	r.c.tickAdopted(ctx)
	if st := r.c.Status(a); st.State != StateStarting && st.State != StateRunning {
		t.Fatalf("state = %s (%s) before the change", st.State, st.Reason)
	}
	r.game.Daemon().Seed(dockertest.Container{Name: "intruder", Status: "running", Networks: map[string]string{"shared": ""}})
	r.c.tickAdopted(ctx)
	if w := r.c.Status(a).NetworkWarning; w != "" {
		t.Fatalf("warning after one look = %q, want none yet", w)
	}
	r.c.tickAdopted(ctx)
	st := r.c.Status(a)
	if !strings.Contains(st.NetworkWarning, "intruder") {
		t.Fatalf("warning after two looks = %q, want one naming the intruder", st.NetworkWarning)
	}
	if !strings.HasSuffix(st.NetworkWarning, ".") || !strings.Contains(st.NetworkWarning, "which is not Foreman or the game container") {
		t.Errorf("warning = %q, want a sentence that fits adopted mode", st.NetworkWarning)
	}
	if st.State == StateUnknown {
		t.Fatalf("state = unknown (%s), want the game still watched", st.Reason)
	}
	if err := r.c.ForceStop(ctx, a, admin); err != nil {
		t.Fatalf("force stop with the warning standing: %v", err)
	}
	if err := r.c.Start(ctx, a, admin, StartOptions{}); !errors.Is(err, ErrNetwork) || !strings.Contains(err.Error(), "intruder") {
		t.Errorf("start = %v, want a network refusal naming the intruder", err)
	}
}

// SPEC 4.4: a graceful stop works while the network warning stands.
func TestAdoptedGracefulStopWithNetworkWarning(t *testing.T) {
	ctx := context.Background()
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24"}, map[string]string{"shared": ""})
	a := r.newWorld("Alpha")
	r.start(a)
	defer r.run(r.c)()
	r.waitState(a, StateRunning)
	r.game.Daemon().Seed(dockertest.Container{Name: "intruder", Status: "running", Networks: map[string]string{"shared": ""}})
	eventually(t, func() bool { return r.c.Status(a).NetworkWarning != "" }, "the network warning")
	if st := r.c.Status(a); st.State != StateRunning {
		t.Fatalf("state = %s (%s), want running", st.State, st.Reason)
	}
	if err := r.c.Stop(ctx, a, admin, StopOptions{}); err != nil {
		t.Fatalf("stop with the warning standing: %v", err)
	}
	r.waitState(a, StateStopped)
}

// SPEC 4.4: a DNS lookup that fails once is a check that could not be made,
// not a refusal, so it raises no warning; the next good look keeps none.
func TestAdoptedNetworkDNSBlipRaisesNoWarning(t *testing.T) {
	ctx := context.Background()
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24", "proxy-net": "10.9.0.2/24"}, map[string]string{"shared": ""})
	r.c.dockerTCP, r.c.dockerHost = true, "proxy"
	fail := true
	r.c.lookupHost = func(context.Context, string) ([]string, error) {
		if fail {
			return nil, errors.New("temporary DNS failure")
		}
		return []string{"10.9.0.5"}, nil
	}
	a := r.newWorld("Alpha")
	if err := gamedir.Present(r.c.adopted.dir, a); err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	r.c.tickAdopted(ctx)
	r.c.tickAdopted(ctx)
	if w := r.c.Status(a).NetworkWarning; w != "" {
		t.Fatalf("warning after DNS failures = %q, want none", w)
	}
	fail = false
	r.c.tickAdopted(ctx)
	if w := r.c.Status(a).NetworkWarning; w != "" {
		t.Errorf("warning after a good look = %q, want none", w)
	}
}

func prefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, p := range s {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

// SPEC 12.6: GameSubnets gives the whole range of every network Foreman
// shares with the adopted container, from both ends, and not the others;
// it is unknown until the monitor has looked, and a failed look keeps the
// last value.
func TestGameSubnetsAdopted(t *testing.T) {
	ctx := context.Background()
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24"}, map[string]string{"shared": "10.251.0.20/24", "other-net": "10.77.0.4/24"})
	if got, known := r.c.GameSubnets(); known || len(got) != 0 {
		t.Errorf("before any look = %v, %v; want none, unknown", got, known)
	}
	r.c.tickAdopted(ctx)
	got, known := r.c.GameSubnets()
	want := prefixes("10.251.0.0/24")
	if !known || !slices.Equal(got, want) {
		t.Errorf("subnets = %v, %v; want %v, known", got, known, want)
	}
	got[0] = netip.Prefix{}
	if again, _ := r.c.GameSubnets(); again[0] != want[0] {
		t.Error("the caller's copy changed the controller's")
	}
	r.game.Daemon().SetFailure(http.StatusInternalServerError, "down")
	r.c.tickAdopted(ctx)
	r.game.Daemon().SetFailure(0, "")
	if got, known := r.c.GameSubnets(); !known || !slices.Equal(got, want) {
		t.Errorf("after a failed look = %v, %v; want the last value", got, known)
	}
}

// SPEC 12.6: both IPv4 and IPv6 ranges of an endpoint count, masked to the
// network, link-local IPv6 counts on an IPv6 network, and an endpoint with
// no address adds none.
func TestAppendSubnets(t *testing.T) {
	got := appendSubnets(nil, docker.Network{IPAddress: "10.0.1.9", IPPrefixLen: 24, GlobalIPv6Address: "fd00:1::9", GlobalIPv6PrefixLen: 64})
	if want := prefixes("10.0.1.0/24", "fd00:1::/64", "fe80::/10"); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	got = appendSubnets(nil, docker.Network{IPAddress: "10.0.1.9", IPPrefixLen: 24})
	if want := prefixes("10.0.1.0/24"); !slices.Equal(got, want) {
		t.Errorf("IPv4 only: got %v, want %v", got, want)
	}
	if got := appendSubnets(nil, docker.Network{}); len(got) != 0 {
		t.Errorf("no address: %v", got)
	}
}

// SPEC 12.6: in managed mode with Foreman in a container, the range is
// Foreman's own on the game network.
func TestGameSubnetsManagedInAContainer(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.game.Daemon().Seed(dockertest.Container{Name: "foreman-self", Status: "running",
		Networks: map[string]string{"games": "10.252.0.2/24", "proxy-net": "10.253.0.2/24"}})
	seeded, _ := r.game.Daemon().Container("foreman-self")
	r.c = r.newController(rigOptions{selfID: seeded.ID, network: "games"})
	r.c.tick(context.Background())
	if got, known := r.c.GameSubnets(); !known || !slices.Equal(got, prefixes("10.252.0.0/24")) {
		t.Errorf("subnets = %v, %v", got, known)
	}
}

// SPEC 12.6: in managed mode in a container, the game network's range is
// known before the first game starts, even before any monitor look. The
// start call fails here so nothing after it can be what added the range.
func TestGameSubnetsManagedKnownBeforeFirstStart(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.game.Daemon().Seed(dockertest.Container{Name: "foreman-self", Status: "running",
		Networks: map[string]string{"games": "10.252.0.2/24"}})
	seeded, _ := r.game.Daemon().Container("foreman-self")
	r.c = r.newController(rigOptions{selfID: seeded.ID, network: "games"})
	r.game.Daemon().SetCallFailure("POST", "start", 500, "daemon busy")
	id := r.newWorld("Base")
	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); err == nil {
		t.Fatal("start worked with a failing start call")
	}
	if got, known := r.c.GameSubnets(); !known || !slices.Equal(got, prefixes("10.252.0.0/24")) {
		t.Errorf("subnets = %v, %v; want the game network's range", got, known)
	}
}

// SPEC 12.6: in managed mode outside a container, the ranges are known
// once every game container was looked at, and a started game's range is
// added before the next look.
func TestGameSubnetsManagedOnTheHost(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if _, known := r.c.GameSubnets(); known {
		t.Error("known before any look")
	}
	r.c.tick(context.Background())
	if got, known := r.c.GameSubnets(); !known || len(got) != 0 {
		t.Errorf("with no game = %v, %v; want none, known", got, known)
	}
	id := r.newWorld("Base")
	r.start(id)
	// The fake gives each game a loopback address, read as a /16.
	if got, _ := r.c.GameSubnets(); len(got) != 1 || got[0].Bits() != 16 {
		t.Errorf("after a start = %v", got)
	}
}

// keepNetRaw changes the adopted container's definition to keep NET_RAW, as
// compose does when it starts the game with Docker's default capabilities,
// or to drop it again.
func keepNetRaw(r *rig, keep bool) {
	drop := []string{"NET_RAW"}
	if keep {
		drop = nil
	}
	r.game.Daemon().SetCapDrop("other", drop)
}

// SPEC 4.4, 12.6: a running adopted game that keeps NET_RAW is attached to,
// but raises a warning naming the setting and makes the game ranges
// unknown, so trusted-header sign-in is refused until it drops NET_RAW.
func TestAdoptedGameKeepingNetRawWarnsAndRefusesHeaders(t *testing.T) {
	ctx := context.Background()
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24"}, map[string]string{"shared": ""})
	a := r.newWorld("Alpha")
	r.start(a)
	r.c.tickAdopted(ctx)
	if _, known := r.c.GameSubnets(); !known {
		t.Fatal("game ranges unknown before the change")
	}
	if w := r.c.Status(a).NetworkWarning; w != "" {
		t.Fatalf("warning before the change = %q", w)
	}

	keepNetRaw(r, true)
	r.c.tickAdopted(ctx)
	st := r.c.Status(a)
	if !strings.Contains(st.NetworkWarning, "cap_drop: [NET_RAW]") {
		t.Fatalf("warning after one look = %q, want one naming cap_drop: [NET_RAW]", st.NetworkWarning)
	}
	if subnets, known := r.c.GameSubnets(); known || len(subnets) != 0 {
		t.Errorf("GameSubnets = %v, %v; want unknown while NET_RAW is kept", subnets, known)
	}
	if st.State == StateUnknown {
		t.Errorf("state = unknown (%s), want the game still watched", st.Reason)
	}
	if err := r.c.ForceStop(ctx, a, admin); err != nil {
		t.Fatalf("force stop with the warning standing: %v", err)
	}

	keepNetRaw(r, false)
	r.c.tickAdopted(ctx)
	if w := r.c.Status(a).NetworkWarning; w != "" {
		t.Errorf("warning after NET_RAW is dropped = %q, want none", w)
	}
	if _, known := r.c.GameSubnets(); !known {
		t.Error("game ranges still unknown after NET_RAW is dropped")
	}
}

// SPEC 4.4: a graceful stop works while the NET_RAW warning stands.
func TestAdoptedGracefulStopWithNetRawWarning(t *testing.T) {
	ctx := context.Background()
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24"}, map[string]string{"shared": ""})
	a := r.newWorld("Alpha")
	r.start(a)
	defer r.run(r.c)()
	r.waitState(a, StateRunning)
	keepNetRaw(r, true)
	eventually(t, func() bool { return strings.Contains(r.c.Status(a).NetworkWarning, "NET_RAW") }, "the NET_RAW warning")
	if err := r.c.Stop(ctx, a, admin, StopOptions{}); err != nil {
		t.Fatalf("stop with the warning standing: %v", err)
	}
	r.waitState(a, StateStopped)
}
