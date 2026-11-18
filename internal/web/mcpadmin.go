package web

import "github.com/modelcontextprotocol/go-sdk/mcp"

// addAdminTools adds the admin scope's tools, one adder per SPEC 21.7 area.
// A tool that deletes or overwrites is two-step (SPEC 21.8): it sets the
// twoStep annotations and follows the example in mcpconfirm_test.go. Every
// other admin tool sets oneStep, or readOnly when it changes nothing.
func (s *server) addAdminTools(srv *mcp.Server) {
	s.addWorldTools(srv)
	s.addSnapshotTools(srv)
	s.addLiveTools(srv)
	s.addModTools(srv)
	s.addSettingsTools(srv)
	s.addVersionTools(srv)
	s.addForemanTools(srv)
}
