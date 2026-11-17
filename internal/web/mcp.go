package web

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/version"
)

// PathMCP is the MCP endpoint (SPEC 21.1).
const PathMCP = "/mcp"

// mcpMaxBody caps an /mcp request body (SPEC 14.19); limitBody applies it.
const mcpMaxBody = 4 << 20

// mcpWriteTime is how long an /mcp answer may take to write: an action
// waits up to mcpWait, plus room for the answer (SPEC 21.5).
const mcpWriteTime = 65 * time.Second

// mcpTokenKey holds the request's API token in the SDK's TokenInfo.
const mcpTokenKey = "foreman_token"

// mcpRoutes adds the MCP endpoint (SPEC 21). Each request passes, in order:
// MCP on (403), no foreign Origin (403), POST only (405), the per-address failure limit (429), the
// bearer token (401), the per-token limits (429) and one JSON-RPC message
// per body (400).
// Nothing here reads a cookie or a trusted header (SPEC 14.19).
func (s *server) mcpRoutes() {
	servers := s.mcpServers()
	sdk := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return servers[mcpRequestToken(mcpauth.TokenInfoFromContext(r.Context())).Scope]
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// mcpOrigin covers DNS rebinding, and a proxy on the same host would
		// trip the SDK's own check. limitBody caps the body.
		DisableLocalhostProtection: true,
	})
	limits := auth.NewTokenLimiter(s.now)
	addrs := auth.NewAddrLimiter(s.now)
	bearer := mcpauth.RequireBearerToken(s.mcpVerifier(s.tokens, addrs), &mcpauth.RequireBearerTokenOptions{
		// Foreman's own state check decides expiry, by its own clock.
		AllowMissingExpiration: true,
	})
	inner := bearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := mcpRequestToken(mcpauth.TokenInfoFromContext(r.Context()))
		release, wait, ok := limits.Acquire(tok.ID)
		if !ok {
			tooMany(w, wait, "Too many requests for this token.")
			return
		}
		defer release()
		// The server's 30-second write timeout would cut off an action's
		// 50-second wait (SPEC 21.5).
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(mcpWriteTime)); err != nil {
			s.log.Error("lift the MCP write deadline", "error", err)
		}
		if !oneMessage(w, r) {
			return
		}
		sdk.ServeHTTP(w, r)
	}))
	s.mux.HandleFunc(PathMCP, func(w http.ResponseWriter, r *http.Request) {
		if !s.mcpOn(r.Context()) {
			http.Error(w, "MCP access is off. An admin can turn it on on Foreman's API tokens page.", http.StatusForbidden)
			return
		}
		if !s.mcpOrigin(r) {
			http.Error(w, "Requests from a web page are refused.", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		if ok, wait := addrs.Allow(auth.ClientAddr(r, s.cfg.TrustedProxies)); !ok {
			tooMany(w, wait, "Too many failed token checks from this address.")
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// mcpOrigin reports whether a request's Origin may reach /mcp: none, or
// exactly FOREMAN_PUBLIC_URL's. Go's cross-origin check accepts an Origin
// equal to Host, which a DNS rebinding page can fake (SPEC 14.19); MCP
// clients send no Origin.
func (s *server) mcpOrigin(r *http.Request) bool {
	origin, sent := r.Header["Origin"]
	if !sent {
		return true
	}
	u := s.cfg.PublicURL
	return u != nil && len(origin) == 1 && origin[0] == u.Scheme+"://"+u.Host
}

func tooMany(w http.ResponseWriter, wait time.Duration, text string) {
	w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
	http.Error(w, text, http.StatusTooManyRequests)
}

// oneMessage reads the body and refuses a JSON-RPC batch, which the SDK
// still accepts from older protocol versions (SPEC 14.19). It puts the body
// back for the SDK and reports whether the request may go on.
func oneMessage(w http.ResponseWriter, r *http.Request) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "The request body is too large.", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "The request body could not be read.", http.StatusBadRequest)
		return false
	}
	if t := bytes.TrimLeft(body, " \t\r\n"); len(t) > 0 && t[0] == '[' {
		http.Error(w, "Send one JSON-RPC message per request; batches are refused.", http.StatusBadRequest)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return true
}

// mcpVerifier checks a bearer token once per request: a call already
// running when its token is revoked may finish, and every later request is
// refused. An unknown token gets a plain 401; an expired or revoked one a
// 401 naming its state (SPEC 14.19). A working token clears the address's
// failures.
func (s *server) mcpVerifier(tokens *auth.APITokens, addrs *auth.AddrLimiter) mcpauth.TokenVerifier {
	return func(ctx context.Context, secret string, r *http.Request) (*mcpauth.TokenInfo, error) {
		tok, err := tokens.Check(ctx, secret)
		switch {
		case errors.Is(err, auth.ErrTokenUnknown):
			return nil, mcpauth.ErrInvalidToken
		case errors.Is(err, auth.ErrTokenExpired):
			return nil, fmt.Errorf("%w: this token has expired; its owner can renew it on Foreman's API tokens page", mcpauth.ErrInvalidToken)
		case errors.Is(err, auth.ErrTokenRevoked):
			return nil, fmt.Errorf("%w: this token has been revoked; make a new one", mcpauth.ErrInvalidToken)
		case err != nil:
			s.log.Error("check an API token", "error", err)
			return nil, errors.New("the token could not be checked")
		}
		addrs.Succeed(auth.ClientAddr(r, s.cfg.TrustedProxies))
		return &mcpauth.TokenInfo{Scopes: []string{tok.Scope}, UserID: fmt.Sprint(tok.UserID),
			Extra: map[string]any{mcpTokenKey: tok}}, nil
	}
}

// mcpRequestToken returns the token the verifier stored, or a zero token,
// whose scope picks no server.
func mcpRequestToken(ti *mcpauth.TokenInfo) store.APIToken {
	if ti == nil {
		return store.APIToken{}
	}
	tok, _ := ti.Extra[mcpTokenKey].(store.APIToken)
	return tok
}

// mcpServers builds one server per scope, so a client is listed only its
// scope's tools and any other is unknown (SPEC 21.3). The admin server has
// the operate tools until the admin tools exist (SPEC 21.7).
func (s *server) mcpServers() map[string]*mcp.Server {
	v := version.Get()
	impl := &mcp.Implementation{Name: "foreman", Version: cmp.Or(v.Version, v.Commit, "dev")}
	out := map[string]*mcp.Server{}
	for _, scope := range []string{auth.ScopeRead, auth.ScopeOperate, auth.ScopeAdmin} {
		srv := mcp.NewServer(impl, nil)
		s.addReadTools(srv)
		if auth.ScopeIncludes(scope, auth.ScopeOperate) {
			s.addActionTools(srv)
		}
		out[scope] = srv
	}
	return out
}
