package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// inspectServer answers every inspect call with body and nothing else.
func inspectServer(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := versionPrefix.ReplaceAllString(r.URL.Path, "/")
		switch path {
		case "/_ping":
			_, _ = w.Write([]byte("OK"))
		case "/version":
			_, _ = w.Write([]byte(`{"Version":"29.0.0","ApiVersion":"1.52"}`))
		default:
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	c, err := New("tcp://" + strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A VPN sidecar as Docker reports it: two networks, one with IPv6, a published
// UDP port, an exposed TCP port nobody published (null), and credentials in
// its environment.
const sidecarInspect = `{
 "Id":"c0ffee","Name":"/tailscale",
 "Config":{"Image":"tailscale/tailscale:v1.80.0","Env":["TS_AUTHKEY=tskey-secret-value","TS_USERSPACE=false","PATH=/usr/bin"]},
 "HostConfig":{"NetworkMode":"foreman-games"},
 "NetworkSettings":{
  "Networks":{
   "foreman-games":{"IPAddress":"172.30.0.5","IPPrefixLen":24,"GlobalIPv6Address":"fd00:30::5","GlobalIPv6PrefixLen":64},
   "shared":{"IPAddress":"10.9.0.7","IPPrefixLen":16,"GlobalIPv6Address":"","GlobalIPv6PrefixLen":0}},
  "Ports":{"34197/udp":[{"HostIp":"0.0.0.0","HostPort":"34197"},{"HostIp":"::","HostPort":"34197"}],"27015/tcp":null}}}`

func TestInspectReadsSidecarFields(t *testing.T) {
	got, err := inspectServer(t, sidecarInspect).InspectContainer(context.Background(), "tailscale")
	if err != nil {
		t.Fatal(err)
	}
	if got.NetworkMode != "foreman-games" {
		t.Errorf("NetworkMode = %q", got.NetworkMode)
	}
	wantNets := map[string]Network{
		"foreman-games": {IPAddress: "172.30.0.5", IPPrefixLen: 24, GlobalIPv6Address: "fd00:30::5", GlobalIPv6PrefixLen: 64},
		"shared":        {IPAddress: "10.9.0.7", IPPrefixLen: 16},
	}
	if !reflect.DeepEqual(got.Networks, wantNets) {
		t.Errorf("Networks = %+v, want %+v", got.Networks, wantNets)
	}
	wantPorts := map[string][]PortBinding{
		"34197/udp": {{HostIP: "0.0.0.0", HostPort: "34197"}, {HostIP: "::", HostPort: "34197"}},
		"27015/tcp": nil,
	}
	if !reflect.DeepEqual(got.Ports, wantPorts) {
		t.Errorf("Ports = %+v, want %+v", got.Ports, wantPorts)
	}
	if _, exposed := got.Ports["27015/tcp"]; !exposed {
		t.Error("an exposed but unpublished port must still be listed")
	}
	if len(got.Env) != 3 {
		t.Errorf("Env has %d lines, want 3", len(got.Env))
	}
}

// A game in another container's namespace has no networks and no ports of
// its own, and Docker may send them as {} or null.
func TestInspectSharedNamespaceHasNoOwnNetworks(t *testing.T) {
	for _, nested := range []string{`"Networks":{},"Ports":{}`, `"Networks":null,"Ports":null`, ``} {
		body := `{"Id":"g","Name":"/game","HostConfig":{"NetworkMode":"container:c0ffee"},"NetworkSettings":{` + nested + `}}`
		got, err := inspectServer(t, body).InspectContainer(context.Background(), "game")
		if err != nil {
			t.Fatal(err)
		}
		if got.NetworkMode != "container:c0ffee" || len(got.Networks) != 0 || len(got.Ports) != 0 {
			t.Errorf("%q: mode %q, networks %v, ports %v", nested, got.NetworkMode, got.Networks, got.Ports)
		}
	}
}

func TestEnvValue(t *testing.T) {
	c := Container{Env: []string{"A=1", "B=x=y", "EMPTY=", "NOEQUALS", "AB=2", "A=later"}}
	cases := []struct {
		key, want string
		ok        bool
	}{
		{"A", "1", true}, // the first line wins
		{"B", "x=y", true},
		{"EMPTY", "", true},
		{"NOEQUALS", "", false},
		{"AB", "2", true},
		{"MISSING", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		v, ok := c.EnvValue(tc.key)
		if v != tc.want || ok != tc.ok {
			t.Errorf("EnvValue(%q) = %q, %v; want %q, %v", tc.key, v, ok, tc.want, tc.ok)
		}
	}
}

// A reply Foreman cannot read must not put the environment, which can hold a
// VPN key, into the error.
func TestInspectErrorsDoNotQuoteTheEnvironment(t *testing.T) {
	bodies := map[string]string{
		"env is not a list": `{"Id":"c","Config":{"Env":"TS_AUTHKEY=tskey-secret-value"}}`,
		"truncated":         `{"Id":"c","Config":{"Env":["TS_AUTHKEY=tskey-secret-value"`,
		"ports mistyped":    `{"Id":"c","Config":{"Env":["TS_AUTHKEY=tskey-secret-value"]},"NetworkSettings":{"Ports":"TS_AUTHKEY=tskey-secret-value"}}`,
	}
	for name, body := range bodies {
		_, err := inspectServer(t, body).InspectContainer(context.Background(), "sidecar")
		if err == nil {
			t.Errorf("%s: want an error", name)
			continue
		}
		if strings.Contains(err.Error(), "tskey-secret-value") || strings.Contains(err.Error(), "TS_AUTHKEY") {
			t.Errorf("%s: the error quotes the environment: %v", name, err)
		}
	}
}
