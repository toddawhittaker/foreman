package game

import (
	"context"
	"net/netip"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

func prefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, p := range s {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
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

// SPEC 12.6: with Foreman in a container, the range is Foreman's own on the
// game network.
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

// SPEC 12.6: in a container, the game network's range is known before the
// first game starts, even before any monitor look. The start call fails
// here so nothing after it can be what added the range.
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

// SPEC 12.6: outside a container, the ranges are known once every game
// container was looked at, and a started game's range is added before the
// next look.
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
