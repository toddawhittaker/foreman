package fakegame

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

// A game that dies while Docker is refusing a stop or kill must still be
// restarted by its policy; a container already exited before the call is
// left alone.
func TestRefusedStopKeepsTheRestartPolicy(t *testing.T) {
	for _, tc := range []struct {
		before, want string
	}{{"running", "restarting"}, {"restarting", "restarting"}, {"exited", "exited"}} {
		t.Run(tc.before, func(t *testing.T) {
			g := New(t, Config{})
			spec := docker.ContainerSpec{}
			spec.HostConfig.RestartPolicy.Name = "unless-stopped"
			g.daemon.Seed(dockertest.Container{ID: "c1", Name: "foreman-w1", Status: "exited", Spec: spec})
			// The game died under the stop's no-restart mark.
			g.setManual("c1", true)
			g.refused("c1", tc.before)
			if c, _ := g.daemon.Container("c1"); c.Status != tc.want {
				t.Errorf("status %q, want %q", c.Status, tc.want)
			}
		})
	}
}
