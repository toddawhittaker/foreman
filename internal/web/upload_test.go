package web

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// Ordinary forms keep the small body cap; an upload route gets its own,
// and runs on a real server so its deadlines can be lifted.
func TestUploadRouteHasItsOwnBodyLimit(t *testing.T) {
	s := &server{mux: http.NewServeMux(), uploadLimits: map[string]int64{}, log: slog.New(slog.DiscardHandler)}
	read := func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		w.Header().Set("X-Read", strconv.FormatInt(n, 10))
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	}
	const limit = 1 << 20
	s.handleUpload("POST /up", limit, read)
	s.mux.HandleFunc("POST /form", read)
	ts := httptest.NewServer(s.limitBody(s.mux))
	defer ts.Close()

	post := func(path string, n int) *http.Response {
		t.Helper()
		resp, err := http.Post(ts.URL+path, "application/octet-stream", strings.NewReader(strings.Repeat("x", n)))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp
	}
	if r := post("/up", 512<<10); r.StatusCode != 200 || r.Header.Get("X-Read") != strconv.Itoa(512<<10) {
		t.Errorf("upload under its limit: %d, read %s", r.StatusCode, r.Header.Get("X-Read"))
	}
	if r := post("/form", 512<<10); r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("form over %d bytes: %d", maxFormBytes, r.StatusCode)
	}
	if r := post("/up", limit+1); r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("upload over its limit: %d", r.StatusCode)
	}
}

func multipartBody(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if err := mw.WriteField("note", "ignored"); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, mw.FormDataContentType()
}

func uploadRequest(t *testing.T, body *bytes.Buffer, ct string, limit int64) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/up", body)
	r.Header.Set("Content-Type", ct)
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, limit)
	return r
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestReceiveFileStreamsToTempBesideDestination(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "saves"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)
	content := bytes.Repeat([]byte("z"), 5000)
	body, ct := multipartBody(t, "save", `C:\games\my save.zip`, content)

	up, err := receiveFile(uploadRequest(t, body, ct, 1<<20), root, "saves", "save", dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if up.FileName != "my save.zip" || up.Size != 5000 || filepath.Dir(up.Temp) != "saves" ||
		!strings.HasPrefix(filepath.Base(up.Temp), world.PrefixPartial) {
		t.Fatalf("uploaded %+v", up)
	}
	info, err := os.Stat(filepath.Join(dir, up.Temp))
	if err != nil || info.Mode().Perm() != world.FileMode || info.Size() != 5000 {
		t.Fatalf("temp file: %v %v", info, err)
	}
	if err := world.Commit(root, up.Temp, "saves/my save.zip"); err != nil {
		t.Fatal(err)
	}
	if got := dirNames(t, filepath.Join(dir, "saves")); len(got) != 1 || got[0] != "my save.zip" {
		t.Errorf("saves: %v", got)
	}
}

func TestReceiveFileRefusals(t *testing.T) {
	dir := t.TempDir()
	root := openRoot(t, dir)

	// Over the limit while streaming: nothing is left behind.
	body, ct := multipartBody(t, "save", "a.zip", bytes.Repeat([]byte("z"), 3000))
	r := uploadRequest(t, body, ct, 2000)
	r.ContentLength = -1
	_, err := receiveFile(r, root, ".", "save", dir, 2000)
	var ue uploadError
	if !errors.As(err, &ue) || !strings.Contains(ue.msg, "larger than the limit") {
		t.Errorf("over limit: %v", err)
	}
	if got := dirNames(t, dir); len(got) != 0 {
		t.Errorf("left behind: %v", got)
	}

	// A declared length over the limit is refused before reading.
	body, ct = multipartBody(t, "save", "a.zip", []byte("z"))
	r = uploadRequest(t, body, ct, 1<<20)
	r.ContentLength = 1 << 21
	if _, err := receiveFile(r, root, ".", "save", dir, 1<<20); !errors.As(err, &ue) {
		t.Errorf("declared too big: %v", err)
	}

	// A declared length over the free space is refused up front.
	body, ct = multipartBody(t, "save", "a.zip", []byte("z"))
	r = uploadRequest(t, body, ct, 1<<62)
	r.ContentLength = 1 << 61
	if _, err := receiveFile(r, root, ".", "save", dir, 1<<62); !errors.As(err, &ue) || !strings.Contains(ue.msg, "free") {
		t.Errorf("no space: %v", err)
	}

	// No file in the expected field.
	body, ct = multipartBody(t, "other", "a.zip", []byte("z"))
	if _, err := receiveFile(uploadRequest(t, body, ct, 1<<20), root, ".", "save", dir, 1<<20); !errors.As(err, &ue) {
		t.Errorf("missing field: %v", err)
	}
	// Not multipart at all.
	r = httptest.NewRequest(http.MethodPost, "/up", strings.NewReader("a=b"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := receiveFile(r, root, ".", "save", dir, 1<<20); !errors.As(err, &ue) {
		t.Errorf("not multipart: %v", err)
	}
	if got := dirNames(t, dir); len(got) != 0 {
		t.Errorf("left behind: %v", got)
	}
}

func writeZip(t *testing.T, path string, entries map[string]int) {
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
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckZip(t *testing.T) {
	dir := t.TempDir()
	root := openRoot(t, dir)
	writeZip(t, filepath.Join(dir, "good.zip"), map[string]int{"save/level.dat0": 100, "save/control.lua": 50})
	writeZip(t, filepath.Join(dir, "dotdot.zip"), map[string]int{"../evil": 1})
	writeZip(t, filepath.Join(dir, "abs.zip"), map[string]int{"/etc/passwd": 1})
	writeZip(t, filepath.Join(dir, "big.zip"), map[string]int{"a": 600, "b": 600})
	if err := os.WriteFile(filepath.Join(dir, "notzip.zip"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkZip(root, "good.zip", 1000); err != nil {
		t.Errorf("good: %v", err)
	}
	for name, want := range map[string]string{
		"dotdot.zip": "unsafe name", "abs.zip": "unsafe name", "big.zip": "more than", "notzip.zip": "not a zip",
	} {
		var ue uploadError
		if err := checkZip(root, name, 1000); !errors.As(err, &ue) || !strings.Contains(ue.msg, want) {
			t.Errorf("%s: %v, want %q", name, err, want)
		}
	}
	if err := checkZip(root, "../outside.zip", 1000); err == nil {
		t.Error("opened a file outside the root")
	}
}
