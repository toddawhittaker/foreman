//go:build game

package game

// The crash catalogue against the real game (SPEC 8.7, 8.8): each scenario
// makes the game stop for one known reason, and the controller must name
// that reason from the catalogue. Run by `make test-catalogue VERSION=x.y.z`
// for one image, or by `make test-game` for each line it runs. This file is
// realgame1 because it runs on all three lines.

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
)

func TestRealGameCatalogue(t *testing.T) {
	version := os.Getenv("FOREMAN_TEST_CATALOGUE_VERSION")
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	ran := false
	for _, g := range gameTags {
		if (version != "" && version != g.version) || (version == "" && only != "" && only != g.tag) {
			continue
		}
		ran = true
		t.Run("v"+g.version, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runCatalogue(t, g.tag, g.fixture, g.version)
		})
	}
	if !ran {
		t.Fatalf("no pinned image runs Factorio %q", version)
	}
}

func (e *gameEnv) runCatalogue(t *testing.T, tag, fixture, version string) {
	c, _ := e.controller(t)
	ctx := context.Background()
	line := probeLine(version)
	saves := map[string]time.Duration{"cap.zip": time.Hour}

	// startFails starts the world and returns how the start failed.
	startFails := func(t *testing.T, id string) *StartError {
		t.Helper()
		err := c.Start(ctx, id, admin, StartOptions{})
		var se *StartError
		if !errors.As(err, &se) {
			t.Fatalf("start gave %v, want a failed start", err)
		}
		t.Logf("start failed: %v (kind %q)", err, se.Reason.Kind)
		return se
	}
	putMod := func(t *testing.T, id, name string, files map[string]string) {
		t.Helper()
		files["info.json"] = `{"name":"` + name + `","version":"1.0.0","title":"` + name + `","author":"foreman tests",` +
			`"factorio_version":"` + line + `"` + files["info.json"] + `}`
		p := e.modsPath(id, name+"_1.0.0.zip")
		if err := os.WriteFile(p, zipOfFiles(t, name, "1.0.0", files), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a mod's own error", func(t *testing.T) {
		id := e.newWorld(t, tag, version, "moderror", fixture, saves)
		putMod(t, id, "brokenlua", map[string]string{"data.lua": `error("deliberate failure for the catalogue")` + "\n"})
		if se := startFails(t, id); se.Reason.Kind != crashreasons.ModError {
			t.Errorf("kind %q, want %q", se.Reason.Kind, crashreasons.ModError)
		}
	})

	t.Run("a missing dependency", func(t *testing.T) {
		id := e.newWorld(t, tag, version, "missingdep", fixture, saves)
		putMod(t, id, "needsdep", map[string]string{
			"info.json": `,"dependencies":["nonexistent-mod >= 1.0.0"]`,
			"data.lua":  "-- nothing\n",
		})
		if line == "1.1" {
			// 1.1 skips the mod without a word and starts (testdata README, 10).
			if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
				t.Fatalf("1.1 did not start without the mod: %v", err)
			}
			if err := c.ForceStop(ctx, id, admin); err != nil {
				t.Fatal(err)
			}
			return
		}
		if se := startFails(t, id); se.Reason.Kind != crashreasons.ModMissingDependency {
			t.Errorf("kind %q, want %q", se.Reason.Kind, crashreasons.ModMissingDependency)
		}
	})

	t.Run("a save the game cannot read", func(t *testing.T) {
		// Foreman reads a save before it starts the game, so a bad save
		// reaches the game only on Docker's restart after an exit.
		id := e.newWorld(t, tag, version, "badsave", fixture, saves)
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		junk := make([]byte, 4096)
		_, _ = rand.Read(junk)
		// Newer than the save the game writes as it quits, so the restart
		// loads it.
		e.putSave(t, id, "zzz-corrupt.zip", junk, time.Now().Add(time.Hour))
		e.dockerCLI(t, "exec", ContainerName(id), "sh", "-c", "kill -TERM 1")
		st := waitCrashLoop(t, c, id)
		if st.Crash.Kind != crashreasons.SaveUnreadable {
			t.Errorf("kind %q (%s), want %q", st.Crash.Kind, st.CrashReason, crashreasons.SaveUnreadable)
		}
	})

	t.Run("out of memory", func(t *testing.T) {
		id := e.newWorld(t, tag, version, "oom", fixture, saves)
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		// Foreman sets no memory limit, so the test lowers it under the
		// running game, as the captures did (testdata README, 10). The
		// kernel kills the game, and each restart, while it loads.
		e.dockerCLI(t, "update", "--memory", "96m", "--memory-swap", "96m", ContainerName(id))
		st := waitCrashLoop(t, c, id)
		if st.Crash.Kind != crashreasons.OutOfMemory {
			t.Errorf("kind %q (%s), want %q; OOMKilled %v", st.Crash.Kind, st.CrashReason, crashreasons.OutOfMemory, e.inspect(t, id).State.OOMKilled)
		}
	})
}

// dockerCLI runs the docker command for what the API client does not offer.
func (e *gameEnv) dockerCLI(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "docker", args...).CombinedOutput() //nolint:gosec // fixed arguments on the test's own container
	if err != nil {
		t.Fatalf("docker %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func waitCrashLoop(t *testing.T, c *Controller, id string) Status {
	t.Helper()
	var st Status
	waitFor(t, 3*time.Minute, "the game to crash-loop", func() bool {
		st = c.Status(id)
		return st.State == StateCrashLooping
	})
	return st
}
