package web

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// deadlineRecorder lets the long routes lift their deadlines, which a plain
// recorder refuses.
type deadlineRecorder struct{ *httptest.ResponseRecorder }

func (deadlineRecorder) SetReadDeadline(time.Time) error  { return nil }
func (deadlineRecorder) SetWriteDeadline(time.Time) error { return nil }

func (hn *harness) serveLong(r *http.Request) *httptest.ResponseRecorder {
	hn.t.Helper()
	r.RemoteAddr = peerUntrusted
	rec := httptest.NewRecorder()
	hn.h.ServeHTTP(deadlineRecorder{rec}, r)
	return rec
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
		"2026-11-03 13:25 UTC", "Loads next", "15 B",
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
}

func TestChooseSave(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)

	rec := hn.chooseSave(c, id, url.Values{"load": {"old.zip"}})
	hn.wantRedirect(rec, "/worlds/"+id+"/saves?chosen=1")
	body := hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	// The older save is marked as the one that loads, and its radio is checked.
	if !strings.Contains(body, `value="old.zip" checked`) || strings.Contains(body, `value="new.zip" checked`) {
		t.Errorf("radio not on old.zip:\n%s", body)
	}
	row := body[strings.Index(body, ">old.zip<"):]
	if i := strings.Index(row, "Loads next"); i < 0 || i > strings.Index(row, "</tr>") {
		t.Error("old.zip not marked as loading next")
	}

	hn.wantRedirect(hn.chooseSave(c, id, url.Values{"load": {""}}), "/worlds/"+id+"/saves?chosen=1")
	body = hn.get("/worlds/"+id+"/saves", withCookie(c)).Body.String()
	if !strings.Contains(body, `value="" checked`) {
		t.Error("newest not checked after reset")
	}

	for name, form := range map[string]url.Values{
		"unknown": {"load": {"nope.zip"}},
		"escape":  {"load": {"../world.json"}},
		"none":    {},
	} {
		rec := hn.chooseSave(c, id, form)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `href="#load-save"`) {
			t.Errorf("%s: %d", name, rec.Code)
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
	if !strings.Contains(body, "no longer in the saves folder") || !strings.Contains(body, `value="" checked`) {
		t.Error("missing chosen save not explained")
	}
}
