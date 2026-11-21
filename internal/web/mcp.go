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

// mcpRoutes adds the MCP endpoint (SPEC 21) and the REST API (SPEC 23),
// which share one set of token checks and so one budget per token and per
// address (SPEC 23.9). Each /mcp request passes, in order: MCP on (403), no
// foreign Origin (403), POST only (405), the shared checks of tokenChecks,
// and one JSON-RPC message per body (400).
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
	checks := s.tokenChecks()
	inner := checks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		inner.ServeHTTP(w, r)
	})
	s.apiRoutes(checks, sdk, servers)
}

// tokenChecks builds the checks a token request passes after its path's own
// gate: the per-address failure limit (429), the bearer token (401), the
// per-token limits (429), the whole body read (413, 400) and the token
// checked again once the body is in. Its limiters are made once, so /mcp
// and /api/ share them (SPEC 23.9). The wrapped handler gets the body back
// unread.
func (s *server) tokenChecks() func(http.Handler) http.Handler {
	limits := auth.NewTokenLimiter(s.now)
	addrs := auth.NewAddrLimiter(s.now)
	bearer := mcpauth.RequireBearerToken(s.mcpVerifier(s.tokens, addrs), &mcpauth.RequireBearerTokenOptions{
		// Foreman's own state check decides expiry, by its own clock.
		AllowMissingExpiration: true,
	})
	return func(next http.Handler) http.Handler {
		checked := bearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if !readBody(w, r) {
				return
			}
			// The body may have taken a while: a revoke made meanwhile counts
			// (SPEC 12.10, "takes effect at once").
			if err := s.tokens.Recheck(r.Context(), tok.ID); err != nil {
				s.refuseToken(w, err)
				return
			}
			next.ServeHTTP(w, r)
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, wait := addrs.Allow(auth.ClientAddr(r, s.cfg.TrustedProxies)); !ok {
				tooMany(w, wait, "Too many failed token checks from this address.")
				return
			}
			checked.ServeHTTP(w, r)
		})
	}
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

// readBody reads the whole body, so a slow one ends before the token's
// second check, and puts it back. It reports whether the request may go on.
func readBody(w http.ResponseWriter, r *http.Request) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "The request body is too large.", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "The request body could not be read.", http.StatusBadRequest)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return true
}

// oneMessage refuses a JSON-RPC batch, which the SDK still accepts from
// older protocol versions (SPEC 14.19). readBody has already read the body
// into memory, so this read cannot fail.
func oneMessage(w http.ResponseWriter, r *http.Request) bool {
	body, _ := io.ReadAll(r.Body)
	if t := bytes.TrimLeft(body, " \t\r\n"); len(t) > 0 && t[0] == '[' {
		http.Error(w, "Send one JSON-RPC message per request; batches are refused.", http.StatusBadRequest)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return true
}

// tokenRefusal turns a token check's error into the SDK's: an unknown token
// is a plain invalid token, an expired or revoked one names its state
// (SPEC 14.19), and a store failure is logged and not described.
func (s *server) tokenRefusal(err error) error {
	switch {
	case errors.Is(err, auth.ErrTokenUnknown):
		return mcpauth.ErrInvalidToken
	case errors.Is(err, auth.ErrTokenExpired):
		return fmt.Errorf("%w: this token has expired; its owner can renew it on Foreman's API tokens page", mcpauth.ErrInvalidToken)
	case errors.Is(err, auth.ErrTokenRevoked):
		return fmt.Errorf("%w: this token has been revoked; make a new one", mcpauth.ErrInvalidToken)
	}
	s.log.Error("check an API token", "error", err)
	return errors.New("the token could not be checked")
}

// refuseToken answers a request whose token failed the check after its body
// was read, as the bearer check would have answered it.
func (s *server) refuseToken(w http.ResponseWriter, err error) {
	refusal := s.tokenRefusal(err)
	if errors.Is(refusal, mcpauth.ErrInvalidToken) {
		http.Error(w, refusal.Error(), http.StatusUnauthorized)
		return
	}
	http.Error(w, refusal.Error(), http.StatusInternalServerError)
}

// mcpVerifier checks a bearer token when the request arrives; the handler
// checks it again once the body is read. A call already running when its
// token is revoked may finish, and every later request is refused. An
// unknown token gets a plain 401; an expired or revoked one a
// 401 naming its state (SPEC 14.19). A working token clears the address's
// failures.
func (s *server) mcpVerifier(tokens *auth.APITokens, addrs *auth.AddrLimiter) mcpauth.TokenVerifier {
	return func(ctx context.Context, secret string, r *http.Request) (*mcpauth.TokenInfo, error) {
		tok, err := tokens.Check(ctx, secret)
		if err != nil {
			return nil, s.tokenRefusal(err)
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
// scope's tools and any other is unknown (SPEC 21.3). The admin server adds
// the admin tools (SPEC 21.7).
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
		if scope == auth.ScopeAdmin {
			s.addAdminTools(srv)
		}
		out[scope] = srv
	}
	return out
}
