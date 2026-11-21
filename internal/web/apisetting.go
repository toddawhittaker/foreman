package web

import "context"

// SettingAPI is the app setting that turns REST API access on; its values
// are APIOn and APIOff, and a missing one means off (SPEC 23.8). It is
// separate from the MCP switch.
const (
	SettingAPI = "api"
	APIOn      = "on"
	APIOff     = "off"
)

// settingAPI is the REST API switch for reading only; its writes audit as
// api-set rather than through setBoolSetting.
var settingAPI = boolSetting{Name: SettingAPI, Default: false}

// apiOn reports whether REST API access is on. FOREMAN_API=off wins over
// the setting, and a setting that cannot be read counts as off, so a
// database fault never opens the endpoint.
func (s *server) apiOn(ctx context.Context) bool {
	if s.cfg.APIForcedOff {
		return false
	}
	return s.readBool(ctx, settingAPI)
}

// setAPI stores the admin's choice and takes effect at once. The caller
// refuses it while FOREMAN_API=off and audits it as api-set (SPEC 23.8).
// v is APIOn or APIOff.
func (s *server) setAPI(ctx context.Context, v, by string) error {
	return s.store.SetAppSetting(ctx, SettingAPI, v, by, s.now())
}
