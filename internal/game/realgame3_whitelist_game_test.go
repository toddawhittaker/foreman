//go:build game

package game

// The world's whitelist switch on the real game (SPEC 8.3). With the switch
// on, the game is started with --use-server-whitelist and --server-whitelist,
// keeps the list in the world's file across a restart, and starts when the
// file does not exist yet. With it off, a whitelist file in the folder does
// not stop the game starting. No game client can join, so enforcement itself
// is not observed: the game's own reply to /whitelist get and its file are.
// Run by `make test-game`, never by `make verify`.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameWhitelistSwitch(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runWhitelist(t, g.tag, g.fixture, g.version)
		})
	}
}

func (e *gameEnv) runWhitelist(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	ages := map[string]time.Duration{"base.zip": time.Hour}
	file := func(id string) string {
		return filepath.Join(e.worldDir(id), world.ConfigDir, world.ServerWhiteListFile)
	}
	get := func(t *testing.T, id string) string {
		t.Helper()
		_, reply, err := c.Console(ctx, id, admin, "/whitelist get", "/whitelist get")
		if err != nil {
			t.Fatalf("/whitelist get: %v", err)
		}
		return strings.TrimSpace(reply)
	}
	start := func(t *testing.T, id string) {
		t.Helper()
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
		}
		if st := c.Status(id); st.State != StateRunning {
			t.Fatalf("state %s (%s), want running", st.State, st.Reason)
		}
	}
	stop := func(t *testing.T, id string) {
		t.Helper()
		if err := c.Stop(ctx, id, admin, StopOptions{Countdown: time.Second}); err != nil {
			t.Fatalf("stop: %v", err)
		}
	}

	t.Run("switch on, no whitelist file: the game starts with an empty list", func(t *testing.T) {
		id := e.newWorld(t, tag, version, "wl-nofile", fixture, ages)
		if err := e.worlds.SetUseWhitelist(id, true); err != nil {
			t.Fatal(err)
		}
		start(t, id)
		defer stop(t, id)
		args := strings.Join(logLines(c, id), "\n")
		if !strings.Contains(args, `"--use-server-whitelist"`) || !strings.Contains(args, `"--server-whitelist"`) {
			t.Errorf("the game's program arguments lack the whitelist flags")
		}
		if got := get(t, id); got != "The whitelist is empty." {
			t.Errorf("/whitelist get replied %q", got)
		}
		if kept, err := c.WhitelistKept(ctx, id); err != nil || !kept {
			t.Errorf("WhitelistKept %v %v", kept, err)
		}
	})

	t.Run("switch on, file with a name: an added name survives a restart", func(t *testing.T) {
		id := e.newWorld(t, tag, version, "wl-keep", fixture, ages)
		if err := e.worlds.SetUseWhitelist(id, true); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file(id), []byte(`["hank"]`), 0o600); err != nil {
			t.Fatal(err)
		}
		start(t, id)
		if got := get(t, id); got != "Whitelisted players: hank" {
			t.Errorf("before: /whitelist get replied %q", got)
		}
		if _, err := c.Player(ctx, id, admin, PlayerWhitelistAdd, "ivy", ""); err != nil {
			t.Fatalf("whitelist add: %v", err)
		}
		stop(t, id)
		b, err := os.ReadFile(file(id)) //nolint:gosec // the test's own world folder
		if err != nil || !strings.Contains(string(b), "hank") || !strings.Contains(string(b), "ivy") {
			t.Errorf("whitelist file after stop: %q %v", b, err)
		}
		start(t, id)
		defer stop(t, id)
		if got := get(t, id); got != "Whitelisted players: hank and ivy" {
			t.Errorf("after restart: /whitelist get replied %q", got)
		}
	})

	t.Run("switch off, file present: the game starts", func(t *testing.T) {
		id := e.newWorld(t, tag, version, "wl-off", fixture, ages)
		if err := os.WriteFile(file(id), []byte(`["hank"]`), 0o600); err != nil {
			t.Fatal(err)
		}
		start(t, id)
		defer stop(t, id)
		if strings.Contains(strings.Join(logLines(c, id), "\n"), `"--server-whitelist"`) {
			t.Error("the whitelist was passed with the switch off")
		}
		if kept, err := c.WhitelistKept(ctx, id); err != nil || kept {
			t.Errorf("WhitelistKept %v %v", kept, err)
		}
	})
}
