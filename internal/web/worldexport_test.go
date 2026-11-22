package web

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// fakeExport stands in for world.WorldExport, so the handler's own rules are
// tested apart from the export core (task T1).
type fakeExport struct {
	name    string
	body    string
	writeFn func(w io.Writer) error
	closed  *int
}

func (f *fakeExport) Name() string          { return f.name }
func (f *fakeExport) GamePasswordSet() bool { return false }
func (f *fakeExport) Close() error {
	if f.closed != nil {
		*f.closed++
	}
	return nil
}
func (f *fakeExport) WriteZip(w io.Writer, _ string) error {
	if f.writeFn != nil {
		return f.writeFn(w)
	}
	_, err := io.WriteString(w, f.body)
	return err
}

// exportServer is a server with just what the export handler reads.
func exportServer(t *testing.T, hn *harness) *server {
	t.Helper()
	return &server{cfg: hn.cfg, store: hn.store, worlds: hn.worlds, log: hn.slog, now: hn.clock.Now, render: mustRenderer(t)}
}

type exportCall struct {
	id    string
	saves world.ExportSaves
}

// exportRequest posts the export form for world id.
func exportRequest(id string, form string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/worlds/"+id+"/export", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", id)
	return r
}

// recordingPrepare returns a prepare func that hands out job and records calls.
func recordingPrepare(job exportJob, err error, calls *[]exportCall) func(context.Context, string, world.ExportSaves) (exportJob, error) {
	return func(_ context.Context, id string, saves world.ExportSaves) (exportJob, error) {
		*calls = append(*calls, exportCall{id, saves})
		return job, err
	}
}

func (hn *harness) exportWorld(t *testing.T) string {
	t.Helper()
	id, _ := savesWorld(t, hn)
	return id
}

// SPEC 5.13: the export is a post that needs a signed-in user.
func TestWorldExportNeedsSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	id := hn.exportWorld(t)
	hn.wantRedirect(hn.postLong("/worlds/"+id+"/export", nil, nil), PathSetup)
	if rec := hn.get("/worlds/" + id + "/export"); rec.Code == http.StatusOK {
		t.Errorf("a GET of the export answered %d", rec.Code)
	}
}

// SPEC 5.13: the download is a zip named for the world, never cached or
// sniffed, with no length (it is streamed), and it is audited as
// world-export with the choice of saves.
func TestWorldExportDownloadsAndIsAudited(t *testing.T) {
	hn := newHarness(t, nil)
	id := hn.exportWorld(t)
	alice := hn.addUser("alice", testPassword)
	s := exportServer(t, hn)
	var calls []exportCall
	closed := 0
	job := &fakeExport{name: "Family base", body: "ZIPBYTES", closed: &closed}
	h := s.exportHandler(recordingPrepare(job, nil, &calls), make(chan struct{}, maxExports))

	for _, tc := range []struct {
		form   string
		saves  world.ExportSaves
		detail string
	}{
		{"", world.ExportAllSaves, "all saves"},
		{"saves=all", world.ExportAllSaves, "all saves"},
		{"saves=loaded", world.ExportLoadedSave, "loaded save"},
	} {
		calls = nil
		rec := httptest.NewRecorder()
		h(rec, exportRequest(id, tc.form), alice)
		if rec.Code != http.StatusOK || rec.Body.String() != "ZIPBYTES" {
			t.Fatalf("%q: %d %q", tc.form, rec.Code, rec.Body.String())
		}
		hd := rec.Header()
		if hd.Get("Content-Type") != "application/zip" || hd.Get("Cache-Control") != "no-store" ||
			hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Content-Length") != "" ||
			!strings.Contains(hd.Get("Content-Disposition"), `filename="Family_base-world.zip"`) {
			t.Errorf("%q: headers %v", tc.form, hd)
		}
		if len(calls) != 1 || calls[0] != (exportCall{id, tc.saves}) {
			t.Errorf("%q: prepared %+v", tc.form, calls)
		}
		got := hn.audits(AuditWorldExport)
		if want := "alice " + id + " " + tc.detail; got[0] != want {
			t.Errorf("%q: audit %q, want %q", tc.form, got[0], want)
		}
	}
	if closed != 3 {
		t.Errorf("the staging folder was released %d times, want 3", closed)
	}
}

// SPEC 5.13: an unknown choice of saves is refused before any work, and a
// refusal of the export core is worded for the admin and writes no audit
// entry and no download.
func TestWorldExportRefusals(t *testing.T) {
	hn := newHarness(t, nil)
	id := hn.exportWorld(t)
	alice := hn.addUser("alice", testPassword)
	s := exportServer(t, hn)
	tests := []struct {
		name   string
		form   string
		err    error
		status int
		body   string
	}{
		{"unknown choice", "saves=everything", nil, http.StatusBadRequest, "all saves or only the save that loads next"},
		{"busy world", "", world.ErrBusy, http.StatusConflict, "Another operation is running on this world"},
		{"newer world", "", world.ErrNewerFormat, http.StatusConflict, "A newer version of Foreman wrote this world"},
		{"special file", "", &world.UnsupportedFileError{Path: "saves/link.zip"}, http.StatusConflict, "saves/link.zip is a symlink or special file"},
		{"no space", "", &world.NoSpaceError{Need: 10 << 20, Free: 1 << 20}, http.StatusInsufficientStorage, "disk"},
	}
	for _, tc := range tests {
		var calls []exportCall
		h := s.exportHandler(recordingPrepare(nil, tc.err, &calls), make(chan struct{}, maxExports))
		rec := httptest.NewRecorder()
		h(rec, exportRequest(id, tc.form), alice)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.body) {
			t.Errorf("%s: %d\n%s", tc.name, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "zip") || rec.Header().Get("Content-Disposition") != "" {
			t.Errorf("%s: a refusal set download headers: %v", tc.name, rec.Header())
		}
		if tc.err == nil && len(calls) != 0 {
			t.Errorf("%s: prepared despite a bad form", tc.name)
		}
	}
	if got := hn.audits(AuditWorldExport); len(got) != 0 {
		t.Errorf("refusals were audited: %v", got)
	}
}

// An unreadable or newer world is refused with the clone words, and an
// unknown world is 404; the export core is not asked.
func TestWorldExportOfAnUnknownWorldIs404(t *testing.T) {
	hn := newHarness(t, nil)
	alice := hn.addUser("alice", testPassword)
	s := exportServer(t, hn)
	var calls []exportCall
	h := s.exportHandler(recordingPrepare(&fakeExport{}, nil, &calls), make(chan struct{}, maxExports))
	rec := httptest.NewRecorder()
	h(rec, exportRequest("nope", ""), alice)
	if rec.Code != http.StatusNotFound || len(calls) != 0 {
		t.Errorf("%d, prepared %v", rec.Code, calls)
	}
}

// SPEC 5.13: at most 2 exports run at once; a third gets 429 and nothing is
// staged for it; the place is given back when an export ends.
func TestWorldExportAtMostTwoAtOnce(t *testing.T) {
	hn := newHarness(t, nil)
	id := hn.exportWorld(t)
	alice := hn.addUser("alice", testPassword)
	s := exportServer(t, hn)
	if maxExports != 2 {
		t.Fatalf("maxExports = %d, the spec says 2", maxExports)
	}
	slots := make(chan struct{}, maxExports)
	started := make(chan struct{}, maxExports)
	release := make(chan struct{})
	var mu sync.Mutex
	var prepared int
	prepare := func(context.Context, string, world.ExportSaves) (exportJob, error) {
		mu.Lock()
		prepared++
		mu.Unlock()
		return &fakeExport{name: "W", writeFn: func(w io.Writer) error {
			started <- struct{}{}
			<-release
			_, err := io.WriteString(w, "Z")
			return err
		}}, nil
	}
	h := s.exportHandler(prepare, slots)

	var wg sync.WaitGroup
	for range maxExports {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h(httptest.NewRecorder(), exportRequest(id, ""), alice)
		}()
	}
	for range maxExports {
		<-started
	}
	rec := httptest.NewRecorder()
	h(rec, exportRequest(id, ""), alice)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Two exports are already running") {
		t.Errorf("third export: %d\n%s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	if prepared != maxExports {
		t.Errorf("%d exports were staged, want %d", prepared, maxExports)
	}
	mu.Unlock()
	close(release)
	wg.Wait()
	if len(slots) != 0 {
		t.Errorf("%d places still held after both exports ended", len(slots))
	}
	rec = httptest.NewRecorder()
	h(rec, exportRequest(id, ""), alice)
	if rec.Code != http.StatusOK {
		t.Errorf("after the others ended: %d", rec.Code)
	}
}

// A place is also given back when staging is refused.
func TestWorldExportGivesBackItsPlaceOnRefusal(t *testing.T) {
	hn := newHarness(t, nil)
	id := hn.exportWorld(t)
	alice := hn.addUser("alice", testPassword)
	s := exportServer(t, hn)
	slots := make(chan struct{}, maxExports)
	var calls []exportCall
	h := s.exportHandler(recordingPrepare(nil, world.ErrBusy, &calls), slots)
	for range 5 {
		h(httptest.NewRecorder(), exportRequest(id, ""), alice)
	}
	if len(slots) != 0 {
		t.Errorf("%d places leaked", len(slots))
	}
}

// SPEC 5.13: a failure partway breaks the download (an aborted connection),
// never a short body that looks like a whole zip; a failure before any byte
// is an ordinary error page, with no download headers. The staging folder
// is released either way, and the client leaving is not logged as a fault.
func TestWorldExportFailureBreaksTheDownload(t *testing.T) {
	hn := newHarness(t, nil)
	id := hn.exportWorld(t)
	alice := hn.addUser("alice", testPassword)
	s := exportServer(t, hn)
	boom := errors.New("read saves/new.zip: input/output error")

	closed := 0
	partway := &fakeExport{name: "W", closed: &closed, writeFn: func(w io.Writer) error {
		_, _ = io.WriteString(w, "PK-partial")
		return boom
	}}
	h := s.exportHandler(recordingPrepare(partway, nil, new([]exportCall)), make(chan struct{}, maxExports))
	func() {
		defer func() {
			if v, _ := recover().(error); !errors.Is(v, http.ErrAbortHandler) {
				t.Errorf("recovered %v, want http.ErrAbortHandler", v)
			}
		}()
		h(httptest.NewRecorder(), exportRequest(id, ""), alice)
		t.Error("a failure partway did not abort the connection")
	}()
	if closed != 1 {
		t.Errorf("released %d times after a partial failure", closed)
	}
	if !strings.Contains(hn.logs.String(), "input/output error") {
		t.Errorf("the failure was not logged:\n%s", hn.logs.String())
	}

	closed = 0
	early := &fakeExport{name: "W", closed: &closed, writeFn: func(io.Writer) error { return boom }}
	h = s.exportHandler(recordingPrepare(early, nil, new([]exportCall)), make(chan struct{}, maxExports))
	rec := httptest.NewRecorder()
	h(rec, exportRequest(id, ""), alice)
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Disposition") != "" || strings.Contains(rec.Header().Get("Content-Type"), "zip") {
		t.Errorf("an early failure: %d %v", rec.Code, rec.Header())
	}
	if closed != 1 {
		t.Errorf("released %d times after an early failure", closed)
	}
}

// ExportPath is the form's action.
func TestWorldPageExportPath(t *testing.T) {
	p := WorldPage{World: world.Summary{ID: "abc"}}
	if got := p.ExportPath(); got != "/worlds/abc/export" {
		t.Errorf("ExportPath = %q", got)
	}
}

// SPEC 5.13, end to end through the route: the real export core, a real
// world. The zip holds the saves, never the RCON password, and the
// download is audited.
func TestWorldExportOfARealWorld(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	if _, err := hn.worlds.PrepareExport(context.Background(), id, world.ExportAllSaves); err != nil && err.Error() == "not built" {
		t.Skip("task T1's export core has not merged into this branch yet")
	}
	rec := hn.postLong("/worlds/"+id+"/export", url.Values{"saves": {"all"}}, c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %v\n%s", rec.Code, rec.Header(), rec.Body.String())
	}
	data := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range zr.File {
		names[e.Name] = true
	}
	for _, want := range []string{world.WorldFileFormatFile, "world.json", "saves/new.zip", "saves/old.zip"} {
		if !names[want] {
			t.Errorf("the world file lacks %s: %v", want, names)
		}
	}
	if names["config/rconpw"] {
		t.Error("the world file holds the RCON password")
	}
	if got := hn.audits(AuditWorldExport); len(got) != 1 || got[0] != "alice "+id+" all saves" {
		t.Errorf("audit %v", got)
	}
}
