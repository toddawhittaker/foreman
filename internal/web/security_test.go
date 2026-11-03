package web

import (
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
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, body: string(b)}
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
