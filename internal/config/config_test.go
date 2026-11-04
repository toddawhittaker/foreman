package config_test

import (
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/config"
)

func load(vars map[string]string) (*config.Config, error) {
	return config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := &config.Config{
		DataDir:     "/data",
		GameUID:     845,
		GameGID:     845,
		Listen:      ":8080",
		AuthLocal:   true,
		SessionIdle: 168 * time.Hour,
		DockerHost:  "unix:///var/run/docker.sock",
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("defaults:\n got %+v\nwant %+v", c, want)
	}
	if c.SecureCookies() {
		t.Error("SecureCookies() with no public URL")
	}
}

func TestLoadAllSet(t *testing.T) {
	c, err := load(map[string]string{
		"FOREMAN_DATA_DIR":            "/srv/foreman",
		"FOREMAN_HOST_DATA_DIR":       "/host/foreman",
		"FOREMAN_GAME_UID":            "1000",
		"FOREMAN_GAME_GID":            "1001",
		"FOREMAN_LISTEN":              "127.0.0.1:9000",
		"FOREMAN_PUBLIC_URL":          "https://foreman.example.com",
		"FOREMAN_AUTH_METHODS":        " Local , HEADERS ",
		"FOREMAN_AUTH_USER_HEADER":    "x-authentik-username",
		"FOREMAN_AUTH_GROUPS_HEADER":  "x-authentik-groups",
		"FOREMAN_AUTH_REQUIRED_GROUP": "factorio",
		"FOREMAN_TRUSTED_PROXIES":     "10.0.0.0/8, 192.168.1.5 ,fd00::1",
		"FOREMAN_SESSION_IDLE":        "30m",
		"DOCKER_HOST":                 "tcp://socket-proxy:2375",
		"FOREMAN_GAME_NETWORK":        "games",
	})
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := url.Parse("https://foreman.example.com")
	want := &config.Config{
		DataDir:       "/srv/foreman",
		HostDataDir:   "/host/foreman",
		GameUID:       1000,
		GameGID:       1001,
		Listen:        "127.0.0.1:9000",
		PublicURL:     pub,
		AuthLocal:     true,
		AuthHeaders:   true,
		UserHeader:    "X-Authentik-Username",
		GroupsHeader:  "X-Authentik-Groups",
		RequiredGroup: "factorio",
		TrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
			netip.MustParsePrefix("192.168.1.5/32"),
			netip.MustParsePrefix("fd00::1/128"),
		},
		SessionIdle: 30 * time.Minute,
		DockerHost:  "tcp://socket-proxy:2375",
		GameNetwork: "games",
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("\n got %+v\nwant %+v", c, want)
	}
	if !c.SecureCookies() {
		t.Error("SecureCookies() = false for an https public URL")
	}
}

func TestSecureCookies(t *testing.T) {
	tests := map[string]bool{
		"https://f.example.com":      true,
		"HTTPS://f.example.com":      true,
		"http://f.example.com":       false,
		"http://localhost:8080":      false,
		"https://f.example.com/base": true,
	}
	for in, want := range tests {
		c, err := load(map[string]string{"FOREMAN_PUBLIC_URL": in})
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := c.SecureCookies(); got != want {
			t.Errorf("%s: SecureCookies() = %v, want %v", in, got, want)
		}
	}
}

func TestLoadAuthMethods(t *testing.T) {
	headersOK := map[string]string{
		"FOREMAN_AUTH_USER_HEADER":    "Remote-User",
		"FOREMAN_AUTH_GROUPS_HEADER":  "Remote-Groups",
		"FOREMAN_AUTH_REQUIRED_GROUP": "factorio",
		"FOREMAN_TRUSTED_PROXIES":     "10.0.0.0/8",
	}
	with := func(methods string) map[string]string {
		m := map[string]string{"FOREMAN_AUTH_METHODS": methods}
		for k, v := range headersOK {
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name        string
		vars        map[string]string
		local, hdrs bool
		wantErr     string
	}{
		{name: "local", vars: map[string]string{"FOREMAN_AUTH_METHODS": "local"}, local: true},
		{name: "headers", vars: with("headers"), hdrs: true},
		{name: "both", vars: with("local,headers"), local: true, hdrs: true},
		{name: "spaces and case", vars: with(" HEADERS ,  Local"), local: true, hdrs: true},
		{name: "duplicates", vars: map[string]string{"FOREMAN_AUTH_METHODS": "local,local"}, local: true},
		{name: "unknown", vars: map[string]string{"FOREMAN_AUTH_METHODS": "local,ldap"}, wantErr: "FOREMAN_AUTH_METHODS has unknown methods \"ldap\""},
		{name: "oidc is not yet known", vars: map[string]string{"FOREMAN_AUTH_METHODS": "oidc"}, wantErr: "unknown methods \"oidc\""},
		{name: "empty means the default", vars: map[string]string{"FOREMAN_AUTH_METHODS": ""}, local: true},
		{name: "only commas", vars: map[string]string{"FOREMAN_AUTH_METHODS": " , ,"}, wantErr: "lists no methods"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := load(tt.vars)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.AuthLocal != tt.local || c.AuthHeaders != tt.hdrs {
				t.Errorf("local=%v headers=%v, want %v %v", c.AuthLocal, c.AuthHeaders, tt.local, tt.hdrs)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"uid negative", map[string]string{"FOREMAN_GAME_UID": "-1"}, "FOREMAN_GAME_UID must be a whole number"},
		{"uid text", map[string]string{"FOREMAN_GAME_UID": "factorio"}, "FOREMAN_GAME_UID must be a whole number"},
		{"uid too large", map[string]string{"FOREMAN_GAME_UID": "99999999999999"}, "FOREMAN_GAME_UID must be a whole number"},
		{"uid zero", map[string]string{"FOREMAN_GAME_UID": "0"}, "FOREMAN_GAME_UID must not be 0"},
		{"gid zero", map[string]string{"FOREMAN_GAME_GID": "0"}, "FOREMAN_GAME_GID must not be 0"},
		{"gid decimal", map[string]string{"FOREMAN_GAME_GID": "1.5"}, "FOREMAN_GAME_GID must be a whole number"},
		{"listen no port", map[string]string{"FOREMAN_LISTEN": "localhost"}, "FOREMAN_LISTEN is not a valid listen address"},
		{"listen bad port", map[string]string{"FOREMAN_LISTEN": ":http"}, "FOREMAN_LISTEN is not a valid listen address"},
		{"listen port too big", map[string]string{"FOREMAN_LISTEN": ":70000"}, "FOREMAN_LISTEN is not a valid listen address"},
		{"url relative", map[string]string{"FOREMAN_PUBLIC_URL": "foreman.example.com"}, "FOREMAN_PUBLIC_URL must be an absolute http or https URL"},
		{"url wrong scheme", map[string]string{"FOREMAN_PUBLIC_URL": "ftp://foreman.example.com"}, "FOREMAN_PUBLIC_URL must be an absolute"},
		{"url no host", map[string]string{"FOREMAN_PUBLIC_URL": "https://"}, "FOREMAN_PUBLIC_URL must be an absolute"},
		{"url unparsable", map[string]string{"FOREMAN_PUBLIC_URL": "http://[::1"}, "FOREMAN_PUBLIC_URL must be an absolute"},
		{"idle text", map[string]string{"FOREMAN_SESSION_IDLE": "a week"}, "FOREMAN_SESSION_IDLE must be a positive duration"},
		{"idle zero", map[string]string{"FOREMAN_SESSION_IDLE": "0s"}, "FOREMAN_SESSION_IDLE must be a positive duration"},
		{"idle negative", map[string]string{"FOREMAN_SESSION_IDLE": "-5m"}, "FOREMAN_SESSION_IDLE must be a positive duration"},
		{"idle no unit", map[string]string{"FOREMAN_SESSION_IDLE": "60"}, "FOREMAN_SESSION_IDLE must be a positive duration"},
		{"proxy bad cidr", map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.0.0.0/33"}, `FOREMAN_TRUSTED_PROXIES has entries that are not an address or range (like 10.0.0.0/8): "10.0.0.0/33"`},
		{"proxy hostname", map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.0.0.1,authentik"}, `"authentik"`},
		{"proxy zone", map[string]string{"FOREMAN_TRUSTED_PROXIES": "fe80::1%eth0"}, "FOREMAN_TRUSTED_PROXIES"},
		{"headers without proxies", map[string]string{"FOREMAN_AUTH_METHODS": "headers", "FOREMAN_AUTH_USER_HEADER": "Remote-User", "FOREMAN_AUTH_GROUPS_HEADER": "Remote-Groups", "FOREMAN_AUTH_REQUIRED_GROUP": "factorio"}, "FOREMAN_TRUSTED_PROXIES must list at least one address range"},
		{"headers with only blank proxies", map[string]string{"FOREMAN_AUTH_METHODS": "headers", "FOREMAN_AUTH_USER_HEADER": "Remote-User", "FOREMAN_AUTH_GROUPS_HEADER": "Remote-Groups", "FOREMAN_AUTH_REQUIRED_GROUP": "factorio", "FOREMAN_TRUSTED_PROXIES": " , "}, "FOREMAN_TRUSTED_PROXIES must list at least one"},
		{"headers without required group", map[string]string{"FOREMAN_AUTH_METHODS": "headers", "FOREMAN_AUTH_USER_HEADER": "Remote-User", "FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8", "FOREMAN_AUTH_GROUPS_HEADER": "Remote-Groups"}, "FOREMAN_AUTH_REQUIRED_GROUP must be set when FOREMAN_AUTH_METHODS includes headers"},
		{"headers without groups header", map[string]string{"FOREMAN_AUTH_METHODS": "headers", "FOREMAN_AUTH_USER_HEADER": "Remote-User", "FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8", "FOREMAN_AUTH_REQUIRED_GROUP": "factorio"}, "FOREMAN_AUTH_GROUPS_HEADER must be set when FOREMAN_AUTH_METHODS includes headers"},
		{"headers without user header", map[string]string{"FOREMAN_AUTH_METHODS": "local,headers", "FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8", "FOREMAN_AUTH_GROUPS_HEADER": "Remote-Groups", "FOREMAN_AUTH_REQUIRED_GROUP": "factorio"}, "FOREMAN_AUTH_USER_HEADER must be set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := load(tt.vars)
			if err == nil {
				t.Fatalf("want an error containing %q, got config %+v", tt.want, c)
			}
			if c != nil {
				t.Errorf("config should be nil on error, got %+v", c)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want containing %q", err, tt.want)
			}
		})
	}
}

func TestHeadersOffIgnoresHeaderSettings(t *testing.T) {
	// Proxies and a user header with headers off are allowed; they just do nothing.
	if _, err := load(map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8"}); err != nil {
		t.Error(err)
	}
	if _, err := load(map[string]string{"FOREMAN_AUTH_USER_HEADER": "Remote-User"}); err != nil {
		t.Error(err)
	}
	if _, err := load(map[string]string{"FOREMAN_AUTH_REQUIRED_GROUP": "factorio"}); err != nil {
		t.Error(err)
	}
}

func TestLoadReportsAllProblems(t *testing.T) {
	_, err := load(map[string]string{
		"FOREMAN_GAME_UID":        "x",
		"FOREMAN_LISTEN":          "nope",
		"FOREMAN_PUBLIC_URL":      "nope",
		"FOREMAN_AUTH_METHODS":    "headers",
		"FOREMAN_SESSION_IDLE":    "soon",
		"FOREMAN_TRUSTED_PROXIES": "bad",
	})
	if err == nil {
		t.Fatal("want errors")
	}
	for _, v := range []string{
		"FOREMAN_GAME_UID", "FOREMAN_LISTEN", "FOREMAN_PUBLIC_URL",
		"FOREMAN_AUTH_USER_HEADER", "FOREMAN_AUTH_GROUPS_HEADER", "FOREMAN_AUTH_REQUIRED_GROUP",
		"FOREMAN_SESSION_IDLE", "FOREMAN_TRUSTED_PROXIES",
	} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("error does not mention %s:\n%v", v, err)
		}
	}
	if lines := strings.Count(err.Error(), "\n") + 1; lines < 9 {
		t.Errorf("want at least 9 problems, got %d:\n%v", lines, err)
	}
}

func TestProxiesAreNormalised(t *testing.T) {
	c, err := load(map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.1.2.3/8,::ffff:172.16.0.1,,"})
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.1/32")}
	if !reflect.DeepEqual(c.TrustedProxies, want) {
		t.Errorf("got %v, want %v", c.TrustedProxies, want)
	}
}

func TestEmptyValuesAreUnset(t *testing.T) {
	c, err := load(map[string]string{
		"FOREMAN_DATA_DIR": "", "FOREMAN_GAME_UID": "", "FOREMAN_LISTEN": "",
		"FOREMAN_PUBLIC_URL": "", "FOREMAN_AUTH_METHODS": "", "FOREMAN_SESSION_IDLE": "", "DOCKER_HOST": "", "FOREMAN_TRUSTED_PROXIES": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := load(nil)
	if !reflect.DeepEqual(c, d) {
		t.Errorf("empty values should equal defaults:\n got %+v\nwant %+v", c, d)
	}
}

// Adopted mode and its settings were removed (ADR 0005); Foreman no longer
// reads them, so a leftover one changes nothing.
func TestRemovedSettingsAreIgnored(t *testing.T) {
	c, err := load(map[string]string{
		"FOREMAN_MODE":               "adopted",
		"FOREMAN_GAME_CONTAINER":     "factorio",
		"FOREMAN_GAME_DIR":           "/srv/factorio",
		"FOREMAN_RCON_PASSWORD":      "hunter2-pw",
		"FOREMAN_RCON_PASSWORD_FILE": "/nonexistent/rconpw",
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := load(nil)
	if !reflect.DeepEqual(c, d) {
		t.Errorf("removed settings changed the config:\n got %+v\nwant %+v", c, d)
	}
}
