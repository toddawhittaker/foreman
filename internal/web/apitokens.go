package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
)

// PathAPITokens is the API tokens page (SPEC 12.10, 21.2).
const PathAPITokens = "/settings/api-tokens"

// Paths under PathAPITokens.
const (
	pathAPITokenMCP = PathAPITokens + "/mcp"
	pathAPITokenAPI = PathAPITokens + "/api"
	pathAPIToken    = PathAPITokens + "/{id}"
)

// Audit actions of SPEC 12.9.
const (
	AuditTokenCreate = "token-create"
	AuditTokenRenew  = "token-renew"
	AuditTokenRevoke = "token-revoke"
	AuditMCPSet      = "mcp-set"
	AuditAPISet      = "api-set"
)

// Done messages the page shows after a redirect.
const (
	tokenDoneRenewed = "renewed"
	tokenDoneRevoked = "revoked"
	tokenDoneMCPOn   = "mcp-on"
	tokenDoneMCPOff  = "mcp-off"
	tokenDoneAPIOn   = "api-on"
	tokenDoneAPIOff  = "api-off"
)

// roleAdmin is the users table's only role in v1 (SPEC 12.1).
const roleAdmin = "admin"

// TokenRow is one token as the page shows it: never its secret or hash.
type TokenRow struct {
	ID       int64
	Owner    string
	Name     string
	Scope    string
	State    auth.TokenState
	Expires  time.Time
	Created  time.Time
	LastUsed time.Time // zero when never used
	// Mine is true for the signed-in user's own token, which they renew.
	Mine bool
}

func tokenRow(t store.APIToken, me int64, now time.Time) TokenRow {
	return TokenRow{
		ID: t.ID, Owner: t.Owner, Name: t.Name, Scope: t.Scope, State: auth.State(t, now),
		Expires: auth.ExpiresAt(t), Created: t.CreatedAt, LastUsed: t.LastUsedAt, Mine: t.UserID == me,
	}
}

// StateWord is the state in words.
func (t TokenRow) StateWord() i18n.Msg {
	switch t.State {
	case auth.TokenExpired:
		return i18n.M("apitokens.state.expired")
	case auth.TokenRevoked:
		return i18n.M("apitokens.state.revoked")
	}
	return i18n.M("apitokens.state.working")
}

// StateClass is the state's CSS class suffix.
func (t TokenRow) StateClass() string {
	switch t.State {
	case auth.TokenExpired:
		return "expired"
	case auth.TokenRevoked:
		return "revoked"
	}
	return "working"
}

// Expired is true once the period has run out and the token is not revoked.
func (t TokenRow) Expired() bool { return t.State == auth.TokenExpired }

// ScopeWord is the scope in words.
func (t TokenRow) ScopeWord() i18n.Msg { return scopeWord(t.Scope) }

func scopeWord(scope string) i18n.Msg {
	switch scope {
	case auth.ScopeOperate:
		return i18n.M("apitokens.scope.operate")
	case auth.ScopeAdmin:
		return i18n.M("apitokens.scope.admin")
	}
	return i18n.M("apitokens.scope.read")
}

func scopeHint(scope string) i18n.Msg {
	switch scope {
	case auth.ScopeOperate:
		return i18n.M("apitokens.scope.operate.hint")
	case auth.ScopeAdmin:
		return i18n.M("apitokens.scope.admin.hint")
	}
	return i18n.M("apitokens.scope.read.hint")
}

// Revoked is true once the token has ended for good.
func (t TokenRow) Revoked() bool { return t.State == auth.TokenRevoked }

// RenewPath is where its Renew button posts.
func (t TokenRow) RenewPath() string { return tokenPath(t.ID, "renew") }

// RevokePath is its revoke confirm page.
func (t TokenRow) RevokePath() string { return tokenPath(t.ID, "revoke") }

func tokenPath(id int64, what string) string {
	return PathAPITokens + "/" + strconv.FormatInt(id, 10) + "/" + what
}

// TokenForm is the create form's values and errors.
type TokenForm struct {
	Name      string
	Scope     string
	NameError i18n.Msg
}

// ScopeOption is one radio button of the scope choice.
type ScopeOption struct {
	Value   string
	Name    i18n.Msg
	Hint    i18n.Msg
	Checked bool
}

// Scopes lists the scopes in order, smallest first, the form's checked.
func (f TokenForm) Scopes() []ScopeOption {
	var out []ScopeOption
	for _, v := range []string{auth.ScopeRead, auth.ScopeOperate, auth.ScopeAdmin} {
		out = append(out, ScopeOption{Value: v, Name: scopeWord(v), Hint: scopeHint(v), Checked: v == f.Scope})
	}
	return out
}

// NameMax is the longest name.
func (TokenForm) NameMax() int { return auth.TokenNameMax }

// APITokensPage lists tokens, creates them and switches MCP and REST API access.
type APITokensPage struct {
	Mine []TokenRow
	// All is every user's token; only an admin sees it (SPEC 12.10).
	All   []TokenRow
	Admin bool
	Form  TokenForm
	// MCPOn is the stored choice; MCPForced is FOREMAN_MCP=off.
	MCPOn     bool
	MCPForced bool
	// APIOn and APIForced are the same for REST API access (SPEC 23.8).
	APIOn     bool
	APIForced bool
	// Done is the action just finished and DoneName the token it named.
	Done     string
	DoneName string
}

func (APITokensPage) templateName() string { return "apitokens" }
func (APITokensPage) navItem() string      { return generalPath() }

func (p APITokensPage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists the form's errors.
func (p APITokensPage) Problems() []Problem {
	return problems(Problem{"token-name", p.Form.NameError})
}

// Rows is the one token list: every token for an admin, else the user's own.
func (p APITokensPage) Rows() []TokenRow {
	if p.Admin {
		return p.All
	}
	return p.Mine
}

// Path is where the create form posts.
func (APITokensPage) Path() string { return PathAPITokens }

// MCPPath is where the switch posts.
func (APITokensPage) MCPPath() string { return pathAPITokenMCP }

// APIPath is where the REST API switch posts.
func (APITokensPage) APIPath() string { return pathAPITokenAPI }

// APITokenCreatedPage shows a new secret, once (SPEC 12.10, 14.19).
type APITokenCreatedPage struct {
	Token  TokenRow
	Secret string
	// Endpoint is the MCP address and APIBase the REST API's base address.
	Endpoint string
	APIBase  string
	// AccessOn is true when MCP or REST API access is on; the note shows
	// only when both are off, a forced-off one counting as off.
	AccessOn bool
	// MCPForced and APIForced hide the note's link to a switch that cannot be used.
	MCPForced bool
	APIForced bool
	// Admin may turn access on, so the off note links to the switches.
	Admin bool
}

func (APITokenCreatedPage) templateName() string { return "apitokencreated" }
func (APITokenCreatedPage) navItem() string      { return generalPath() }

// BackPath is the tokens page.
func (APITokenCreatedPage) BackPath() string { return PathAPITokens }

// APITokenRevokePage asks before a revoke, which is final.
type APITokenRevokePage struct {
	Token TokenRow
}

func (APITokenRevokePage) templateName() string { return "apitokenrevoke" }
func (APITokenRevokePage) navItem() string      { return generalPath() }

// BackPath is the tokens page.
func (APITokenRevokePage) BackPath() string { return PathAPITokens }

func (s *server) apiTokenRoutes() {
	s.mux.HandleFunc("GET "+PathAPITokens, s.requireUser(s.handleAPITokens))
	s.mux.HandleFunc("POST "+PathAPITokens, s.requireUser(s.handleAPITokenCreate))
	s.mux.HandleFunc("POST "+pathAPITokenMCP, s.requireUser(s.handleMCPSet))
	s.mux.HandleFunc("POST "+pathAPITokenAPI, s.requireUser(s.handleAPISet))
	s.mux.HandleFunc("POST "+pathAPIToken+"/renew", s.requireUser(s.handleAPITokenRenew))
	s.mux.HandleFunc("GET "+pathAPIToken+"/revoke", s.requireUser(s.handleAPITokenRevokeAsk))
	s.mux.HandleFunc("POST "+pathAPIToken+"/revoke", s.requireUser(s.handleAPITokenRevoke))
}

func isAdmin(u store.User) bool { return u.Role == roleAdmin }

// mayRevoke is the revoke rule: the owner or any admin (SPEC 12.10).
// auth.Revoke does not check it, so every revoke goes through here.
func mayRevoke(u store.User, t store.APIToken) bool {
	return t.UserID == u.ID || isAdmin(u)
}

// tokensPage builds the page for u with form f.
func (s *server) tokensPage(ctx context.Context, u store.User, f TokenForm) (APITokensPage, error) {
	all, err := s.tokens.List(ctx)
	if err != nil {
		return APITokensPage{}, err
	}
	now := s.now()
	p := APITokensPage{Admin: isAdmin(u), Form: f, MCPOn: s.mcpOn(ctx), MCPForced: s.cfg.MCPForcedOff, APIOn: s.apiOn(ctx), APIForced: s.cfg.APIForcedOff}
	for _, t := range all {
		row := tokenRow(t, u.ID, now)
		if row.Mine {
			p.Mine = append(p.Mine, row)
		}
		if p.Admin {
			p.All = append(p.All, row)
		}
	}
	return p, nil
}

func (s *server) handleAPITokens(w http.ResponseWriter, r *http.Request, u store.User) {
	p, err := s.tokensPage(r.Context(), u, TokenForm{Scope: auth.ScopeRead})
	if err != nil {
		s.fail(w, r, "list API tokens", err)
		return
	}
	q := r.URL.Query()
	switch d := q.Get("done"); d {
	case tokenDoneRevoked:
		p.Done, p.DoneName = d, q.Get("name")
	case tokenDoneMCPOn, tokenDoneMCPOff, tokenDoneAPIOn, tokenDoneAPIOff:
		p.Done = d
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handleAPITokenCreate makes a token and answers with its secret, once.
func (s *server) handleAPITokenCreate(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	f := TokenForm{Name: r.PostFormValue("name"), Scope: r.PostFormValue("scope")}
	if !auth.ScopeIncludes(f.Scope, auth.ScopeRead) {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	secret, t, err := s.tokens.Create(r.Context(), u.ID, f.Name, f.Scope)
	switch {
	case errors.Is(err, auth.ErrTokenName):
		f.NameError = i18n.M("apitokens.create.name.invalid", "max", auth.TokenNameMax)
	case errors.Is(err, store.ErrTokenNameTaken):
		f.NameError = i18n.M("apitokens.create.name.taken")
	case err != nil:
		s.fail(w, r, "create an API token", err)
		return
	}
	if !f.NameError.IsZero() {
		p, err := s.tokensPage(r.Context(), u, f)
		if err != nil {
			s.fail(w, r, "list API tokens", err)
			return
		}
		s.page(w, r, http.StatusUnprocessableEntity, u.Username, p)
		return
	}
	s.audit(r.Context(), actor(u), AuditTokenCreate, "", tokenAudit(t))
	// Render sends every signed-in page with no-store; the secret is never
	// cached or shown again (SPEC 14.19).
	s.page(w, r, http.StatusOK, u.Username, APITokenCreatedPage{
		Token: tokenRow(t, u.ID, s.now()), Secret: secret, Endpoint: s.publicAddress(r, PathMCP), APIBase: s.publicAddress(r, path.Dir(PathAPITools)),
		AccessOn: s.mcpOn(r.Context()) || s.apiOn(r.Context()), MCPForced: s.cfg.MCPForcedOff, APIForced: s.cfg.APIForcedOff, Admin: isAdmin(u),
	})
}

// tokenAudit is an audit detail naming the token, its owner and its scope
// (SPEC 12.9).
// Free text is quoted, so a name or username cannot pose as another field.
func tokenAudit(t store.APIToken) string {
	return "token_id=" + strconv.FormatInt(t.ID, 10) + " token=" + strconv.Quote(t.Name) +
		" owner=" + strconv.Quote(t.Owner) + " scope=" + t.Scope
}

// publicAddress is this server's address for the path suffix:
// FOREMAN_PUBLIC_URL's, else this request's (SPEC 12.10).
func (s *server) publicAddress(r *http.Request, suffix string) string {
	if s.cfg.PublicURL != nil {
		u := *s.cfg.PublicURL
		u.Path = strings.TrimSuffix(u.Path, "/") + suffix
		u.RawQuery, u.Fragment = "", ""
		return u.String()
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Behind a trusted proxy the browser's scheme is the proxy's to say;
	// anyone else's header is ignored.
	if auth.PeerTrusted(r, s.cfg.TrustedProxies) {
		switch p := r.Header.Get("X-Forwarded-Proto"); p {
		case "https", "http":
			scheme = p
		}
	}
	return (&url.URL{Scheme: scheme, Host: r.Host, Path: suffix}).String()
}

// tokenFromPath reads the token the path names, or answers 404.
func (s *server) tokenFromPath(w http.ResponseWriter, r *http.Request, u store.User) (store.APIToken, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.notFound(w, r, u)
		return store.APIToken{}, false
	}
	t, err := s.tokens.Get(r.Context(), id)
	if errors.Is(err, store.ErrTokenNotFound) {
		s.notFound(w, r, u)
		return store.APIToken{}, false
	}
	if err != nil {
		s.fail(w, r, "read an API token", err)
		return store.APIToken{}, false
	}
	return t, true
}

// handleAPITokenRenew renews the owner's token in one click and returns to
// the page the form came from (SPEC 12.10, 14.17). Pages never read the
// Authorization header, so a token cannot renew itself.
func (s *server) handleAPITokenRenew(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.notFound(w, r, u)
		return
	}
	t, err := s.tokens.Renew(r.Context(), id, u.ID)
	switch {
	case errors.Is(err, store.ErrTokenNotFound):
		// Another user's token is not theirs to renew.
		s.notFound(w, r, u)
		return
	case errors.Is(err, store.ErrTokenRevoked):
		s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict, Detail: i18n.M("apitokens.renew.revoked")})
		return
	case err != nil:
		s.fail(w, r, "renew an API token", err)
		return
	}
	s.audit(r.Context(), actor(u), AuditTokenRenew, "", tokenAudit(t))
	http.Redirect(w, r, renewedURL(r.PostFormValue("return"), t.Name), http.StatusSeeOther)
}

// renewedURL is the page the Renew came from, when safe, else the tokens
// page, with a Renewed message. An old done or name is dropped first, so
// the page never shows a stale message (SPEC 14.17).
func renewedURL(back, name string) string {
	u, err := url.Parse(returnPath(back))
	if err != nil || u.Path == "" {
		return doneURL(tokenDoneRenewed, name)
	}
	q := u.Query()
	q.Del("done")
	q.Del("name")
	q.Set("done", tokenDoneRenewed)
	q.Set("name", name)
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String()
}

func doneURL(done, name string) string {
	return PathAPITokens + "?" + url.Values{"done": {done}, "name": {name}}.Encode()
}

func (s *server) handleAPITokenRevokeAsk(w http.ResponseWriter, r *http.Request, u store.User) {
	t, ok := s.tokenFromPath(w, r, u)
	if !ok {
		return
	}
	if !mayRevoke(u, t) {
		s.page(w, r, http.StatusForbidden, u.Username, ErrorPage{Status: http.StatusForbidden})
		return
	}
	row := tokenRow(t, u.ID, s.now())
	if row.Revoked() {
		http.Redirect(w, r, PathAPITokens, http.StatusSeeOther)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, APITokenRevokePage{Token: row})
}

// handleAPITokenRevoke ends a token for good, after the confirm page.
func (s *server) handleAPITokenRevoke(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	t, ok := s.tokenFromPath(w, r, u)
	if !ok {
		return
	}
	if !mayRevoke(u, t) {
		s.page(w, r, http.StatusForbidden, u.Username, ErrorPage{Status: http.StatusForbidden})
		return
	}
	t, err := s.tokens.Revoke(r.Context(), t.ID)
	switch {
	case errors.Is(err, store.ErrTokenRevoked):
		// Already ended: the outcome the user asked for.
		http.Redirect(w, r, PathAPITokens, http.StatusSeeOther)
		return
	case errors.Is(err, store.ErrTokenNotFound):
		s.notFound(w, r, u)
		return
	case err != nil:
		s.fail(w, r, "revoke an API token", err)
		return
	}
	s.audit(r.Context(), actor(u), AuditTokenRevoke, "", tokenAudit(t))
	http.Redirect(w, r, doneURL(tokenDoneRevoked, t.Name), http.StatusSeeOther)
}

// handleMCPSet turns MCP access on or off; FOREMAN_MCP=off refuses it
// (SPEC 21.2).
func (s *server) handleMCPSet(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	if !isAdmin(u) {
		s.page(w, r, http.StatusForbidden, u.Username, ErrorPage{Status: http.StatusForbidden})
		return
	}
	if s.cfg.MCPForcedOff {
		s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict, Detail: i18n.M("apitokens.mcp.forced")})
		return
	}
	v := r.PostFormValue("mcp")
	if v != MCPOn && v != MCPOff {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	if err := s.setMCP(r.Context(), v, u.Username); err != nil {
		s.fail(w, r, "save the MCP setting", err)
		return
	}
	s.audit(r.Context(), actor(u), AuditMCPSet, "", v)
	http.Redirect(w, r, PathAPITokens+"?done=mcp-"+v, http.StatusSeeOther)
}

// handleAPISet turns REST API access on or off; FOREMAN_API=off refuses it
// (SPEC 23.8).
func (s *server) handleAPISet(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	if !isAdmin(u) {
		s.page(w, r, http.StatusForbidden, u.Username, ErrorPage{Status: http.StatusForbidden})
		return
	}
	if s.cfg.APIForcedOff {
		s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict, Detail: i18n.M("apitokens.api.forced")})
		return
	}
	v := r.PostFormValue("api")
	if v != APIOn && v != APIOff {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	if err := s.setAPI(r.Context(), v, u.Username); err != nil {
		s.fail(w, r, "save the REST API setting", err)
		return
	}
	s.audit(r.Context(), actor(u), AuditAPISet, "", v)
	http.Redirect(w, r, PathAPITokens+"?done=api-"+v, http.StatusSeeOther)
}

// tokenBanner is the expiry banner's tokens for the signed-in user, on
// every signed-in page (SPEC 12.10, "Reminder"). An htmx request draws no
// layout, so it skips the lookup; a failed lookup shows none.
func (s *server) tokenBanner(r *http.Request, user string) []TokenRow {
	if user == "" || s.tokens == nil || s.store == nil || isHTMX(r) {
		return nil
	}
	ctx := r.Context()
	u, found, err := s.store.UserByUsername(ctx, user)
	if err != nil {
		s.log.Warn("read the signed-in user for the token banner", "error", err)
		return nil
	}
	if !found {
		return nil
	}
	ts, err := s.tokens.Expiring(ctx, u.ID)
	if err != nil {
		s.log.Error("list expiring API tokens", "error", err)
		return nil
	}
	now := s.now()
	out := make([]TokenRow, 0, len(ts))
	for _, t := range ts {
		out = append(out, tokenRow(t, u.ID, now))
	}
	return out
}

// dropSpoofedRenewed removes a done=renewed message unless it names one of
// the signed-in user's own tokens that is working, so a crafted link cannot
// show a made-up Done message.
func (s *server) dropSpoofedRenewed(r *http.Request, user string) *http.Request {
	name := renewedName(r, user)
	if name == "" || s.ownsLiveToken(r.Context(), user, name) {
		return r
	}
	r2 := r.Clone(r.Context())
	q := r2.URL.Query()
	q.Del("done")
	q.Del("name")
	r2.URL.RawQuery = q.Encode()
	return r2
}

// ownsLiveToken reports whether the user has a working token of this name.
func (s *server) ownsLiveToken(ctx context.Context, user, name string) bool {
	if s.tokens == nil || s.store == nil {
		return false
	}
	u, found, err := s.store.UserByUsername(ctx, user)
	if err != nil {
		s.log.Warn("read the signed-in user to check a renewed token", "error", err)
		return false
	}
	if !found {
		return false
	}
	ts, err := s.tokens.OwnedBy(ctx, u.ID)
	if err != nil {
		s.log.Warn("list the signed-in user's API tokens to check a renewed token", "error", err)
		return false
	}
	now := s.now()
	for _, t := range ts {
		if t.Name == name && auth.State(t, now) == auth.TokenWorking {
			return true
		}
	}
	return false
}
