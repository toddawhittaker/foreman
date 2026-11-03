package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"git.thewhittakers.org/todd/foreman/internal/auth"
)

// These tests drive the whole handler over a real TCP listener, so the
// direct peer is the loopback address the kernel reports, not a value the
// test sets.

type liveServer struct {
	*harness
	url    string
	client *http.Client
}

func newLiveServer(t *testing.T, env map[string]string) *liveServer {
	t.Helper()
	hn := newHarness(t, env)
	ts := httptest.NewServer(hn.h)
	t.Cleanup(ts.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &liveServer{harness: hn, url: ts.URL, client: client}
}

// reply is a response already read and closed.
type reply struct {
	StatusCode int
	Header     http.Header
	cookies    []*http.Cookie
	body       string
}

func (ls *liveServer) do(method, path string, form url.Values, header http.Header) reply {
	ls.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, ls.url+path, body)
	if err != nil {
		ls.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := ls.client.Do(req)
	if err != nil {
		ls.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		ls.t.Fatal(err)
	}
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, cookies: resp.Cookies(), body: string(b)}
}

func cookieFrom(resp reply) *http.Cookie {
	for _, c := range resp.cookies {
		if c.Name == auth.CookieName {
			return c
		}
	}
	return nil
}

func setupForm(user, pw string) url.Values {
	return url.Values{"username": {user}, "password": {pw}, "confirm": {pw}}
}

// SPEC 12.5: identity headers from a peer outside the trusted ranges are
// ignored, and so is X-Forwarded-For.
func TestLiveSpoofedIdentityFromUntrustedPeerIsIgnored(t *testing.T) {
	for _, methods := range []string{"headers", "local,headers"} {
		t.Run(methods, func(t *testing.T) {
			ls := newLiveServer(t, map[string]string{
				"FOREMAN_AUTH_METHODS":        methods,
				"FOREMAN_AUTH_USER_HEADER":    "X-Authentik-Username",
				"FOREMAN_AUTH_GROUPS_HEADER":  "X-Authentik-Groups",
				"FOREMAN_AUTH_REQUIRED_GROUP": "foreman",
				"FOREMAN_TRUSTED_PROXIES":     "10.0.0.0/8",
			})
			h := http.Header{}
			h.Set("X-Authentik-Username", "mallory")
			h.Set("X-Authentik-Groups", "admins")
			// Claims to come through a trusted proxy; the real peer is loopback.
			h.Set("X-Forwarded-For", "10.0.0.5")
			resp := ls.do(http.MethodGet, "/", nil, h)
			if resp.StatusCode == http.StatusOK || strings.Contains(resp.body, "mallory") {
				t.Fatalf("spoofed header reached home: %d", resp.StatusCode)
			}
			if _, ok, _ := ls.store.UserByUsername(t.Context(), "mallory"); ok {
				t.Fatal("a spoofed header created a user")
			}
		})
	}
}

// SPEC 12.4 and 12.5: a client cannot dodge the per-address limit by
// sending a different X-Forwarded-For each time, and the logged client is
// the real peer.
func TestLiveForwardedForFromUntrustedPeerDoesNotChangeClient(t *testing.T) {
	ls := newLiveServer(t, map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8"})
	ls.addUser("alice", testPassword)
	var last reply
	for i := 0; i <= auth.LimitPerAddress; i++ {
		h := http.Header{}
		h.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i+1))
		last = ls.do(http.MethodPost, PathSignIn, signInForm(fmt.Sprintf("user%d", i), "wrong password"), h)
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("attempt %d with rotating X-Forwarded-For: %d, want 429", auth.LimitPerAddress+1, last.StatusCode)
	}
	logs := ls.logs.String()
	if strings.Contains(logs, "203.0.113.") {
		t.Error("the log used an X-Forwarded-For address from an untrusted peer")
	}
	if !strings.Contains(logs, `"client":"127.0.0.1"`) {
		t.Error("the log does not name the real peer")
	}
}

// SPEC 12.3: setup closes once an admin exists.
func TestLiveSecondSetupCreatesNoUser(t *testing.T) {
	ls := newLiveServer(t, nil)
	first := ls.do(http.MethodPost, PathSetup, setupForm("admin", testPassword), nil)
	if first.StatusCode != http.StatusSeeOther || first.Header.Get("Location") != PathHome {
		t.Fatalf("first setup: %d to %q", first.StatusCode, first.Header.Get("Location"))
	}
	second := ls.do(http.MethodPost, PathSetup, setupForm("bob", testPassword), nil)
	if second.StatusCode != http.StatusSeeOther || second.Header.Get("Location") != PathSignIn {
		t.Errorf("second setup: %d to %q, want 303 to sign-in", second.StatusCode, second.Header.Get("Location"))
	}
	if cookieFrom(second) != nil {
		t.Error("the second setup started a session")
	}
	if _, ok, _ := ls.store.UserByUsername(t.Context(), "bob"); ok {
		t.Fatal("the second setup created a user")
	}
	if n := ls.userCount(); n != 1 {
		t.Fatalf("users: %d, want 1", n)
	}
}

// SPEC 14.7: state-changing forms refuse cross-origin requests.
func TestLiveCrossOriginPostsAreRefused(t *testing.T) {
	cross := func() http.Header {
		h := http.Header{}
		h.Set("Origin", "https://evil.example")
		return h
	}
	t.Run("setup", func(t *testing.T) {
		ls := newLiveServer(t, nil)
		resp := ls.do(http.MethodPost, PathSetup, setupForm("admin", testPassword), cross())
		if resp.StatusCode != http.StatusForbidden || cookieFrom(resp) != nil {
			t.Fatalf("cross-origin setup: %d", resp.StatusCode)
		}
		if n := ls.userCount(); n != 0 {
			t.Fatalf("cross-origin setup created %d users", n)
		}
	})
	t.Run("sign-in", func(t *testing.T) {
		ls := newLiveServer(t, nil)
		ls.addUser("alice", testPassword)
		h := http.Header{}
		h.Set("Sec-Fetch-Site", "cross-site")
		resp := ls.do(http.MethodPost, PathSignIn, signInForm("alice", testPassword), h)
		if resp.StatusCode != http.StatusForbidden || cookieFrom(resp) != nil {
			t.Fatalf("cross-site sign-in: %d", resp.StatusCode)
		}
	})
	t.Run("sign-out", func(t *testing.T) {
		ls := newLiveServer(t, nil)
		ls.addUser("alice", testPassword)
		c := cookieFrom(ls.do(http.MethodPost, PathSignIn, signInForm("alice", testPassword), nil))
		if c == nil {
			t.Fatal("no session cookie")
		}
		h := cross()
		h.Set("Cookie", c.Name+"="+c.Value)
		if resp := ls.do(http.MethodPost, PathSignOut, nil, h); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin sign-out: %d", resp.StatusCode)
		}
		h = http.Header{}
		h.Set("Cookie", c.Name+"="+c.Value)
		if resp := ls.do(http.MethodGet, "/", nil, h); resp.StatusCode != http.StatusOK {
			t.Fatalf("the refused sign-out ended the session: %d", resp.StatusCode)
		}
	})
}

// SPEC 14.8: no log line holds a password, a session token or its hash.
func TestLiveFullFlowLogsNoSecrets(t *testing.T) {
	ls := newLiveServer(t, nil)
	const wrong = "not the password at all"
	setup := ls.do(http.MethodPost, PathSetup, setupForm("admin", testPassword), nil)
	c1 := cookieFrom(setup)
	ls.do(http.MethodPost, PathSignIn, signInForm("admin", wrong), nil)
	signIn := ls.do(http.MethodPost, PathSignIn, signInForm("admin", testPassword), nil)
	c2 := cookieFrom(signIn)
	if c1 == nil || c2 == nil {
		t.Fatal("setup or sign-in set no session cookie")
	}
	h := http.Header{}
	h.Set("Cookie", c2.Name+"="+c2.Value)
	ls.do(http.MethodGet, "/", nil, h)
	ls.do(http.MethodPost, PathSignOut, nil, h)

	logs := ls.logs.String()
	if !strings.Contains(logs, "sign-in failed") {
		t.Fatal("the capture saw no sign-in log; the test is not looking at the right logs")
	}
	secrets := map[string]string{"password": testPassword, "wrong password": wrong, "stored hash": ls.passwordHash("admin")}
	for i, c := range []*http.Cookie{c1, c2} {
		sum := sha256.Sum256([]byte(c.Value))
		secrets[fmt.Sprintf("token %d", i)] = c.Value
		secrets[fmt.Sprintf("token %d hash", i)] = hex.EncodeToString(sum[:])
	}
	for name, s := range secrets {
		if strings.Contains(logs, s) {
			t.Errorf("the logs contain the %s", name)
		}
	}
}

// SPEC 12.8 and ruling R5: the session cookie's attributes.
func TestLiveSessionCookieAttributes(t *testing.T) {
	for _, c := range []struct {
		publicURL string
		secure    bool
	}{
		{"https://foreman.example.com", true},
		{"http://foreman.example.com", false},
		{"", false},
	} {
		t.Run(c.publicURL, func(t *testing.T) {
			env := map[string]string{}
			if c.publicURL != "" {
				env["FOREMAN_PUBLIC_URL"] = c.publicURL
			}
			ls := newLiveServer(t, env)
			for _, step := range []struct {
				name string
				resp func() reply
			}{
				{"setup", func() reply {
					return ls.do(http.MethodPost, PathSetup, setupForm("admin", testPassword), nil)
				}},
				{"sign-in", func() reply {
					return ls.do(http.MethodPost, PathSignIn, signInForm("admin", testPassword), nil)
				}},
			} {
				resp := step.resp()
				ck := cookieFrom(resp)
				if ck == nil {
					t.Fatalf("%s: no session cookie", step.name)
				}
				raw := resp.Header.Get("Set-Cookie")
				if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" {
					t.Errorf("%s: %q, want HttpOnly, SameSite=Lax, Path=/", step.name, raw)
				}
				if ck.RawExpires != "" || ck.MaxAge != 0 {
					t.Errorf("%s: %q, want a browser-session cookie with no Expires or Max-Age", step.name, raw)
				}
				if ck.Secure != c.secure {
					t.Errorf("%s: %q, want Secure=%v", step.name, raw, c.secure)
				}
			}
		})
	}
}
