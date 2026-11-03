package web

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return r
}

// A 60-character name with markup in it: it must be escaped and must wrap.
var longName = "<b>" + strings.Repeat("n", 53) + "</b>"

type pageCase struct {
	name   string
	user   string
	status int
	page   Page
}

func pageCases() []pageCase {
	return []pageCase{
		{"worlds empty", "todd", 200, WorldsPage{}},
		{"worlds long user", longName, 200, WorldsPage{}},
		{"worlds listed", "todd", 200, WorldsPage{Worlds: []world.Summary{
			{ID: "base", Name: longName, GameVersion: "2.0.72", SpaceAge: true, Active: true, DiskBytes: 2_100_000_000,
				LastPlayed: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)},
			{ID: "future", Name: "Future", Newer: true},
			{ID: "broken", Name: "broken", Unreadable: true},
		}}},
		{"saves empty", "todd", 200, SavesPage{WorldName: longName, WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves",
			LoadHref: "/worlds/base/saves", NewestSelected: true, UploadLimit: MaxSaveUpload}},
		{"saves listed", "todd", 200, SavesPage{WorldName: longName, WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves",
			LoadHref: "/worlds/base/load-save", UploadLimit: MaxSaveUpload, LoadSave: "a.zip", OlderHref: "/worlds/base/saves?all=1",
			Uploaded: "a.zip", Chosen: true, Rows: []SaveRow{
				{Name: "a.zip", Href: "/worlds/base/saves/a.zip", Size: 2_100_000_000, Loads: true, Selected: true,
					Saved: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)},
				{Name: longName, Href: "/worlds/base/saves/x.zip", Size: 12},
			}}},
		{"saves errors", "todd", 422, SavesPage{WorldName: "Base", WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves",
			LoadHref: "/worlds/base/load-save", UploadLimit: MaxSaveUpload, LoadSave: "gone.zip", LoadMissing: true, NewestSelected: true,
			UploadError: "This file is not a zip file.", LoadError: "Choose which save loads."}},
		{"saves newer", "todd", 200, SavesPage{WorldName: "Base", Newer: true, WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves"}},
		{"world", "todd", 200, WorldPage{World: world.Summary{ID: "base", Name: longName, Active: true}, Name: longName}},
		{"world rename error", "todd", 400, WorldPage{World: world.Summary{ID: "base", Name: "Base"}, Name: "Other",
			NameError: "Another world already has this name. Choose a different name."}},
		{"world newer", "todd", 200, WorldPage{World: world.Summary{ID: "future", Name: "Future", Newer: true}}},
		{"import empty", "todd", 200, ImportPage{InboxDir: "/data/import", Folder: ImportForm{IDPrefix: "folder"}, Upload: ImportForm{IDPrefix: "upload"}}},
		{"import errors", "todd", 400, ImportPage{InboxDir: "/data/import",
			Inbox:  []world.InboxFolder{{Name: longName, Saves: 1, Bytes: 10}, {Name: "b", Saves: 2, HasMods: true}},
			Folder: ImportForm{IDPrefix: "folder", Source: "b", SourceError: "Choose a folder.", NameError: "Enter a name.", VersionError: "Bad version."},
			Upload: ImportForm{IDPrefix: "upload"}}},
		{"import upload errors", "todd", 400, ImportPage{InboxDir: "/data/import", Folder: ImportForm{IDPrefix: "folder"},
			Upload: ImportForm{IDPrefix: "upload", SourceError: "Not a zip.", NameError: "Enter a name.", VersionError: "Bad version."}}},
		{"signed out", "", 200, SignedOutPage{}},
		{"sign in empty", "", 200, SignInPage{}},
		{"sign in one error", "", 401, SignInPage{Username: "todd",
			PasswordError: "The username or password is not right. Check both and try again."}},
		{"sign in two errors", "", 400, SignInPage{UsernameError: "Enter your username", PasswordError: "Enter your password"}},
		{"sign in rate limited", "", 429, SignInPage{Username: longName, RateLimited: true, RetryAfter: 14*time.Minute + time.Second}},
		{"setup empty", "", 200, SetupPage{}},
		{"setup with hint", "", 200, SetupPage{MinPasswordLength: 12}},
		{"setup all errors", "", 400, SetupPage{Username: longName, MinPasswordLength: 12, //nolint:gosec // error wording, not a credential
			UsernameError: "Enter a username", PasswordError: "Use at least 12 characters",
			ConfirmError: "Enter the same password in both fields"}},
		{"error 403", "todd", 403, ErrorPage{Status: 403}},
		{"error 404", "", 404, ErrorPage{Status: 404}},
		{"error 500", "", 500, ErrorPage{Status: 500}},
		{"error other with detail", "", 400, ErrorPage{Status: 400, Detail: "Choose a file that ends in .zip."}},
	}
}

func render(t *testing.T, r *Renderer, c pageCase) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := r.Render(rec, c.status, c.user, c.page); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return rec, rec.Body.String()
}

var (
	reInput      = regexp.MustCompile(`<input\b[^>]*>`)
	reID         = regexp.MustCompile(`\bid="([^"]*)"`)
	reDescribed  = regexp.MustCompile(`aria-describedby="([^"]*)"`)
	reHrefAnchor = regexp.MustCompile(`href="#([^"]*)"`)
	reTitle      = regexp.MustCompile(`<title>([^<]+)</title>`)
)

// Every page renders and meets the document rules every screen shares
// (SPEC 17.1, DESIGN 2 and 7.2).
func TestEveryPageRenders(t *testing.T) {
	r := newTestRenderer(t)
	for _, c := range pageCases() {
		t.Run(c.name, func(t *testing.T) {
			rec, body := render(t, r, c)

			if rec.Code != c.status {
				t.Errorf("status %d, want %d", rec.Code, c.status)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type %q", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control %q", got)
			}
			if !strings.HasPrefix(body, "<!doctype html>") {
				t.Errorf("body does not start with a doctype: %.40q", body)
			}
			if !strings.Contains(body, `<html lang="en">`) {
				t.Error("no lang on html")
			}
			if m := reTitle.FindAllStringSubmatch(body, -1); len(m) != 1 || strings.TrimSpace(m[0][1]) == "" {
				t.Errorf("want one non-empty title, got %v", m)
			}
			if n := strings.Count(body, "<h1"); n != 1 {
				t.Errorf("%d h1 elements, want 1", n)
			}
			if strings.Contains(body, "<b>") {
				t.Error("user text was not escaped")
			}

			ids := map[string]int{}
			for _, m := range reID.FindAllStringSubmatch(body, -1) {
				ids[m[1]]++
			}
			for id, n := range ids {
				if n > 1 {
					t.Errorf("id %q used %d times", id, n)
				}
			}
			for _, in := range reInput.FindAllString(body, -1) {
				// Radios and checkboxes sit inside their label (DESIGN 7.2).
				if strings.Contains(in, `type="radio"`) || strings.Contains(in, `type="checkbox"`) {
					continue
				}
				m := reID.FindStringSubmatch(in)
				if m == nil {
					t.Errorf("input without id: %s", in)
					continue
				}
				if !strings.Contains(body, `<label for="`+m[1]+`">`) {
					t.Errorf("input %q has no label", m[1])
				}
				if strings.Contains(in, "placeholder") {
					t.Errorf("input %q uses a placeholder", m[1])
				}
			}
			for _, m := range reDescribed.FindAllStringSubmatch(body, -1) {
				for _, id := range strings.Fields(m[1]) {
					if ids[id] == 0 {
						t.Errorf("aria-describedby names missing id %q", id)
					}
				}
			}
			for _, m := range reHrefAnchor.FindAllStringSubmatch(body, -1) {
				if ids[m[1]] == 0 {
					t.Errorf("link to missing id #%s", m[1])
				}
			}
			if strings.Contains(body, "autofocus") && strings.Count(body, "autofocus") != 1 {
				t.Error("more than one autofocus")
			}

			// Sign-out is a POST form shown only when someone is signed in (DESIGN 6).
			signOut := `<form method="post" action="` + PathSignOut + `"`
			if got := strings.Contains(body, signOut); got != (c.user != "") {
				t.Errorf("sign-out form shown=%v with user %q", got, c.user)
			}
		})
	}
}

// Each template file has a page type, so the test above covers every one.
func TestEveryTemplateHasAPage(t *testing.T) {
	r := newTestRenderer(t)
	files, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		if name == "layout" {
			continue
		}
		if _, ok := r.pages[name]; !ok {
			t.Errorf("template %s has no page type", f)
		}
	}
	covered := map[string]bool{}
	for _, c := range pageCases() {
		covered[c.page.templateName()] = true
	}
	for name := range r.pages {
		if !covered[name] {
			t.Errorf("page %s has no render case", name)
		}
	}
}

func TestErrorSummary(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		page    Page
		heading string
		links   []string
	}{
		{SignInPage{PasswordError: "x"}, "Fix this problem to continue", []string{"password"}},
		{SignInPage{UsernameError: "a", PasswordError: "b"}, "Fix 2 problems to continue", []string{"username", "password"}},
		{SetupPage{UsernameError: "a", PasswordError: "b", ConfirmError: "c"}, "Fix 3 problems to continue", []string{"username", "password", "confirm"}},
	}
	for _, c := range cases {
		_, body := render(t, r, pageCase{page: c.page, status: 400})
		if !strings.Contains(body, c.heading) {
			t.Errorf("%T: no heading %q", c.page, c.heading)
		}
		var got []string
		for _, m := range reHrefAnchor.FindAllStringSubmatch(body, -1) {
			if m[1] != "main" {
				got = append(got, m[1])
			}
		}
		if !slices.Equal(got, c.links) {
			t.Errorf("%T: summary links %v, want %v", c.page, got, c.links)
		}
		if !strings.Contains(body, `id="error-summary" tabindex="-1" autofocus`) {
			t.Errorf("%T: summary does not take focus", c.page)
		}
	}

	_, body := render(t, r, pageCase{page: SignInPage{}, status: 200})
	if strings.Contains(body, "error-summary") {
		t.Error("summary shown with no errors")
	}
}

// A form with errors says so first in the title, and the summary is named
// by its own content, not by aria-labelledby (WCAG 3.3.1).
func TestErrorTitleAndSummaryName(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		page  Page
		title string
	}{
		{SignInPage{}, "Sign in – Foreman"},
		{SignInPage{PasswordError: "x"}, "Error: Sign in – Foreman"},
		{SignInPage{RateLimited: true}, "Error: Sign in – Foreman"},
		{SignInPage{Busy: true}, "Error: Sign in – Foreman"},
		{SetupPage{}, "Create the first admin account – Foreman"},
		{SetupPage{ConfirmError: "x"}, "Error: Create the first admin account – Foreman"},
		{ErrorPage{Status: 404}, "Page not found – Foreman"},
	}
	for _, c := range cases {
		_, body := render(t, r, pageCase{page: c.page, status: 400})
		m := reTitle.FindStringSubmatch(body)
		if m == nil || m[1] != c.title {
			t.Errorf("%T %+v: title %v, want %q", c.page, c.page, m, c.title)
		}
		if strings.Contains(body, "aria-labelledby") {
			t.Errorf("%T: aria-labelledby on the page", c.page)
		}
	}
}

// Pages must not be framed by another site (clickjacking on the sign-in form).
func TestPagesRefuseFraming(t *testing.T) {
	r := newTestRenderer(t)
	for _, c := range pageCases() {
		rec, _ := render(t, r, c)
		if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Errorf("%s: Content-Security-Policy %q", c.name, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options %q", c.name, got)
		}
	}
}

func TestSignInRateLimit(t *testing.T) {
	cases := []struct {
		after time.Duration
		want  string
	}{
		{0, "a few minutes"},
		{time.Second, "1 minute"},
		{time.Minute, "1 minute"},
		{14*time.Minute + time.Second, "15 minutes"},
	}
	for _, c := range cases {
		if got := (SignInPage{RateLimited: true, RetryAfter: c.after}).Wait(); got != c.want {
			t.Errorf("Wait(%v) = %q, want %q", c.after, got, c.want)
		}
	}

	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 429, page: SignInPage{RateLimited: true, PasswordError: "x"}})
	if !strings.Contains(body, "Too many failed sign-ins. Wait a few minutes, then try again.") {
		t.Error("no rate-limit message")
	}
	if strings.Contains(body, "Fix this problem") {
		t.Error("rate limit should replace the field error summary")
	}
}

// A busy server is a form-level message: no field is marked invalid, and
// the message takes focus like the rate-limit one.
func TestSignInBusyIsFormLevel(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 503, page: SignInPage{Busy: true}})
	if !strings.Contains(body, "Foreman is busy. Wait a moment and try again.") {
		t.Error("no busy message")
	}
	if strings.Contains(body, "aria-invalid") {
		t.Error("a busy page marks a field invalid")
	}
	if !strings.Contains(body, `id="error-summary" tabindex="-1" autofocus`) {
		t.Error("busy message does not take focus")
	}
}

func TestSignInNeverEchoesPassword(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 401, page: SignInPage{Username: "todd", PasswordError: "x"}})
	for _, in := range reInput.FindAllString(body, -1) {
		if strings.Contains(in, `type="password"`) && strings.Contains(in, "value=") {
			t.Errorf("password input carries a value: %s", in)
		}
	}
	if !strings.Contains(body, `value="todd"`) {
		t.Error("username not refilled")
	}
}

func TestErrorPageCopy(t *testing.T) {
	for _, s := range []int{403, 404, 500, 418} {
		p := ErrorPage{Status: s}
		if p.Heading() == "" || p.Explanation() == "" {
			t.Errorf("status %d has no copy", s)
		}
	}
	if got := (ErrorPage{Status: 403, Detail: "Ask for the admins group."}).Explanation(); got != "Ask for the admins group." {
		t.Errorf("Detail not used: %q", got)
	}
}

// The vendored htmx is exactly the pinned release (STACK 3, SPEC 3.7).
func TestVendoredChecksums(t *testing.T) {
	sums, err := staticFS.ReadFile("static/vendor/SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		want, name, ok := strings.Cut(sc.Text(), "  ")
		if !ok {
			t.Fatalf("bad SHA256SUMS line %q", sc.Text())
		}
		body, err := staticFS.ReadFile("static/vendor/" + name)
		if err != nil {
			t.Fatalf("%s listed but missing: %v", name, err)
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s: sha256 %s, want %s", name, got, want)
		}
		listed[name] = true
	}
	if !listed["htmx.min.js"] {
		t.Error("htmx.min.js is not in SHA256SUMS")
	}
	entries, err := fs.ReadDir(staticFS, "static/vendor")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "SHA256SUMS" && !listed[e.Name()] {
			t.Errorf("vendored file %s has no checksum", e.Name())
		}
	}
}

func TestStaticHandler(t *testing.T) {
	r := newTestRenderer(t)
	h := r.Static()
	css, err := r.staticURL("foreman.css")
	if err != nil {
		t.Fatal(err)
	}
	js, err := r.staticURL("vendor/htmx.min.js")
	if err != nil {
		t.Fatal(err)
	}

	get := func(target string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get(css)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("css: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("hashed URL Cache-Control %q", cc)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("no nosniff")
	}
	etag := rec.Header().Get("ETag")
	if rec := get(css, "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d", rec.Code)
	}

	rec = get(js)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Errorf("js: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// An old or missing version must not be cached forever.
	for _, u := range []string{PathStatic + "foreman.css", PathStatic + "foreman.css?v=old"} {
		if cc := get(u).Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control %q", u, cc)
		}
	}

	for _, u := range []string{PathStatic, PathStatic + "vendor/", PathStatic + "vendor/SHA256SUMS", PathStatic + "nope.css", PathStatic + "../render.go"} {
		if rec := get(u); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", u, rec.Code)
		}
	}
}

// Every asset URL a page links to is served by the static handler.
func TestPageAssetsResolve(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, page: WorldsPage{}, user: "todd"})
	re := regexp.MustCompile(`(?:href|src)="(` + regexp.QuoteMeta(PathStatic) + `[^"]+)"`)
	found := re.FindAllStringSubmatch(body, -1)
	if len(found) != 2 {
		t.Fatalf("want the stylesheet and htmx, found %v", found)
	}
	for _, m := range found {
		u := strings.ReplaceAll(m[1], "&amp;", "&")
		rec := httptest.NewRecorder()
		r.Static().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != 200 {
			t.Errorf("%s: %d", u, rec.Code)
		}
	}
}

// The stylesheet uses colour only through tokens (DESIGN 3).
func TestCSSHasNoRawColoursOutsideTokens(t *testing.T) {
	css, err := staticFS.ReadFile("static/foreman.css")
	if err != nil {
		t.Fatal(err)
	}
	hex := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	for i, line := range strings.Split(string(css), "\n") {
		if hex.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "--color-") {
			t.Errorf("line %d: raw colour outside a token: %s", i+1, line)
		}
	}
}
