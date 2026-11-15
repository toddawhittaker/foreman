package config

import (
	"strings"
	"testing"
)

func sidecarEnv(vars map[string]string) env {
	return env{getenv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
}

// The sidecar's name is read and checked (SPEC 4.13).
func TestLoadSidecarReadsNameOrID(t *testing.T) {
	for _, name := range []string{"tailscale", "vpn_1", "ts.sidecar-2", "0123abcd", strings.Repeat("a", 64)} {
		var errs []error
		c := &Config{GameNetwork: "games"}
		loadSidecar(sidecarEnv(map[string]string{"FOREMAN_GAME_NETWORK_CONTAINER": name}), c, &errs)
		if len(errs) != 0 || c.GameNetworkContainer != name {
			t.Errorf("%q: field %q, errors %v", name, c.GameNetworkContainer, errs)
		}
	}
}

func TestLoadSidecarUnsetOrEmptyLeavesFieldEmpty(t *testing.T) {
	for _, vars := range []map[string]string{nil, {"FOREMAN_GAME_NETWORK_CONTAINER": ""}} {
		var errs []error
		c := &Config{} // no game network: it is required only with a sidecar
		loadSidecar(sidecarEnv(vars), c, &errs)
		if len(errs) != 0 || c.GameNetworkContainer != "" {
			t.Errorf("%v: field %q, errors %v", vars, c.GameNetworkContainer, errs)
		}
	}
}

func TestLoadSidecarRequiresTheGameNetwork(t *testing.T) {
	var errs []error
	c := &Config{}
	loadSidecar(sidecarEnv(map[string]string{"FOREMAN_GAME_NETWORK_CONTAINER": "vpn"}), c, &errs)
	if len(errs) != 1 || c.GameNetworkContainer != "" {
		t.Fatalf("field %q, errors %v", c.GameNetworkContainer, errs)
	}
	for _, want := range []string{"FOREMAN_GAME_NETWORK", "FOREMAN_GAME_NETWORK_CONTAINER"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("error %q does not name %s", errs[0], want)
		}
	}
}

// A value that is not a plain container name or ID is refused.
func TestLoadSidecarRejectsNamesThatAreNotContainers(t *testing.T) {
	for _, bad := range []string{"container:other", "a b", "-vpn", ".vpn", "a/b", "vpn;rm", "vpn\n", "$(x)", "/vpn"} {
		var errs []error
		c := &Config{GameNetwork: "games"}
		loadSidecar(sidecarEnv(map[string]string{"FOREMAN_GAME_NETWORK_CONTAINER": bad}), c, &errs)
		if len(errs) != 1 || c.GameNetworkContainer != "" || !strings.Contains(errs[0].Error(), "FOREMAN_GAME_NETWORK_CONTAINER") {
			t.Errorf("%q: field %q, errors %v", bad, c.GameNetworkContainer, errs)
		}
	}
}
