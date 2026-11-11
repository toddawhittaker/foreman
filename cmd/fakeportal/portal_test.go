package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
)

var fake *Fake

// The real portal client, unchanged, must reach the fake through nothing but
// the environment (HTTPS_PROXY and SSL_CERT_FILE). Go reads both once, so they
// are set before any request is made.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakeportal-test-")
	if err != nil {
		panic(err)
	}
	t := &exitTB{}
	fake, err = Start(t, dir)
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HTTPS_PROXY", fake.Proxy)
	_ = os.Setenv("SSL_CERT_FILE", fake.CAFile)
	code := m.Run()
	fake.Close()
	t.cleanup()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func control(t *testing.T, path string, body any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(fake.Control+path, "application/json", bytes.NewReader(b)) //nolint:noctx // a test hook on loopback
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s: %d %s", path, resp.StatusCode, msg)
	}
}

type state struct {
	Requests []request `json:"requests"`
	Connects int       `json:"connects"`
	Refused  []string  `json:"refused"`
}

func getState(t *testing.T) (state, string) {
	t.Helper()
	resp, err := http.Get(fake.Control + "/state") //nolint:noctx // a test hook on loopback
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var s state
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s, string(raw)
}

func count(s state, host, path string) int {
	n := 0
	for _, r := range s.Requests {
		if r.Host == host && strings.HasPrefix(r.Path, path) {
			n++
		}
	}
	return n
}

func download(t *testing.T, c *modportal.Client, user, token, mod, version string) error {
	t.Helper()
	m, err := c.Mod(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	var rel modportal.Release
	for _, r := range m.Releases {
		if r.Version == version {
			rel = r
		}
	}
	if rel.Version == "" {
		t.Fatalf("%s %s is not on the fake", mod, version)
	}
	f, err := os.CreateTemp(t.TempDir(), "dl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	return c.Download(context.Background(), user, token, rel, f)
}

func TestRealClientReadsTheFakeThroughTheProxy(t *testing.T) {
	c := modportal.New()
	hits, err := c.Search(context.Background(), "2.0", "visible")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Name != "visible-planets" {
		t.Fatalf("hits = %+v", hits)
	}
	m, err := c.Mod(context.Background(), "visible-planets")
	if err != nil {
		t.Fatal(err)
	}
	// The case of a mod the portal lacks: 1.5.5 is installed in a world but the portal lacks it.
	for _, r := range m.Releases {
		if r.Version == "1.5.5" {
			t.Fatal("visible-planets 1.5.5 must be absent from the fake portal")
		}
	}
	s, _ := getState(t)
	if count(s, "mods.factorio.com", "/api/mods") < 2 {
		t.Errorf("requests = %+v", s.Requests)
	}
}

func TestDownloadRedirectsToADownloadHostAndChecksTheSum(t *testing.T) {
	control(t, "/add", map[string]any{"name": "fp-dl", "version": "1.0.0"})
	c := modportal.New()
	if err := download(t, c, modportaltest.Username, modportaltest.Token, "fp-dl", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	s, _ := getState(t)
	if count(s, "mods.factorio.com", "/download/fp-dl/") != 1 || count(s, "dl-mod.factorio.com", "/cdn/") != 1 {
		t.Errorf("the download did not go portal then download host: %+v", s.Requests)
	}
}

func TestDownloadWithoutCredentialsIsSentToLogin(t *testing.T) {
	control(t, "/add", map[string]any{"name": "fp-nocreds", "version": "1.0.0"})
	before, _ := getState(t)
	err := download(t, modportal.New(), "", "", "fp-nocreds", "1.0.0")
	if !errors.Is(err, modportal.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
	s, _ := getState(t)
	if count(s, "dl-mod.factorio.com", "/cdn/") != count(before, "dl-mod.factorio.com", "/cdn/") {
		t.Error("a download with no credentials must not reach the file")
	}
}

func TestModesChangeWhatADownloadMeets(t *testing.T) {
	for mode, want := range map[string]error{
		"wrong-sha1":     modportal.ErrChecksum,
		"unauthorized":   modportal.ErrUnauthorized,
		"redirect-login": modportal.ErrUnauthorized,
		"server-error":   modportal.ErrUnreachable,
	} {
		t.Run(mode, func(t *testing.T) {
			name := "fp-" + mode
			control(t, "/add", map[string]any{"name": name, "version": "1.0.0"})
			control(t, "/mode", map[string]any{"name": name, "version": "1.0.0", "mode": mode})
			err := download(t, modportal.New(), modportaltest.Username, modportaltest.Token, name, "1.0.0")
			if !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
		})
	}
}

func TestAnAddedModIsSearchable(t *testing.T) {
	control(t, "/add", map[string]any{"name": "fp-searchable", "version": "2.3.4", "deps": []string{"base >= 2.0", "flib >= 0.16"}})
	hits, err := modportal.New().Search(context.Background(), "2.0", "fp-searchable")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].LatestVersion != "2.3.4" {
		t.Fatalf("hits = %+v", hits)
	}
	m, err := modportal.New().Mod(context.Background(), "fp-searchable")
	if err != nil || len(m.Releases) != 1 || len(m.Releases[0].Dependencies) != 2 {
		t.Fatalf("mod = %+v, err = %v", m, err)
	}
}

// A release may be made for another game line, so a 2.1 world has releases
// to choose from.
func TestAnAddedReleaseKeepsItsGameLine(t *testing.T) {
	control(t, "/add", map[string]any{"name": "fp-line", "version": "1.0.0"})
	control(t, "/add", map[string]any{"name": "fp-line", "version": "1.1.0", "line": "2.1"})
	m, err := modportal.New().Mod(context.Background(), "fp-line")
	if err != nil || len(m.Releases) != 2 {
		t.Fatalf("mod = %+v, err = %v", m, err)
	}
	if got := m.Releases[0].FactorioVersion + " " + m.Releases[1].FactorioVersion; got != "2.0 2.1" {
		t.Errorf("game lines %q, want 2.0 then 2.1", got)
	}
}

func TestPortalDownAndFailingAPI(t *testing.T) {
	control(t, "/down", map[string]any{"down": true})
	_, err := modportal.New().Mod(context.Background(), "visible-planets")
	control(t, "/down", map[string]any{"down": false})
	if !errors.Is(err, modportal.ErrUnreachable) {
		t.Fatalf("down: err = %v", err)
	}

	control(t, "/fail", map[string]any{"status": 401})
	_, err = modportal.New().Mod(context.Background(), "visible-planets")
	control(t, "/fail", map[string]any{"status": 0})
	if !errors.Is(err, modportal.ErrUnreachable) {
		t.Fatalf("401: err = %v", err)
	}
	if _, err := modportal.New().Mod(context.Background(), "visible-planets"); err != nil {
		t.Fatalf("after restore: %v", err)
	}
}

// The proxy is the only door out of the test, so it must stay shut to anything
// but the portal's own hosts, on port 443 (the guard against a test reaching
// the internet).
func TestTheProxyRefusesEveryOtherHost(t *testing.T) {
	for _, host := range []string{"example.com:443", "factorio.com:443", "mods.factorio.com:80", "evilfactorio.com:443", "mods.factorio.com.evil.com:443", "127.0.0.1:22", "mods.factorio.com"} {
		if hostAllowed(host) {
			t.Errorf("hostAllowed(%q) = true", host)
		}
	}
	for _, host := range []string{"mods.factorio.com:443", "dl-mod.factorio.com:443"} {
		if !hostAllowed(host) {
			t.Errorf("hostAllowed(%q) = false", host)
		}
	}

	resp, err := http.DefaultTransport.(*http.Transport).Clone().RoundTrip(connectRequest(t, "example.com:443"))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a tunnel to example.com went through")
	}
	if !strings.Contains(err.Error(), "Forbidden") {
		t.Errorf("err = %v, want 403 Forbidden from the proxy", err)
	}
	s, _ := getState(t)
	found := false
	for _, h := range s.Refused {
		found = found || h == "example.com:443"
	}
	if !found {
		t.Errorf("refused = %v", s.Refused)
	}
}

func connectRequest(t *testing.T, host string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// A client that does not trust the test CA must be refused: the CA file is
// what makes the fake believable, and nothing else may trust it.
func TestTheCertificateNeedsTheTestCA(t *testing.T) {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}}
	req := connectRequest(t, "mods.factorio.com:443")
	if resp, err := tr.RoundTrip(req); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a client with no trusted roots accepted the fake's certificate")
	}
}

func TestStateNeverHoldsTheToken(t *testing.T) {
	control(t, "/add", map[string]any{"name": "fp-secret", "version": "1.0.0"})
	_ = download(t, modportal.New(), modportaltest.Username, modportaltest.Token, "fp-secret", "1.0.0")
	_, raw := getState(t)
	for _, secret := range []string{modportaltest.Token, "token="} {
		if strings.Contains(raw, secret) {
			t.Errorf("state holds %q:\n%s", secret, raw)
		}
	}
}
