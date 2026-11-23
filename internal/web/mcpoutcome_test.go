package web

import (
	"strings"
	"testing"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/store"
)

// SPEC 21.5, 14.10: only an admin-scope token is told to change a setting
// itself; an operate-scope token is told to ask an admin.
func TestMCPOutcomeAskAnAdminByScope(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	s := &server{game: g.ctrl, runs: newGameRuns(), worlds: g.worlds, store: g.store, cfg: g.cfg, log: g.slog, now: g.clock.Now}
	t.Cleanup(s.runs.close)
	sum, err := g.worlds.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	res := actionResult{Action: game.AuditMods, At: g.clock.Now(),
		Err: &game.ModDownloadError{Mod: "fp-big", Version: "1.2.3", Err: &modportal.TooLargeError{Size: 238<<20 + 1, Limit: 200 << 20}}}
	for scope, wantAsk := range map[string]bool{auth.ScopeRead: true, auth.ScopeOperate: true, auth.ScopeAdmin: false} {
		req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "t"}, Extra: &mcp.RequestExtra{TokenInfo: &mcpauth.TokenInfo{
			Extra: map[string]any{mcpTokenKey: store.APIToken{Scope: scope}}}}}
		o := s.outcome(sum, res, mcpAdmin(req))
		if got := strings.Contains(o.UntrustedFailure, "Ask an admin"); got != wantAsk {
			t.Errorf("scope %s: asks an admin %v, want %v: %s", scope, got, wantAsk, o.UntrustedFailure)
		}
	}
}
