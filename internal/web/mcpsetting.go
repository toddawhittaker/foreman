package web

import "context"

// SettingMCP is the app setting that turns MCP access on; its values are
// MCPOn and MCPOff, and a missing one means off (SPEC 21.2).
const (
	SettingMCP = "mcp"
	MCPOn      = "on"
	MCPOff     = "off"
)

// settingMCP is the MCP switch for reading only; its writes audit as
// mcp-set rather than through setBoolSetting.
var settingMCP = boolSetting{Name: SettingMCP, Default: false}

// mcpOn reports whether MCP access is on. FOREMAN_MCP=off wins over the
// setting, and a setting that cannot be read counts as off, so a database
// fault never opens the endpoint.
func (s *server) mcpOn(ctx context.Context) bool {
	if s.cfg.MCPForcedOff {
		return false
	}
	return s.readBool(ctx, settingMCP)
}

// setMCP stores the admin's choice and takes effect at once. The caller
// refuses it while FOREMAN_MCP=off and audits it as mcp-set (SPEC 21.2).
// v is MCPOn or MCPOff.
func (s *server) setMCP(ctx context.Context, v, by string) error {
	return s.store.SetAppSetting(ctx, SettingMCP, v, by, s.now())
}
