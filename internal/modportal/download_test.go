package modportal

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // the portal publishes SHA-1 sums
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
)

func releaseOf(s modportaltest.Served) Release {
	return Release{Version: s.Version, FileName: s.FileName, DownloadURL: s.DownloadURL, SHA1: s.SHA1, FactorioVersion: modportaltest.GameLine}
}

func download(c *Client, r Release, dst *os.File) error {
	return c.Download(context.Background(), modportaltest.Username, modportaltest.Token, r, dst)
}

func TestDownloadWritesAndChecks(t *testing.T) {
	p, c, _ := fakePortal(t)
	s := p.AddZip("Flow Control", "3.1.0", "base >= 2.0")
	dst := tempFile(t)
	if err := download(c, releaseOf(s), dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst.Name())
	if !bytes.Equal(got, s.Body) || p.Downloads("Flow Control") != 1 {
		t.Errorf("file differs from the served zip, or %d downloads", p.Downloads("Flow Control"))
	}
}

func TestDownloadAfterModLookup(t *testing.T) {
	p, c, _ := fakePortal(t)
	p.AddZip("tiny", "1.0.0")
	p.AddZip("tiny", "1.1.0", "base >= 2.0", "? other")
	m, err := c.Mod(context.Background(), "tiny")
	if err != nil || len(m.Releases) != 2 || m.Releases[1].Dependencies[1] != "? other" {
		t.Fatalf("%+v, %v", m, err)
	}
	dst := tempFile(t)
	if err := download(c, m.Releases[1], dst); err != nil || fileSize(t, dst) == 0 {
		t.Errorf("download: %v", err)
	}
}

func TestDownloadRedirects(t *testing.T) {
	for hops, wantOK := range map[int]bool{1: true, 2: true, 3: false, 4: false} {
		p, c, _ := fakePortal(t)
		p.Hops = hops
		s := p.AddZip("m", "1.0.0")
		p.SetMode(s, modportaltest.Redirect)
		dst := tempFile(t)
		err := download(c, releaseOf(s), dst)
		if wantOK && err != nil {
			t.Errorf("%d redirects: %v", hops, err)
		}
		if !wantOK {
			mustNoSecret(t, "too many redirects", err)
			if fileSize(t, dst) != 0 {
				t.Errorf("%d redirects left bytes in the file", hops)
			}
		}
	}
}

func TestDownloadFailures(t *testing.T) {
	type failCase struct {
		name   string
		mode   modportaltest.Mode
		want   error
		setup  func(*Client)
		manual func(*Client, Release) error
	}
	cases := []failCase{
		{name: "wrong sha1", mode: modportaltest.WrongSHA1, want: ErrChecksum},
		{name: "oversized stream", mode: modportaltest.Oversized, want: ErrUnreachable, setup: func(c *Client) { c.maxDownload = 4096 }},
		{name: "foreign redirect", mode: modportaltest.RedirectForeign, want: ErrUnreachable},
		{name: "login redirect", mode: modportaltest.RedirectLogin, want: ErrUnauthorized},
		{name: "401", mode: modportaltest.Unauthorized, want: ErrUnauthorized},
		{name: "server error", mode: modportaltest.ServerError, want: ErrUnreachable},
		{name: "slow reply", mode: modportaltest.Slow, want: ErrUnreachable, setup: func(c *Client) { c.downloadTimeout = 150 * time.Millisecond }},
		{name: "declared too large", mode: modportaltest.Normal, want: ErrUnreachable, setup: func(c *Client) { c.maxDownload = 10 }},
		{name: "wrong credentials", mode: modportaltest.Normal, want: ErrUnauthorized, manual: func(c *Client, r Release) error {
			return c.Download(context.Background(), modportaltest.Username, "wrong-"+modportaltest.Token, r, tempFile(t))
		}},
		{name: "no checksum published", mode: modportaltest.Normal, want: ErrChecksum, manual: func(c *Client, r Release) error {
			r.SHA1 = ""
			return download(c, r, tempFile(t))
		}},
		{name: "cancelled", mode: modportaltest.Slow, want: ErrUnreachable, manual: func(c *Client, r Release) error {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			return c.Download(ctx, modportaltest.Username, modportaltest.Token, r, tempFile(t))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, _ := fakePortal(t)
			if tc.setup != nil {
				tc.setup(c)
			}
			s := p.AddZip("m", "1.0.0")
			p.SetMode(s, tc.mode)
			var err error
			dst := tempFile(t)
			if tc.manual != nil {
				err = tc.manual(c, releaseOf(s))
			} else {
				err = download(c, releaseOf(s), dst)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			mustNoSecret(t, tc.name, err)
			if fileSize(t, dst) != 0 {
				t.Errorf("%d bytes were left in the file", fileSize(t, dst))
			}
			if tc.mode == modportaltest.RedirectForeign && p.ForeignHits() != 0 {
				t.Error("the foreign host was contacted")
			}
		})
	}
}

func TestDownloadForbiddenAndGone(t *testing.T) {
	for code, want := range map[int]error{http.StatusForbidden: ErrUnauthorized, http.StatusNotFound: ErrNotOnPortal} {
		c := handlerClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
		err := download(c, Release{Version: "1.0.0", FileName: "m_1.0.0.zip", DownloadURL: "/download/m/abc", SHA1: "x"}, tempFile(t))
		if !errors.Is(err, want) {
			t.Errorf("%d: got %v", code, err)
		}
		mustNoSecret(t, "status", err)
	}
}

func TestDownloadPortalDownAndRawErrorLeaks(t *testing.T) {
	dst := tempFile(t)
	r := Release{Version: "1.0.0", FileName: "m_1.0.0.zip", DownloadURL: "/download/m/abc", SHA1: "x"}
	c := NewForTest("http://127.0.0.1:1")
	err := download(c, r, dst)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("got %v", err)
	}
	mustNoSecret(t, "portal down", err)

	// The reason for rebuilding errors: what Go returns would leak.
	q := url.Values{"username": {modportaltest.Username}, "token": {modportaltest.Token}}
	_, raw := http.Get("http://127.0.0.1:1/download/m/abc?" + q.Encode()) //nolint:noctx,bodyclose // proves the raw error leaks
	if raw == nil || !strings.Contains(raw.Error(), modportaltest.Token) {
		t.Fatalf("the raw Go error does not carry the token, so this test proves nothing: %v", raw)
	}
}

func TestRequestErrorRemovesSecrets(t *testing.T) {
	err := requestError(errors.New(`bad Location "https://x/?token=`+url.QueryEscape("a+b/c")+`&u=bob" for a+b/c`), "a+b/c", "bob")
	if s := err.Error(); strings.Contains(s, "a+b") || strings.Contains(s, "%2F") || strings.Contains(s, "bob") || !errors.Is(err, ErrUnreachable) {
		t.Errorf("%v", err)
	}
}

func TestDownloadRefusesBadAddresses(t *testing.T) {
	p, c, _ := fakePortal(t)
	s := p.AddZip("m", "1.0.0")
	for _, u := range []string{
		"", "http://evil.example/download/m/x", "https://mods.factorio.com/download/m/x", "//evil.example/download/m/x",
		"/other/m/x", "download/m/x", "/download/../x", "/download/%2e%2e/x", "/download/m/x?a=b", "/download/m/x#f",
		`/download\m/x`, "/Download/m/x", "/download", "/download/m/x\n",
	} {
		r := releaseOf(s)
		r.DownloadURL = u
		err := download(c, r, tempFile(t))
		if err == nil {
			t.Errorf("%q was accepted", u)
		}
		mustNoSecret(t, u, err)
	}
	if p.Downloads("m") != 0 {
		t.Error("a refused address was requested")
	}
}

func TestDownloadRefusesBadFileNames(t *testing.T) {
	p, c, _ := fakePortal(t)
	s := p.AddZip("m", "1.0.0")
	for _, tc := range []struct{ file, version string }{
		{"m_1.0.1.zip", "1.0.0"}, {"m_1.0.0.tar", "1.0.0"}, {"m_1.0.0.zip", ""}, {"", "1.0.0"},
		{"_1.0.0.zip", "1.0.0"}, {"a/m_1.0.0.zip", "1.0.0"}, {`a\m_1.0.0.zip`, "1.0.0"},
		{"../m_1.0.0.zip", "1.0.0"}, {"m.._1.0.0.zip", "1.0.0"}, {".m_1.0.0.zip", "1.0.0"},
		{"m\n_1.0.0.zip", "1.0.0"}, {"m\x00_1.0.0.zip", "1.0.0"}, {"m\x7f_1.0.0.zip", "1.0.0"},
		{"m_1/0.0.zip", "1/0.0"}, {"m_1..0.zip", "1..0"}, {"m_1.0.0.zip\n", "1.0.0"},
	} {
		r := releaseOf(s)
		r.FileName, r.Version = tc.file, tc.version
		err := download(c, r, tempFile(t))
		if err == nil {
			t.Errorf("file %q version %q was accepted", tc.file, tc.version)
		}
		mustNoSecret(t, tc.file, err)
	}
	if p.Downloads("m") != 0 {
		t.Error("a refused release was requested")
	}
}

func TestDownloadAcceptsNamesWithUnderscoresAndSpaces(t *testing.T) {
	p, c, _ := fakePortal(t)
	for _, name := range []string{"a_b_c", "Power Armor MK3"} {
		s := p.AddZip(name, "0.1.0")
		if err := download(c, releaseOf(s), tempFile(t)); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestDownloadWriteFailureIsNotANetworkError(t *testing.T) {
	p, c, _ := fakePortal(t)
	s := p.AddZip("m", "1.0.0")
	dst := tempFile(t)
	_ = dst.Close()
	err := download(c, releaseOf(s), dst)
	if err == nil || errors.Is(err, ErrUnreachable) || errors.Is(err, ErrChecksum) {
		t.Errorf("got %v", err)
	}
}

// rt answers each request from a function, so the real redirect rule runs
// with no network.
type rt func(*http.Request) *http.Response

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r), nil }

func TestRealRedirectRule(t *testing.T) {
	for _, tc := range []struct {
		to string
		ok bool
	}{
		{"https://dl.factorio.com/file", true},
		{"https://mods-data.factorio.com/file", true},
		{"https://mods.factorio.com:8443/file", true},
		{"https://factorio.com/file", false},
		{"http://dl.factorio.com/file", false},
		{"https://evilfactorio.com/file", false},
		{"https://dl.factorio.com.evil.example/file", false},
		{"https://dl.factorio.com@evil.example/file", false},
		{"https://evil.example/file", false},
		{"ftp://dl.factorio.com/file", false},
	} {
		var reached []string
		c := New()
		c.dl.Transport = rt(func(r *http.Request) *http.Response {
			reached = append(reached, r.URL.Host)
			if r.URL.Host == "mods.factorio.com" {
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": {tc.to}}, Body: http.NoBody, Request: r}
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}
		})
		r := Release{Version: "1.0.0", FileName: "m_1.0.0.zip", DownloadURL: "/download/m/abc", SHA1: "da39a3ee5e6b4b0d3255bfef95601890afd80709"}
		err := download(c, r, tempFile(t))
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.to, err)
		}
		if !tc.ok {
			mustNoSecret(t, tc.to, err)
			if len(reached) != 1 {
				t.Errorf("%s: a refused redirect was followed: %v", tc.to, reached)
			}
		}
	}
}

func TestRealLoginRedirectIsUnauthorized(t *testing.T) {
	for _, loc := range []string{"https://mods.factorio.com/login?next=/x", "https://www.factorio.com/login"} {
		c := New()
		c.dl.Transport = rt(func(r *http.Request) *http.Response {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {loc}}, Body: http.NoBody, Request: r}
		})
		r := Release{Version: "1.0.0", FileName: "m_1.0.0.zip", DownloadURL: "/download/m/abc", SHA1: "x"}
		err := download(c, r, tempFile(t))
		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: got %v", loc, err)
		}
		mustNoSecret(t, loc, err)
	}
}

func TestRealRequestShape(t *testing.T) {
	var got *http.Request
	c := New()
	c.dl.Transport = rt(func(r *http.Request) *http.Response {
		got = r
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: http.NoBody, Request: r}
	})
	r := Release{Version: "3.1.0", FileName: "Flow Control_3.1.0.zip", DownloadURL: "/download/Flow%20Control/abc", SHA1: "x"}
	_ = c.Download(context.Background(), "bob", "t o+k", r, tempFile(t))
	if got.URL.Scheme != "https" || got.URL.Host != "mods.factorio.com" || got.URL.EscapedPath() != "/download/Flow%20Control/abc" ||
		got.URL.Query().Get("username") != "bob" || got.URL.Query().Get("token") != "t o+k" {
		t.Errorf("request was %s", got.URL.Redacted())
	}
}

// SPEC 14.10: the token rides in the download's query, and Go copies the
// previous address into Referer on a redirect, so the header is removed.
func TestDownloadRedirectSendsNoReferer(t *testing.T) {
	body := []byte("zipbytes")
	sum := sha1.Sum(body) //nolint:gosec // the portal's own sum
	var referer []string
	c := handlerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download/m/1" {
			http.Redirect(w, r, "/files/m_1.0.0.zip", http.StatusFound)
			return
		}
		referer = append(referer, r.Header.Get("Referer"))
		_, _ = w.Write(body)
	})
	r := Release{Version: "1.0.0", FileName: "m_1.0.0.zip", DownloadURL: "/download/m/1", SHA1: hex.EncodeToString(sum[:])}
	if err := download(c, r, tempFile(t)); err != nil {
		t.Fatal(err)
	}
	if len(referer) != 1 || referer[0] != "" {
		t.Errorf("Referer on the redirected request %q, want none", referer)
	}
}
