package web

import (
	"context"
	"testing"

	"github.com/toddawhittaker/foreman/internal/config"
)

// SPEC 21.2: MCP is off unless an admin turned it on, and FOREMAN_MCP=off
// forces it off.
func TestMCPSettingDefaultsToOff(t *testing.T) {
	hn := newHarness(t, nil)
	cfg := &config.Config{}
	s := &server{cfg: cfg, store: hn.store, log: hn.slog, now: hn.clock.Now}
	ctx := context.Background()
	if s.mcpOn(ctx) {
		t.Error("a fresh database has MCP on")
	}
	if err := s.setMCP(ctx, MCPOn, "alice"); err != nil || !s.mcpOn(ctx) {
		t.Errorf("after turning on: on=%v, err=%v", s.mcpOn(ctx), err)
	}
	cfg.MCPForcedOff = true
	if s.mcpOn(ctx) {
		t.Error("FOREMAN_MCP=off did not win over the setting")
	}
	cfg.MCPForcedOff = false
	if err := s.setMCP(ctx, MCPOff, "alice"); err != nil || s.mcpOn(ctx) {
		t.Errorf("after turning off: on=%v, err=%v", s.mcpOn(ctx), err)
	}
	if err := hn.store.SetAppSetting(ctx, SettingMCP, "yes", "alice", hn.clock.Now()); err != nil || s.mcpOn(ctx) {
		t.Errorf("a value other than on turned MCP on: err=%v", err)
	}
	_ = s.setMCP(ctx, MCPOn, "alice")
	_ = hn.store.Close()
	if s.mcpOn(ctx) {
		t.Error("a setting that cannot be read counted as on")
	}
}
