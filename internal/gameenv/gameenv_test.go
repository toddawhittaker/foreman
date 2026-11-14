package gameenv

import "testing"

// The start script and the fake game read this name from the container's
// environment, so a rename would silently break RCON binding (SPEC 4.13).
func TestRCONBindName(t *testing.T) {
	if RCONBind != "FOREMAN_RCON_BIND" {
		t.Errorf("RCONBind = %q", RCONBind)
	}
}
