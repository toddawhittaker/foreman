package game

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gamedir"
)

// These tests pin down the adopted container's privilege and network
// refusals (SPEC 4.4, 4.6, 14).

// SPEC 4.6, 14: Foreman refuses an adopted container that is privileged or
// can reach the Docker socket, since a game in it could control Docker.
func TestAdoptedContainerPrivilegeRefusals(t *testing.T) {
	cases := []struct {
		name       string
		privileged bool
		extra      []docker.Mount
		want       string
	}{
		{"privileged", true, nil, "privileged"},
		{"socket at the usual place", false, []docker.Mount{{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"}}, "Docker socket"},
		{"socket under /run", false, []docker.Mount{{Source: "/run/docker.sock", Destination: "/sock"}}, "Docker socket"},
		{"socket under another name", false, []docker.Mount{{Source: "/srv/docker.sock", Destination: "/tmp/d"}}, "Docker socket"},
		{"the folder that holds the socket", false, []docker.Mount{{Source: "/var/run", Destination: "/host-run"}}, "Docker socket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newAdoptedRig(t, adoptedOptions{gameContainer: "other", noRun: true})
			r.game.Daemon().Seed(dockertest.Container{
				Name: "other", Status: "exited", Privileged: tc.privileged,
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

// sharedNetworkRig is an adopted rig inside a container: Foreman is
// "foreman-self" on selfNets and the game is "other" on gameNets, both with
// the game folder (SPEC 4.4, 4.8).
func sharedNetworkRig(t *testing.T, selfNets, gameNets map[string]string) *rig {
	t.Helper()
	r, dir := newAdoptedRig(t, adoptedOptions{gameContainer: "other", noRun: true})
	r.game.Daemon().Seed(dockertest.Container{
		Name: "other", Status: "exited",
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

// SPEC 4.4, 7.1: when the shared network cannot be chosen, the monitor's
// Unknown reason gives that cause, not a claim about the RCON password.
func TestAdoptedUnknownReasonNamesTheNetworkRefusal(t *testing.T) {
	r := sharedNetworkRig(t, map[string]string{"shared": "10.251.0.9/24", "extra": "10.252.0.9/24"}, map[string]string{"shared": "", "extra": ""})
	a := r.newWorld("Alpha")
	if err := gamedir.Present(r.c.adopted.dir, a); err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
	defer r.run(r.c)()
	eventually(t, func() bool {
		w := r.c.world(a)
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.adoptErr != nil
	}, "the monitor to refuse the container")
	st := r.c.Status(a)
	if st.State != StateUnknown || !strings.Contains(st.Reason, "Set FOREMAN_GAME_NETWORK") {
		t.Errorf("status = %s (%s), want unknown naming FOREMAN_GAME_NETWORK", st.State, st.Reason)
	}
	if strings.Contains(st.Reason, "RCON password") {
		t.Errorf("reason blames the RCON password: %s", st.Reason)
	}
}
