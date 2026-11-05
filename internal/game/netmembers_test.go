package game

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

// SPEC 4.4: a refusal for other containers on the game network names each
// one with what Foreman can tell about it, says why sharing is refused, and
// gives the usual fixes before a different network.
func TestNetworkMembersRefusalSaysWhatAndHow(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	d := r.game.Daemon()
	d.Seed(dockertest.Container{Name: "foreman-self", Status: "running", Networks: map[string]string{"games": "172.30.0.9/24"}})
	self, _ := d.Container("foreman-self")
	r.c.network, r.c.selfID = "games", self.ID
	other := docker.ContainerSpec{Labels: map[string]string{LabelInstall: "/srv/other", LabelWorld: "oct"}}
	d.Seed(dockertest.Container{Name: "foreman-oct2026", Status: "exited", Networks: map[string]string{"games": ""}, Spec: other})
	d.Seed(dockertest.Container{Name: "foreman-y2026", Status: "exited", Networks: map[string]string{"games": ""}, Spec: other})
	d.Seed(dockertest.Container{Name: "socket-proxy", Status: "running", Networks: map[string]string{"games": "172.30.0.11/24"}})

	_, err := r.c.gameNetwork(context.Background())
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("got %v, want an ErrNetwork", err)
	}
	msg := err.Error()
	for _, want := range []string{
		`"games"`,
		"because a game could reach them",
		"foreman-oct2026 (a stopped game from another Foreman installation)",
		"foreman-y2026 (a stopped game from another Foreman installation)",
		"socket-proxy (a running container that is not a Foreman game)",
		// A stopped container may be removed; a running one is only disconnected.
		"docker rm foreman-oct2026 foreman-y2026",
		"docker network disconnect games socket-proxy",
		"FOREMAN_GAME_NETWORK",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, msg)
		}
	}
	// The usual fixes come before the one that needs a new network.
	if strings.Index(msg, "docker rm") > strings.Index(msg, "FOREMAN_GAME_NETWORK") {
		t.Errorf("the new network is offered before removing or disconnecting:\n%s", msg)
	}
	// A running container is never offered for removal.
	if strings.Contains(msg, "docker rm foreman-oct2026 foreman-y2026 socket-proxy") || strings.Contains(msg, "docker rm socket-proxy") {
		t.Errorf("a running container is offered for removal:\n%s", msg)
	}
}

// With only running containers there is nothing to remove, so no removal
// command is given.
func TestNetworkMembersRefusalOnlyRunning(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	d := r.game.Daemon()
	d.Seed(dockertest.Container{Name: "foreman-self", Status: "running", Networks: map[string]string{"games": "172.30.0.9/24"}})
	self, _ := d.Container("foreman-self")
	r.c.network, r.c.selfID = "games", self.ID
	d.Seed(dockertest.Container{Name: "socket-proxy", Status: "running", Networks: map[string]string{"games": "172.30.0.11/24"}})
	_, err := r.c.gameNetwork(context.Background())
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("got %v, want an ErrNetwork", err)
	}
	if strings.Contains(err.Error(), "docker rm") {
		t.Errorf("offered removal of a running container:\n%s", err)
	}
	if !strings.Contains(err.Error(), "docker network disconnect games socket-proxy") {
		t.Errorf("no disconnect command:\n%s", err)
	}
}
