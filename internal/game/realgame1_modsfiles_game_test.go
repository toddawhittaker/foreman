//go:build game

package game

// What the real game does with the mods folder when Foreman is not the one
// changing it (SPEC 9.2 to 9.7, 6.2, 17.8): which zip it loads when two
// versions of one mod sit side by side, and whether it rewrites
// mod-list.json or mod-settings.dat as it starts and as it exits. The
// answers decide what ApplyMods and the mods fingerprint may assume. Run by
// `make test-game`, never by `make verify`.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// fileState is a file's content hash, or "absent".
func fileState(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // the test's own folder
	if errors.Is(err, fs.ErrNotExist) {
		return "absent"
	}
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func TestRealGameModFilesHandling(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runModFiles(t, g.tag, g.fixture, g.version)
		})
	}
}

// modFilesObserved is what one start and stop did to the two mod files.
type modFilesObserved struct {
	loaded                    []string // versions of the probe mod the game loaded
	listBefore, listRunning   string   // content hashes
	listAfter                 string
	entriesBefore, entriesEnd []string // the mod list's meaning, in file order
	settingsBefore            string
	settingsRunning           string
	settingsAfter             string
}

// listEntries reads a mod-list.json as "name=enabled" strings in file order,
// so two files that differ only in key order or spacing compare equal.
func listEntries(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Mods []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"mods"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	var out []string
	for _, m := range doc.Mods {
		out = append(out, fmt.Sprintf("%s=%t", m.Name, m.Enabled))
	}
	return out
}

func (e *gameEnv) runModFiles(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	fetch := newFetcher()
	c.SetModFetcher(fetch)

	// A mod-list.json Foreman wrote that names the mod, and one that does not.
	cases := []struct {
		name   string
		listed bool
	}{
		{"mod-list.json names the mod as on", true},
		{"mod-list.json does not name the mod", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := e.newWorld(t, tag, version, map[bool]string{true: "listed", false: "unlisted"}[tc.listed],
				fixture, map[string]time.Duration{"main.zip": time.Hour})
			if tc.listed {
				// Foreman's own install on a stopped world writes the list and leaves the game off.
				op := e.modsOp(t, id, "install "+probeMod+" 1.0.0", world.ModChange{}, offerProbe(t, fetch, version, "1.0.0"))
				if err := c.ApplyMods(ctx, id, admin, op, StopOptions{}); err != nil {
					t.Fatalf("install on a stopped world: %v", err)
				}
			} else {
				unlock, err := e.worlds.Lock(id)
				if err != nil {
					t.Fatal(err)
				}
				err = e.worlds.EnsureModList(id)
				unlock()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(e.modsPath(id, probeFile("1.0.0")), probeZip(t, version, "1.0.0"), 0o600); err != nil { //nolint:gosec // the test's own folder
					t.Fatal(err)
				}
			}
			// The second version is put in by hand next to the first.
			if err := os.WriteFile(e.modsPath(id, probeFile("2.0.0")), probeZip(t, version, "2.0.0"), 0o600); err != nil { //nolint:gosec // the test's own folder
				t.Fatal(err)
			}
			list, settings := e.modsPath(id, world.ModListFile), e.modsPath(id, world.ModSettingsFile)

			startStop := func(label string) modFilesObserved {
				t.Helper()
				o := modFilesObserved{listBefore: fileState(t, list), settingsBefore: fileState(t, settings),
					entriesBefore: listEntries(t, list)}
				if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
					t.Fatalf("%s: start: %v", label, err)
				}
				if st := c.Status(id); st.State != StateRunning {
					t.Fatalf("%s: state %s (%s), want running", label, st.State, st.Reason)
				}
				for _, l := range currentRun(c, id) {
					if strings.Contains(l, "Loading mod "+probeMod+" ") {
						o.loaded = append(o.loaded, strings.Fields(l)[4])
					}
				}
				o.listRunning, o.settingsRunning = fileState(t, list), fileState(t, settings)
				if err := c.Stop(ctx, id, admin, StopOptions{}); err != nil {
					t.Fatalf("%s: stop: %v", label, err)
				}
				o.listAfter, o.settingsAfter = fileState(t, list), fileState(t, settings)
				o.entriesEnd = listEntries(t, list)
				t.Logf("%s: loaded %v; mod-list.json before=%s running=%s afterStop=%s; mod-settings.dat before=%s running=%s afterStop=%s",
					label, o.loaded, o.listBefore, o.listRunning, o.listAfter, o.settingsBefore, o.settingsRunning, o.settingsAfter)
				t.Logf("%s: mod-list.json entries before %v, after %v", label, o.entriesBefore, o.entriesEnd)
				return o
			}

			first := startStop("run 1")
			second := startStop("run 2")

			// Two versions side by side: the game loads the higher one only.
			for label, o := range map[string]modFilesObserved{"run 1": first, "run 2": second} {
				if len(o.loaded) != 1 || o.loaded[0] != "2.0.0" {
					t.Errorf("%s: with 1.0.0 and 2.0.0 in mods/ the game loaded %v, want only 2.0.0", label, o.loaded)
				}
				// Start does not touch the mod list.
				if o.listRunning != o.listBefore {
					t.Errorf("%s: mod-list.json changed between the start and InGame", label)
				}
				// mod-settings.dat is written when the game starts, never at exit.
				if o.settingsRunning == "absent" {
					t.Errorf("%s: the game wrote no mod-settings.dat although the mod defines a setting", label)
				}
				if o.settingsAfter != o.settingsRunning {
					t.Errorf("%s: mod-settings.dat changed at exit", label)
				}
			}
			if first.settingsBefore != "absent" {
				t.Fatal("the world had a mod-settings.dat before the first start")
			}
			if second.settingsBefore != first.settingsAfter || second.settingsRunning != first.settingsAfter {
				t.Error("the game rewrote an existing mod-settings.dat")
			}

			// At exit the game rewrites mod-list.json: the same mods in the same
			// order, with the mod added as on when the list left it out, and
			// bytes in the game's own format. A second run leaves it alone.
			want := slices.Clone(first.entriesBefore)
			if !tc.listed {
				want = append(want, probeMod+"=true")
			}
			if !slices.Equal(first.entriesEnd, want) {
				t.Errorf("run 1: mod-list.json means %v after the stop, want %v", first.entriesEnd, want)
			}
			t.Logf("RESULT mod-list.json bytes changed at the first exit: %t", first.listAfter != first.listBefore)
			if second.listAfter != first.listAfter {
				t.Error("run 2: the game rewrote a mod-list.json it had written itself")
			}
		})
	}
}
