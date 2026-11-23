package web

import (
	"context"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 9.15: the Server page's notice names a gone mod by its title, in the
// warning and in its fix's button.
func TestServerModNoticeNamesTheModByTitle(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("beta", "2.0.0")
	id := g.world20("Base")
	g.writeTitledMod(id, "beta", "1.0.0", "Beta Pack")
	_, frag := g.getStatus(modsPath(id) + "/notice")
	wantIn(t, "notice", plain(frag), "Beta Pack 1.0.0 is gone from the mod portal", "Update Beta Pack")
	if strings.Contains(plain(frag), "beta 1.0.0") {
		t.Errorf("the notice names the mod by its name: %s", plain(frag))
	}
}

// SPEC 9.15, 17.9: a mod change's result on the Mods tab of a world that is
// not active names a mod by its title.
func TestModsTabResultNamesModsByTitle(t *testing.T) {
	g := newModsRig(t, true)
	g.world20("Active")
	id := g.world20("Other")
	g.writeTitledMod(id, "alpha", "1.0.0", "Alpha Pack")
	_, body := g.getStatus(kindPlanPath(id, mods.KindDisable, "alpha"))
	g.wantRedirectTo(modsPath(id)+ownResultQuery, g.post(modsPath(id)+"/plan", planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	wantIn(t, "mods tab", g.get(modsPath(id)+ownResultQuery), "Disabled Alpha Pack in Other.")
}

// DESIGN 7.10: a world with no save offers the Saves tab, which owns the
// upload, as a link ending in a chevron (DESIGN 7.1), and the Saves tab's
// field is a drop zone.
func TestUploadsOfAnotherTabAreChevronLinks(t *testing.T) {
	g := newModsRig(t, true)
	id, err := g.worlds.Create(context.Background(), "Empty", v20, false)
	if err != nil {
		t.Fatal(err)
	}
	_, tab := g.getStatus(modsPath(id))
	wantIn(t, "mods tab", tab, `<a href="/worlds/`+id+`/saves" class="link-tab" id="act-upload-save">Upload a save</a>`)
	saves := g.get(worldPath(id) + "/saves")
	wantIn(t, "saves tab", saves, `data-dropzone`, `id="save-file" name="save" type="file" accept=".zip"`, "A save never replaces one with the same name.", `src="/static/dropzone.`)
}

// DESIGN 7.10: a field's extra description (the settings help paragraph)
// reads after the limits hint and before a server error.
func TestDropZoneDescribedByReadsBetweenHintAndError(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "alpha_1.0.0")
	_, tab := g.getStatus(modsPath(id))
	wantIn(t, "mods tab", tab, `aria-describedby="settings-file-hint settings-file-help"`, `id="settings-file-help"`, `aria-describedby="mod-file-hint"`)

	// A refused upload adds its error id after the description.
	w := world.Summary{ID: "base", Name: "Base", GameVersion: v20, Active: true}
	_, body := renderIn(t, newTestRenderer(t), pageCase{"mods", "todd", 200, ModsPage{World: w, Mods: []world.InstalledMod{{Name: "alpha", Version: "1.0.0", Enabled: true}},
		HasSettings: true, SettingsError: i18n.M("worlds.upload.not_zip")}}, "en")
	wantIn(t, "refused", body, `aria-describedby="settings-file-hint settings-file-help settings-file-error"`)
}

// DESIGN 7.10: the empty Worlds list offers the Import page's upload as a
// link ending in a chevron, since the result is used there.
func TestEmptyWorldsListOffersTheUploadAsALink(t *testing.T) {
	g := newModsRig(t, true)
	wantIn(t, "worlds", g.get(PathWorlds), `<a href="/import#upload-heading" class="link-tab" id="act-upload-save">Upload a save</a>`)
	g.world20("Base")
	if strings.Contains(g.get(PathWorlds), `id="act-upload-save"`) {
		t.Error("a list with a world offers the empty state's link")
	}
}

// DESIGN 7.10: the hint, the script's message and the server's refusal give
// one figure for a limit: whole binary units, as the spec words them.
func TestUploadLimitsAreOneFigure(t *testing.T) {
	for _, c := range []struct {
		name  string
		bytes uint64
		want  string
	}{
		{"mod zip", uint64(modUploadMB) << 20, "1 GB"},
		{"mod-settings.dat", MaxSettingsUpload, "4 MB"},
		{"save", MaxSaveUpload, "1 GB"},
		{"favorite", world.MaxFavoriteUpload, "8 MB"},
	} {
		z := DropZone{MaxBytes: c.bytes}
		if got := english(z.Limit()); got != c.want {
			t.Errorf("%s: the zone says %q, want %q", c.name, got, c.want)
		}
		if got := tooLarge(int64(c.bytes)).Error(); !strings.HasSuffix(got, "limit of "+c.want+".") { //nolint:gosec // a small constant
			t.Errorf("%s: the server says %q, want the figure %q", c.name, got, c.want)
		}
	}
}

// DESIGN 7.10: the zone's hint and the script's too-big message read the
// same limit as the server's, and the settings label keeps its file name in
// code style.
func TestModsTabUploadHintsAndLabel(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "alpha_1.0.0")
	_, tab := g.getStatus(modsPath(id))
	wantIn(t, "mods tab", tab, `data-limit="1 GB"`, `data-limit="4 MB"`,
		`New <code>mod-settings.dat</code></label>`)
}
