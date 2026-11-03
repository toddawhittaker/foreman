package web

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// These tests attack the HTTP side of the file-path and upload trust
// boundaries (SPEC 14): hostile path segments and form values, hostile
// zips, lying Content-Length headers, a disk that fills mid-upload, and two
// admins at once. Every refusal must leave the data folder as it was.

// The tiny-filesystem and disk-tree helpers mirror the ones in
// internal/world's tests; test files cannot be imported across packages.

const tinyFSEnv = "FOREMAN_TEST_TINY_FS"

// onTinyFS reruns the calling test in a user and mount namespace with
// TMPDIR on a fresh tmpfs mounted with opts. It returns true in the child.
func onTinyFS(t *testing.T, opts string) bool {
	t.Helper()
	if os.Getenv(tinyFSEnv) != "" {
		// The 2 GB reserve would refuse everything on a tiny filesystem.
		t.Cleanup(world.SetSpaceReserveForTest(0))
		return true
	}
	mnt := t.TempDir()
	probe := exec.Command("unshare", "-rm", "sh", "-c", `mount -t tmpfs -o "$1" tmpfs "$2"`, "sh", opts, mnt) //nolint:gosec // fixed test command
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("cannot mount a tmpfs in a user namespace here (%v: %s); this test needs unshare and unprivileged user namespaces", err, out)
	}
	cmd := exec.Command("unshare", "-rm", "sh", "-c", //nolint:gosec // fixed test command
		`mount -t tmpfs -o "$1" tmpfs "$2" && exec "$3" -test.run "$4" -test.v`,
		"sh", opts, mnt, os.Args[0], "^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), tinyFSEnv+"=1", "TMPDIR="+mnt)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("on a tiny filesystem: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "--- SKIP") {
		t.Skipf("skipped on the tiny filesystem:\n%s", out)
	}
	t.Logf("on a tiny filesystem:\n%s", out)
	return false
}

// dataTree records every entry under dir, symlinks by target, never
// followed.
func dataTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "link:" + target
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, err := os.ReadFile(p) //nolint:gosec // a test temp file
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out[rel] = "file:" + string(b) + "@" + info.ModTime().String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameData(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s: %s is %.80q, want %.80q", what, p, got[p], w)
		}
	}
	for p, g := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("%s: new entry %s (%.80q)", what, p, g)
		}
	}
}

// secCase is a world with a save and a snapshot, a second world, a secret
// file beside the worlds folder, and a signed-in admin.
type secCase struct {
	ls     *liveServer
	cookie http.Header
	id     string
	other  string
	snap   string
	data   string
}

const secret = "TOPSECRET"

func newSecCase(t *testing.T) *secCase {
	t.Helper()
	ls := newLiveServer(t, nil)
	c := ls.signIn()
	id, _ := savesWorld(t, ls.harness)
	other, err := ls.worlds.Create(t.Context(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := ls.worlds.TakeSnapshot(id, "target", "alice")
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Dir(ls.worlds.Dirs().Worlds)
	if err := os.WriteFile(filepath.Join(data, "secret.zip"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ls.worlds.Dirs().Imports, "real"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ls.worlds.Dirs().Imports, "real", "a.zip"), saveZip(t), 0o600); err != nil {
		t.Fatal(err)
	}
	return &secCase{ls: ls, cookie: http.Header{"Cookie": {c.String()}}, id: id, other: other, snap: snap.Name, data: data}
}

func (sc *secCase) tree(t *testing.T) map[string]string {
	t.Helper()
	return dataTree(t, sc.data)
}

// SPEC 14: hostile path segments and form values over HTTP, with dots,
// encoded slashes, absolute paths and control characters, are refused
// without a server fault, never reveal a file outside the target, and
// change nothing.
func TestHostilePathsOverHTTP(t *testing.T) {
	sc := newSecCase(t)
	before := sc.tree(t)
	badSegs := []string{
		"..", "%2e%2e", "..%2f..%2fsecret.zip", "%2e%2e%2f%2e%2e%2fsecret.zip", "%2fetc%2fpasswd",
		"a%00b", "a%0ab", "%0d%0a", "%5c..%5c..%5csecret.zip", "..%252f..%252fsecret.zip",
		"..%2f" + sc.other, "%2e", "x%7f",
	}
	type req struct {
		method, path string
		form         url.Values
	}
	var reqs []req
	for _, bad := range badSegs {
		reqs = append(reqs,
			req{"GET", "/worlds/" + bad, nil},
			req{"GET", "/worlds/" + bad + "/saves", nil},
			req{"POST", "/worlds/" + bad + "/rename", url.Values{"name": {"Hijacked"}}},
			req{"POST", "/worlds/" + bad + "/load-save", url.Values{"load": {"new.zip"}}},
			req{"GET", "/worlds/" + bad + "/snapshots", nil},
			req{"POST", "/worlds/" + bad + "/snapshots", url.Values{"label": {"x"}}},
			req{"GET", "/worlds/" + sc.id + "/saves/" + bad, nil},
			req{"GET", "/worlds/" + sc.id + "/saves/" + bad + ".zip", nil},
			req{"GET", "/worlds/" + bad + "/snapshots/" + sc.snap + "/restore", nil},
			req{"POST", "/worlds/" + bad + "/snapshots/" + sc.snap + "/restore", url.Values{}},
			req{"GET", "/worlds/" + sc.id + "/snapshots/" + bad + "/restore", nil},
			req{"POST", "/worlds/" + sc.id + "/snapshots/" + bad + "/restore", url.Values{}},
			req{"POST", "/worlds/" + sc.id + "/snapshots/" + sc.snap + bad + "/restore", url.Values{}},
		)
	}
	for _, bad := range []string{"../secret.zip", "../../secret.zip", "/etc/passwd", "a/b.zip", "new.zip\x00", "..", "../" + sc.other + "/world.json"} {
		reqs = append(reqs, req{"POST", "/worlds/" + sc.id + "/load-save", url.Values{"load": {bad}}})
	}
	for _, bad := range []string{"..", ".", "../worlds", "../worlds/" + sc.id, "/etc", "/", "real/..", "real/../..",
		"real\x00", "real\n", "%2e%2e", "../snapshots/" + sc.id, ".hidden", ""} {
		reqs = append(reqs, req{"POST", PathImportFolder, url.Values{"folder": {bad}, "name": {"Imported"}, "game_version": {"2.0.72"}}})
	}
	for _, r := range reqs {
		got := sc.ls.do(r.method, r.path, r.form, sc.cookie)
		what := fmt.Sprintf("%s %s %v", r.method, r.path, r.form)
		if got.StatusCode == http.StatusOK || got.StatusCode == http.StatusSeeOther || got.StatusCode >= 500 {
			t.Errorf("%s: status %d", what, got.StatusCode)
		}
		if strings.Contains(got.body, secret) {
			t.Errorf("%s: response holds the secret file", what)
		}
	}
	sameData(t, "data folder", sc.tree(t), before)
}

// rawZip writes entries with the names and stated unpacked sizes given,
// whatever their real contents, as a hostile tool would.
func rawZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, e := range entries {
		body := []byte("x")
		w, err := zw.CreateRaw(&zip.FileHeader{
			Name: e.name, Method: zip.Store, CRC32: crc32.ChecksumIEEE(body),
			CompressedSize64: uint64(len(body)), UncompressedSize64: e.stated,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type zipEntry struct {
	name   string
	stated uint64
}

func hostileZips(t *testing.T) map[string][]byte {
	t.Helper()
	good := saveZip(t)
	return map[string][]byte{
		"dot-dot entry":         rawZip(t, []zipEntry{{"level.dat", 1}, {"../../world.json", 1}}),
		"nested dot-dot entry":  rawZip(t, []zipEntry{{"save/../../../etc/cron.d/x", 1}}),
		"absolute entry":        rawZip(t, []zipEntry{{"/etc/passwd", 1}}),
		"bare dot-dot entry":    rawZip(t, []zipEntry{{"..", 1}}),
		"empty entry name":      rawZip(t, []zipEntry{{"", 1}}),
		"stated size over cap":  rawZip(t, []zipEntry{{"level.dat", 1 << 40}}),
		"stated sizes overflow": rawZip(t, []zipEntry{{"a", 1 << 63}, {"b", 1 << 63}, {"c", 5}}),
		"truncated zip":         good[:len(good)-10],
		"not a zip":             []byte("this is plain text pretending to be a save"),
		"empty file":            {},
	}
}

func (sc *secCase) upload(t *testing.T, path string, fields map[string]string, filename string, content []byte) reply {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, k := range []string{"name", "game_version", "space_age"} {
		if v, ok := fields[k]; ok {
			_ = mw.WriteField(k, v)
		}
	}
	fw, err := mw.CreateFormFile("save", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	return sc.send(t, path, mw.FormDataContentType(), &b, int64(b.Len()))
}

// send posts body with Content-Type ct; length -1 sends it chunked, with
// no declared length.
func (sc *secCase) send(t *testing.T, path, ct string, body io.Reader, length int64) reply {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, sc.ls.url+path, io.NopCloser(body))
	if err != nil {
		t.Fatal(err)
	}
	r.ContentLength = length
	r.Header.Set("Content-Type", ct)
	r.Header.Set("Cookie", sc.cookie.Get("Cookie"))
	resp, err := sc.ls.client.Do(r)
	if err != nil {
		// The server may close the connection once it refuses a body.
		return reply{}
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, body: string(got)}
}

var hostileImportFields = map[string]string{"name": "Imported", "game_version": "2.0.72"}

// SPEC 14.6, R17: a hostile or broken zip is refused, as a save upload and
// as an import, and leaves no file and no world behind.
func TestHostileZipsAreRefused(t *testing.T) {
	sc := newSecCase(t)
	before := sc.tree(t)
	for name, z := range hostileZips(t) {
		got := sc.upload(t, "/worlds/"+sc.id+"/saves", nil, "hostile.zip", z)
		if got.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("save upload of %s: status %d, want 422", name, got.StatusCode)
		}
		got = sc.upload(t, PathImportUpload, hostileImportFields, "hostile.zip", z)
		if got.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("import upload of %s: status %d, want 422", name, got.StatusCode)
		}
		sameData(t, name, sc.tree(t), before)
	}
}

// SPEC 5.11, 14: the file name a browser sends is reduced to a plain save
// name; one that would reach outside the saves folder, or is not a save
// name, is refused and nothing outside saves/ changes.
func TestHostileUploadFileNames(t *testing.T) {
	sc := newSecCase(t)
	worldDir := filepath.Join(sc.ls.worlds.Dirs().Worlds, sc.id)
	for _, name := range []string{"../../secret.zip", `..\..\secret.zip`, "/etc/evil.zip", "../world.json",
		"x\n.zip", ".zip", ".hidden.zip", "..", "../" + sc.other + "/saves/x.zip", "C:\\Users\\a\\base.zip"} {
		outside := dataTree(t, sc.data)
		for p := range outside {
			if strings.HasPrefix(p, filepath.Join("worlds", sc.id, "saves")) {
				delete(outside, p)
			}
		}
		got := sc.upload(t, "/worlds/"+sc.id+"/saves", nil, name, saveZip(t))
		if got.StatusCode >= 500 {
			t.Errorf("upload as %q: status %d", name, got.StatusCode)
		}
		after := dataTree(t, sc.data)
		for p := range after {
			if strings.HasPrefix(p, filepath.Join("worlds", sc.id, "saves")) {
				delete(after, p)
			}
		}
		sameData(t, fmt.Sprintf("outside saves/ after upload as %q", name), after, outside)
		entries, _ := os.ReadDir(filepath.Join(worldDir, "saves"))
		for _, e := range entries {
			if !world.ValidSaveName(e.Name()) {
				t.Errorf("upload as %q left %q in saves/", name, e.Name())
			}
		}
	}
}

// rawPost sends a request by hand so its Content-Length can lie, then
// closes the sending side and reads whatever answer comes.
func (sc *secCase) rawPost(t *testing.T, path, ct string, body []byte, declared int64) int {
	t.Helper()
	u, _ := url.Parse(sc.ls.url)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n",
		path, u.Host, sc.cookie.Get("Cookie"), ct, declared)
	_, _ = conn.Write([]byte(head))
	_, _ = conn.Write(body)
	_ = conn.(*net.TCPConn).CloseWrite()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// SPEC 14.6: a NUL in the file name a browser sends makes the multipart
// header malformed. That is the client's fault and must be refused in
// words, not logged and answered as a server fault.
func TestMalformedMultipartHeaderIsARefusal(t *testing.T) {
	sc := newSecCase(t)
	before := sc.tree(t)
	for _, path := range []string{"/worlds/" + sc.id + "/saves", PathImportUpload} {
		got := sc.upload(t, path, hostileImportFields, "x\x00.zip", saveZip(t))
		if got.StatusCode < 400 || got.StatusCode >= 500 {
			t.Errorf("%s: status %d, want a 4xx refusal", path, got.StatusCode)
		}
	}
	sameData(t, "data folder", sc.tree(t), before)
}

// SPEC 14.6, R19: an upload whose Content-Length lies, high or low, never
// leaves a partial file, temp file or world behind. A body cut short by a
// low length is refused; a complete body under a high length may be kept,
// but only whole.
func TestUploadWithLyingContentLength(t *testing.T) {
	sc := newSecCase(t)
	zipped := saveZip(t)
	for i, declared := range []int64{1000, -40, -1 << 20, 0, MaxSaveUpload + 1, 1 << 62} {
		for _, path := range []string{"/worlds/" + sc.id + "/saves", PathImportUpload} {
			name := fmt.Sprintf("lying-%d.zip", i)
			var b bytes.Buffer
			mw := multipart.NewWriter(&b)
			_ = mw.WriteField("name", fmt.Sprintf("Imported %d", i))
			_ = mw.WriteField("game_version", "2.0.72")
			fw, _ := mw.CreateFormFile("save", name)
			_, _ = fw.Write(zipped)
			_ = mw.Close()
			n := int64(b.Len())
			length := max(n+declared, 0)
			if declared > MaxSaveUpload {
				length = declared
			}
			before := sc.tree(t)
			status := sc.rawPost(t, path, mw.FormDataContentType(), b.Bytes(), length)
			after := sc.tree(t)
			what := fmt.Sprintf("%s with Content-Length %d (real %d)", path, length, n)
			if length < n && status == http.StatusSeeOther {
				t.Errorf("%s: a cut-short body was accepted", what)
			}
			for p, v := range after {
				if _, ok := before[p]; ok || !strings.HasPrefix(v, "file:") {
					continue
				}
				if filepath.Base(p) == name && strings.HasPrefix(v, "file:"+string(zipped)+"@") {
					continue // the whole save, kept
				}
				if filepath.Base(p) == world.MetaFile && status == http.StatusSeeOther && path == PathImportUpload {
					continue // the new world an accepted import made
				}
				t.Errorf("%s: left %s (%.60q)", what, p, v)
			}
			if status != http.StatusSeeOther {
				sameData(t, what, after, before)
			}
		}
	}
	sc.ls.noStaging()
}

// R19: when the disk fills partway through an upload (here another writer
// takes the rest of the space after the upload began), the temp file is
// removed, no save or world appears, and the space is given back.
func TestDiskFullMidUpload(t *testing.T) {
	if !onTinyFS(t, "size=3m") {
		return
	}
	sc := newSecCase(t)
	before := sc.tree(t)
	free, err := world.FreeBytes(sc.data)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/worlds/" + sc.id + "/saves", PathImportUpload} {
		conn, rest := sc.slowUpload(t, path, "big.zip", bytes.Repeat([]byte("z"), 2<<20))
		// Send enough to start the temp file, then wait until it has grown.
		if _, err := conn.Write(rest[:600<<10]); err != nil {
			t.Fatal(err)
		}
		rest = rest[600<<10:]
		for range 100 {
			if now, _ := world.FreeBytes(sc.data); now+256<<10 < free {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		// Another writer takes whatever is left.
		filler := filepath.Join(sc.data, "filler")
		fillDisk(t, filler)
		_, _ = conn.Write(rest)
		status, body, ok := answer(conn, 10*time.Second)
		_ = conn.Close()
		if err := os.Remove(filler); err != nil {
			t.Fatal(err)
		}
		if !ok || status == http.StatusSeeOther || status == http.StatusOK || status >= 500 {
			t.Errorf("%s: status %d (answered %v), want a refusal in words", path, status, ok)
		}
		if !strings.Contains(body, "disk filled up") {
			t.Errorf("%s: the page does not say the disk filled up", path)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && len(sc.tree(t)) != len(before) {
			time.Sleep(50 * time.Millisecond)
		}
		sameData(t, path, sc.tree(t), before)
		after, err := world.FreeBytes(sc.data)
		if err != nil {
			t.Fatal(err)
		}
		if after+64<<10 < free {
			t.Errorf("%s: free space went from %d to %d; the partial upload was kept", path, free, after)
		}
	}
}

// fillDisk writes to name until the filesystem has no room left.
func fillDisk(t *testing.T, name string) {
	t.Helper()
	f, err := os.Create(name) //nolint:gosec // a test temp file
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	chunk := make([]byte, 64<<10)
	for {
		if _, err := f.Write(chunk); err != nil {
			return
		}
	}
}

// R15: while another operation holds a world, every change to it over
// HTTP is refused with the busy message and changes nothing.
func TestBusyWorldOverHTTP(t *testing.T) {
	sc := newSecCase(t)
	unlock, err := sc.ls.worlds.Lock(sc.id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	before := sc.tree(t)
	reqs := map[string]func() reply{
		"rename": func() reply {
			return sc.ls.do("POST", "/worlds/"+sc.id+"/rename", url.Values{"name": {"Taken"}}, sc.cookie)
		},
		"load save": func() reply {
			return sc.ls.do("POST", "/worlds/"+sc.id+"/load-save", url.Values{"load": {"old.zip"}}, sc.cookie)
		},
		"snapshot": func() reply { return sc.ls.do("POST", snapshotsPath(sc.id), url.Values{"label": {"x"}}, sc.cookie) },
		"restore":  func() reply { return sc.ls.do("POST", restorePath(sc.id, sc.snap), url.Values{}, sc.cookie) },
	}
	for name, do := range reqs {
		got := do()
		if got.StatusCode < 400 || got.StatusCode >= 500 {
			t.Errorf("%s while busy: status %d, want a refusal", name, got.StatusCode)
		}
		if !strings.Contains(strings.ToLower(got.body), "another operation is running on this world") {
			t.Errorf("%s while busy: the page does not say the world is busy", name)
		}
	}
	sameData(t, "data folder", sc.tree(t), before)
}

// R15: a finished save upload waits for a busy world instead of throwing
// the file away, and is saved once the world is free. (Giving up after the
// wait is tested in internal/world, which can shorten the wait.)
func TestSaveUploadWaitsForABusyWorld(t *testing.T) {
	sc := newSecCase(t)
	unlock, err := sc.ls.worlds.Lock(sc.id)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		unlock()
		close(released)
	}()
	got := sc.upload(t, "/worlds/"+sc.id+"/saves", nil, "waited.zip", saveZip(t))
	<-released
	if got.StatusCode != http.StatusSeeOther {
		t.Fatalf("upload to a world that was busy for a moment: status %d\n%s", got.StatusCode, got.body)
	}
	if _, err := os.Stat(filepath.Join(sc.ls.worlds.Dirs().Worlds, sc.id, "saves", "waited.zip")); err != nil {
		t.Errorf("the waited-for save is missing: %v", err)
	}
}
