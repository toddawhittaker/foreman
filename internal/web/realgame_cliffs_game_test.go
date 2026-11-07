//go:build game

package web

// Cliffs off, against the real game images (SPEC 5.6): the settings file the
// Generate form writes with a planet's cliffs box unticked must give a world
// with no cliffs on that planet, while a planet left on keeps its cliffs. The
// file comes from the form's own code, the world from Foreman's own Generate
// and Start, and the cliffs are counted over RCON after the planets' chunks
// around the origin are made. Run by `make test-game`, never by `make verify`.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// cliffsPrefix starts the name of every world this test makes, so its
// container (foreman-fixes2rg-...) is recognisable and removed at the end.
const cliffsPrefix = "fixes2rg"

// cliffsRadius is how many chunks around the origin are made on each planet.
const cliffsRadius = 16

// cliffsStep is how many rings of chunks one RCON call makes, so a call stays
// inside Foreman's RCON time limit.
const cliffsStep = 2

var cliffsLines = []struct {
	tag, version string
	spaceAge     bool
	planets      []string
}{
	{"1.1", "1.1.110", false, []string{"nauvis"}},
	{"2.0", "2.0.77", true, []string{"nauvis", "gleba", "fulgora", "vulcanus"}},
	{"2.1", "2.1.24", true, []string{"nauvis", "gleba", "fulgora", "vulcanus"}},
}

func TestRealGameCliffsOff(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range cliffsLines {
		if only != "" && only != l.tag {
			continue
		}
		if _, err := dc.InspectImage(context.Background(), game.ImageRef(l.version)); err != nil {
			t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", l.version, err)
		}
		t.Run("v"+l.tag, func(t *testing.T) {
			// Every planet with a cliffs box has it off.
			var off []string
			for _, p := range l.planets {
				if mapgen.CliffToggle(l.tag, p) {
					off = append(off, p)
				}
			}
			runCliffs(t, dc, l.tag, l.version, l.spaceAge, l.planets, off)
			// A control run: 1.1 with the box left on, and on 2.x only Gleba
			// off, so each box is seen to reach only its own planet.
			kept := []string{}
			if l.spaceAge {
				kept = []string{"gleba"}
			}
			runCliffs(t, dc, l.tag, l.version, l.spaceAge, l.planets, kept)
		})
	}
}

// cliffsOffForm posts the form with the cliffs box of each planet in off
// unticked, and returns the map-gen-settings.json the form writes.
func cliffsOffForm(t *testing.T, line string, spaceAge bool, off []string) []byte {
	t.Helper()
	vals := url.Values{}
	for _, p := range off {
		vals.Set(fieldName(p, p+"_cliff", mgOn), "off")
	}
	_, out, problems := parseMapGenValues(vals, line, spaceAge)
	if len(problems) > 0 || out == nil {
		t.Fatalf("the form refused cliffs off on %v: %+v", off, problems)
	}
	return out
}

// checkCliffFile checks the file keeps to what SPEC 5.6 allows: on 1.1 the
// richness is 0 when off; on 2.x only the planets' own controls are, and the top-level
// richness, which reaches every planet, is not.
func checkCliffFile(t *testing.T, line string, file []byte, off []string) {
	t.Helper()
	var doc struct {
		Cliff    map[string]any            `json:"cliff_settings"`
		Controls map[string]map[string]any `json:"autoplace_controls"`
	}
	if err := json.Unmarshal(file, &doc); err != nil {
		t.Fatalf("the form wrote no JSON: %v\n%s", err, file)
	}
	if line == "1.1" {
		want := 1.0
		if slices.Contains(off, "nauvis") {
			want = 0.0
		}
		if doc.Cliff["richness"] != want {
			t.Errorf("1.1 cliff_settings.richness = %v, want %v", doc.Cliff["richness"], want)
		}
		return
	}
	if doc.Cliff["richness"] == 0.0 {
		t.Errorf("cliff_settings.richness is 0 on %s; it would reach every planet", line)
	}
	for _, p := range []string{"nauvis", "gleba", "fulgora"} {
		want := 1.0
		if slices.Contains(off, p) {
			want = 0.0
		}
		if size := doc.Controls[p+"_cliff"]["size"]; size != want {
			t.Errorf("%s_cliff size = %v, want %v", p, size, want)
		}
	}
}

func runCliffs(t *testing.T, dc *docker.Client, line, version string, spaceAge bool, planets, off []string) {
	label := "off " + strings.Join(off, "+")
	if len(off) == 0 {
		label = "all on"
	}
	t.Run(label, func(t *testing.T) {
		file := cliffsOffForm(t, line, spaceAge, off)
		checkCliffFile(t, line, file, off)
		settings, err := mapgen.MapSettings(version, "default", nil)
		if err != nil {
			t.Fatal(err)
		}

		var ctrl *game.Controller
		hn := newHarnessWith(t, nil, func(worlds *world.Manager, st *store.Store) *game.Controller {
			c, err := game.New(game.Config{
				Docker: dc, Worlds: worlds, Audit: st, Switches: st,
				HostDataDir: filepath.Dir(worlds.Dirs().Worlds), GameUID: os.Getuid(), GameGID: os.Getgid(),
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			t.Cleanup(func() { cancel(); <-done })
			ctrl = c
			return c
		})
		// Signing in makes the user the audit log needs.
		hn.signIn()
		worlds := hn.worlds

		seed := uint32(123)
		id, err := worlds.Generate(context.Background(), world.NewWorld{
			Name: fmt.Sprintf("%s %s %s", cliffsPrefix, line, label), GameVersion: version, SpaceAge: spaceAge,
			Seed: &seed, MapGenSettings: file, MapSettings: settings,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(game.ContainerName(id), "foreman-"+cliffsPrefix+"-") {
			t.Fatalf("container name %s is not recognisable as this test's", game.ContainerName(id))
		}
		actor := game.Actor{UserID: 1, Username: "alice"}
		t.Cleanup(func() {
			_ = ctrl.Stop(context.Background(), id, actor, game.StopOptions{})
			if err := dc.RemoveContainer(context.Background(), game.ContainerName(id), true); err != nil && !strings.Contains(err.Error(), "not found") {
				t.Errorf("could not remove test container: %v", err)
			}
		})

		begin := time.Now()
		if err := ctrl.Start(context.Background(), id, actor, game.StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		t.Logf("TIMING %s create and start: %s", version, time.Since(begin).Round(100*time.Millisecond))

		lua := func(body string) string {
			t.Helper()
			cmd := "/silent-command " + body
			_, reply, err := ctrl.Console(context.Background(), id, actor, cmd, cmd)
			if err != nil {
				t.Fatalf("RCON: %v", err)
			}
			return strings.TrimSpace(reply)
		}
		// The first Lua command of a game only warns that achievements are
		// off; the second runs.
		lua(`rcon.print("warm")`)
		if got := lua(`rcon.print("warm")`); got != "warm" {
			t.Fatalf("the second Lua command printed %q, want warm", got)
		}

		counts := map[string]int{}
		for _, p := range planets {
			begin := time.Now()
			surface := `local s=game.surfaces["nauvis"]`
			if spaceAge {
				surface = `local pl=game.planets["` + p + `"]; local s=pl.surface or pl.create_surface()`
			}
			for r := cliffsStep; r <= cliffsRadius; r += cliffsStep {
				lua(surface + `; s.request_to_generate_chunks({0,0},` + strconv.Itoa(r) + `); s.force_generate_chunk_requests(); rcon.print("ok")`)
			}
			reply := lua(surface + `; rcon.print(s.count_entities_filtered{type="cliff"})`)
			n, err := strconv.Atoi(reply)
			if err != nil {
				t.Fatalf("%s: the cliff count is %q", p, reply)
			}
			counts[p] = n
			t.Logf("TIMING %s %s chunks and count: %s", version, p, time.Since(begin).Round(100*time.Millisecond))
		}
		t.Logf("CLIFFS %s off=%v: %v", version, off, counts)
		for _, p := range planets {
			switch {
			case slices.Contains(off, p) && counts[p] != 0:
				t.Errorf("%s has %d cliffs with its cliffs box off, want 0", p, counts[p])
			case !slices.Contains(off, p) && counts[p] == 0:
				t.Errorf("%s has no cliffs with its cliffs box left on, want some", p)
			}
		}
	})
}
