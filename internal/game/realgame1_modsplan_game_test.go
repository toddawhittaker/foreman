//go:build game

package game

// Mod changes planned the way the web page plans them, applied to the real
// game on each line: the drop warning on a running world (SPEC 9.4), one
// snapshot for a run of changes on a stopped world (SPEC 6.2, 9.4, ADR 0014),
// and a "+" recommended dependency on Factorio 2.1 (SPEC 9.2). The mod portal
// is a fake served from memory, so nothing touches the network. Run by `make
// test-game`, never by `make verify`.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/mods"
)

// fakePortal answers the planner from memory; a name it lacks is not on the
// portal.
type fakePortal map[string]modportal.Mod

func (p fakePortal) Mod(_ context.Context, name string) (modportal.Mod, error) {
	m, ok := p[name]
	if !ok {
		return modportal.Mod{}, modportal.ErrNotOnPortal
	}
	return m, nil
}

func (p fakePortal) Mods(ctx context.Context, names []string) map[string]modportal.ModResult {
	out := map[string]modportal.ModResult{}
	for _, n := range names {
		m, err := p.Mod(ctx, n)
		out[n] = modportal.ModResult{Mod: m, Err: err}
	}
	return out
}

// planOp plans req for the world and turns the plan into the operation the
// web page hands ApplyMods, with the drops the admin confirmed.
func (e *gameEnv) planOp(t *testing.T, portal fakePortal, id string, req mods.Request, running bool, confirm bool) (ModOp, mods.Plan) {
	t.Helper()
	ctx := context.Background()
	planner := &mods.Planner{Worlds: e.worlds, Portal: portal}
	plan, fp, err := planner.Plan(ctx, id, req)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	installed, err := e.worlds.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	downloads, change, err := mods.ToChange(plan, installed, "")
	if err != nil {
		t.Fatalf("the plan cannot be applied: %v (%+v)", err, plan.Conflicts)
	}
	op := ModOp{Change: change, Fingerprint: fp, Summary: string(req.Kind) + " " + strings.Join(req.Names, ",")}
	if confirm {
		drops, err := planner.SaveDrops(ctx, id, plan, running)
		if err != nil {
			t.Fatal(err)
		}
		op.ConfirmedDrops = drops
	}
	for _, d := range downloads {
		op.Downloads = append(op.Downloads, ModDownload{Mod: d.Name, Release: d.Release})
	}
	return op, plan
}

// probePortal offers the made-up mod at the given versions.
func probePortal(t *testing.T, f *fakeFetcher, gameVersion string, versions ...string) fakePortal {
	t.Helper()
	m := modportal.Mod{Name: probeMod}
	for _, v := range versions {
		m.Releases = append(m.Releases, offerProbe(t, f, gameVersion, v).Release)
	}
	return fakePortal{probeMod: m}
}

func TestRealGameModDropOnRunningWorld(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runDropOnRunningWorld(t, g.tag, g.fixture, g.version)
		})
	}
}

// A mod installed into a running world is not in the save on disk, but the
// stop that applying a plan needs saves the world with it. So turning it off
// must warn about the drop, and with the drop confirmed the game restarts
// without the mod and the pre-start check passes (SPEC 9.4).
func (e *gameEnv) runDropOnRunningWorld(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	fetch := newFetcher()
	c.SetModFetcher(fetch)
	portal := probePortal(t, fetch, version, "1.0.0")
	id := e.newWorld(t, tag, version, "dropping", fixture, map[string]time.Duration{"main.zip": time.Hour})
	stopOpts := StopOptions{Countdown: 2 * time.Second}

	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	e.requireBackInGame(t, c, id, version, time.Time{})

	installOp, _ := e.planOp(t, portal, id, mods.Request{Kind: mods.KindInstall, Names: []string{probeMod}}, true, false)
	prev := c.Status(id).StartedAt
	if err := c.ApplyMods(ctx, id, admin, installOp, stopOpts); err != nil {
		t.Fatalf("install: %v", err)
	}
	e.requireBackInGame(t, c, id, version, prev)
	if !anyLineHas(currentRun(c, id), probeLoaded("1.0.0")) {
		t.Fatalf("the game did not load %s after the install", probeMod)
	}

	// The save on disk still predates the mod, which is what hid the warning.
	planner := &mods.Planner{Worlds: e.worlds, Portal: portal}
	plan, _, err := planner.Plan(ctx, id, mods.Request{Kind: mods.KindDisable, Names: []string{probeMod}})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := planner.SaveDrops(ctx, id, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != 0 {
		t.Fatalf("a stopped world reads the save on disk, which predates the mod, but SaveDrops listed %v", onDisk)
	}
	drops, err := planner.SaveDrops(ctx, id, plan, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(drops, []string{probeMod}) {
		t.Fatalf("SaveDrops on a running world = %v, want the warning for %s", drops, probeMod)
	}

	prev = c.Status(id).StartedAt
	op, _ := e.planOp(t, portal, id, mods.Request{Kind: mods.KindDisable, Names: []string{probeMod}}, true, true)
	if !slices.Equal(op.ConfirmedDrops, []string{probeMod}) {
		t.Fatalf("the operation confirms %v, want %s", op.ConfirmedDrops, probeMod)
	}
	if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
		t.Fatalf("turning the mod off with the drop confirmed: %v", err)
	}
	e.requireBackInGame(t, c, id, version, prev)
	if anyLineHas(currentRun(c, id), "Loading mod "+probeMod) || anyLineHas(currentRun(c, id), "FOREMAN-T10-LOADED") {
		t.Fatal("the game loaded the mod after it was turned off")
	}
	for _, l := range currentRun(c, id) {
		if strings.Contains(l, inGameLine) {
			t.Logf("GAME LOG after the confirmed drop: %s", l)
		}
	}
	installed, err := e.worlds.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range installed {
		if m.Name == probeMod && m.Enabled {
			t.Fatal("the mod is still on in the mod list")
		}
	}
	if err := c.Stop(ctx, id, admin, stopOpts); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestRealGameModRunSnapshot(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runModRunSnapshot(t, g.tag, g.fixture, g.version)
		})
	}
}

// modsSummary is the world's mod list as one comparable string.
func (e *gameEnv) modsSummary(t *testing.T, id string) string {
	t.Helper()
	installed, err := e.worlds.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, m := range installed {
		parts = append(parts, fmt.Sprintf("%s=%t,%s", m.Name, m.Enabled, m.File))
	}
	slices.Sort(parts)
	return strings.Join(parts, " ")
}

// On a stopped world a run of mod changes takes one "Before mod change"
// snapshot, undo restores the world to before the first change, and a start
// that launches the game ends the run (SPEC 6.2, 9.4, ADR 0014).
func (e *gameEnv) runModRunSnapshot(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	fetch := newFetcher()
	c.SetModFetcher(fetch)
	// An install takes the newest release, so the portal gains releases as
	// the test goes, the way the real one does.
	v1 := probePortal(t, fetch, version, "1.0.0")
	v2 := probePortal(t, fetch, version, "1.0.0", "2.0.0")
	v3 := probePortal(t, fetch, version, "1.0.0", "2.0.0", "3.0.0")
	id := e.newWorld(t, tag, version, "run", fixture, map[string]time.Duration{"main.zip": time.Hour})
	stopOpts := StopOptions{Countdown: 2 * time.Second}
	apply := func(op ModOp) {
		t.Helper()
		if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
			t.Fatalf("%s: %v", op.Summary, err)
		}
	}
	snapCount := func(want int) {
		t.Helper()
		if got := len(e.modsSnapshots(t, id)); got != want {
			t.Fatalf("%d \"Before mod change\" snapshots, want %d", got, want)
		}
	}
	if st := c.Status(id); st.State != StateStopped {
		t.Fatalf("the world starts %s, want stopped", st.State)
	}
	before := e.modsSummary(t, id)

	// Three changes in a row take one snapshot.
	op, _ := e.planOp(t, v1, id, mods.Request{Kind: mods.KindInstall, Names: []string{probeMod}}, false, false)
	apply(op)
	snapCount(1)
	first := e.modsSnapshots(t, id)[0]
	op, _ = e.planOp(t, v2, id, mods.Request{Kind: mods.KindUpdate, Names: []string{probeMod}}, false, false)
	apply(op)
	op, _ = e.planOp(t, v2, id, mods.Request{Kind: mods.KindDisable, Names: []string{probeMod}}, false, true)
	apply(op)
	snapCount(1)
	if got := e.modsSnapshots(t, id)[0].Name; got != first.Name {
		t.Fatalf("the third change took a different snapshot %s, want to reuse %s", got, first.Name)
	}
	if snapshotHas(first, "mods/"+probeFile("1.0.0")) || !snapshotHas(first, "saves/main.zip") {
		t.Fatalf("the run's snapshot should hold the world from before the first change: %+v", first.Files)
	}
	if after := e.modsSummary(t, id); after == before {
		t.Fatalf("three changes left the mod list as it was: %s", after)
	}
	if st := c.Status(id); st.State != StateStopped {
		t.Fatalf("a change on a stopped world left it %s", st.State)
	}

	// Undo returns the mod list to before the first change.
	if _, err := c.Restore(ctx, id, admin, first.Name, stopOpts); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := e.modsSummary(t, id); got != before {
		t.Fatalf("after undo the mods are %q, want the original %q", got, before)
	}
	if zips := e.zipsInMods(t, id); len(zips) != 0 {
		t.Fatalf("after undo the mods folder holds %v, want none", zips)
	}

	// A restore ends the run, so the next change snapshots again. A start that
	// reaches InGame ends it too.
	n := len(e.modsSnapshots(t, id))
	op, _ = e.planOp(t, v1, id, mods.Request{Kind: mods.KindInstall, Names: []string{probeMod}}, false, false)
	apply(op)
	op, _ = e.planOp(t, v2, id, mods.Request{Kind: mods.KindUpdate, Names: []string{probeMod}}, false, false)
	apply(op)
	snapCount(n + 1)

	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	e.requireBackInGame(t, c, id, version, time.Time{})
	if !anyLineHas(currentRun(c, id), probeLoaded("2.0.0")) {
		t.Fatalf("the game did not load %s 2.0.0; mods %s", probeMod, e.modsSummary(t, id))
	}
	for _, l := range currentRun(c, id) {
		if strings.Contains(l, inGameLine) {
			t.Logf("GAME LOG: %s", l)
		}
	}
	if err := c.Stop(ctx, id, admin, stopOpts); err != nil {
		t.Fatalf("stop: %v", err)
	}
	op, _ = e.planOp(t, v3, id, mods.Request{Kind: mods.KindUpdate, Names: []string{probeMod}}, false, false)
	apply(op)
	snapCount(n + 2)
	if zips := e.zipsInMods(t, id); !slices.Equal(zips, []string{probeFile("3.0.0")}) {
		t.Fatalf("after the update the mods folder holds %v, want only %s", zips, probeFile("3.0.0"))
	}
	op, _ = e.planOp(t, v3, id, mods.Request{Kind: mods.KindRemove, Names: []string{probeMod}}, false, true)
	apply(op)
	snapCount(n + 2)
}

const (
	recMain = "foreman-r1-main"
	recDep  = "foreman-r1-rec"
)

func recLoaded(name string) string { return "FOREMAN-R1-LOADED " + name }

// recZip builds a mod with an info.json dependency list and a control.lua
// that logs one line when the game loads it.
func recZip(t *testing.T, name, gameLine string, deps ...string) ([]byte, modportal.Release) {
	t.Helper()
	quoted := make([]string, len(deps))
	for i, d := range deps {
		quoted[i] = `"` + d + `"`
	}
	const version = "1.0.0"
	body := zipOfFiles(t, name, version, map[string]string{
		"info.json": `{"name":"` + name + `","version":"` + version + `","title":"` + name + `","author":"foreman tests",` +
			`"description":"made up for the real-game test","factorio_version":"` + gameLine + `",` +
			`"dependencies":[` + strings.Join(quoted, ",") + `]}`,
		"control.lua": `log("` + recLoaded(name) + `")` + "\n",
		"data.lua":    "-- nothing to define\n",
	})
	return body, modportal.Release{
		Version: version, FileName: name + "_" + version + ".zip", FactorioVersion: gameLine, Dependencies: deps,
	}
}

// Factorio 2.1 reads "+" as optional but on by default. A plan from Foreman
// offers the recommended mod ticked, leaves it out when unticked or missing
// from the portal, and the real game starts in every case (SPEC 9.2). The
// prefix does not exist before 2.1, so only that line runs.
func TestRealGameModRecommendedDependency(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if g.tag != "2.1" || (only != "" && only != g.tag) {
			continue
		}
		e.requireImage(t, ImageRef(g.version))
		cases := []struct {
			name         string
			onPortal     bool
			declined     bool
			wantRecInMod bool
		}{
			{"ticked by default", true, false, true},
			{"unticked", true, true, false},
			{"missing from the portal", false, false, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				e.runRecommended(t, g.tag, g.fixture, g.version, tc.name, tc.onPortal, tc.declined, tc.wantRecInMod)
			})
		}
	}
}

func (e *gameEnv) runRecommended(t *testing.T, tag, fixture, version, label string, onPortal, declined, wantRec bool) {
	ctx := context.Background()
	c, _ := e.controller(t)
	fetch := newFetcher()
	c.SetModFetcher(fetch)
	line := probeLine(version)
	mainZip, mainRel := recZip(t, recMain, line, "+ "+recDep)
	recBody, recRel := recZip(t, recDep, line)
	fetch.files[mainRel.FileName] = mainZip
	fetch.files[recRel.FileName] = recBody
	portal := fakePortal{recMain: {Name: recMain, Releases: []modportal.Release{mainRel}}}
	if onPortal {
		portal[recDep] = modportal.Mod{Name: recDep, Releases: []modportal.Release{recRel}}
	}
	id := e.newWorld(t, tag, version, "recommended "+label, fixture, map[string]time.Duration{"main.zip": time.Hour})

	req := mods.Request{Kind: mods.KindInstall, Names: []string{recMain}}
	if declined {
		req.Declined = []string{recDep}
	}
	op, plan := e.planOp(t, portal, id, req, false, false)
	if got := slices.ContainsFunc(plan.Changes, func(ch mods.Change) bool { return ch.Name == recDep }); got != wantRec {
		t.Fatalf("the plan installs %s = %t, want %t: %+v", recDep, got, wantRec, plan.Changes)
	}
	if err := c.ApplyMods(ctx, id, admin, op, StopOptions{}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	e.requireBackInGame(t, c, id, version, time.Time{})
	run := currentRun(c, id)
	if !anyLineHas(run, recLoaded(recMain)) {
		t.Fatalf("the game did not load %s", recMain)
	}
	if got := anyLineHas(run, recLoaded(recDep)); got != wantRec {
		t.Fatalf("the game loaded %s = %t, want %t", recDep, got, wantRec)
	}
	for _, l := range run {
		if strings.Contains(l, "Loading mod "+recMain) || strings.Contains(l, "Loading mod "+recDep) || strings.Contains(l, inGameLine) {
			t.Logf("GAME LOG: %s", l)
		}
	}
	if err := c.Stop(ctx, id, admin, StopOptions{}); err != nil && !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stop: %v", err)
	}
}
