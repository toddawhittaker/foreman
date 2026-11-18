package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// Limits on calls to the identity provider (SPEC 12.7, 14.10).
const (
	oidcTimeout      = 10 * time.Second
	oidcMaxDiscovery = 64 << 10
	oidcMaxBody      = 1 << 20
	discoveryPath    = "/.well-known/openid-configuration"
)

// oidcScopes are fixed, as are the claims read from the ID token (SPEC 12.7).
var oidcScopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}

// The errors of an OIDC sign-in. Each error Finish returns holds only fixed
// words after one of these, never a claim, a code or a token (SPEC 14.14),
// so it is safe to log.
var (
	// ErrOIDCUnavailable: the provider's discovery document could not be
	// fetched or is unusable. The next sign-in tries again.
	ErrOIDCUnavailable = errors.New("the identity provider is unavailable")
	// ErrOIDCRefused: the provider's answer failed a check.
	ErrOIDCRefused = errors.New("the identity provider's answer was refused")
	// ErrOIDCNotAllowed: the user is not in the required group.
	ErrOIDCNotAllowed = errors.New("the user is not in the required group")
)

var errOIDCTooLarge = errors.New("the identity provider's answer is too large")

// OIDCSettings configures an OIDC client.
type OIDCSettings struct {
	Issuer, ClientID string
	ClientSecret     secret.Secret
	RequiredGroup    string
	// RedirectURL is where the provider sends the browser back.
	RedirectURL string
	// Transport makes the calls; nil means the default. Tests give one that
	// trusts their certificate.
	Transport http.RoundTripper
	Now       func() time.Time
}

// OIDC signs users in through an identity provider (SPEC 12.7). It holds
// no per-sign-in state: the caller keeps each OIDCRequest until its
// callback.
type OIDC struct {
	set    OIDCSettings
	client *http.Client

	mu       sync.Mutex
	provider *oidc.Provider // nil until discovery works
}

// NewOIDC returns a client. It calls nothing: the discovery document is
// fetched at the first sign-in (SPEC 3.7).
func NewOIDC(set OIDCSettings) *OIDC {
	base := set.Transport
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	if set.Now == nil {
		set.Now = time.Now
	}
	return &OIDC{set: set, client: &http.Client{
		Transport: cappedTransport{base: base},
		Timeout:   oidcTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// with makes every call under ctx use the limited client: go-oidc and
// oauth2 both read it from the context.
func (o *OIDC) with(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, o.client)
}

// discover returns the provider, fetching its discovery document once it
// is needed. A failure is not kept, so the next sign-in tries again.
func (o *OIDC) discover(ctx context.Context) (*oidc.Provider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil {
		return o.provider, nil
	}
	// The provider keeps this context to fetch signing keys later, so it
	// must outlive the request that happened to discover it.
	p, err := oidc.NewProvider(o.with(context.WithoutCancel(ctx)), o.set.Issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOIDCUnavailable, err)
	}
	var doc struct {
		Keys string `json:"jwks_uri"`
	}
	if err := p.Claims(&doc); err != nil {
		return nil, fmt.Errorf("%w: read the discovery document: %w", ErrOIDCUnavailable, err)
	}
	// An http endpoint would carry the client secret or the keys in the clear.
	for _, u := range []string{p.Endpoint().AuthURL, p.Endpoint().TokenURL, doc.Keys} {
		if !strings.HasPrefix(u, "https://") {
			return nil, fmt.Errorf("%w: the discovery document names an endpoint that is not https", ErrOIDCUnavailable)
		}
	}
	o.provider = p
	return p, nil
}

func (o *OIDC) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.set.ClientID,
		ClientSecret: o.set.ClientSecret.Reveal(),
		Endpoint:     p.Endpoint(),
		RedirectURL:  o.set.RedirectURL,
		Scopes:       oidcScopes,
	}
}

// OIDCRequest is one sign-in in progress. State, Nonce and Verifier are
// secrets the caller keeps until the callback; URL is where to send the
// browser.
type OIDCRequest struct {
	State, Nonce, Verifier string
	URL                    string
}

// Begin starts a sign-in with a fresh state, nonce and PKCE verifier (S256).
func (o *OIDC) Begin(ctx context.Context) (OIDCRequest, error) {
	p, err := o.discover(ctx)
	if err != nil {
		return OIDCRequest{}, err
	}
	req := OIDCRequest{State: randomString(), Nonce: randomString(), Verifier: oauth2.GenerateVerifier()}
	req.URL = o.oauth(p).AuthCodeURL(req.State, oidc.Nonce(req.Nonce), oauth2.S256ChallengeOption(req.Verifier))
	return req, nil
}

// OIDCIdentity is who the provider vouched for.
type OIDCIdentity struct {
	Issuer, Subject string
	// Username is the preferred_username claim, unchecked.
	Username string
}

// Finish trades the code for an ID token and checks it: signature, issuer,
// audience, expiry, the nonce Begin made, and the required group in the
// groups claim (SPEC 12.7).
func (o *OIDC) Finish(ctx context.Context, code, verifier, nonce string) (OIDCIdentity, error) {
	refused := func(step string) (OIDCIdentity, error) {
		return OIDCIdentity{}, fmt.Errorf("%w: %s", ErrOIDCRefused, step)
	}
	p, err := o.discover(ctx)
	if err != nil {
		return OIDCIdentity{}, err
	}
	ctx = o.with(ctx)
	tok, err := o.oauth(p).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return refused("the code exchange failed")
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return refused("the token answer has no ID token")
	}
	id, err := p.Verifier(&oidc.Config{ClientID: o.set.ClientID, Now: o.set.Now}).Verify(ctx, raw)
	if err != nil {
		return refused("the ID token failed its checks")
	}
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 {
		return refused("the ID token's nonce does not match")
	}
	var claims struct {
		Username string   `json:"preferred_username"`
		Groups   []string `json:"groups"`
	}
	if err := id.Claims(&claims); err != nil {
		return refused("the ID token's claims are unreadable")
	}
	if id.Subject == "" {
		return refused("the ID token has no subject")
	}
	if !slices.Contains(claims.Groups, o.set.RequiredGroup) {
		return OIDCIdentity{}, ErrOIDCNotAllowed
	}
	return OIDCIdentity{Issuer: id.Issuer, Subject: id.Subject, Username: claims.Username}, nil
}

func randomString() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// The system's random source failing is not survivable.
		panic("read random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// cappedTransport caps each answer's body: the discovery document at 64 KiB,
// everything else at 1 MiB (SPEC 14.10).
type cappedTransport struct{ base http.RoundTripper }

func (t cappedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	limit := int64(oidcMaxBody)
	if strings.HasSuffix(r.URL.Path, discoveryPath) {
		limit = oidcMaxDiscovery
	}
	resp.Body = &cappedBody{rc: resp.Body, left: limit}
	return resp, nil
}

// cappedBody fails the read that passes its limit, rather than cutting the
// body short without a word.
type cappedBody struct {
	rc   io.ReadCloser
	left int64
}

func (c *cappedBody) Read(p []byte) (int, error) {
	if int64(len(p)) > c.left+1 {
		p = p[:c.left+1]
	}
	n, err := c.rc.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return n, errOIDCTooLarge
	}
	return n, err
}

func (c *cappedBody) Close() error { return c.rc.Close() }
