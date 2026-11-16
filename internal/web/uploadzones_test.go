package web

import (
	"context"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
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
// upload, as a button, and the Saves tab's field is a drop zone.
func TestUploadsOfAnotherTabAreButtons(t *testing.T) {
	g := newModsRig(t, true)
	id, err := g.worlds.Create(context.Background(), "Empty", v20, false)
	if err != nil {
		t.Fatal(err)
	}
	_, tab := g.getStatus(modsPath(id))
	wantIn(t, "mods tab", tab, `<a href="/worlds/`+id+`/saves" class="button" id="act-upload-save">Upload a save</a>`)
	saves := g.get(worldPath(id) + "/saves")
	wantIn(t, "saves tab", saves, `data-dropzone`, `id="save-file" name="save" type="file" accept=".zip"`, "A save never replaces one with the same name.", `src="/static/dropzone.`)
}
