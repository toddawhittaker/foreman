package web

import (
	"archive/zip"
	"bytes"
	"context"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// savesWorld makes a world with two saves and returns its id and folder.
func savesWorld(t *testing.T, hn *harness) (id, dir string) {
	t.Helper()
	id, err := hn.worlds.Create(context.Background(), "Family base", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(hn.worlds.Dirs().Worlds, id)
	for name, age := range map[string]time.Duration{"old.zip": 2 * time.Hour, "new.zip": time.Hour} {
		p := filepath.Join(dir, "saves", name)
		if err := os.WriteFile(p, []byte("data of "+name), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC).Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return id, dir
}

func zipBytes(t *testing.T, entries map[string]int) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, size := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(bytes.Repeat([]byte("a"), size)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// deadlineSpy lets the long routes set their deadlines, which a plain
// recorder refuses, and counts every change.
type deadlineSpy struct {
	*httptest.ResponseRecorder
	lifted int
}

func (d *deadlineSpy) SetReadDeadline(time.Time) error  { d.lifted++; return nil }
func (d *deadlineSpy) SetWriteDeadline(time.Time) error { d.lifted++; return nil }

func (hn *harness) serveLong(r *http.Request) *httptest.ResponseRecorder {
	hn.t.Helper()
	r.RemoteAddr = peerUntrusted
	spy := &deadlineSpy{ResponseRecorder: httptest.NewRecorder()}
	hn.h.ServeHTTP(spy, r)
	return spy.ResponseRecorder
}

func (hn *harness) getLong(path string, c *http.Cookie) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if c != nil {
		r.AddCookie(c)
	}
	return hn.serveLong(r)
}

func (hn *harness) uploadSave(c *http.Cookie, id, filename string, content []byte) *httptest.ResponseRecorder {
	hn.t.Helper()
	body, ct := multipartBody(hn.t, "save", filename, content)
	r := httptest.NewRequest(http.MethodPost, "/worlds/"+id+"/saves", body)
	r.Header.Set("Content-Type", ct)
	r.AddCookie(c)
	return hn.serveLong(r)
}

func (hn *harness) chooseSave(c *http.Cookie, id string, form url.Values) *httptest.ResponseRecorder {
	hn.t.Helper()
	return hn.post("/worlds/"+id+"/load-save", form, withCookie(c))
}

func TestSavesNeedSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	id, _ := savesWorld(t, hn)
	hn.wantRedirect(hn.get("/worlds/"+id+"/saves"), PathSetup)
	hn.wantRedirect(hn.getLong("/worlds/"+id+"/saves/new.zip", nil), PathSetup)
	hn.wantRedirect(hn.post("/worlds/"+id+"/load-save", url.Values{"load": {""}}), PathSetup)
	r := httptest.NewRequest(http.MethodPost, "/worlds/"+id+"/saves", strings.NewReader("x"))
	hn.wantRedirect(hn.serveLong(r), PathSetup)
}

func TestSavesPageListsNewestFirst(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)

	rec := hn.get("/worlds/"+id+"/saves", withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	n, o := strings.Index(body, ">new.zip<"), strings.Index(body, ">old.zip<")
	if n < 0 || o < 0 || n > o {
		t.Errorf("want new.zip before old.zip: %d, %d", n, o)
	}
	for _, want := range []string{
		`href="/worlds/` + id + `"`,               // back to the world
		`href="/worlds/` + id + `/saves/new.zip"`, // download link
		`<span class="nowrap">2026-11-03</span> <span class="nowrap">13:25 UTC</span>`, "Loads next", "15 B",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Count(body, "Loads next") != 1 {
		t.Error("want exactly one row marked as loading next")
	}

	if rec := hn.get("/worlds/nope/saves", withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("missing world: %d", rec.Code)
	}
}

func TestSavesPageShowsOlderAfterFifty(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	for i := range 52 {
		p := filepath.Join(dir, "saves", "bulk"+string(rune('a'+i/26))+string(rune('a'+i%26))+".zip")
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	if got := strings.Count(body, `<a href="/worlds/`+id+`/saves/`); got != 50 {
		t.Errorf("shows %d rows, want 50", got)
	}
	if !strings.Contains(body, "Show older saves") {
		t.Error("no Show older link")
	}
	body = hn.get("/worlds/"+id+"/saves?all=1", withCookie(c)).Body.String()
	if got := strings.Count(body, `<a href="/worlds/`+id+`/saves/`); got != 54 {
		t.Errorf("shows %d rows with all, want 54", got)
	}
	if strings.Contains(body, "Show older saves") {
		t.Error("Show older link on the full list")
	}
}

func TestSaveDownload(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)

	rec := hn.getLong("/worlds/"+id+"/saves/new.zip", c)
	if rec.Code != http.StatusOK || rec.Body.String() != "data of new.zip" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if h.Get("Content-Disposition") != `attachment; filename=new.zip` || h.Get("Content-Type") != "application/zip" {
		t.Errorf("headers: %v", h)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("no nosniff")
	}
	for _, p := range []string{"missing.zip", "..%2Fworld.json", ".hidden.zip", "world.json", "a%2Fb.zip"} {
		if rec := hn.getLong("/worlds/"+id+"/saves/"+p, c); rec.Code != http.StatusNotFound {
			t.Errorf("download %q: %d", p, rec.Code)
		}
	}
	if rec := hn.getLong("/worlds/nope/saves/new.zip", c); rec.Code != http.StatusNotFound {
		t.Errorf("missing world: %d", rec.Code)
	}
}

func TestSaveUpload(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	good := zipBytes(t, map[string]int{"level/level.dat0": 100})

	rec := hn.uploadSave(c, id, "fresh.zip", good)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/worlds/"+id+"/saves?uploaded=fresh.zip" {
		t.Fatalf("upload: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	got, err := os.ReadFile(filepath.Join(dir, "saves", "fresh.zip")) //nolint:gosec // a test temp file
	if err != nil || !bytes.Equal(got, good) {
		t.Fatalf("saved file: %v", err)
	}
	body := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String()
	if !strings.Contains(body, "Uploaded") || !strings.Contains(body, "fresh.zip") {
		t.Error("no confirmation after upload")
	}

	// Refusals change nothing: no new file and no temp file.
	before := dirList(t, filepath.Join(dir, "saves"))
	for _, tc := range []struct {
		what, name string
		content    []byte
		status     int
		msg        string
	}{
		{"duplicate", "fresh.zip", good, http.StatusConflict, "already has a save"},
		{"not a zip", "bad.zip", []byte("hello"), http.StatusUnprocessableEntity, "not a zip"},
		{"unsafe entry", "evil.zip", zipBytes(t, map[string]int{"../evil": 1}), http.StatusUnprocessableEntity, "unsafe name"},
		{"zip bomb", "bomb.zip", zipBytes(t, map[string]int{"a": 1 << 20}), http.StatusUnprocessableEntity, "would unpack to more than"},
		{"wrong extension", "save.txt", good, http.StatusUnprocessableEntity, "must end in .zip"},
		{"hidden", ".hidden.zip", good, http.StatusUnprocessableEntity, "must end in .zip"},
	} {
		rec := hn.uploadSave(c, id, tc.name, tc.content)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.msg) {
			t.Errorf("%s: %d, want %d with %q", tc.what, rec.Code, tc.status, tc.msg)
		}
		if !strings.Contains(rec.Body.String(), `href="#save-file"`) {
			t.Errorf("%s: no error summary link to the field", tc.what)
		}
	}
	if after := dirList(t, filepath.Join(dir, "saves")); !equalStrings(before, after) {
		t.Errorf("saves folder changed: %v to %v", before, after)
	}
	got, _ = os.ReadFile(filepath.Join(dir, "saves", "fresh.zip")) //nolint:gosec // a test temp file
	if !bytes.Equal(got, good) {
		t.Error("an existing save was overwritten")
	}

	// A browser on Windows sends a full path; the folder part is dropped,
	// and a path trick never leaves the saves folder.
	rec = hn.uploadSave(c, id, `C:\games\..\..\trick.zip`, good)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("path name: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "saves", "trick.zip")); err != nil {
		t.Errorf("trick.zip not in saves: %v", err)
	}

	if rec := hn.uploadSave(c, "nope", "a.zip", good); rec.Code != http.StatusNotFound {
		t.Errorf("missing world: %d", rec.Code)
	}
}

func dirList(t *testing.T, dir string) []string {
	t.Helper()
	return dirNames(t, dir)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSaveUploadRefusedForNewerWorld(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	if err := os.WriteFile(filepath.Join(dir, "world.json"), []byte(`{"format_version": 9, "name": "Future"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := hn.uploadSave(c, id, "a.zip", zipBytes(t, map[string]int{"x": 1}))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "newer version of Foreman") {
		t.Errorf("upload: %d", rec.Code)
	}
	rec = hn.chooseSave(c, id, url.Values{"load": {"new.zip"}})
	if rec.Code != http.StatusConflict {
		t.Errorf("choose: %d", rec.Code)
	}
	// The page still lists and links the saves, with no forms.
	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	if !strings.Contains(body, "new.zip") || strings.Contains(body, "<form method=\"post\" action=\"/worlds/"+id) {
		t.Error("newer world page wrong")
	}
	// With nothing to change, the table has no button column or header.
	if got := strings.Count(body, "<th "); got != 3 {
		t.Errorf("a newer world's table has %d headers, want 3", got)
	}
	if row := saveRow(t, body, "new.zip"); strings.Count(row, "<td") != 3 || strings.Contains(row, "<button") {
		t.Errorf("a newer world's row has a button cell:\n%s", row)
	}
}

// saveRow returns the table row that holds the named save's link.
func saveRow(t *testing.T, body, name string) string {
	t.Helper()
	i := strings.Index(body, ">"+name+"</a>")
	if i < 0 {
		t.Fatalf("no row for %s", name)
	}
	start := strings.LastIndex(body[:i], "<tr")
	end := i + strings.Index(body[i:], "</tr>")
	return body[start:end]
}

// loadButton is the start of a row's "Load next" button, with the value it posts.
func loadButton(value string) string {
	return `<button type="submit" class="button" name="load" value="` + value + `">`
}

// loadSaveOf reads a world's chosen save from its world.json.
func (hn *harness) loadSaveOf(id string) string {
	hn.t.Helper()
	list, err := hn.worlds.Saves(id)
	if err != nil {
		hn.t.Fatal(err)
	}
	return list.LoadSave
}

// SPEC 7.11, DESIGN 8.3: each save's row says whether it loads next, and
// every other row has one button that makes it the one that loads. The
// newest row's button chooses "the newest save", so later autosaves win.
func TestChooseSave(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	loads := i18n.English.T("worlds.saves.loads_next")

	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	if strings.Contains(body, `type="radio"`) {
		t.Error("the page still has a radio list")
	}
	if row := saveRow(t, body, "new.zip"); !strings.Contains(row, loads) || strings.Contains(row, "<button") {
		t.Errorf("newest row should load next with no button:\n%s", row)
	}
	if row := saveRow(t, body, "old.zip"); strings.Contains(row, loads) || !strings.Contains(row, loadButton("old.zip")) ||
		!strings.Contains(row, "Load next: old.zip") {
		t.Errorf("older row should offer Load next:\n%s", row)
	}

	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {"old.zip"}}), "/worlds/"+id+"/saves?chosen=1")
	body = hn.get("/worlds/"+id+"/saves?chosen=1", withCookie(c)).Body.String()
	wantIn(t, "chosen page", body, "The server will load <code>old.zip</code> the next time it starts.")
	if row := saveRow(t, body, "old.zip"); !strings.Contains(row, loads) || strings.Contains(row, "<button") {
		t.Errorf("chosen row should load next with no button:\n%s", row)
	}
	if row := saveRow(t, body, "new.zip"); strings.Contains(row, loads) || !strings.Contains(row, loadButton("")) {
		t.Errorf("newest row should offer the newest save:\n%s", row)
	}
	// It loads the newest save, whichever file that is then, so it is named for that, not its file (SPEC 7.11).
	if row := saveRow(t, body, "new.zip"); !strings.Contains(row, "Load next: the newest save") || strings.Contains(row, "Load next: new.zip") {
		t.Errorf("newest row's button should be named for the newest save:\n%s", row)
	}
	if got := hn.loadSaveOf(id); got != "old.zip" {
		t.Errorf("load_save %q", got)
	}

	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {""}}), "/worlds/"+id+"/saves?chosen=1")
	body = hn.get("/worlds/"+id+"/saves?chosen=1", withCookie(c)).Body.String()
	wantIn(t, "newest page", body, i18n.English.T("worlds.saves.chosen_newest"))
	if row := saveRow(t, body, "new.zip"); !strings.Contains(row, loads) {
		t.Error("newest not loading next after the reset")
	}
	if got := hn.loadSaveOf(id); got != "" {
		t.Errorf("load_save %q after the reset", got)
	}

	// The posted name is checked against the world's saves on the server.
	for name, form := range map[string]url.Values{
		"unknown": {"load": {"nope.zip"}},
		"escape":  {"load": {"../world.json"}},
		"none":    {},
	} {
		rec := hn.chooseSave(c, id, form)
		body := rec.Body.String()
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `href="#load-save-error"`) ||
			!strings.Contains(body, `id="load-save-error" tabindex="-1"`) {
			t.Errorf("%s: %d", name, rec.Code)
		}
		if got := hn.loadSaveOf(id); got != "" {
			t.Errorf("%s: load_save changed to %q", name, got)
		}
	}
	if rec := hn.chooseSave(c, "nope", url.Values{"load": {""}}); rec.Code != http.StatusNotFound {
		t.Errorf("missing world: %d", rec.Code)
	}
}

func TestSavePageWhenChosenSaveVanished(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {"old.zip"}}), "/worlds/"+id+"/saves?chosen=1")
	if err := os.Remove(filepath.Join(dir, "saves", "old.zip")); err != nil {
		t.Fatal(err)
	}
	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	if !strings.Contains(body, "no longer in the saves folder") {
		t.Error("missing chosen save not explained")
	}
	if row := saveRow(t, body, "new.zip"); !strings.Contains(row, i18n.English.T("worlds.saves.loads_next")) {
		t.Error("the newest save should load next")
	}
	// The stale choice must be clearable: the newest row posts "the newest save".
	if row := saveRow(t, body, "new.zip"); !strings.Contains(row, loadButton("")) {
		t.Errorf("newest row should offer to clear the missing choice:\n%s", row)
	}
	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {""}}), "/worlds/"+id+"/saves?chosen=1")
	if got := hn.loadSaveOf(id); got != "" {
		t.Errorf("load_save %q after clearing", got)
	}
}

// SPEC 7.11: when the chosen save is the newest file, its row shows the badge
// and still offers the button, so the choice can be cleared.
func TestSavePageWhenChosenSaveIsNewest(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {"new.zip"}}), "/worlds/"+id+"/saves?chosen=1")
	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	row := saveRow(t, body, "new.zip")
	if !strings.Contains(row, i18n.English.T("worlds.saves.loads_next")) || !strings.Contains(row, loadButton("")) {
		t.Errorf("newest chosen row should show the badge and the button:\n%s", row)
	}
}

// SPEC 7.11, 17.7: the Server page names the chosen save only when an older
// one is chosen and its file is still there. With no choice, the newest
// chosen, or the chosen file gone, the newest loads and nothing is named.
func TestLoadsNext(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	s := &server{worlds: hn.worlds}

	if got := s.loadsNext(id); got != "" {
		t.Errorf("no choice: %q", got)
	}
	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {"old.zip"}}), "/worlds/"+id+"/saves?chosen=1")
	if got := s.loadsNext(id); got != "old.zip" {
		t.Errorf("older chosen: %q", got)
	}
	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {"new.zip"}}), "/worlds/"+id+"/saves?chosen=1")
	if got := s.loadsNext(id); got != "" {
		t.Errorf("newest chosen by name: %q", got)
	}
	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {"old.zip"}}), "/worlds/"+id+"/saves?chosen=1")
	if err := os.Remove(filepath.Join(dir, "saves", "old.zip")); err != nil {
		t.Fatal(err)
	}
	if got := s.loadsNext(id); got != "" {
		t.Errorf("chosen file gone: %q", got)
	}
	if got := s.loadsNext("nope"); got != "" {
		t.Errorf("unknown world: %q", got)
	}
}

// DESIGN 8.3: the Load next buttons share one column, after Saved, so they
// line up whatever the names' lengths, and the column has a header.
func TestSavesButtonsHaveTheirOwnColumn(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	wantIn(t, "header", body, `<th scope="col" class="visually-hidden" role="columnheader">Load next</th></tr>`)
	row := saveRow(t, body, "old.zip")
	nameCell := row[:strings.Index(row, "</td>")]
	if strings.Contains(nameCell, "<button") {
		t.Errorf("the button is still in the name cell:\n%s", nameCell)
	}
	if !strings.HasSuffix(strings.TrimSpace(row[:strings.LastIndex(row, "</td>")]), "</button>") {
		t.Errorf("the button is not in the row's last cell:\n%s", row)
	}
}

// DESIGN 8.3: the button cell has its own class, and a row with no button has
// a truly empty cell, so the phone layout can collapse it; a world a newer
// Foreman wrote has no button cells at all.
func TestSavesButtonCell(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	if row := saveRow(t, body, "new.zip"); !strings.Contains(row, `<td role="cell" class="save-load" data-label=""></td>`) {
		t.Errorf("the newest row's button cell is not empty:\n%s", row)
	}
	if row := saveRow(t, body, "old.zip"); !strings.Contains(row, `<td role="cell" class="save-load" data-label=""><button `) {
		t.Errorf("the older row's button cell:\n%s", row)
	}
	if err := os.WriteFile(filepath.Join(dir, "world.json"), []byte(`{"format_version": 9, "name": "Future"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	body = hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	if strings.Contains(body, "save-load") {
		t.Error("a newer world's table has a button cell")
	}
}

// writeAutosave adds an autosave file to a world, saved at the given time.
func writeAutosave(t *testing.T, dir, name string, at time.Time) {
	t.Helper()
	p := filepath.Join(dir, "saves", name)
	if err := os.WriteFile(p, []byte("data of "+name), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// SPEC 5.11: an autosave reads as the world's name and the save time, with
// the real file name as smaller text; a save with a name of its own keeps it.
func TestSavesPageNamesAutosaves(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	writeAutosave(t, dir, "_autosave3.zip", time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC))
	writeAutosave(t, dir, "_autosave1.zip", time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC))

	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	row := saveRowByHref(t, body, id, "_autosave3.zip")
	for _, want := range []string{
		`Family base, <time datetime="2026-11-03T15:00:00Z"><span class="nowrap">2026-11-03</span> <span class="nowrap">15:00 UTC</span></time>`,
		`<span class="save-file">_autosave3.zip</span>`,
		"Loads next",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("autosave row lacks %q:\n%s", want, row)
		}
	}
	if strings.Contains(row, ">_autosave3.zip</a>") {
		t.Errorf("the file name is still the link text:\n%s", row)
	}
	// Newest first: the three autosave and named rows keep the time order.
	a3, a1, nw := strings.Index(body, "/saves/_autosave3.zip"), strings.Index(body, "/saves/_autosave1.zip"), strings.Index(body, "/saves/new.zip")
	if a3 < 0 || a1 < 0 || nw < 0 || a3 > a1 || a1 > nw {
		t.Errorf("order: autosave3 %d, autosave1 %d, new %d", a3, a1, nw)
	}
	// A save the person named keeps its own name and shows no secondary text.
	if named := saveRow(t, body, "new.zip"); strings.Contains(named, "save-file") || strings.Contains(named, "Family base,") {
		t.Errorf("a named save was renamed:\n%s", named)
	}
	// Only the shown name changes: the load choice still posts the file name.
	if !strings.Contains(saveRowByHref(t, body, id, "_autosave1.zip"), loadButton("_autosave1.zip")) {
		t.Error("the Load next button no longer posts the file name")
	}
}

// A world's name is the user's text, so it is escaped in the page.
func TestSavesPageEscapesWorldNameInAutosaveRow(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, err := hn.worlds.Create(context.Background(), `<b>"x" & y`, "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	writeAutosave(t, filepath.Join(hn.worlds.Dirs().Worlds, id), "_autosave2.zip", time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC))
	row := saveRowByHref(t, hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String(), id, "_autosave2.zip")
	if strings.Contains(row, "<b>") || !strings.Contains(row, "&lt;b&gt;") {
		t.Errorf("world name not escaped:\n%s", row)
	}
}

// saveRowByHref returns the table row whose link goes to the named save.
func saveRowByHref(t *testing.T, body, id, name string) string {
	t.Helper()
	i := strings.Index(body, `href="`+saveHref(id, name)+`"`)
	if i < 0 {
		t.Fatalf("no row for %s", name)
	}
	start := strings.LastIndex(body[:i], "<tr")
	end := i + strings.Index(body[i:], "</tr>")
	return body[start:end]
}

// SPEC 5.11: a downloaded autosave is named for the world and the save time
// (in UTC, the server's zone), not _autosave3.zip.
func TestAutosaveDownloadName(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, dir := savesWorld(t, hn)
	writeAutosave(t, dir, "_autosave3.zip", time.Date(2026, 11, 15, 19, 46, 5, 0, time.UTC))

	rec := hn.getLong("/worlds/"+id+"/saves/_autosave3.zip", c)
	if rec.Code != http.StatusOK || rec.Body.String() != "data of _autosave3.zip" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body.String())
	}
	want := `attachment; filename="Family_base-2026-11-15-19-46-05UTC.zip"; filename*=UTF-8''Family_base-2026-11-15-19-46-05UTC.zip`
	if got := rec.Header().Get("Content-Disposition"); got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
}

// A world's name is user text: whatever it holds, the download header stays
// one valid line, with no quote, CR or LF breaking out of it.
func TestAutosaveDownloadNameIsSafeForAnyWorldName(t *testing.T) {
	names := []string{
		`say "hi"`,
		"line\r\nSet-Cookie: x=1",
		"Мир фабрик 🏭",
		`a"; filename="evil.exe`,
		"..",
		"   ",
		strings.Repeat("long ", 100),
		"semi;colon%25",
	}
	hn := newHarness(t, nil)
	c := hn.signIn()
	for _, name := range names {
		id, err := hn.worlds.Create(context.Background(), name, "2.0.72", true)
		if err != nil {
			t.Logf("world name %q refused: %v", name, err)
			continue
		}
		writeAutosave(t, filepath.Join(hn.worlds.Dirs().Worlds, id), "_autosave1.zip", time.Date(2026, 11, 15, 19, 46, 5, 0, time.UTC))
		rec := hn.getLong("/worlds/"+id+"/saves/_autosave1.zip", c)
		if rec.Code != http.StatusOK {
			t.Errorf("%q: status %d", name, rec.Code)
			continue
		}
		got := rec.Header().Get("Content-Disposition")
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("%q: header holds a line break: %q", name, got)
		}
		typ, params, err := mime.ParseMediaType(got)
		if err != nil || typ != "attachment" {
			t.Errorf("%q: header %q does not parse: %v", name, got, err)
			continue
		}
		file := params["filename"]
		if !strings.HasSuffix(file, "-2026-11-15-19-46-05UTC.zip") || strings.ContainsAny(file, `/\;"`) || len(params) != 1 {
			t.Errorf("%q: file name %q, params %v", name, file, params)
		}
	}
}
