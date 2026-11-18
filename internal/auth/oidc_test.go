package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// discoveryServer answers the discovery document with body, or with what
// h writes when it is given.
func discoveryServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func oidcFor(srv *httptest.Server) *OIDC {
	return NewOIDC(OIDCSettings{
		Issuer: srv.URL, ClientID: "c", ClientSecret: secret.New("s"), RequiredGroup: "g",
		RedirectURL: "https://foreman.example.com/signin/oidc/callback", Transport: srv.Client().Transport,
	})
}

func discoveryJSON(issuer, scheme string) string {
	base := scheme + strings.TrimPrefix(issuer, "https")
	return `{"issuer":"` + issuer + `","authorization_endpoint":"` + base + `/auth","token_endpoint":"` + base +
		`/token","jwks_uri":"` + base + `/keys","id_token_signing_alg_values_supported":["ES256"]}`
}

// SPEC 12.7, 14.10: the discovery document is read at most 64 KiB, no
// redirect is followed, and every endpoint it names must be https.
func TestOIDCDiscoveryLimits(t *testing.T) {
	tests := []struct {
		name string
		h    func(srv **httptest.Server) http.HandlerFunc
	}{
		{"too large", func(srv **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				doc := discoveryJSON((*srv).URL, "https")
				_, _ = w.Write([]byte(doc[:len(doc)-1] + `,"pad":"` + strings.Repeat("x", oidcMaxDiscovery) + `"}`))
			}
		}},
		{"redirect", func(srv **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == discoveryPath {
					http.Redirect(w, r, "/elsewhere"+discoveryPath, http.StatusFound)
					return
				}
				_, _ = w.Write([]byte(discoveryJSON((*srv).URL, "https")))
			}
		}},
		{"http endpoints", func(srv **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(discoveryJSON((*srv).URL, "http")))
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var srv *httptest.Server
			srv = discoveryServer(t, tt.h(&srv))
			_, err := oidcFor(srv).Begin(context.Background())
			if !errors.Is(err, ErrOIDCUnavailable) {
				t.Fatalf("Begin: %v, want ErrOIDCUnavailable", err)
			}
		})
	}
}

// SPEC 12.7: discovery happens at the first sign-in, is kept once it
// works, and is tried again after a failure.
func TestOIDCDiscoveryIsLazyAndRetried(t *testing.T) {
	var srv *httptest.Server
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	srv = discoveryServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(discoveryJSON(srv.URL, "https")))
	})
	o := oidcFor(srv)
	if calls.Load() != 0 {
		t.Fatalf("NewOIDC made %d calls", calls.Load())
	}
	if _, err := o.Begin(context.Background()); !errors.Is(err, ErrOIDCUnavailable) {
		t.Fatalf("first Begin: %v", err)
	}
	fail.Store(false)
	for range 2 {
		req, err := o.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(req.URL, srv.URL+"/auth?") || req.State == "" || req.Nonce == "" || req.Verifier == "" {
			t.Fatalf("request %+v", req)
		}
		for _, want := range []string{"code_challenge_method=S256", "scope=openid+profile+email+groups", "nonce=" + req.Nonce, "state=" + req.State} {
			if !strings.Contains(req.URL, want) {
				t.Errorf("URL lacks %q", want)
			}
		}
		if strings.Contains(req.URL, req.Verifier) {
			t.Error("the URL carries the PKCE verifier")
		}
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("discovery fetched %d times, want 2 (one failure, one success kept)", n)
	}
}

// SPEC 14.10: an answer over its cap fails the read instead of being cut
// short quietly.
func TestCappedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", oidcMaxBody+1)))
	}))
	defer srv.Close()
	c := &http.Client{Transport: cappedTransport{base: http.DefaultTransport}}
	resp, err := c.Get(srv.URL + "/keys")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 4096)
	total := 0
	for {
		n, err := resp.Body.Read(buf)
		total += n
		if errors.Is(err, errOIDCTooLarge) {
			break
		}
		if err != nil {
			t.Fatalf("read ended with %v after %d bytes", err, total)
		}
	}
	if total != oidcMaxBody+1 {
		t.Errorf("read %d bytes before failing", total)
	}
}
