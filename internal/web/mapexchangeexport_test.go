package web

import (
	"bytes"
	"context"
	"errors"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/mapexchange"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	seededMapGen   = `{"seed":4242,"width":0,"height":0}`
	seededSettings = `{"pollution":{"enabled":false}}`
)

// putSave writes a save into a world's saves folder.
func putSave(t *testing.T, hn *harness, id, name string, data []byte) string {
	t.Helper()
	dir := filepath.Join(hn.worlds.Dirs().Worlds, id, world.SavesDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// generated makes a world the way Generate does, with the given map files.
func generated(t *testing.T, hn *harness, name, mapGen string) string {
	t.Helper()
	id, err := hn.worlds.Generate(context.Background(), world.NewWorld{Name: name, GameVersion: "2.0.77", SpaceAge: true,
		MapGenSettings: []byte(mapGen), MapSettings: []byte(seededSettings)})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// textarea returns the text of the page's string field, unescaped, or "".
func textarea(page, id string) string {
	_, rest, ok := strings.Cut(page, `id="`+id+`-value"`)
	if !ok {
		return ""
	}
	_, rest, _ = strings.Cut(rest, ">")
	text, _, _ := strings.Cut(rest, "</textarea>")
	return html.UnescapeString(text)
}

func TestWorldPageOffersTheStringFromItsMapFiles(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := generated(t, hn, "Seeded", seededMapGen)
	want, err := mapexchange.Encode("2.0.77", []byte(seededMapGen), []byte(seededSettings))
	if err != nil {
		t.Fatal(err)
	}

	page := hn.get(worldPath(id), withCookie(c)).Body.String()
	if got := textarea(page, "exchange"); got != want {
		t.Errorf("field holds %q, want %q", got, want)
	}
	for _, need := range []string{`readonly`, `data-copy-from="exchange-value"`, "map settings only, not mods"} {
		if !strings.Contains(page, need) {
			t.Errorf("page lacks %q", need)
		}
	}
	if strings.Contains(page, `id="exchange-copy" disabled`) {
		t.Error("the copy button is disabled though there is a string")
	}
	// The string is never in an address.
	if strings.Contains(page, "href=\""+worldPath(id)+"?") && strings.Contains(page, "%3E%3E%3E") {
		t.Error("the string reached an address")
	}
}

func TestImportedWorldPageOffersTheStringFromItsSave(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	data := fixtureBytes(t, "2.0.77-space-age.zip")
	hn.wantRedirect(hn.postImportUpload(c, map[string]string{"name": "Imported"}, "a.zip", data), "/worlds/imported?imported=1")
	want, err := savefile.ExchangeString(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	page := hn.get(worldPath("imported"), withCookie(c)).Body.String()
	if got := textarea(page, "exchange"); got != want {
		t.Errorf("field holds %q, want %q", got, want)
	}
}

func TestWorldWithARandomSeedFallsBackToItsSave(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := generated(t, hn, "Random", `{"seed":null,"width":0}`)

	// With no save, the reason names the random seed and the button is off.
	page := hn.get(worldPath(id), withCookie(c)).Body.String()
	if textarea(page, "exchange") != "" || !strings.Contains(page, "seed is left to chance") ||
		!strings.Contains(page, `id="exchange-copy" disabled aria-describedby="exchange-reason"`) {
		t.Errorf("no save: want a disabled button with the reason:\n%s", page)
	}

	data := fixtureBytes(t, "2.0.77-space-age.zip")
	putSave(t, hn, id, "a.zip", data)
	want, err := savefile.ExchangeString(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	page = hn.get(worldPath(id), withCookie(c)).Body.String()
	if got := textarea(page, "exchange"); got != want {
		t.Errorf("with a save the field holds %q, want %q", got, want)
	}
}

func TestWorldPageSaysWhyThereIsNoString(t *testing.T) {
	cases := []struct {
		name string
		save func(t *testing.T) []byte
		want string
	}{
		{"no level-init", saveZip, "holds no map settings that Foreman can read"},
		{"not a zip", func(*testing.T) []byte { return []byte("not a zip") }, "could not read the world's newest save"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hn := newHarness(t, nil)
			c := hn.signIn()
			id := generated(t, hn, "Odd", `{"seed":null}`)
			putSave(t, hn, id, "a.zip", tc.save(t))
			page := html.UnescapeString(hn.get(worldPath(id), withCookie(c)).Body.String())
			if textarea(page, "exchange") != "" || !strings.Contains(page, tc.want) || !strings.Contains(page, `id="exchange-copy" disabled`) {
				t.Errorf("want a disabled button and %q:\n%s", tc.want, page)
			}
		})
	}
}

func TestWorldWithNoMapAtAllSaysSo(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, err := hn.worlds.Create(context.Background(), "Bare", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	page := hn.get(worldPath(id), withCookie(c)).Body.String()
	if !strings.Contains(page, "no map settings files and no save yet") || !strings.Contains(page, `id="exchange-copy" disabled`) {
		t.Errorf("want the no-map reason:\n%s", page)
	}
}

func TestFavoritesListOffersTheStringFromStoredMapFiles(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	seeded, err := favs.Make(context.Background(), generated(t, hn, "Seeded", seededMapGen), "Seeded start")
	if err != nil {
		t.Fatal(err)
	}
	random, err := favs.Make(context.Background(), generated(t, hn, "Random", `{"width":0}`), "Random start")
	if err != nil {
		t.Fatal(err)
	}
	bare, err := favs.Make(context.Background(), favSourceNoMap(t, hn), "Bare start")
	if err != nil {
		t.Fatal(err)
	}
	want, err := mapexchange.Encode("2.0.77", []byte(seededMapGen), []byte(seededSettings))
	if err != nil {
		t.Fatal(err)
	}
	page := hn.get(PathFavorites, withCookie(c)).Body.String()
	if got := textarea(page, "exchange-"+seeded); got != want {
		t.Errorf("seeded favorite holds %q, want %q", got, want)
	}
	for _, id := range []string{random, bare} {
		if textarea(page, "exchange-"+id) != "" || !strings.Contains(page, `id="exchange-`+id+`-copy" disabled`) {
			t.Errorf("favorite %s: want a disabled button", id)
		}
	}
	for _, need := range []string{"seed is left to chance", "keeps no map settings"} {
		if !strings.Contains(page, need) {
			t.Errorf("favorites page lacks %q", need)
		}
	}
}

func favSourceNoMap(t *testing.T, hn *harness) string {
	t.Helper()
	id, err := hn.worlds.Create(context.Background(), "Plain", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// A save's string is cached by file, path, size and time, so a page view
// does not read the save again (SPEC 5.6).
func TestSaveStringIsCachedPerFile(t *testing.T) {
	hn := newHarness(t, nil)
	data := fixtureBytes(t, "2.0.77-space-age.zip")
	id := generated(t, hn, "Cached", `{"seed":null}`)
	path := putSave(t, hn, id, "a.zip", data)
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	save, found, err := hn.worlds.NewestSave(id)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	c := newSaveExchanges(2)
	first, err := c.fromSave(context.Background(), hn.worlds, id, save)
	if err != nil || first == "" {
		t.Fatalf("first read: %q, %v", first, err)
	}
	// Same size and time, different bytes: only a cache could still answer.
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, len(data)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	if again, err := c.fromSave(context.Background(), hn.worlds, id, save); err != nil || again != first {
		t.Errorf("second read: %q, %v; want the cached string", again, err)
	}
	// A new time is a new file: it is read again, and now fails.
	later := at.Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	save.ModTime = later
	if _, err := c.fromSave(context.Background(), hn.worlds, id, save); !errors.Is(err, savefile.ErrNotZip) {
		t.Errorf("after a change: %v, want ErrNotZip", err)
	}
}

// At most maxSaveReads reads run at once; a request waits for a slot and
// gives up when it is canceled.
func TestSaveReadsWaitForASlot(t *testing.T) {
	hn := newHarness(t, nil)
	id := generated(t, hn, "Busy", `{"seed":null}`)
	putSave(t, hn, id, "a.zip", fixtureBytes(t, "2.0.77-space-age.zip"))
	save, _, err := hn.worlds.NewestSave(id)
	if err != nil {
		t.Fatal(err)
	}
	c := newSaveExchanges(maxSaveReads)
	for range maxSaveReads {
		c.slots <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.fromSave(ctx, hn.worlds, id, save); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("with every slot taken: %v, want the request's deadline", err)
	}
	<-c.slots
	if got, err := c.fromSave(context.Background(), hn.worlds, id, save); err != nil || got == "" {
		t.Errorf("with a slot free: %q, %v", got, err)
	}
	if len(c.slots) != 1 {
		t.Errorf("a read left %d slots taken, want only the one the test holds", len(c.slots))
	}
}

func TestExchangeReasons(t *testing.T) {
	cases := []struct {
		err   error
		key   string
		known bool
	}{
		{mapexchange.ErrNoSeed, "mapgen.exchange.reason.no_seed", true},
		{savefile.ErrNoLevelInit, "mapgen.exchange.reason.no_settings", true},
		{savefile.ErrNoMapSettings, "mapgen.exchange.reason.no_settings", true},
		{&savefile.UnsupportedVersionError{Version: savefile.Version{Major: 0, Minor: 17, Patch: 79}}, "mapgen.exchange.reason.unsupported", true},
		{&mapexchange.UnsupportedError{Version: [4]uint16{3, 0, 1, 0}}, "mapgen.exchange.reason.unsupported", true},
		{savefile.ErrNotZip, "mapgen.exchange.reason.unreadable", true},
		{savefile.ErrTooLarge, "mapgen.exchange.reason.unreadable", true},
		{savefile.ErrTruncated, "mapgen.exchange.reason.unreadable", true},
		{errors.New("disk on fire"), "mapgen.exchange.reason.failed", false},
	}
	for _, tc := range cases {
		reason, known := exchangeReason(tc.err)
		if reason.Key != tc.key || known != tc.known {
			t.Errorf("%v: got %q, %v; want %q, %v", tc.err, reason.Key, known, tc.key, tc.known)
		}
	}
}
