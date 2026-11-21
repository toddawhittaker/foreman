package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/auth"
)

// SPEC 23.3, 14.19: an unknown /api/ path needs an API token like every
// other API path; only a valid token learns it does not exist.
func TestAPICatchAllChecksTheToken(t *testing.T) {
	const unknown = "/api/v1/nothing-here"
	hn := newHarness(t, nil)
	u := mcpOn(t, hn)
	secret, tok := apiToken(t, hn, u, "Script", auth.ScopeRead)

	// Switch off: 403 before any token lookup.
	if rec := apiReq(hn, http.MethodGet, unknown, "", nil); rec.Code != http.StatusForbidden {
		t.Errorf("switch off, no token: %d %q", rec.Code, rec.Body)
	}
	if rec := apiReq(hn, http.MethodGet, unknown, "", bearer(secret)); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "off") {
		t.Errorf("switch off, token: %d %q", rec.Code, rec.Body)
	}

	apiOnFor(t, hn)
	if rec := apiReq(hn, http.MethodGet, unknown, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d %q", rec.Code, rec.Body)
	}
	if rec := apiReq(hn, http.MethodGet, unknown, "", bearer(auth.TokenPrefix+strings.Repeat("Z", 43))); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: %d %q", rec.Code, rec.Body)
	}
	rec := apiReq(hn, http.MethodGet, unknown, "", bearer(secret))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("valid token: %d %q", rec.Code, rec.Body)
	}

	if _, err := auth.NewAPITokens(hn.store, hn.clock.Now).Revoke(context.Background(), tok.ID); err != nil {
		t.Fatal(err)
	}
	if rec := apiReq(hn, http.MethodGet, unknown, "", bearer(secret)); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "revoked") {
		t.Errorf("revoked token: %d %q", rec.Code, rec.Body)
	}
}
