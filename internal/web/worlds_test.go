package web

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (hn *harness) signIn() *http.Cookie {
	hn.t.Helper()
	hn.addUser("alice", testPassword)
	c := sessionCookie(hn.post(PathSignIn, signInForm("alice", testPassword)))
	if c == nil {
		hn.t.Fatal("sign-in set no cookie")
	}
	return c
}

func TestRootRedirectsToWorlds(t *testing.T) {
	hn := newHarness(t, nil)
	hn.wantRedirect(hn.get("/"), PathWorlds)
}

func TestWorldListNeedsSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	hn.wantRedirect(hn.get(PathWorlds), PathSetup)
}

func TestWorldListEmptyAndFilled(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()

	body := hn.get(PathWorlds, withCookie(c)).Body.String()
	if !strings.Contains(body, "No worlds yet") {
		t.Errorf("empty state missing:\n%s", body)
	}

	ctx := context.Background()
	if _, err := hn.worlds.Create(ctx, "Zeta", "2.0.72", true); err != nil {
		t.Fatal(err)
	}
	if _, err := hn.worlds.Create(ctx, "Alpha <b>", "1.1.110", false); err != nil {
		t.Fatal(err)
	}
	rec := hn.get(PathWorlds, withCookie(c))
	body = rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	// The first world made is active and listed first (SPEC 5.4).
	z, a := strings.Index(body, "Zeta"), strings.Index(body, "Alpha &lt;b&gt;")
	if z < 0 || a < 0 || z > a {
		t.Errorf("want Zeta (active) before Alpha: %d, %d", z, a)
	}
	if strings.Count(body, `class="badge badge--active"`) != 1 {
		t.Error("want exactly one Active badge")
	}
	if strings.Contains(body, "No worlds yet") {
		t.Error("empty state shown with worlds")
	}
}

func saveZip(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, err := zw.Create("level.dat0")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("map"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// postImportUpload sends the upload form with its text fields before the file.
func (hn *harness) postImportUpload(c *http.Cookie, fields map[string]string, filename string, content []byte) *httptest.ResponseRecorder {
	hn.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, k := range []string{"name", "game_version", "space_age"} {
		if v, ok := fields[k]; ok {
			_ = mw.WriteField(k, v)
		}
	}
	fw, err := mw.CreateFormFile("save", filename)
	if err != nil {
		hn.t.Fatal(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, PathImportUpload, &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(c)
	return hn.serveLive(r)
}

func (hn *harness) postLive(path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(c)
	return hn.serveLive(r)
}

// serveLive sends r through a real listener: the import routes lift the
// connection's deadlines, which a ResponseRecorder cannot do.
func (hn *harness) serveLive(r *http.Request) *httptest.ResponseRecorder {
	hn.t.Helper()
	ts := httptest.NewServer(hn.h)
	defer ts.Close()
	u, err := url.Parse(ts.URL + r.URL.RequestURI())
	if err != nil {
		hn.t.Fatal(err)
	}
	r.RequestURI, r.URL, r.Host = "", u, u.Host
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(r)
	if err != nil {
		hn.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	rec := httptest.NewRecorder()
	for k, v := range resp.Header {
		rec.Header()[k] = v
	}
	rec.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(rec, resp.Body)
	return rec
}

func (hn *harness) noStaging() {
	hn.t.Helper()
	entries, _ := os.ReadDir(hn.worlds.Dirs().Worlds)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			hn.t.Errorf("left behind %s", e.Name())
		}
	}
}

func TestWorldPageAndRename(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	ctx := context.Background()
	id, _ := hn.worlds.Create(ctx, "Base", "2.0.72", true)
	if _, err := hn.worlds.Create(ctx, "Other", "2.0.72", false); err != nil {
		t.Fatal(err)
	}

	body := hn.get(PathWorlds, withCookie(c)).Body.String()
	if !strings.Contains(body, `href="/worlds/base">Base</a>`) || !strings.Contains(body, `href="/import"`) {
		t.Errorf("list lacks world or import links:\n%s", body)
	}
	body = hn.get("/worlds/"+id, withCookie(c)).Body.String()
	for _, want := range []string{"<h1>Base", "/worlds/base/saves", "/worlds/base/snapshots", "/worlds/base/rename", "2.0.72"} {
		if !strings.Contains(body, want) {
			t.Errorf("world page lacks %q", want)
		}
	}
	if rec := hn.get("/worlds/missing", withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("missing world: %d", rec.Code)
	}

	hn.wantRedirect(hn.post("/worlds/base/rename", url.Values{"name": {"New base"}}, withCookie(c)), "/worlds/base")
	if body := hn.get("/worlds/base", withCookie(c)).Body.String(); !strings.Contains(body, "<h1>New base") {
		t.Error("rename did not show")
	}
	rec := hn.post("/worlds/base/rename", url.Values{"name": {"OTHER"}}, withCookie(c))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Another world already has this name") {
		t.Errorf("duplicate name: %d\n%s", rec.Code, rec.Body.String())
	}
}

func TestImportFolderRoute(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	src := filepath.Join(hn.worlds.Dirs().Imports, "saves-2025")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.zip"), saveZip(t), 0o600); err != nil {
		t.Fatal(err)
	}
	body := hn.get(PathImport, withCookie(c)).Body.String()
	if !strings.Contains(body, `value="saves-2025"`) {
		t.Errorf("inbox folder not offered:\n%s", body)
	}

	rec := hn.postLive(PathImportFolder, url.Values{"folder": {"saves-2025"}, "name": {""}, "game_version": {"2.0"}}, c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Fix 2 problems") {
		t.Errorf("bad fields: %d\n%s", rec.Code, rec.Body.String())
	}
	rec = hn.postLive(PathImportFolder, url.Values{"folder": {"../worlds"}, "name": {"Old"}, "game_version": {"2.0.72"}}, c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Choose a folder") {
		t.Errorf("escape: %d", rec.Code)
	}
	hn.wantRedirect(hn.postLive(PathImportFolder, url.Values{"folder": {"saves-2025"}, "name": {"Old"}, "game_version": {"2.0.72"}, "space_age": {"on"}}, c), "/worlds/old")
	list, _ := hn.worlds.List(context.Background())
	if len(list) != 1 || !list[0].SpaceAge || !list[0].Active {
		t.Errorf("list %+v", list)
	}
	hn.noStaging()
}

func TestImportUploadRoute(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	good := map[string]string{"name": "Uploaded", "game_version": "1.1.110"}

	for _, tc := range []struct {
		fields   map[string]string
		filename string
		content  []byte
		want     string
		status   int
	}{
		{good, ".hidden.zip", saveZip(t), "does not start with a dot", http.StatusBadRequest},
		{good, "save.txt", saveZip(t), "ends in .zip", http.StatusBadRequest},
		{good, "save.zip", []byte("not a zip"), "not a zip file", http.StatusUnprocessableEntity},
		{map[string]string{"name": " ", "game_version": "x"}, "save.zip", saveZip(t), "Fix 2 problems", http.StatusBadRequest},
	} {
		rec := hn.postImportUpload(c, tc.fields, tc.filename, tc.content)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: %d, want %q\n%s", tc.filename, rec.Code, tc.want, rec.Body.String())
		}
	}
	hn.noStaging()
	if list, _ := hn.worlds.List(context.Background()); len(list) != 0 {
		t.Fatalf("refusals made worlds: %+v", list)
	}

	hn.wantRedirect(hn.postImportUpload(c, good, `C:\games\my save.zip`, saveZip(t)), "/worlds/uploaded")
	if _, err := os.Stat(filepath.Join(hn.worlds.Dirs().Worlds, "uploaded", "saves", "my save.zip")); err != nil {
		t.Error(err)
	}
	rec := hn.postImportUpload(c, map[string]string{"name": "UPLOADED", "game_version": "1.1.110"}, "b.zip", saveZip(t))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Another world already has this name") {
		t.Errorf("duplicate: %d", rec.Code)
	}
	hn.noStaging()
}

// SPEC 5.5: a folder with no save at its top level is listed in words as
// not importable, with its radio button disabled. The one importable folder
// starts chosen, with the world named after it. With nothing importable the
// form says so.
func TestImportListsFoldersWithoutSaves(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	empty := filepath.Join(hn.worlds.Dirs().Imports, "current")
	if err := os.MkdirAll(filepath.Join(empty, "mods"), 0o750); err != nil {
		t.Fatal(err)
	}
	body := hn.get(PathImport, withCookie(c)).Body.String()
	for _, want := range []string{`value="current" disabled>`, "<code>current</code>: no save at its top level, so it cannot be imported",
		"There is nothing to import"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "0 saves") {
		t.Errorf("empty folder listed by count:\n%s", body)
	}

	src := filepath.Join(hn.worlds.Dirs().Imports, "saves-2025")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.zip"), saveZip(t), 0o600); err != nil {
		t.Fatal(err)
	}
	body = hn.get(PathImport, withCookie(c)).Body.String()
	for _, want := range []string{`value="saves-2025"`, " checked", `id="folder-name" name="name" type="text" value="saves-2025"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "There is nothing to import") {
		t.Errorf("says nothing to import beside an importable folder:\n%s", body)
	}

	// The server still refuses the empty folder.
	rec := hn.postLive(PathImportFolder, url.Values{"folder": {"current"}, "name": {"Cur"}, "game_version": {"2.0.72"}}, c)
	if rec.Code == http.StatusSeeOther {
		t.Errorf("imported a folder with no save")
	}
}
