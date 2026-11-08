//go:build game

package game

// A mod change against the real game on each line (SPEC 9.2 to 9.7, 7.4, 6.2
// and 9.13): ApplyMods installs, updates and disables a
// made-up mod in a running world, with a fake download in place of the portal.
// The mod's control.lua logs one unique line per version, so the game's own
// log shows which version it loaded. Run by `make test-game`, never by `make
// verify`. This file is realgame1 because it runs on all three lines.

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

// probeMod is the made-up mod's name.
const probeMod = "foreman-t10-probe"

// probeLoaded is the line the mod's control.lua logs when its version loads.
func probeLoaded(version string) string { return "FOREMAN-T10-LOADED " + version }

// probeLine is the game line a version string such as "2.0.77" belongs to.
func probeLine(version string) string {
	parts := strings.Split(version, ".")
	return parts[0] + "." + parts[1]
}

// zipOfFiles builds a mod as the portal serves one: a single top folder named
// name_version, holding the given files.
func zipOfFiles(t *testing.T, name, version string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dir := name + "_" + version + "/"
	for _, file := range slices.Sorted(maps.Keys(files)) {
		f, err := zw.Create(dir + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(files[file])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// probeZip builds the made-up mod: an info.json, a settings.lua, a data.lua
// and a control.lua that logs a unique line.
func probeZip(t *testing.T, gameVersion, version string) []byte {
	t.Helper()
	return zipOfFiles(t, probeMod, version, map[string]string{
		"info.json": `{"name":"` + probeMod + `","version":"` + version + `","title":"Foreman probe",` +
			`"author":"foreman tests","description":"made up for the real-game test",` +
			`"factorio_version":"` + probeLine(gameVersion) + `"}`,
		"control.lua": `log("` + probeLoaded(version) + `")` + "\n",
		// The game prints its "Loading mod" lines only for mods with a data stage.
		"data.lua": "-- nothing to define\n",
		// A setting gives the game a reason to write mod-settings.dat.
		"settings.lua": `data:extend({{type = "bool-setting", name = "foreman-t10-flag", setting_type = "startup", default_value = true}})` + "\n",
	})
}

func probeFile(version string) string { return probeMod + "_" + version + ".zip" }

// offerProbe serves a version of the made-up mod and returns the download
// that asks for it.
func offerProbe(t *testing.T, f *fakeFetcher, gameVersion, version string) ModDownload {
	t.Helper()
	f.files[probeFile(version)] = probeZip(t, gameVersion, version)
	return ModDownload{Mod: probeMod, Release: modportal.Release{
		Version: version, FileName: probeFile(version), FactorioVersion: probeLine(gameVersion),
	}}
}

func (e *gameEnv) modsPath(id string, parts ...string) string {
	return filepath.Join(append([]string{e.worldDir(id), world.ModsDir}, parts...)...)
}

// modsOp is the change a planner would hand ApplyMods for this world now.
func (e *gameEnv) modsOp(t *testing.T, id, summary string, change world.ModChange, downloads ...ModDownload) ModOp {
	t.Helper()
	fp, err := e.worlds.ModsFingerprint(id)
	if err != nil {
		t.Fatal(err)
	}
	return ModOp{Downloads: downloads, Change: change, Fingerprint: fp, Summary: summary}
}

// zipsInMods lists the zip files in the world's mods folder.
func (e *gameEnv) zipsInMods(t *testing.T, id string) []string {
	t.Helper()
	entries, err := os.ReadDir(e.modsPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		if strings.HasSuffix(en.Name(), ".zip") {
			out = append(out, en.Name())
		}
	}
	return out
}

// modsSnapshots lists the "Before mod change" snapshots, oldest first.
func (e *gameEnv) modsSnapshots(t *testing.T, id string) []world.Snapshot {
	t.Helper()
	list, err := e.worlds.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []world.Snapshot
	for _, s := range list {
		if s.Reason == world.ReasonMods {
			out = append(out, s)
		}
	}
	// Snapshots come newest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// currentRun is the controller's relayed log from the latest start of the
// game. The relay buffer keeps earlier runs, and every run begins by loading
// the core mod.
func currentRun(c *Controller, id string) []string {
	lines := logLines(c, id)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "Loading mod core ") {
			return lines[i:]
		}
	}
	return lines
}

func anyLineHas(lines []string, text string) bool {
	for _, l := range lines {
		if strings.Contains(l, text) {
			return true
		}
	}
	return false
}

func snapshotHas(s world.Snapshot, path string) bool {
	for _, f := range s.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

// staged lists the temp files at the top of the worlds folder.
func (e *gameEnv) staged(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(e.worlds.Dirs().Worlds)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), world.PrefixPartial) {
			out = append(out, en.Name())
		}
	}
	return out
}

// requireBackInGame is the check a mod change ends with: a new run of the
// game (started after prev) reached InGame, logged no error, and the
// controller says so.
func (e *gameEnv) requireBackInGame(t *testing.T, c *Controller, id, version string, prev time.Time) {
	t.Helper()
	st := c.Status(id)
	if st.State != StateRunning || st.GameVersion != version {
		t.Fatalf("state %s (%s), game version %q, want running on %s", st.State, st.Reason, st.GameVersion, version)
	}
	if !st.StartedAt.After(prev) {
		t.Fatalf("the game started at %v, not after %v, so it was not restarted", st.StartedAt, prev)
	}
	if !anyLineHas(currentRun(c, id), inGameLine) {
		t.Fatalf("the game's log has no %q line for the new run", inGameLine)
	}
	for _, l := range currentRun(c, id) {
		if strings.Contains(l, " Error ") {
			t.Errorf("the game logged an error: %s", l)
		}
	}
	if _, reply, err := c.Console(context.Background(), id, admin, "/version", "/version"); err != nil || !strings.Contains(reply, version) {
		t.Fatalf("/version reply %q, err %v", reply, err)
	}
}

func TestRealGameModsChange(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runModsChange(t, g.tag, g.fixture, g.version)
		})
	}
}

func (e *gameEnv) runModsChange(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	fetch := newFetcher()
	c.SetModFetcher(fetch)
	id := e.newWorld(t, tag, version, "mods", fixture, map[string]time.Duration{"main.zip": time.Hour})
	stopOpts := StopOptions{Countdown: 2 * time.Second}

	step := func(name string, f func(t *testing.T)) {
		t.Helper()
		begin := time.Now()
		ok := t.Run(name, f)
		t.Logf("TIMING %s %s: %s", tag, name, time.Since(begin).Round(100*time.Millisecond))
		if !ok {
			t.FailNow() // later steps depend on this one
		}
	}
	backInGame := func(t *testing.T, prev time.Time) {
		t.Helper()
		e.requireBackInGame(t, c, id, version, prev)
	}
	loads := func(v string) bool { return anyLineHas(currentRun(c, id), "Loading mod "+probeMod+" "+v) }
	logged := func(text string) bool { return anyLineHas(currentRun(c, id), text) }
	dumpModLines := func(t *testing.T) {
		for _, l := range currentRun(c, id) {
			if strings.Contains(l, probeMod) || strings.Contains(l, "FOREMAN-T10") {
				t.Logf("GAME LOG: %s", l)
			}
		}
	}

	step("1 the game starts without the mod", func(t *testing.T) {
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		backInGame(t, time.Time{})
		if logged("Loading mod " + probeMod) {
			t.Fatal("the game loaded the mod before it was installed")
		}
	})

	step("2 install while the game runs", func(t *testing.T) {
		prev := c.Status(id).StartedAt
		dl := offerProbe(t, fetch, version, "1.0.0")
		op := e.modsOp(t, id, "install "+probeMod+" 1.0.0", world.ModChange{}, dl)
		began := time.Now()
		if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
			t.Fatalf("ApplyMods: %v", err)
		}
		t.Logf("install took %s", time.Since(began).Round(100*time.Millisecond))
		backInGame(t, prev)
		dumpModLines(t)
		if got := e.zipsInMods(t, id); len(got) != 1 || got[0] != probeFile("1.0.0") {
			t.Fatalf("mods folder holds %v, want just %s", got, probeFile("1.0.0"))
		}
		if !loads("1.0.0") || !logged(probeLoaded("1.0.0")) {
			t.Fatalf("the game did not load %s 1.0.0 (a Loading mod line and the control.lua line are both expected)", probeMod)
		}
		snaps := e.modsSnapshots(t, id)
		if len(snaps) != 1 || snaps[0].Label != world.LabelBeforeMods {
			t.Fatalf("snapshots with reason mods: %+v, want one labelled %q", snaps, world.LabelBeforeMods)
		}
		if snapshotHas(snaps[0], "mods/"+probeFile("1.0.0")) {
			t.Fatal("the snapshot taken before the install already holds the new mod")
		}
		if !snapshotHas(snaps[0], "saves/main.zip") {
			t.Fatal("the snapshot does not hold the save")
		}
		if s := e.staged(t); len(s) != 0 {
			t.Fatalf("staged files left behind: %v", s)
		}
	})

	step("3 update to a second version", func(t *testing.T) {
		prev := c.Status(id).StartedAt
		dl := offerProbe(t, fetch, version, "2.0.0")
		op := e.modsOp(t, id, "update "+probeMod+" to 2.0.0",
			world.ModChange{Remove: []string{probeFile("1.0.0")}}, dl)
		if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
			t.Fatalf("ApplyMods: %v", err)
		}
		backInGame(t, prev)
		dumpModLines(t)
		if got := e.zipsInMods(t, id); len(got) != 1 || got[0] != probeFile("2.0.0") {
			t.Fatalf("mods folder holds %v, want just %s", got, probeFile("2.0.0"))
		}
		if !loads("2.0.0") || !logged(probeLoaded("2.0.0")) {
			t.Fatalf("the game did not load %s 2.0.0", probeMod)
		}
		if loads("1.0.0") || logged(probeLoaded("1.0.0")) {
			t.Fatal("the game's log for the new run still shows version 1.0.0 loading")
		}
		snaps := e.modsSnapshots(t, id)
		if len(snaps) != 2 || !snapshotHas(snaps[1], "mods/"+probeFile("1.0.0")) || snapshotHas(snaps[1], "mods/"+probeFile("2.0.0")) {
			t.Fatalf("the second snapshot should hold the 1.0.0 zip and not the 2.0.0 one: %+v", snaps)
		}
	})

	// The save the stop just wrote lists the mod, so the pre-start check (SPEC
	// 7.4) refuses the start that follows turning it off, and the world waits
	// stopped with the snapshot to undo (SPEC 9.4, 10.3).
	step("4 disable: the pre-start check refuses, the world stays stopped", func(t *testing.T) {
		op := e.modsOp(t, id, "turn off "+probeMod, world.ModChange{Enabled: map[string]bool{probeMod: false}})
		err := c.ApplyMods(ctx, id, admin, op, stopOpts)
		var pe *PrecheckError
		if !errors.As(err, &pe) || !pe.Check.CanStartAnyway() {
			t.Fatalf("ApplyMods = %v, want a pre-start refusal that allows going ahead", err)
		}
		if len(pe.Check.Problems) != 1 || pe.Check.Problems[0].Kind != world.ProblemModDisabled ||
			len(pe.Check.Problems[0].Mods) != 1 || pe.Check.Problems[0].Mods[0] != probeMod {
			t.Fatalf("the refusal is %+v, want the mod-disabled problem for %s", pe.Check.Problems, probeMod)
		}
		if st := c.Status(id); st.State != StateStopped {
			t.Fatalf("state %s (%s), want stopped", st.State, st.Reason)
		}
		if got := e.zipsInMods(t, id); len(got) != 1 || got[0] != probeFile("2.0.0") {
			t.Fatalf("turning a mod off must keep its zip; mods folder holds %v", got)
		}
		if n := len(e.modsSnapshots(t, id)); n != 3 {
			t.Fatalf("want 3 mod snapshots after 3 changes, got %d", n)
		}
	})

	// The refusal left the mod off in a stopped world. Turning it back on and
	// starting gives the running game the next step needs.
	step("4b turn the mod back on and start", func(t *testing.T) {
		op := e.modsOp(t, id, "turn on "+probeMod, world.ModChange{Enabled: map[string]bool{probeMod: true}})
		if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
			t.Fatalf("ApplyMods: %v", err)
		}
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		backInGame(t, time.Time{})
		if !loads("2.0.0") {
			t.Fatalf("the game did not load %s 2.0.0 after it was turned on", probeMod)
		}
	})

	// With the admin's confirmation that the game deletes the mod's content
	// from the save, the same change passes the check and the world ends
	// running (SPEC 9.4, 9.10).
	step("4c a confirmed drop: the game reaches InGame without the mod", func(t *testing.T) {
		prev := c.Status(id).StartedAt
		op := e.modsOp(t, id, "turn off "+probeMod, world.ModChange{Enabled: map[string]bool{probeMod: false}})
		op.ConfirmedDrops = []string{probeMod}
		if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
			t.Fatalf("ApplyMods: %v", err)
		}
		backInGame(t, prev)
		dumpModLines(t)
		if logged("Loading mod "+probeMod) || logged("FOREMAN-T10-LOADED") {
			t.Fatal("the game loaded the mod after it was turned off")
		}
		list, err := os.ReadFile(e.modsPath(id, world.ModListFile)) //nolint:gosec // the test's own folder
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("mod-list.json while the game runs: %s", strings.Join(strings.Fields(string(list)), " "))
		mods, err := e.worlds.Mods(id)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range mods {
			if m.Name == probeMod {
				found = true
				if m.Enabled {
					t.Fatal("Mods() lists the mod as on after it was turned off")
				}
			}
		}
		if !found {
			t.Fatal("Mods() lost the disabled mod")
		}
	})

	step("5 stop", func(t *testing.T) {
		if err := c.Stop(ctx, id, admin, stopOpts); err != nil {
			t.Fatalf("stop: %v", err)
		}
	})
}
