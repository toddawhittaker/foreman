package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// One test per condition of SPEC 14.18 that Foreman's own code can break.

const secretText = "the-rcon-password-must-never-leave"

// leaks fails the test when a reply holds the secret file's text.
func leaks(t *testing.T, what string, rep reply) {
	t.Helper()
	if strings.Contains(rep.body, secretText) {
		t.Errorf("%s served a file that is not an offered mod zip", what)
	}
}

// SPEC 14.18, Paths: an address that is not byte for byte a listed zip's file
// name, in the running world, gets a plain 404 whatever tricks it plays.
func TestPublicModFilePathTricksGetA404(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0", "alpha_1.1.0", "beta_1.0.0")
	dir := filepath.Join(p.worlds.Dirs().Worlds, p.id)
	if err := os.WriteFile(filepath.Join(dir, "rconpw"), []byte(secretText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "saves", "a.zip"), []byte(secretText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.modsDir(p.id), world.ModListFile),
		[]byte(`{"mods":[{"name":"beta","enabled":false}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.modsDir(p.id), ".hidden_1.0.0.zip"), []byte(secretText), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link, even one inside the world, to a file that is no mod.
	if err := os.Symlink(filepath.Join(dir, "rconpw"), filepath.Join(p.modsDir(p.id), "evil_1.0.0.zip")); err != nil {
		t.Fatal(err)
	}
	// A folder named like a zip.
	if err := os.MkdirAll(filepath.Join(p.modsDir(p.id), "dir_1.0.0.zip"), 0o750); err != nil {
		t.Fatal(err)
	}

	base := PathPublicMods + "/" + p.id + "/files/"
	for _, file := range []string{
		"alpha_1.0.0.zip",             // the game loads 1.1.0
		"beta_1.0.0.zip",              // turned off
		"evil_1.0.0.zip",              // a link
		"dir_1.0.0.zip",               // a folder
		".hidden_1.0.0.zip",           // hidden
		"base",                        // built in
		"mod-list.json",               // not a mod
		"mod-settings.dat",            //
		"nope.zip",                    //
		"Alpha_1.1.0.zip",             // case
		"alpha_1.1.0.zip%20",          // trailing space
		"alpha_1.1.0.zip%00",          // NUL
		"alpha_1.1.0.zip/",            // trailing slash
		"alpha_1.1.0.zip/..",          //
		"%2e%2e%2fsaves%2fa.zip",      // encoded ../
		"..%2fsaves%2fa.zip",          //
		"..%2Frconpw",                 //
		"%2e%2e%5csaves%5ca.zip",      // encoded backslash
		"..\\saves\\a.zip",            //
		"%252e%252e%252fsaves%252fa",  // double encoded
		"alpha_1.1.0.zip%2f..%2fbeta", //
		"%2Fetc%2Fpasswd",             // absolute
		"..",                          //
		".",                           //
		"",                            //
	} {
		rep := p.anonFinal(base + file)
		if rep.StatusCode != http.StatusNotFound {
			t.Errorf("GET .../files/%s: %d, want 404", file, rep.StatusCode)
		}
		leaks(t, file, rep)
	}
	// The query is no part of the name: this is the real file, and only it.
	if rep := p.anon(base + "alpha_1.1.0.zip?x=1.zip"); rep.StatusCode != http.StatusOK || rep.Header.Get("Content-Length") == "" {
		t.Errorf("a query after the name: %d", rep.StatusCode)
	}
	// Control: the listed zip itself is served.
	if rep := p.anon(base + "alpha_1.1.0.zip"); rep.StatusCode != http.StatusOK {
		t.Errorf("the offered zip: %d", rep.StatusCode)
	}
}

// SPEC 14.18, Paths: the world must be the running world's id.
func TestPublicModFileOfAnotherWorldGetsA404(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	other, err := p.worlds.Create(context.Background(), "Other", v20, false)
	if err != nil {
		t.Fatal(err)
	}
	p.writeMod(other, "alpha", "1.0.0")
	p.writeMod(other, "other-only", "1.0.0")

	for _, path := range []string{
		PublicModFilePath(other, "alpha_1.0.0.zip"),
		PublicModFilePath(other, "other-only_1.0.0.zip"),
		PublicModsAllPath(other),
		PathPublicMods + "/nope/files/alpha_1.0.0.zip",
		PathPublicMods + "/" + strings.ToUpper(p.id) + "/files/alpha_1.0.0.zip",
		PathPublicMods + "/" + p.id + "%20/files/alpha_1.0.0.zip",
		PathPublicMods + "/..%2f" + p.id + "/files/alpha_1.0.0.zip",
		PathPublicMods + "/%2e%2e/" + p.id + "/files/alpha_1.0.0.zip",
		PathPublicMods + "/" + p.id + "%2f..%2f" + other + "/files/other-only_1.0.0.zip",
		PathPublicMods + "//files/alpha_1.0.0.zip",
	} {
		if rep := p.anonFinal(path); rep.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, rep.StatusCode)
		}
	}
}

// anonFinal is anon, following the one redirect the server's path cleaning
// may send, so a path with dots ends where any other path would.
func (p *publicRig) anonFinal(path string) reply {
	p.t.Helper()
	rep := p.anon(path)
	if rep.StatusCode >= 300 && rep.StatusCode < 400 {
		to := rep.Header.Get("Location")
		if !strings.HasPrefix(to, PathPublic) {
			p.t.Errorf("GET %s redirected to %q, outside the public paths", path, to)
		}
		return p.anon(to)
	}
	return rep
}

// SPEC 9.16: while the game is stopped, starting, stopping or crash-looping
// the world is not running and nothing is served.
func TestPublicModFileOfAStoppedWorldGetsA404(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	file, all := PublicModFilePath(p.id, "alpha_1.0.0.zip"), PublicModsAllPath(p.id)
	if p.anon(file).StatusCode != http.StatusOK {
		t.Fatal("a running world's mod is not served")
	}
	p.wantRedirectHome(p.post(gamePath(p.id, "stop"), url.Values{"countdown": {"0"}}))
	p.waitPage(stopped)
	for _, path := range []string{file, all} {
		if rep := p.anon(path); rep.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s with the game stopped: %d, want 404", path, rep.StatusCode)
		}
	}
}

// SPEC 14.18, Paths: the same-world check stops a world swapped under a
// listing, and a switch to another world ends the offer of the first.
func TestPublicModFileStopsServingWhenAnotherWorldIsActive(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	other, err := p.worlds.Create(context.Background(), "Other", v20, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.store.SetActiveWorld(context.Background(), other, p.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if rep := p.anon(PublicModFilePath(p.id, "alpha_1.0.0.zip")); rep.StatusCode != http.StatusNotFound {
		t.Errorf("a world that is no longer the active one: %d, want 404", rep.StatusCode)
	}
}

// SPEC 14.18, No identity: a public route never reads a session, trusted
// headers or an API token, never creates a user and sets no cookie.
func TestPublicRoutesReadNoSessionAndSetNoCookie(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	hash := sha256.Sum256([]byte(p.cookie.Value))
	token := hex.EncodeToString(hash[:])
	_, before, found, err := p.store.SessionUser(context.Background(), token)
	if err != nil || !found {
		t.Fatalf("the signed-in session: found=%v err=%v", found, err)
	}
	// Longer than the session's refresh interval, so reading it would refresh it.
	p.clock.Advance(10 * time.Minute)

	withIdentity := http.Header{
		"Cookie":          {p.cookie.String()},
		"Authorization":   {"Bearer abc"},
		"X-Forwarded-For": {"203.0.113.9"},
	}
	for _, path := range []string{
		PathPublicMods,
		PublicModFilePath(p.id, "alpha_1.0.0.zip"),
		PublicModsAllPath(p.id),
		PublicModFilePath(p.id, "nope.zip"),
		PathPublic + "nope",
	} {
		rep := p.do(http.MethodGet, path, nil, withIdentity)
		if cookies := rep.Header.Values("Set-Cookie"); len(cookies) != 0 {
			t.Errorf("GET %s set cookies %v", path, cookies)
		}
		if strings.Contains(rep.body, "alice") {
			t.Errorf("GET %s knew the signed-in user", path)
		}
	}
	_, after, found, err := p.store.SessionUser(context.Background(), token)
	if err != nil || !found || !after.Equal(before) {
		t.Errorf("the session was read: found=%v, last seen %v then %v, err=%v", found, before, after, err)
	}

	// The same holds while the page is off.
	p.setting(false)
	if rep := p.do(http.MethodGet, PathPublicMods, nil, withIdentity); len(rep.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("an off page set cookies %v", rep.Header.Values("Set-Cookie"))
	}
}

// SPEC 14.18, No identity: trusted headers from the proxy sign nobody in
// and make no user on a public path.
func TestPublicRoutesIgnoreTrustedHeaders(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	if err := hn.store.SetAppSetting(context.Background(), SettingPublicMods, PublicModsOn, "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{PathPublicMods, PathPublic + "nope", PathPublicMods + "/w/files/x.zip", PathPublicMods + "/w/all.zip"} {
		rec := hn.get(path, asUser("mallory")...)
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusSeeOther {
			t.Errorf("GET %s: %d, a public path asked for a sign-in", path, rec.Code)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Errorf("GET %s set cookies", path)
		}
		if strings.Contains(rec.Body.String(), "mallory") {
			t.Errorf("GET %s showed the header's user", path)
		}
	}
	if _, found, err := hn.store.UserByUsername(context.Background(), "mallory"); err != nil || found {
		t.Errorf("a public request made a user: found=%v err=%v", found, err)
	}
}

// SPEC 14.18, No identity: every public answer carries the headers, the
// refusals included, and a download says nothing a mod's file name does not.
func TestPublicResponsesCarryTheHeaders(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	for _, path := range []string{
		PathPublicMods,
		PublicModFilePath(p.id, "alpha_1.0.0.zip"),
		PublicModsAllPath(p.id),
		PublicModFilePath(p.id, "nope.zip"),
	} {
		rep := p.anon(path)
		if rep.Header.Get("X-Frame-Options") != "DENY" || !strings.Contains(rep.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("GET %s: frames are not refused: %v", path, rep.Header)
		}
		if rep.Header.Get("X-Content-Type-Options") != "nosniff" || rep.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("GET %s: missing nosniff or referrer policy: %v", path, rep.Header)
		}
		if cc := rep.Header.Get("Cache-Control"); cc != "no-cache" && cc != "no-store" {
			t.Errorf("GET %s: Cache-Control %q", path, cc)
		}
	}
}

// SPEC 14.18, Logging: the access log has the path and no query, and no
// secret reaches a public answer or the log.
func TestPublicAccessLogHasThePathOnly(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	rep := p.anon(PublicModFilePath(p.id, "alpha_1.0.0.zip") + "?visitor=ada")
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("download: %d", rep.StatusCode)
	}
	log := p.logs.String()
	if strings.Contains(log, "visitor=ada") || strings.Contains(log, "ada") {
		t.Errorf("the access log holds the query string:\n%s", log)
	}
	if !strings.Contains(log, "/files/alpha_1.0.0.zip") {
		t.Errorf("the access log lacks the path:\n%s", log)
	}
}
