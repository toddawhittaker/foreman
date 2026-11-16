package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// titledZip is a mod zip whose info.json carries a title.
func titledZip(t *testing.T, name, version, title string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name + "_" + version + "/info.json")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := json.Marshal(map[string]any{"name": name, "version": version, "title": title, "factorio_version": "2.0"})
	_, _ = w.Write(info)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeTitledMod puts a zip with a title into the world's mods folder.
func (g *modsRig) writeTitledMod(id, name, version, title string) {
	g.t.Helper()
	if err := os.MkdirAll(g.modsDir(id), 0o750); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.modsDir(id), name+"_"+version+".zip"), titledZip(g.t, name, version, title), 0o600); err != nil {
		g.t.Fatal(err)
	}
}

// SPEC 9.15, 9.6: the settings page names each mod by its title, linked to
// the portal in a new tab, and by its name when the mod has no title. The
// title is escaped, and a folder mod is not linked.
func TestModSettingsNamesModsByTitle(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base")
	g.writeTitledMod(id, "fmtest", "1.0.0", `Test <b>Mod</b>`)
	folder := filepath.Join(g.modsDir(id), "fmlocal")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "info.json"), []byte(`{"name":"fmlocal","version":"1.0.0","title":"Local Thing"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	defs := fmtestSettingDefs(t)
	local := fmtestDefs()[0]
	local.Name, local.Mod = "fm-local-bool", "fmlocal"
	defs.Defs = append(defs.Defs, local)
	fakeDefs(t, defs, nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	body, _ := g.settingsForm(id)
	wantIn(t, "form", body,
		`<legend><span class="mod-title" translate="no">Test &lt;b&gt;Mod&lt;/b&gt;</span></legend>`,
		`<a href="https://mods.factorio.com/mod/fmtest" class="portal-link" target="_blank" rel="noopener noreferrer">Mod portal<span class="visually-hidden"> for <span class="ident" translate="no">Test &lt;b&gt;Mod&lt;/b&gt;</span>, opens in a new tab</span></a>`,
		`<legend><span class="mod-title" translate="no">Local Thing</span></legend>`)
	if strings.Contains(body, "mods.factorio.com/mod/fmlocal") {
		t.Error("a folder mod is linked")
	}
	if strings.Contains(body, "<b>Mod</b>") {
		t.Error("the title was not escaped")
	}
}

// A mod with no title in its info.json is named by its slug, as before.
func TestModSettingsNamesAModWithoutATitleByItsName(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	body, _ := g.settingsForm(id)
	wantIn(t, "form", body, `<legend><span class="mod-title ident" translate="no">fmtest</span></legend>`, `href="https://mods.factorio.com/mod/fmtest" class="portal-link"`)
}

// SPEC 9.15, 17.9: the Done message and the failed download name the mods
// by title; a mod with none keeps its name; the title is never translated.
func TestModResultSentencesUseTitles(t *testing.T) {
	res := modResult{
		Changes: []mods.Change{change(mods.ActInstall, "fp-lamp", "1.0.0"), change(mods.ActInstall, "fp-plain", "1.0.0")},
		Titles:  map[string]string{"fp-lamp": "Lamp Pack"},
	}
	if got := english(modsDone(res, "Base")); !strings.HasPrefix(got, "Installed Lamp Pack and fp-plain in Base.") {
		t.Errorf("done: %q", got)
	}
	err := &game.ModDownloadError{Mod: "fp-lamp", Version: "1.0.0", Err: modportal.ErrChecksum}
	msg, _ := modsFailure(err, res, "Base", game.StateRunning, PathServer)
	if got := english(msg); !strings.HasPrefix(got, "Lamp Pack 1.0.0 did not match") {
		t.Errorf("failure: %q", got)
	}
}

// SPEC 9.15, 7.4: the pre-start refusal and its fix name a mod by title.
func TestPrecheckNamesModsByTitle(t *testing.T) {
	titles := map[string]string{"fp-gone": "Gone Pack", "fp-off": "Off Pack"}
	one := world.Problem{Kind: world.ProblemModGone, Titles: titles, Gone: []world.GoneMod{{Name: "fp-gone", Version: "1.0.0", Update: "1.1.0", Title: "Gone Pack"}}}
	if got := english(problemMsg(one)); !strings.Contains(got, "no longer has Gone Pack (version 1.0.0)") {
		t.Errorf("gone: %q", got)
	}
	off := world.Problem{Kind: world.ProblemModDisabled, Titles: titles, Mods: []string{"fp-off", "fp-plain"}, Save: "a.zip"}
	if got := english(problemMsg(off)); !strings.Contains(got, "Off Pack, fp-plain") {
		t.Errorf("disabled: %q", got)
	}
	for _, c := range []struct {
		gone world.GoneMod
		want string
	}{
		{world.GoneMod{Name: "fp-gone", Version: "1", Update: "2", Title: "Gone Pack"}, "Update Gone Pack"},
		{world.GoneMod{Name: "fp-gone", Version: "1", Title: "Gone Pack"}, "Remove Gone Pack"},
		{world.GoneMod{Name: "fp-gone", Version: "1", Update: "2"}, "Update fp-gone"},
	} {
		if got := english(goneFix("w1", []world.GoneMod{c.gone}).Label); got != c.want {
			t.Errorf("label %q, want %q", got, c.want)
		}
	}
}

// SPEC 9.15: on the settings page a mod the portal client's cache already
// knows the portal lacks is not linked; the page asks the portal nothing.
func TestModSettingsDoesNotLinkAModThePortalLacks(t *testing.T) {
	var client ModPortal
	g := newModsRigWrapped(t, false, func(p ModPortal) ModPortal { client = p; return p })
	id := g.world20("Base")
	g.writeTitledMod(id, "fmtest", "1.0.0", "Test Mod")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	body, _ := g.settingsForm(id)
	wantIn(t, "before the portal said", body, `<a href="https://mods.factorio.com/mod/fmtest" class="portal-link"`)

	if _, err := client.Mod(t.Context(), "fmtest"); !errors.Is(err, modportal.ErrNotOnPortal) {
		t.Fatalf("the fake portal has the mod, so the test cannot say it lacks it: %v", err)
	}
	body, _ = g.settingsForm(id)
	wantIn(t, "after", body, `<legend><span class="mod-title" translate="no">Test Mod</span></legend>`)
	if strings.Contains(body, "mods.factorio.com/mod/fmtest") {
		t.Error("a mod the portal lacks is linked")
	}
}
