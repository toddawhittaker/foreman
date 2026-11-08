package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 9.8: the differences counted as the sync would change them.
func TestCompareSave(t *testing.T) {
	v := func(minor uint16) savefile.Version { return savefile.Version{Major: 1, Minor: minor} }
	save := savefile.Save{Mods: []savefile.Mod{
		{Name: "base", Version: savefile.Version{Major: 2}},
		{Name: "space-age", Version: savefile.Version{Major: 2}},
		{Name: "same", Version: v(0)}, {Name: "older", Version: v(0)},
		{Name: "missing", Version: v(0)}, {Name: "off", Version: v(0)},
	}}
	installed := []world.InstalledMod{
		// Built-in versions are the game's and never count; Space Age off counts as off.
		{Name: "base", Version: "2.0.77", BuiltIn: true, Enabled: true},
		{Name: "space-age", Version: "2.0.77", BuiltIn: true},
		{Name: "quality", Version: "2.0.77", BuiltIn: true},
		{Name: "same", Version: "1.0.0", Enabled: true},
		{Name: "older", Version: "1.1.0", Enabled: true},
		{Name: "off", Version: "1.0.0"},
		{Name: "extra", Version: "1.0.0", Enabled: true},
		{Name: "extra-off", Version: "1.0.0"},
	}
	got := compareSave(installed, save)
	want := SaveDiff{Missing: 1, Off: 2, Version: 1, Extra: 1}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got.Matches() {
		t.Error("differing mods read as a match")
	}
	// The save's base and same, against the world's built-ins and same.
	if d := compareSave(installed[:4], savefile.Save{Mods: []savefile.Mod{save.Mods[0], save.Mods[2]}}); !d.Matches() {
		t.Errorf("matching mods read as %+v", d)
	}
}

// #373: the tab says whether the mods match the save, and offers the
// match only when they differ or the save cannot be read.
func TestModsTabSaysWhetherTheSaveMatches(t *testing.T) {
	g := newModsRig(t, true)
	id, err := g.worlds.Create(context.Background(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	g.copySave(id, "2.1.24-space-age-testmod.zip")
	g.writeMod(id, "foreman-test", "0.1.0")
	_, body := g.getStatus(modsPath(id))
	wantIn(t, "matching tab", body, `The mods match the save <code>base.zip</code>.`,
		// #372: with Space Age on, all built-in mods are on.
		"Space Age is on, so all of them are on.", `id="act-built-in-settings">Change Space Age</a>`)
	if strings.Contains(body, `id="act-sync"`) {
		t.Error("the tab offers to match mods that already match")
	}

	g.writeMod(id, "beta", "1.0.0")
	_, body = g.getStatus(modsPath(id))
	wantIn(t, "differing tab", body, "The mods differ from the save", "1 enabled mod is not in the save", `id="act-sync"`)

	// A save Foreman cannot read keeps the offer, worded as before.
	save := filepath.Join(g.worlds.Dirs().Worlds, id, world.SavesDir, "base.zip")
	if err := os.WriteFile(save, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, body = g.getStatus(modsPath(id))
	wantIn(t, "unreadable save", body, "Makes the world&#39;s mods the ones the save", `id="act-sync"`)
	if strings.Contains(body, "The mods differ") || strings.Contains(body, "The mods match") {
		t.Error("an unreadable save is read as matching or not")
	}
}

// #371: a world with only built-in mods has no Mod settings section; a
// refused upload opens its folded form, so the error summary reaches it.
func TestModsTabFoldsTheRareForms(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	_, body := g.getStatus(modsPath(id))
	if strings.Contains(body, `id="settings-heading"`) {
		t.Error("a world without mods offers mod settings")
	}
	wantIn(t, "tab", body, `<details class="disclosure" id="mod-upload">`, "Install a mod from a .zip file you have.")
	rep := g.postMultipart(modsPath(id)+"/upload", [][2]string{{"kind", "upload"}}, "mod", "x.zip", []byte("not a zip"))
	wantIn(t, "refused upload", rep.body, `<details class="disclosure" id="mod-upload" open>`, `id="mod-file-error"`)
}
