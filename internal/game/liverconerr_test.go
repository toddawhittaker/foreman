package game

import (
	"context"
	"errors"
	"testing"
)

// SPEC 8.4, 4.13: a game that runs and is in game, but whose RCON client is
// missing (Foreman restarted and the sidecar's network is gone), is not "not
// running". Every RCON entry point says it cannot reach the game, never
// dereferences the missing client, and a game that is stopped or still
// loading keeps ErrNotRunning.
func TestLiveRCONErrorByState(t *testing.T) {
	cases := []struct {
		name   string
		docker string
		inGame bool
		want   error
	}{
		{"running and in game without a client", "running", true, ErrRCONUnreachable},
		{"stopped", "exited", false, ErrNotRunning},
		{"no container", "", false, ErrNotRunning},
		{"running but still loading", "running", false, ErrNotRunning},
		{"exited though last seen in game", "exited", true, ErrNotRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigOptions{noRun: true})
			w := r.c.world("w")
			w.mu.Lock()
			w.dockerStatus, w.run.inGame, w.rc = tc.docker, tc.inGame, nil
			w.mu.Unlock()

			ctx := context.Background()
			if _, _, err := r.c.liveRCON(w); !errors.Is(err, tc.want) {
				t.Errorf("liveRCON: %v, want %v", err, tc.want)
			}
			if _, err := r.c.Players(ctx, "w"); !errors.Is(err, tc.want) {
				t.Errorf("Players: %v, want %v", err, tc.want)
			}
			if _, _, err := r.c.Console(ctx, "w", admin, "/players", ""); !errors.Is(err, tc.want) {
				t.Errorf("Console: %v, want %v", err, tc.want)
			}
			if err := r.c.SendChat(ctx, "w", admin, "hello"); !errors.Is(err, tc.want) {
				t.Errorf("SendChat: %v, want %v", err, tc.want)
			}
			if _, err := r.c.Player(ctx, "w", admin, PlayerKick, "bob", ""); !errors.Is(err, tc.want) {
				t.Errorf("Player: %v, want %v", err, tc.want)
			}
		})
	}
}
