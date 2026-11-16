package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/mods"
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
		`<legend><a href="https://mods.factorio.com/mod/fmtest" class="mod-title" target="_blank" rel="noopener noreferrer"><span translate="no">Test &lt;b&gt;Mod&lt;/b&gt;</span>`,
		`<legend><span class="mod-title" translate="no">Local Thing</span></legend>`)
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
	wantIn(t, "form", body, `<legend><a href="https://mods.factorio.com/mod/fmtest" class="mod-title" target="_blank" rel="noopener noreferrer"><span class="ident" translate="no">fmtest</span>`)
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
