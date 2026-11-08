package web

import (
	"github.com/toddawhittaker/foreman/internal/i18n"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

var anyTag = regexp.MustCompile(`<[^>]*>`)

// text is a page as a person reads it: no tags, entities decoded, spaces
// collapsed.
func text(body string) string {
	return strings.Join(strings.Fields(html.UnescapeString(anyTag.ReplaceAllString(body, " "))), " ")
}

func englishAll(ms []i18n.Msg) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = english(m)
	}
	return out
}

// SPEC 9.4, 17.5: the undo confirmation says, in the admin's words, which
// mods undo takes away, puts back, returns to an old version or turns back
// on or off.
func TestModUndoStepsSayWhatUndoDoes(t *testing.T) {
	was := modState{
		zips:     map[string]string{"visible-planets": "1.5.5", "far-reach": "1.1.0", "gone": "2.0.0"},
		listed:   map[string]bool{"base": true, "space-age": false, "far-reach": true},
		settings: []byte("old"), hasSet: true,
	}
	now := modState{
		zips:     map[string]string{"visible-planets": "1.7.2", "far-reach": "1.1.0", "fp-lamp": "1.0.0", "fp-lamp-lib": "1.0.0"},
		listed:   map[string]bool{"base": true, "space-age": true, "far-reach": false},
		settings: []byte("new"), hasSet: true,
	}
	got := englishAll(modUndoSteps(was, now))
	want := []string{
		"Remove fp-lamp 1.0.0 and fp-lamp-lib 1.0.0.",
		"Put back gone 2.0.0.",
		"Put visible-planets back to 1.5.5.",
		"Enable far-reach again.",
		"Disable space-age again.",
		"Put back the mod settings, mod-settings.dat, as they were.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := modUndoSteps(was, was); len(got) != 0 {
		t.Errorf("the same mods: %q", englishAll(got))
	}
	// Without a mod list on one side the built-in mods follow the world's
	// settings, so only zip mods are compared.
	was.listed = nil
	got = englishAll(modUndoSteps(was, now))
	if slices.Contains(got, "Disable space-age again.") || !slices.Contains(got, "Enable far-reach again.") {
		t.Errorf("no mod list: %q", got)
	}
}

// SPEC 9.4 (ADR 0014), 17.9, 17.10: on a stopped world each mod change of a
// run offers undo; the first says "Undo this mod change", later ones name
// when the run began, and the run keeps one snapshot. The undo's
// confirmation lists what it rolls back.
func TestUndoAfterARunOfModChanges(t *testing.T) {
	g := newModsRig(t, true)
	g.world20("Active")
	id := g.world20("Other", "alpha_1.0.0", "beta_1.0.0")
	disable := func(name string) string {
		t.Helper()
		_, body := g.getStatus(kindPlanPath(id, mods.KindDisable, name))
		g.wantRedirectTo(modsPath(id)+ownResultQuery, g.post(modsPath(id)+"/plan", planValues(t, body)))
		if err := g.waitResult(id, game.AuditMods); err != nil {
			t.Fatal(err)
		}
		return g.get(modsPath(id) + ownResultQuery)
	}
	_, before := g.getStatus(kindPlanPath(id, mods.KindDisable, "alpha"))
	wantIn(t, "plan before a run", plain(before), "Take a snapshot labelled")
	tab := disable("alpha")
	wantIn(t, "first change", text(tab), "Disabled alpha in Other.", `Foreman took the snapshot "Before mod change" first.`, "Undo this mod change")

	// Inside the run the plan says it keeps the run's snapshot, not that it takes one.
	_, inRun := g.getStatus(kindPlanPath(id, mods.KindDisable, "beta"))
	wantIn(t, "plan in a run", plain(inRun), "Keep the snapshot from ", ", which already covers this change.")
	if strings.Contains(inRun, "Take a snapshot") {
		t.Error("a plan inside a run still promises a new snapshot")
	}

	tab = disable("beta")
	if n := g.modSnapshots(id); n != 1 {
		t.Fatalf("%d mod snapshots after two changes on a stopped world", n)
	}
	list, err := g.worlds.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	since := list[0].TakenAt.UTC().Format("2006-01-02 15:04 UTC")
	wantIn(t, "second change", text(tab), "Disabled beta in Other.",
		`Foreman took no new snapshot: "Before mod change" from `+since+` undoes this change and the ones before it.`,
		"Undo the mod changes since "+since)
	if strings.Contains(text(tab), "Undo this mod change") {
		t.Error("the second change of a run still says it undoes one change")
	}

	page := text(g.get(restorePath(id, list[0].Name)))
	wantIn(t, "undo confirmation", page, "Undo the mod changes in Other?",
		`The mods go back to how they were in the snapshot "Before mod change" from `+since,
		"Enable alpha and beta again.", "Restore snapshot")
}

// SPEC 9.4: a snapshot whose mod list cannot be read leaves the plain
// restore page, never a guess.
func TestModUndoPageFallsBackWhenTheSnapshotCannotBeRead(t *testing.T) {
	g := newModsRig(t, true)
	g.world20("Active")
	id := g.world20("Other", "alpha_1.0.0")
	if err := os.WriteFile(filepath.Join(g.modsDir(id), world.ModListFile), []byte(`{"mods":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := g.worlds.AutoSnapshot(id, world.ReasonMods, world.LabelBeforeMods, "todd")
	if err != nil {
		t.Fatal(err)
	}
	g.writeMod(id, "beta", "1.0.0")
	page := text(g.get(restorePath(id, snap.Name)))
	wantIn(t, "readable", page, "Undo the mod changes in Other?", "Remove beta 1.0.0.")

	bad := filepath.Join(g.worlds.Dirs().Snapshots, id, snap.Name, world.ModsDir, world.ModListFile)
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	page = text(g.get(restorePath(id, snap.Name)))
	wantIn(t, "unreadable", page, `Restore the snapshot "Before mod change" from`)
	if strings.Contains(page, "Undo the mod changes") {
		t.Error("an unreadable snapshot still claims what undo does")
	}
}
