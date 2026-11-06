package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cloneForm(name, mapChoice string) url.Values {
	return url.Values{"name": {name}, "map": {mapChoice}}
}

func TestCloneNeedsSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	id, _ := savesWorld(t, hn)
	hn.wantRedirect(hn.get("/worlds/"+id+"/clone"), PathSetup)
	hn.wantRedirect(hn.postLong("/worlds/"+id+"/clone", cloneForm("x", "fresh"), nil), PathSetup)
}

// SPEC 5.7: the form names the save it would keep and when it was saved,
// and suggests a name.
func TestCloneFormNamesTheSaveItKeeps(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	rec := hn.get("/worlds/"+id+"/clone", withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"Clone Family base", `value="Family base (copy)"`, "<code>new.zip</code>", `<time datetime="2026-11-03T13:25:00Z">`,
		`id="clone-keep" name="map" value="keep" checked`, `id="clone-fresh" name="map" value="fresh"`, "Game version 2.0.72, with Space Age.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q\n%s", want, body)
		}
	}
	if !strings.Contains(hn.get("/worlds/"+id, withCookie(c)).Body.String(), `href="/worlds/`+id+`/clone"`) {
		t.Error("the world page does not link to Clone")
	}
}

func TestCloneFormOfAWorldWithoutASaveOffersOnlyAFreshMap(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := hn.withActive("2.0.77")
	body := hn.get("/worlds/"+id+"/clone", withCookie(c)).Body.String()
	if strings.Contains(body, `type="radio"`) || !strings.Contains(body, "This world has no save to keep.") || !strings.Contains(body, `name="map" value="fresh"`) {
		t.Errorf("no-save form:\n%s", body)
	}
	if rec := hn.get("/worlds/nope/clone", withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown world: %d", rec.Code)
	}
}

// SPEC 5.7: keeping the save copies the one that loads and sets it to load.
func TestCloneKeepingTheSave(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	rec := hn.postLong("/worlds/"+id+"/clone", cloneForm("The copy", "keep"), c)
	nid := idFromLocation(t, rec)
	if nid == id {
		t.Fatal("same world")
	}
	if got, err := os.ReadFile(filepath.Join(hn.worlds.Dirs().Worlds, nid, "saves", "new.zip")); err != nil || string(got) != "data of new.zip" { //nolint:gosec // a test temp file
		t.Errorf("saved copy %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(hn.worlds.Dirs().Worlds, nid, "saves", "old.zip")); err == nil {
		t.Error("a save other than the one that loads was copied")
	}
	if m := hn.meta(nid); m.NewMap != nil || m.LoadSave != "new.zip" || m.GameVersion != "2.0.72" || !m.SpaceAge {
		t.Errorf("meta %+v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, "saves", "new.zip")); err != nil {
		t.Error("the source lost its save")
	}
	if got := hn.audits(AuditClone); len(got) != 1 || got[0] != "alice "+nid+" from "+id+", keep" {
		t.Errorf("audit %q", got)
	}
	page := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String()
	if !strings.Contains(page, "Cloned Family base as The copy.") {
		t.Errorf("no Done message naming both worlds:\n%s", page)
	}
}

// SPEC 5.7: a fresh map has no save and a random seed, made at the first start.
func TestCloneWithAFreshMap(t *testing.T) {
	fixedSeed(t, 2024)
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	nid := idFromLocation(t, hn.postLong("/worlds/"+id+"/clone", cloneForm("Clean", "fresh"), c))
	m := hn.meta(nid)
	if m.NewMap == nil || m.NewMap.Seed == nil || *m.NewMap.Seed != 2024 || m.LoadSave != "" {
		t.Errorf("meta %+v new_map %+v", m, m.NewMap)
	}
	entries, _ := os.ReadDir(filepath.Join(hn.worlds.Dirs().Worlds, nid, "saves"))
	if len(entries) != 0 {
		t.Errorf("%d saves copied", len(entries))
	}
}

func TestCloneRefusals(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	noSave := hn.withActive("2.0.77")
	cases := []struct {
		name string
		path string
		form url.Values
		want string
	}{
		{"blank name", id, cloneForm(" ", "keep"), "Enter a name for the world."},
		{"taken name", id, cloneForm("family BASE", "keep"), "Another world already has this name."},
		{"no choice", id, url.Values{"name": {"x"}}, "Choose whether to keep the save or start a fresh map."},
		{"unknown choice", id, cloneForm("x", "everything"), "Choose whether to keep the save or start a fresh map."},
		{"keep with no save", noSave, cloneForm("x", "keep"), "This world has no save to keep. Start a fresh map instead."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := hn.worldCount()
			rec := hn.postLong("/worlds/"+tc.path+"/clone", tc.form, c)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("status %d, want 400 with %q\n%s", rec.Code, tc.want, rec.Body.String())
			}
			if hn.worldCount() != before {
				t.Error("a world was made")
			}
		})
	}
	// The name the admin typed is kept on the form.
	if body := hn.postLong("/worlds/"+id+"/clone", cloneForm("family base", "keep"), c).Body.String(); !strings.Contains(body, `value="family base"`) {
		t.Error("the typed name was lost")
	}
}

func TestCloneOfAnUnknownWorldIs404(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	if rec := hn.postLong("/worlds/nope/clone", cloneForm("x", "fresh"), c); rec.Code != http.StatusNotFound {
		t.Errorf("status %d", rec.Code)
	}
}

// The Done message after a clone names the source only while it exists, so a made-up link says nothing.
func TestClonedMessageIgnoresMadeUpLinks(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := hn.withActive("2.0.77")
	for _, q := range []string{"?cloned=nope", "?cloned=../x"} {
		if body := hn.get("/worlds/"+id+q, withCookie(c)).Body.String(); strings.Contains(body, "Cloned ") {
			t.Errorf("%s: a message was shown", q)
		}
	}
}
