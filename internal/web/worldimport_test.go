package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// wfSpec describes a world file the tests build.
type wfSpec struct {
	name, gameVersion string
	spaceAge          bool
	password          bool
	foremanVersion    string
	formatVersion     int
}

// wfBytes builds a world file as SPEC 5.13 describes it: the format file
// first, world.json, one save, one mod and the settings file.
func wfBytes(t *testing.T, sp wfSpec) []byte {
	t.Helper()
	if sp.gameVersion == "" {
		sp.gameVersion = "2.0.77"
	}
	if sp.formatVersion == 0 {
		sp.formatVersion = world.WorldFileFormat
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	put := func(name string, data []byte) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	doc, _ := json.Marshal(world.WorldFileDoc{FormatVersion: sp.formatVersion, ExportedAt: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC),
		ForemanVer: sp.foremanVersion, Saves: world.ExportAllSaves, GamePasswordSet: sp.password})
	put(world.WorldFileFormatFile, doc)
	meta, _ := json.Marshal(map[string]any{"format_version": 1, "name": sp.name, "game_version": sp.gameVersion,
		"space_age": sp.spaceAge, "load_save": "base.zip", "created_at": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	put(world.MetaFile, meta)
	put(world.SavesDir+"/base.zip", saveZip(t))
	put(world.ModsDir+"/flib_0.12.9.zip", modZip(t, "flib", "0.12.9"))
	put(world.ConfigDir+"/"+world.ServerSettingsFile, []byte(`{"name":"Club"}`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// wfUpload posts content as the world file field of the upload form.
func wfUpload(t *testing.T, ls *liveServer, c *http.Cookie, content []byte) reply {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, err := mw.CreateFormFile(worldFileField, "club-world.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	return wfSend(t, ls, c, PathWorldFileImport, mw.FormDataContentType(), &b)
}

func wfSend(t *testing.T, ls *liveServer, c *http.Cookie, path, ct string, body *bytes.Buffer) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ls.url+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ct)
	if c != nil {
		req.AddCookie(c)
	}
	resp, err := ls.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, body: out.String()}
}

var stagedValue = regexp.MustCompile(`name="staged" value="([^"]*)"`)

// wfStaged is the staged file's name the preview posts back.
func wfStaged(t *testing.T, page string) string {
	t.Helper()
	m := stagedValue.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the preview has no staged field:\n%.600s", page)
	}
	return html.UnescapeString(m[1])
}

// stagedFiles lists the staged world files in the worlds folder.
func stagedFiles(t *testing.T, hn *harness) []string {
	t.Helper()
	var out []string
	for _, n := range dirNames(t, hn.worlds.Dirs().Worlds) {
		if strings.HasPrefix(n, world.PrefixWorldFile) {
			out = append(out, n)
		}
	}
	return out
}

func worldCount(t *testing.T, hn *harness) int {
	t.Helper()
	list, err := hn.worlds.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

func wfWantAll(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s lacks %q:\n%.1500s", what, w, body)
		}
	}
}

func wfWantNone(t *testing.T, what, body string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(body, n) {
			t.Errorf("%s holds %q", what, n)
		}
	}
}

// SPEC 5.14: an upload is staged and previewed; nothing becomes a world
// until the preview is confirmed.
func TestWorldFileUploadPreviewsAndMakesNoWorld(t *testing.T) {
	g := newImportRig(t, nil)
	before := worldCount(t, g.harness)
	rep := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Club base", gameVersion: "2.0.77", spaceAge: true, foremanVersion: "1.4.0"}))
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", rep.StatusCode, g.logs.String())
	}
	wfWantAll(t, "preview", rep.body, `id="world-file-name"`, `value="Club base"`, `id="act-import-world-file"`,
		`action="`+PathWorldFileConfirm+`"`, "2.0.77", "1.4.0", "1 save", "1 mod file")
	wfWantNone(t, "preview of a file with no password", rep.body, `id="world-file-password-warning"`)
	// The mod and save warning always shows.
	wfWantAll(t, "preview", rep.body, `id="world-file-mods-warning"`)
	staged := wfStaged(t, rep.body)
	if !strings.HasPrefix(staged, world.PrefixWorldFile) || strings.ContainsAny(staged, `/\`) {
		t.Errorf("staged name %q", staged)
	}
	if got := stagedFiles(t, g.harness); len(got) != 1 || got[0] != staged {
		t.Errorf("staged files %v, want only %q", got, staged)
	}
	if after := worldCount(t, g.harness); after != before {
		t.Errorf("worlds %d after an upload, want %d: nothing is made before confirm", after, before)
	}
	// The staged name is in the form, never in an address.
	if strings.Contains(rep.body, "href=\""+PathWorldFileConfirm+"?") || strings.Contains(rep.body, "?staged=") {
		t.Error("the staged name is in an address")
	}
}

// SPEC 5.14: the preview warns when the exported world had a game password,
// and says nothing of the password's value.
func TestWorldFilePreviewWarnsOfALostPassword(t *testing.T) {
	g := newImportRig(t, nil)
	rep := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Club", password: true}))
	wfWantAll(t, "preview", rep.body, `id="world-file-password-warning"`)
}

// The file is untrusted: a Foreman version that is not a version is not
// shown, and a name from the file is escaped.
func TestWorldFilePreviewShowsNothingHostile(t *testing.T) {
	g := newImportRig(t, nil)
	rep := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: `<script>alert(1)</script>`, foremanVersion: `<img src=x onerror=alert(1)>`}))
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("status %d", rep.StatusCode)
	}
	wfWantNone(t, "preview", rep.body, "<script>alert", "<img src=x")
}

// SPEC 5.14: confirm makes a new world under the typed name, removes the
// staged file, goes to the new world's page and records world-import.
func TestWorldFileConfirmMakesTheWorld(t *testing.T) {
	g := newImportRig(t, nil)
	rep := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Club", foremanVersion: "1.4.0"}))
	staged := wfStaged(t, rep.body)
	conf := g.post(PathWorldFileConfirm, url.Values{"staged": {staged}, "name": {"Club moved"}})
	w := g.worldByName("Club moved")
	g.wantRedirectTo(importedPath(w.ID, false), conf)
	if got := stagedFiles(t, g.harness); len(got) != 0 {
		t.Errorf("staged files left after the import: %v", got)
	}
	var found bool
	for _, e := range g.audit() {
		if e.Action == "world-import" && e.WorldID == w.ID {
			found = true
			if !strings.Contains(e.Detail, "1.4.0") {
				t.Errorf("audit detail %q lacks the file's Foreman version", e.Detail)
			}
		}
	}
	if !found {
		t.Error("the import is not audited as world-import")
	}
}

// SPEC 5.14: a name in use is refused on the name field and the staged file
// is kept, so another name needs no second upload.
func TestWorldFileNameClashKeepsTheStagedFile(t *testing.T) {
	g := newImportRig(t, nil)
	if _, err := g.worlds.Create(context.Background(), "Club", "2.1.24", false); err != nil {
		t.Fatal(err)
	}
	rep := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Club"}))
	staged := wfStaged(t, rep.body)
	worlds := worldCount(t, g.harness)

	clash := g.post(PathWorldFileConfirm, url.Values{"staged": {staged}, "name": {"club"}})
	if clash.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400:\n%.600s", clash.StatusCode, clash.body)
	}
	wfWantAll(t, "refusal", clash.body, "<title>Error: ", `id="world-file-name-error"`, `aria-invalid="true"`, `id="error-summary"`, `href="#world-file-name"`)
	if got := wfStaged(t, clash.body); got != staged {
		t.Errorf("the staged name changed to %q", got)
	}
	if got := stagedFiles(t, g.harness); len(got) != 1 {
		t.Errorf("staged files %v after a clash, want the one kept", got)
	}
	if worldCount(t, g.harness) != worlds {
		t.Error("a world was made by a refused import")
	}

	ok := g.post(PathWorldFileConfirm, url.Values{"staged": {staged}, "name": {"Club copy"}})
	g.wantRedirectTo(importedPath(g.worldByName("Club copy").ID, false), ok)
}

// A blank name is refused on the field, and the staged file stays.
func TestWorldFileEmptyNameIsRefused(t *testing.T) {
	g := newImportRig(t, nil)
	staged := wfStaged(t, wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Club"})).body)
	rep := g.post(PathWorldFileConfirm, url.Values{"staged": {staged}, "name": {"   "}})
	if rep.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rep.StatusCode)
	}
	wfWantAll(t, "refusal", rep.body, `id="world-file-name-error"`)
	if got := stagedFiles(t, g.harness); len(got) != 1 {
		t.Errorf("staged files %v, want the one kept", got)
	}
}

// SPEC 14.5: a posted staged name that is not a staged world file's name is
// never read or removed, whatever else is in the worlds folder.
func TestWorldFileConfirmRefusesNamesThatAreNotStagedFiles(t *testing.T) {
	g := newImportRig(t, nil)
	worlds := g.worlds.Dirs().Worlds
	// A staged mod upload, a file outside the worlds folder, and a world.
	if err := os.WriteFile(filepath.Join(worlds, world.PrefixPartial+"mod-abc"), []byte("mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(worlds), world.PrefixWorldFile+"outside")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := g.worlds.Create(context.Background(), "Keep", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	before := dataTree(t, filepath.Dir(worlds))
	for _, bad := range []string{
		"", id, world.PrefixPartial + "mod-abc", "../" + world.PrefixWorldFile + "outside",
		world.PrefixWorldFile + "../" + id, world.PrefixWorldFile + `..\x`, "/" + world.PrefixWorldFile + "abc",
		world.PrefixWorldFile + strings.Repeat("a", 200), world.PrefixWorldFile + "a/b",
	} {
		rep := g.post(PathWorldFileConfirm, url.Values{"staged": {bad}, "name": {"Intruder"}})
		if rep.StatusCode != http.StatusGone {
			t.Errorf("staged %q: status %d, want 410", bad, rep.StatusCode)
		}
		wfWantAll(t, "refusal", rep.body, `href="`+PathImport+`"`)
	}
	sameData(t, "after refused staged names", dataTree(t, filepath.Dir(worlds)), before)
}

// SPEC 5.14: a staged file removed after an hour is reported in words, with
// a way back to the Import page.
func TestWorldFileConfirmOfAGoneFile(t *testing.T) {
	g := newImportRig(t, nil)
	rep := g.post(PathWorldFileConfirm, url.Values{"staged": {world.PrefixWorldFile + "0123456789abcdef"}, "name": {"Club"}})
	if rep.StatusCode != http.StatusGone {
		t.Fatalf("status %d, want 410", rep.StatusCode)
	}
	wfWantAll(t, "page", rep.body, `href="`+PathImport+`"`)
	if worldCount(t, g.harness) != 0 {
		t.Error("a world was made from a file that is gone")
	}
}

// SPEC 14.5: a file that is not a world file is refused in fixed words that
// do not repeat the file, and leaves no staged file and no world.
func TestWorldFileUploadRefusals(t *testing.T) {
	g := newImportRig(t, nil)
	for name, content := range map[string][]byte{
		"not a zip":    []byte("PRIVATE-TEXT-IN-THE-FILE"),
		"empty":        nil,
		"newer format": wfBytes(t, wfSpec{name: "Club", formatVersion: world.WorldFileFormat + 1}),
		"a plain save": saveZip(t),
		"damaged tail": wfBytes(t, wfSpec{name: "Club"})[:200],
	} {
		rep := wfUpload(t, g.liveServer, g.cookie, content)
		if rep.StatusCode < 400 || rep.StatusCode >= 500 {
			t.Errorf("%s: status %d, want a 4xx refusal", name, rep.StatusCode)
		}
		wfWantNone(t, name+" refusal", rep.body, "PRIVATE-TEXT-IN-THE-FILE")
		wfWantAll(t, name+" refusal", rep.body, `href="`+PathImport+`"`)
		if got := stagedFiles(t, g.harness); len(got) != 0 {
			t.Errorf("%s left staged files %v", name, got)
		}
	}
	if worldCount(t, g.harness) != 0 {
		t.Error("a refused upload made a world")
	}
	newer := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Club", formatVersion: world.WorldFileFormat + 1}))
	wfWantAll(t, "newer-format refusal", newer.body, "newer version of Foreman")
}

// SPEC 5.14: a request with no file says so, and a request that is not a
// form is refused.
func TestWorldFileUploadWithNoFile(t *testing.T) {
	g := newImportRig(t, nil)
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("other", "x")
	_ = mw.Close()
	rep := wfSend(t, g.liveServer, g.cookie, PathWorldFileImport, mw.FormDataContentType(), &b)
	if rep.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("no file: status %d, want 422", rep.StatusCode)
	}
	rep = wfSend(t, g.liveServer, g.cookie, PathWorldFileImport, "text/plain", bytes.NewBufferString("hi"))
	if rep.StatusCode < 400 || rep.StatusCode >= 500 {
		t.Errorf("not a form: status %d, want a 4xx refusal", rep.StatusCode)
	}
	if got := stagedFiles(t, g.harness); len(got) != 0 {
		t.Errorf("staged files %v", got)
	}
}

// Both routes need a signed-in user.
func TestWorldFileRoutesNeedSignIn(t *testing.T) {
	g := newImportRig(t, nil)
	content := wfBytes(t, wfSpec{name: "Club"})
	rep := wfUpload(t, g.liveServer, nil, content)
	if rep.StatusCode == http.StatusOK {
		t.Errorf("a signed-out upload was accepted")
	}
	rep = wfSend(t, g.liveServer, nil, PathWorldFileConfirm, "application/x-www-form-urlencoded",
		bytes.NewBufferString(url.Values{"staged": {world.PrefixWorldFile + "x"}, "name": {"A"}}.Encode()))
	if rep.StatusCode == http.StatusOK || rep.StatusCode == http.StatusSeeOther && strings.Contains(rep.Header.Get("Location"), "imported") {
		t.Errorf("a signed-out confirm was accepted: %d", rep.StatusCode)
	}
	if len(stagedFiles(t, g.harness)) != 0 || worldCount(t, g.harness) != 0 {
		t.Error("a signed-out request changed the data folder")
	}
}

// SPEC 5.5, 3.7: the preview offers to download the game image when this
// host lacks it, ticked by default, and a ticked box starts the download. A
// version the host has needs no box.
func TestWorldFileDownloadsTheImage(t *testing.T) {
	g := newImportRig(t, nil)
	missing := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Old", gameVersion: "2.0.77"}))
	wfWantAll(t, "preview of a version with no image", missing.body, `id="world-file-download"`)
	if !regexp.MustCompile(`id="world-file-download"[^>]*checked`).MatchString(missing.body) {
		t.Error("the download box is not ticked by default")
	}
	have := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "New", gameVersion: "2.1.24"}))
	wfWantNone(t, "preview of a version with its image", have.body, `id="world-file-download"`)
	wfWantAll(t, "preview of a version with its image", have.body, `id="world-file-image"`)

	g.fake.Daemon().FailPull(image20, "manifest unknown")
	staged := wfStaged(t, missing.body)
	g.wantRedirectTo(importedPath("old", false), g.post(PathWorldFileConfirm, url.Values{"staged": {staged}, "name": {"Old"}, "download": {"on"}}))
	if err := g.waitResult("old", game.AuditImageDownload); err == nil {
		t.Fatal("the download worked, want the failure the daemon gave")
	}
	if !g.hasAudit(game.AuditImageDownload) {
		t.Error("the download is not audited")
	}

	// Unticked, nothing is downloaded.
	again := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Other", gameVersion: "2.0.77"}))
	g.wantRedirectTo(importedPath("other", false), g.post(PathWorldFileConfirm, url.Values{"staged": {wfStaged(t, again.body)}, "name": {"Other"}}))
	if _, ok := g.h.(*Handler).runs.result("other"); ok {
		t.Error("a download ran with the box unticked")
	}
}

// SPEC 14.6: the upload route takes a body of the world file cap and room
// for the form, which is far above the page's own limits.
func TestWorldFileUploadLimitIsTheWorldFileCap(t *testing.T) {
	g := newImportRig(t, nil)
	limit, ok := g.h.(*Handler).srv.uploadLimits["POST "+PathWorldFileImport]
	if !ok {
		t.Fatal("the upload route has no upload limit: it would take only a form's worth")
	}
	if got := limit(context.Background()); got < world.MaxWorldFile || got > world.MaxWorldFile+1<<20 {
		t.Errorf("limit %d, want the world file cap %d and room for the form", got, world.MaxWorldFile)
	}
	if _, ok := g.h.(*Handler).srv.uploadLimits["POST "+PathWorldFileConfirm]; ok {
		t.Error("the confirm is a form post and must keep the form limit")
	}
}

// The Import page's drop zone for a world file names the field the upload
// reads, and takes only a zip.
func TestImportPageWorldFileZone(t *testing.T) {
	z := ImportPage{}.WorldFileZone()
	if z.ID != "world-file" || z.Name != worldFileField || z.Accept != ".zip" || z.MaxBytes != uint64(world.MaxWorldFile) {
		t.Errorf("zone %+v", z)
	}
	if got := (ImportPage{}).WorldFilePath(); got != PathWorldFileImport {
		t.Errorf("path %q", got)
	}
}

func TestStagedName(t *testing.T) {
	for name, want := range map[string]bool{
		world.PrefixWorldFile + "0123456789abcdef": true,
		"":                                  false,
		world.PrefixPartial + "abc":         false,
		world.PrefixWorldFile + "a/b":       false,
		world.PrefixWorldFile + `a\b`:       false,
		"../" + world.PrefixWorldFile + "a": false,
		world.PrefixWorldFile + "..":        true,
		world.PrefixWorldFile + strings.Repeat("x", 100): false,
	} {
		if got := stagedName(name); got != want {
			t.Errorf("stagedName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestCleanVersion(t *testing.T) {
	for in, want := range map[string]string{
		"1.4.0": "1.4.0", "v1.4.0-rc.1+build_7": "v1.4.0-rc.1+build_7", "": "",
		"<b>1</b>": "", "1.0 0": "", "1.0\n": "", strings.Repeat("1", 41): "",
	} {
		if got := cleanVersion(in); got != want {
			t.Errorf("cleanVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// SPEC 6.11: too little disk space keeps the staged file and says so on the
// preview, with the figures from the disk check.
func TestWorldFileNoSpaceKeepsThePreview(t *testing.T) {
	g := newImportRig(t, nil)
	s := g.h.(*Handler).srv
	p := WorldFilePage{Staged: world.PrefixWorldFile + "abc", Name: "Club", Info: world.WorldFileInfo{GameVersion: "2.1.24"}}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, PathWorldFileConfirm, nil)
	s.worldFileImportFailed(rec, r, store.User{Username: "alice"}, p, &world.NoSpaceError{Need: 10 << 30, Reserve: 2 << 30, Free: 1 << 30})
	if rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("status %d, want 507", rec.Code)
	}
	wfWantAll(t, "preview", rec.Body.String(), `id="world-file-refusal"`, `value="`+world.PrefixWorldFile+`abc"`, `id="act-import-world-file"`)
}

// SPEC 5.14: at most two staged world files wait; a third upload removes the
// oldest, and the preview of the newest still works.
func TestWorldFileThirdUploadRemovesTheOldest(t *testing.T) {
	g := newImportRig(t, nil)
	var first, last string
	for i := range 3 {
		rep := wfUpload(t, g.liveServer, g.cookie, wfBytes(t, wfSpec{name: "Club"}))
		staged := wfStaged(t, rep.body)
		if i == 0 {
			first = staged
		}
		last = staged
		// Stagings are told apart by their modified time.
		old := time.Now().Add(time.Duration(i-5) * time.Minute)
		_ = os.Chtimes(filepath.Join(g.worlds.Dirs().Worlds, staged), old, old)
	}
	got := stagedFiles(t, g.harness)
	if len(got) != 2 {
		t.Fatalf("staged files %v, want two", got)
	}
	for _, n := range got {
		if n == first {
			t.Errorf("the oldest staged file %q was kept", first)
		}
	}
	if rep := g.post(PathWorldFileConfirm, url.Values{"staged": {first}, "name": {"Club"}}); rep.StatusCode != http.StatusGone {
		t.Errorf("confirm of the removed file: status %d, want 410", rep.StatusCode)
	}
	g.wantRedirectTo(importedPath("club", false), g.post(PathWorldFileConfirm, url.Values{"staged": {last}, "name": {"Club"}}))
}

// wfImportPageError checks that a refusal the form can fix is the Import
// page itself, with the error tied to the world file field: the error
// summary links to it, the field is invalid and names the error as a
// description, and the title says so first (SPEC 5.14, DESIGN 7.2).
func wfImportPageError(t *testing.T, what string, rep reply, words string) {
	t.Helper()
	if rep.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("%s: status %d, want 422:\n%.500s", what, rep.StatusCode, rep.body)
	}
	if !strings.Contains(rep.body, `id="world-file-heading"`) {
		t.Skip("the Import page has no world file form yet")
	}
	wfWantAll(t, what, rep.body, "<title>Error: ", `id="upload-heading"`, `id="error-summary"`, `href="#world-file"`,
		`id="world-file-error"`, words)
	input := regexp.MustCompile(`<input id="world-file"[^>]*>`).FindString(rep.body)
	wfWantAll(t, what+" file field", input, `aria-invalid="true"`, "world-file-error")
}

// SPEC 5.14: no file chosen is shown on the Import page, tied to the field,
// and makes nothing.
func TestWorldFileNoFileShowsOnTheImportPage(t *testing.T) {
	g := newImportRig(t, nil)
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("other", "x")
	_ = mw.Close()
	rep := wfSend(t, g.liveServer, g.cookie, PathWorldFileImport, mw.FormDataContentType(), &b)
	wfImportPageError(t, "no file", rep, "Choose a file")
	if got := stagedFiles(t, g.harness); len(got) != 0 {
		t.Errorf("staged files %v", got)
	}
}

// SPEC 14.6: a world file over the cap is refused before it is read, and the
// refusal is shown on the Import page, tied to the field. A declared length
// stands in for the 64 GiB body.
func TestWorldFileTooLargeShowsOnTheImportPage(t *testing.T) {
	g := newImportRig(t, nil)
	rep := g.postDeclared(PathWorldFileImport, worldFileField, world.MaxWorldFile+1)
	wfImportPageError(t, "too large", rep, "larger than the limit of")
	if got := stagedFiles(t, g.harness); len(got) != 0 {
		t.Errorf("staged files %v", got)
	}
	if worldCount(t, g.harness) != 0 {
		t.Error("a refused upload made a world")
	}
}

// The world file form's error is part of the page's problems and zone.
func TestImportPageCarriesTheWorldFileError(t *testing.T) {
	var p ImportPage
	if p.hasErrors() || len(p.WorldFileProblems()) != 0 || !p.WorldFileZone().Error.IsZero() {
		t.Fatal("a fresh page has a world file error")
	}
	p.WorldFileError = tooLarge(world.MaxWorldFile).msg
	got := p.WorldFileProblems()
	if !p.hasErrors() || len(got) != 1 || got[0].FieldID != "world-file" || p.WorldFileZone().Error.IsZero() {
		t.Errorf("problems %+v, zone error %v", got, p.WorldFileZone().Error)
	}
}
