// Package idp is a fake OpenID Connect identity provider for tests (SPEC
// 12.7). It serves discovery, the signing keys, an authorize step that
// approves at once, and a token endpoint, over https with a test
// certificate. SetNext chooses the claims of the next sign-in, so a test can
// send a wrong nonce, a wrong audience, an expired token or a missing group.
// cmd/fakeidp runs it for the end-to-end tests; Go tests use it directly.
package idp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc/oidctest"
)

// The fake's own made-up client credentials and its default user.
const (
	ClientID     = "foreman-test"
	ClientSecret = "fake-idp-client-secret" //nolint:gosec // a fake, made up for tests
	Group        = "factorio"
	Subject      = "fake-subject-1"
	Username     = "sso-player"
)

const keyID = "fake-key-1"

// Claims is what the next sign-in's ID token says. The override fields are
// for refusals: empty means the right value.
type Claims struct {
	Subject  string   `json:"sub"`
	Username string   `json:"preferred_username"`
	Groups   []string `json:"groups"`
	Nonce    string   `json:"nonce,omitempty"`
	Audience string   `json:"aud,omitempty"`
	Issuer   string   `json:"iss,omitempty"`
	Expired  bool     `json:"expired,omitempty"`
}

// DefaultClaims is the default user, in the required group.
func DefaultClaims() Claims {
	return Claims{Subject: Subject, Username: Username, Groups: []string{Group}}
}

// Authorize is one authorize request, as the fake saw it.
type Authorize struct {
	ClientID        string `json:"client_id"`
	RedirectURI     string `json:"redirect_uri"`
	Scope           string `json:"scope"`
	ChallengeMethod string `json:"code_challenge_method"`
	HasNonce        bool   `json:"has_nonce"`
}

type grant struct {
	challenge, nonce, redirect string
	claims                     Claims
}

// IdP is a running fake provider.
type IdP struct {
	// URL is the issuer, https://127.0.0.1:PORT.
	URL string
	// CAFile holds the PEM of the test certificate, for SSL_CERT_FILE.
	CAFile string
	// Client trusts the test certificate and follows no redirects.
	Client *http.Client

	srv  *httptest.Server
	key  *ecdsa.PrivateKey
	disc *oidctest.Server
	now  func() time.Time

	mu        sync.Mutex
	next      Claims
	codes     map[string]grant
	authorize []Authorize
	tokens    int
}

// Start runs the fake, writing its certificate into dir. now dates the
// tokens; nil means time.Now.
func Start(dir string, now func() time.Time) (*IdP, error) {
	if now == nil {
		now = time.Now
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	f := &IdP{key: key, now: now, next: DefaultClaims(), codes: map[string]grant{}}
	f.disc = &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{PublicKey: key.Public(), KeyID: keyID, Algorithm: "ES256"}},
		Algorithms: []string{"ES256"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth", f.serveAuthorize)
	mux.HandleFunc("POST /token", f.serveToken)
	mux.Handle("/", f.disc)
	f.srv = httptest.NewTLSServer(mux)
	f.URL = f.srv.URL
	f.disc.SetIssuer(f.URL)
	f.Client = f.srv.Client()
	f.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	f.CAFile = filepath.Join(dir, "fakeidp-ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw})
	if err := os.WriteFile(f.CAFile, certPEM, 0o600); err != nil {
		f.srv.Close()
		return nil, err
	}
	return f, nil
}

// Close stops the fake.
func (f *IdP) Close() { f.srv.Close() }

// Transport trusts the test certificate, for the client under test.
func (f *IdP) Transport() http.RoundTripper { return f.srv.Client().Transport }

// SetNext sets the claims of every sign-in from now on.
func (f *IdP) SetNext(c Claims) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next = c
}

// Authorizes returns the authorize requests seen, in order.
func (f *IdP) Authorizes() []Authorize {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Authorize(nil), f.authorize...)
}

// Tokens is how many token requests were answered with a token.
func (f *IdP) Tokens() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens
}

func (f *IdP) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	f.authorize = append(f.authorize, Authorize{
		ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), Scope: q.Get("scope"),
		ChallengeMethod: q.Get("code_challenge_method"), HasNonce: q.Get("nonce") != "",
	})
	f.mu.Unlock()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	switch {
	case q.Get("response_type") != "code", q.Get("client_id") != ClientID, q.Get("state") == "",
		q.Get("code_challenge_method") != "S256", q.Get("code_challenge") == "", err != nil, redirect.Scheme == "":
		http.Error(w, "bad authorize request", http.StatusBadRequest)
		return
	}
	code := random()
	f.mu.Lock()
	f.codes[code] = grant{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri"), claims: f.next}
	f.mu.Unlock()
	v := redirect.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	redirect.RawQuery = v.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound) //nolint:gosec // a test provider sends the browser back where the client asked, as a real one does
}

func (f *IdP) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, "invalid_request")
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(ClientSecret)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		tokenError(w, "invalid_client")
		return
	}
	code := r.PostForm.Get("code")
	f.mu.Lock()
	g, found := f.codes[code]
	delete(f.codes, code)
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if r.PostForm.Get("grant_type") != "authorization_code" || !found || g.redirect != r.PostForm.Get("redirect_uri") ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		tokenError(w, "invalid_grant")
		return
	}
	idToken, err := f.sign(g)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f.mu.Lock()
	f.tokens++
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": random(), "token_type": "Bearer", "expires_in": 300, "id_token": idToken,
	})
}

func (f *IdP) sign(g grant) (string, error) {
	c, now := g.claims, f.now()
	exp := now.Add(5 * time.Minute)
	if c.Expired {
		exp = now.Add(-time.Minute)
	}
	claims := map[string]any{
		"iss": or(c.Issuer, f.URL), "sub": c.Subject, "aud": or(c.Audience, ClientID),
		"exp": exp.Unix(), "iat": now.Unix(), "nonce": or(c.Nonce, g.nonce),
		"preferred_username": c.Username, "groups": c.Groups,
	}
	b, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return oidctest.SignIDToken(f.key, keyID, "ES256", string(b)), nil
}

func tokenError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func or(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func random() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
