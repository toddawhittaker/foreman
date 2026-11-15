package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// newFavoritesHarness is a Generate harness that offers favorites.
func newFavoritesHarness(t *testing.T) (*harness, *world.Favorites) {
	t.Helper()
	base := newGenerateHarness(t, nil)
	favs, err := world.NewFavorites(filepath.Join(t.TempDir(), "favorites"), base.worlds, base.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Deps{Config: base.cfg, Store: base.store, Worlds: base.worlds, Favorites: favs, Log: base.slog, Now: base.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	base.h = h
	return base, favs
}

const favSecrets = `{"name":"Club","game_password":"hunter2-game","token":"tok-abc123","max_players":8}` //nolint:gosec // fake secrets the test looks for

// favSource makes a world with map files, secret server settings, mod
// settings, two portal mods and a folder mod.
func favSource(t *testing.T, hn *harness) string {
	t.Helper()
	id, err := hn.worlds.Generate(context.Background(), world.NewWorld{Name: "Source", GameVersion: "2.0.77", SpaceAge: true,
		MapGenSettings: []byte(`{"width":0,"height":0}`), MapSettings: []byte(`{"pollution":{"enabled":false}}`)})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(hn.worlds.Dirs().Worlds, id)
	write := func(rel string, data []byte) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(world.ConfigDir, world.ServerSettingsFile), []byte(favSecrets))
	write(filepath.Join(world.ModsDir, world.ModSettingsFile), []byte("modsettings-bytes"))
	write(filepath.Join(world.ModsDir, "Krastorio2_1.3.24.zip"), modZip(t, "Krastorio2", "1.3.24"))
	write(filepath.Join(world.ModsDir, "flib_0.12.9.zip"), modZip(t, "flib", "0.12.9"))
	write(filepath.Join(world.ModsDir, "mylocal", "info.json"), []byte(`{"name":"mylocal","version":"0.1.0"}`))
	return id
}

func TestMakeFavoriteFromWorldPage(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	id := favSource(t, hn)

	page := hn.get(worldPath(id), withCookie(c)).Body.String()
	for _, want := range []string{`action="` + favoriteMakePath(id) + `"`, `name="favorite_name"`, `value="Source"`} {
		if !strings.Contains(page, want) {
			t.Errorf("world page lacks %q", want)
		}
	}
	rec := hn.post(favoriteMakePath(id), url.Values{"favorite_name": {"Club start"}}, withCookie(c))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	list, err := favs.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("favorites %v, %v", list, err)
	}
	if want := worldPath(id) + "?favorite=" + list[0].ID; loc != want {
		t.Fatalf("redirect %q, want %q", loc, want)
	}
	done := html.UnescapeString(hn.get(loc, withCookie(c)).Body.String())
	for _, want := range []string{`Made the favorite "Club start".`, "1 mod is left out", "mylocal", `href="/favorites"`} {
		if !strings.Contains(done, want) {
			t.Errorf("confirmation lacks %q", want)
		}
	}
	if strings.Contains(done, "default map") {
		t.Error("a world with map settings is said to get the default map")
	}
	// A made-up id puts no words on the page.
	if strings.Contains(hn.get(worldPath(id)+"?favorite=ffffffffffffffff", withCookie(c)).Body.String(), "Made the favorite") {
		t.Error("an unknown favorite id showed a confirmation")
	}
}

func TestImportedWorldFavoriteSaysDefaultMap(t *testing.T) {
	hn, _ := newFavoritesHarness(t)
	c := hn.signIn()
	id, err := hn.worlds.Create(context.Background(), "Imported", "2.0.77", true)
	if err != nil {
		t.Fatal(err)
	}
	if page := hn.get(worldPath(id), withCookie(c)).Body.String(); !strings.Contains(page, "get the default map") {
		t.Error("the form does not say the favorite gets the default map")
	}
}

func TestMakeFavoriteRefusesBadNames(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	id := favSource(t, hn)
	if rec := hn.post(favoriteMakePath(id), url.Values{"favorite_name": {"Taken"}}, withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("first make: %d", rec.Code)
	}
	for name, want := range map[string]string{
		"   ":                    "Enter a name for the favorite.",
		"Taken":                  "Another favorite already has this name.",
		strings.Repeat("x", 101): "Use at most 100 characters",
		"bad\u0007name":          "no control characters",
	} {
		rec := hn.post(favoriteMakePath(id), url.Values{"favorite_name": {name}}, withCookie(c))
		body := html.UnescapeString(rec.Body.String())
		if rec.Code != http.StatusBadRequest || !strings.Contains(body, want) || !strings.Contains(body, `id="favorite-name-error"`) {
			t.Errorf("name %q: status %d, want 400 with %q", name, rec.Code, want)
		}
	}
	if list, _ := favs.List(context.Background()); len(list) != 1 {
		t.Errorf("%d favorites, want 1", len(list))
	}
}

func TestFavoritesListAndDelete(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	if body := hn.get(PathFavorites, withCookie(c)).Body.String(); !strings.Contains(body, `id="favorites-empty"`) {
		t.Error("the empty list says nothing")
	}
	favID, err := favs.Make(context.Background(), favSource(t, hn), "Club start")
	if err != nil {
		t.Fatal(err)
	}
	body := hn.get(PathFavorites, withCookie(c)).Body.String()
	for _, want := range []string{"Club start", "Source", "2.0.77", "<dd>2</dd>", favoriteDeletePath(favID), html.EscapeString(generateFromPath(favID))} {
		if !strings.Contains(body, want) {
			t.Errorf("list lacks %q", want)
		}
	}
	confirm := hn.get(favoriteDeletePath(favID), withCookie(c))
	if confirm.Code != http.StatusOK || !strings.Contains(html.UnescapeString(confirm.Body.String()), `Delete the favorite "Club start"?`) {
		t.Fatalf("confirm page: %d", confirm.Code)
	}
	rec := hn.post(favoriteDeletePath(favID), nil, withCookie(c))
	if rec.Code != http.StatusOK || !strings.Contains(html.UnescapeString(rec.Body.String()), `Deleted the favorite "Club start".`) {
		t.Fatalf("delete: %d", rec.Code)
	}
	if list, _ := favs.List(context.Background()); len(list) != 0 {
		t.Error("the favorite is still there")
	}
	if rec := hn.post(favoriteDeletePath(favID), nil, withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d, want 404", rec.Code)
	}
	if rec := hn.get(favoriteDeletePath("NOT-AN-ID"), withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("bad id: %d, want 404", rec.Code)
	}
}

func TestFavoritePagesNeedSignInAndTheSameOrigin(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	id := favSource(t, hn)
	favID, err := favs.Make(context.Background(), id, "Club start")
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []*httptest.ResponseRecorder{hn.get(PathFavorites), hn.post(favoriteMakePath(id), url.Values{"favorite_name": {"x"}})} {
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, PathSignIn) {
			t.Errorf("signed out: %d to %q", rec.Code, loc)
		}
	}
	evil := withHeader("Origin", "https://evil.example")
	if rec := hn.post(favoriteMakePath(id), url.Values{"favorite_name": {"Evil"}}, withCookie(c), evil); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site make: %d", rec.Code)
	}
	if rec := hn.post(favoriteDeletePath(favID), nil, withCookie(c), evil); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site delete: %d", rec.Code)
	}
	if rec := hn.postLong(PathGenerate, generateForm("favorite", favID), c, evil); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site generate: %d", rec.Code)
	}
	if list, _ := favs.List(context.Background()); len(list) != 1 {
		t.Errorf("%d favorites, want 1", len(list))
	}
}

func TestGenerateFillsInFromAFavorite(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	hn.withActive("2.0.72")
	src, err := hn.worlds.Generate(context.Background(), world.NewWorld{Name: "Rich", GameVersion: "2.0.77", SpaceAge: false, Preset: "rich-resources"})
	if err != nil {
		t.Fatal(err)
	}
	favID, err := favs.Make(context.Background(), src, "Rich start")
	if err != nil {
		t.Fatal(err)
	}
	if body := hn.get(PathGenerate, withCookie(c)).Body.String(); !strings.Contains(body, `id="favorite-chooser"`) || !strings.Contains(body, "Rich start") {
		t.Error("Generate does not offer the favorite")
	}
	body := hn.get(generateFromPath(favID), withCookie(c)).Body.String()
	for _, want := range []string{
		`name="favorite" value="` + favID + `"`, `id="favorite-note"`,
		`name="game_version_typed" type="text" value="2.0.77"`, `<option value="rich-resources" selected`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("filled form lacks %q", want)
		}
	}
	if strings.Contains(body, `id="generate-space-age" name="space_age" aria-describedby="generate-space-age-hint" checked`) {
		t.Error("Space Age is ticked for a favorite without it")
	}
	if rec := hn.get(generateFromPath("ffffffffffffffff"), withCookie(c)); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no longer exists") {
		t.Errorf("unknown favorite: %d", rec.Code)
	}
}

func TestGenerateFromAFavoriteWritesSettingsAndOffersItsMods(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	hn.withActive("2.0.77")
	favID, err := favs.Make(context.Background(), favSource(t, hn), "Club start")
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.postLong(PathGenerate, generateForm("name", "From fav", "favorite", favID), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	id, ok := strings.CutSuffix(strings.TrimPrefix(loc.Path, PathWorlds+"/"), "/mods/plan")
	if !ok {
		t.Fatalf("redirect %q, want the review page", loc)
	}
	q := loc.Query()
	if q.Get("kind") != "install" || strings.Join(q["name"], ",") != "Krastorio2,flib" ||
		q.Get("version-Krastorio2") != "1.3.24" || q.Get("version-flib") != "0.12.9" {
		t.Errorf("review query %v", q)
	}
	if q.Get(generatedQuery) != "1" {
		t.Errorf("review query %v has no %s flag", q, generatedQuery)
	}
	// The favorite's mods are saved as the new world's picks; the pinned
	// versions travel only in the address.
	picks, err := hn.store.ModPlan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range picks {
		names = append(names, m.Name)
	}
	slices.Sort(names)
	if strings.Join(names, ",") != "Krastorio2,flib" {
		t.Errorf("saved picks %v, want the favorite's two mods", names)
	}
	server := string(hn.config(id, world.ServerSettingsFile).Data)
	if !strings.Contains(server, `"max_players"`) || strings.Contains(server, "hunter2") || strings.Contains(server, "tok-abc123") {
		t.Errorf("server settings %s", server)
	}
	ms, err := os.ReadFile(filepath.Join(hn.worlds.Dirs().Worlds, id, world.ModsDir, world.ModSettingsFile)) //nolint:gosec // a test temp folder
	if err != nil || string(ms) != "modsettings-bytes" {
		t.Errorf("mod-settings.dat %q, %v", ms, err)
	}
	// Settings kept as filled: the favorite's own map files.
	if got := string(hn.config(id, world.MapSettingsFile).Data); !strings.Contains(got, `"enabled": false`) {
		t.Errorf("map settings %s, want the favorite's", got)
	}
}

func TestGenerateFromAFavoriteWithoutModsGoesToTheWorld(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	src := hn.withActive("2.0.77")
	favID, err := favs.Make(context.Background(), src, "Plain")
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.postLong(PathGenerate, generateForm("name", "From plain", "favorite", favID), c)
	id := idFromLocation(t, rec)
	if want := worldPath(id) + "?" + generatedQuery + "=1"; rec.Header().Get("Location") != want {
		t.Errorf("redirect %q, want %q", rec.Header().Get("Location"), want)
	}
	// A favorite deleted meanwhile is a form error, and no world is made.
	if err := favs.Delete(favID); err != nil {
		t.Fatal(err)
	}
	n := hn.worldCount()
	if rec := hn.postLong(PathGenerate, generateForm("name", "Too late", "favorite", favID), c); rec.Code != http.StatusBadRequest {
		t.Errorf("deleted favorite: %d", rec.Code)
	}
	if hn.worldCount() != n {
		t.Error("a world was made from a deleted favorite")
	}
}

func TestGenerateFromAFavoriteOnAnotherLineLetsTheReviewPick(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	hn.withActive("2.0.77")
	favID, err := favs.Make(context.Background(), favSource(t, hn), "Club start")
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.postLong(PathGenerate, generateForm("name", "Old line", "favorite", favID,
		"game_version_typed", "1.1.110", "space_age", "", "map_key", "1.1|0"), c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	q := loc.Query()
	if strings.Join(q["name"], ",") != "Krastorio2,flib" || q.Get("version-Krastorio2") != "" || q.Get("version-flib") != "" {
		t.Errorf("review query %v, want names without pinned versions", q)
	}
}
