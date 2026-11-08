// Package modportaltest is a fake Factorio mod portal on httptest. It serves
// replies recorded from the real portal (testdata/), zips it generates with
// matching SHA-1 sums, and the failures a download can meet. It does not
// import modportal, so modportal's own tests can use it.
package modportaltest

import (
	"archive/zip"
	"bytes"
	"crypto/sha1" //nolint:gosec // the portal publishes SHA-1 sums
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

//go:embed testdata
var testdata embed.FS

// Credentials the fake accepts on a download.
const (
	Username = "foreman-test"
	Token    = "tok-4f9c7e1d2b8a-not-a-real-token" //nolint:gosec // a fake, made up for tests
)

// GameLine is the factorio_version of a release AddZip generates.
const GameLine = "2.0"

// Mode is how the fake answers a download of one generated file.
type Mode int

// The download modes.
const (
	Normal          Mode = iota
	WrongSHA1            // serves bytes that do not match the published SHA-1
	Oversized            // streams without end
	Redirect             // answers with Portal.Hops redirects on its own host, then the file
	RedirectForeign      // redirects to another host
	RedirectLogin        // redirects to /login
	Unauthorized         // answers 401
	Slow                 // sends nothing until the client gives up
	ServerError          // answers 500
)

// Served describes one generated release, as the client would see it.
type Served struct {
	Name, Version, FileName, DownloadURL, SHA1 string
	FactorioVersion                            string // the game line, such as "2.0"
	Body                                       []byte
}

type file struct {
	Served
	path string
	mode Mode
}

// Portal is the fake. URL is its base address.
type Portal struct {
	URL string

	// Hops is how many redirects the Redirect mode makes (default 1).
	Hops int
	// FullDelay holds each /full reply, so a test can see how many run at once.
	FullDelay time.Duration

	srv, other *httptest.Server
	closing    chan struct{}

	mu       sync.Mutex
	list     []byte
	fixtures map[string][]byte // /full replies by mod name
	gen      map[string][]*file
	files    map[string]*file // by download path
	apiFail  int

	listCalls   int
	fullCalls   map[string]int
	downloads   map[string]int
	foreignHits int
	inFull      int
	maxFull     int
}

// New starts a fake portal that closes when the test ends.
func New(t testing.TB) *Portal {
	t.Helper()
	p := &Portal{
		Hops:      1,
		closing:   make(chan struct{}),
		fixtures:  map[string][]byte{},
		gen:       map[string][]*file{},
		files:     map[string]*file{},
		fullCalls: map[string]int{},
		downloads: map[string]int{},
	}
	var err error
	if p.list, err = testdata.ReadFile("testdata/list.json"); err != nil {
		t.Fatal(err)
	}
	ents, err := fs.Glob(testdata, "testdata/mod_*.json")
	if err != nil || len(ents) == 0 {
		t.Fatalf("no mod fixtures: %v", err)
	}
	for _, f := range ents {
		b, err := testdata.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var head struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(b, &head); err != nil || head.Name == "" {
			t.Fatalf("fixture %s has no name", f)
		}
		p.fixtures[head.Name] = b
	}

	p.other = httptest.NewServer(http.HandlerFunc(p.serveForeign))
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	p.URL = p.srv.URL
	t.Cleanup(func() {
		close(p.closing)
		p.srv.Close()
		p.other.Close()
	})
	return p
}

// AddZip registers a generated release of a mod for GameLine: a small zip
// with an info.json, and the matching SHA-1. The mod's /full reply lists
// every release added for it, oldest first, ahead of any recorded fixture
// of the same name.
func (p *Portal) AddZip(name, version string, deps ...string) Served {
	return p.AddZipFor(GameLine, name, version, deps...)
}

// AddZipFor is AddZip for a release made for another game line, such as
// "2.1".
func (p *Portal) AddZipFor(line, name, version string, deps ...string) Served {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name + "_" + version + "/info.json")
	if err != nil {
		panic(err)
	}
	info, _ := json.Marshal(map[string]any{"name": name, "version": version, "factorio_version": line, "dependencies": deps})
	_, _ = w.Write(info)
	if err := zw.Close(); err != nil {
		panic(err)
	}
	sum := sha1.Sum(buf.Bytes()) //nolint:gosec // see import
	id := hex.EncodeToString(sum[:12])
	f := &file{
		Served: Served{
			Name:            name,
			Version:         version,
			FileName:        name + "_" + version + ".zip",
			DownloadURL:     "/download/" + url.PathEscape(name) + "/" + id,
			SHA1:            hex.EncodeToString(sum[:]),
			FactorioVersion: line,
			Body:            buf.Bytes(),
		},
		path: "/download/" + name + "/" + id,
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gen[name] = append(p.gen[name], f)
	p.files[f.path] = f
	return f.Served
}

// SetMode sets how the fake answers downloads of a generated release.
func (p *Portal) SetMode(s Served, m Mode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range p.files {
		if f.DownloadURL == s.DownloadURL {
			f.mode = m
		}
	}
}

// FailAPI makes every /api reply carry this status. Zero restores them.
func (p *Portal) FailAPI(status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.apiFail = status
}

// ListCalls counts the mod list requests so far.
func (p *Portal) ListCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.listCalls
}

// FullCalls counts the /full requests so far for a mod.
func (p *Portal) FullCalls(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fullCalls[name]
}

// Downloads counts the download requests so far for a mod.
func (p *Portal) Downloads(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.downloads[name]
}

// ForeignHits counts the requests that reached the other host.
func (p *Portal) ForeignHits() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.foreignHits
}

// MaxConcurrentFull is the most /full requests that ran at the same time.
func (p *Portal) MaxConcurrentFull() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxFull
}

// ForeignURL is the address of the second host that the redirect modes use.
func (p *Portal) ForeignURL() string { return p.other.URL }

func (p *Portal) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/mods":
		p.serveList(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/mods/") && strings.HasSuffix(r.URL.Path, "/full"):
		p.serveFull(w, r)
	case strings.HasPrefix(r.URL.Path, "/download/"):
		p.serveDownload(w, r)
	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		p.serveCDN(w, r)
	case r.URL.Path == "/login":
		_, _ = io.WriteString(w, "<html>log in</html>")
	default:
		http.NotFound(w, r)
	}
}

func (p *Portal) failing(w http.ResponseWriter) bool {
	p.mu.Lock()
	status := p.apiFail
	p.mu.Unlock()
	if status == 0 {
		return false
	}
	http.Error(w, "fake failure", status)
	return true
}

func (p *Portal) serveList(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.listCalls++
	p.mu.Unlock()
	if p.failing(w) {
		return
	}
	if r.URL.Query().Get("page_size") != "max" || r.URL.Query().Get("version") == "" {
		http.Error(w, "the fake needs page_size=max and version", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(p.list)
}

func (p *Portal) serveFull(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/mods/"), "/full")
	p.mu.Lock()
	p.fullCalls[name]++
	p.inFull++
	if p.inFull > p.maxFull {
		p.maxFull = p.inFull
	}
	delay := p.FullDelay
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.inFull--
		p.mu.Unlock()
	}()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	if p.failing(w) {
		return
	}
	p.mu.Lock()
	body, ok := p.fixtures[name]
	gen := p.gen[name]
	p.mu.Unlock()
	if len(gen) > 0 {
		body, ok = fullFor(name, gen), true
	}
	if !ok {
		http.Error(w, `{"message":"Mod not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body) //nolint:gosec // fixture or generated JSON, never a request value
}

func fullFor(name string, gen []*file) []byte {
	type release struct {
		DownloadURL string `json:"download_url"`
		FileName    string `json:"file_name"`
		InfoJSON    struct {
			Dependencies    []string `json:"dependencies"`
			FactorioVersion string   `json:"factorio_version"`
		} `json:"info_json"`
		SHA1    string `json:"sha1"`
		Version string `json:"version"`
	}
	doc := struct {
		Name      string    `json:"name"`
		Title     string    `json:"title"`
		Summary   string    `json:"summary"`
		Downloads int       `json:"downloads_count"`
		Releases  []release `json:"releases"`
	}{Name: name, Title: name, Summary: "generated", Downloads: 1}
	for _, f := range gen {
		var rel release
		rel.DownloadURL, rel.FileName, rel.SHA1, rel.Version = f.DownloadURL, f.FileName, f.SHA1, f.Version
		rel.InfoJSON.FactorioVersion = f.FactorioVersion
		rel.InfoJSON.Dependencies = depsOf(f.Body)
		doc.Releases = append(doc.Releases, rel)
	}
	b, _ := json.Marshal(doc)
	return b
}

func depsOf(zipBytes []byte) []string {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil || len(zr.File) == 0 {
		return nil
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()
	var info struct {
		Dependencies []string `json:"dependencies"`
	}
	_ = json.NewDecoder(rc).Decode(&info)
	return info.Dependencies
}

func (p *Portal) serveDownload(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	f := p.files[r.URL.Path]
	if f != nil {
		p.downloads[f.Name]++
	}
	p.mu.Unlock()
	if f == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if f.mode == Unauthorized || q.Get("username") != Username || q.Get("token") != Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch f.mode {
	case ServerError:
		http.Error(w, "broken", http.StatusInternalServerError)
	case RedirectLogin:
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
	case RedirectForeign:
		http.Redirect(w, r, p.other.URL+"/file", http.StatusFound)
	case Redirect:
		http.Redirect(w, r, cdnPath(f, p.Hops-1), http.StatusFound)
	case Slow:
		select {
		case <-r.Context().Done():
		case <-p.closing:
		case <-time.After(30 * time.Second):
		}
	case Oversized:
		p.stream(w, r)
	case WrongSHA1:
		body := append([]byte(nil), f.Body...)
		body[len(body)-1] ^= 0xff
		serveBytes(w, body)
	default:
		serveBytes(w, f.Body)
	}
}

func cdnPath(f *file, hopsLeft int) string {
	id := f.DownloadURL[strings.LastIndex(f.DownloadURL, "/")+1:]
	return "/cdn/" + id + "/" + strconv.Itoa(hopsLeft)
}

// serveCDN is the end of a Redirect chain: /cdn/<id>/<hops left>.
func (p *Portal) serveCDN(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/cdn/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	left, err := strconv.Atoi(parts[1])
	p.mu.Lock()
	var f *file
	for _, c := range p.files {
		if strings.HasSuffix(c.DownloadURL, "/"+parts[0]) {
			f = c
		}
	}
	p.mu.Unlock()
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	if left > 0 {
		http.Redirect(w, r, cdnPath(f, left-1), http.StatusFound)
		return
	}
	serveBytes(w, f.Body)
}

func serveBytes(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	_, _ = w.Write(b)
}

// stream writes zeros until the client goes away.
func (p *Portal) stream(w http.ResponseWriter, r *http.Request) {
	chunk := make([]byte, 32<<10)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-p.closing:
			return
		default:
		}
		if _, err := w.Write(chunk); err != nil {
			return
		}
	}
}

func (p *Portal) serveForeign(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	p.foreignHits++
	p.mu.Unlock()
	_, _ = fmt.Fprint(w, "the foreign host must never be reached")
}
