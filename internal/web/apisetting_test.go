package web

import (
	"context"
	"testing"

	"github.com/toddawhittaker/foreman/internal/config"
)

// SPEC 23.8: the API is off unless an admin turned it on, and FOREMAN_API=off
// forces it off.
func TestAPISettingDefaultsToOff(t *testing.T) {
	hn := newHarness(t, nil)
	cfg := &config.Config{}
	s := &server{cfg: cfg, store: hn.store, log: hn.slog, now: hn.clock.Now}
	ctx := context.Background()
	if s.apiOn(ctx) {
		t.Error("a fresh database has the API on")
	}
	if err := s.setAPI(ctx, APIOn, "alice"); err != nil || !s.apiOn(ctx) {
		t.Errorf("after turning on: on=%v, err=%v", s.apiOn(ctx), err)
	}
	if s.mcpOn(ctx) {
		t.Error("turning the API on turned MCP on")
	}
	cfg.APIForcedOff = true
	if s.apiOn(ctx) {
		t.Error("FOREMAN_API=off did not win over the setting")
	}
	cfg.APIForcedOff = false
	if err := s.setAPI(ctx, APIOff, "alice"); err != nil || s.apiOn(ctx) {
		t.Errorf("after turning off: on=%v, err=%v", s.apiOn(ctx), err)
	}
	if err := hn.store.SetAppSetting(ctx, SettingAPI, "yes", "alice", hn.clock.Now()); err != nil || s.apiOn(ctx) {
		t.Errorf("a value other than on turned the API on: err=%v", err)
	}
	_ = s.setAPI(ctx, APIOn, "alice")
	_ = hn.store.Close()
	if s.apiOn(ctx) {
		t.Error("a setting that cannot be read counted as on")
	}
}
