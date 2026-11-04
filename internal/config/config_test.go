package config_test

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/secret"
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
		Mode:        "managed",
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
		"FOREMAN_MODE":                "Adopted",
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
		"FOREMAN_GAME_CONTAINER":      "factorio",
		"FOREMAN_GAME_DIR":            "/srv/factorio",
		"FOREMAN_RCON_PASSWORD":       "s3cret-rcon",
	})
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := url.Parse("https://foreman.example.com")
	want := &config.Config{
		Mode:          "adopted",
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

		GameContainer: "factorio",
		GameDir:       "/srv/factorio",
		RCONPassword:  secret.New("s3cret-rcon"),
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
		{"bad mode", map[string]string{"FOREMAN_MODE": "swarm"}, `FOREMAN_MODE must be "managed" or "adopted"`},
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
		"FOREMAN_MODE":            "swarm",
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
		"FOREMAN_MODE", "FOREMAN_GAME_UID", "FOREMAN_LISTEN", "FOREMAN_PUBLIC_URL",
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
		"FOREMAN_MODE": "", "FOREMAN_DATA_DIR": "", "FOREMAN_GAME_UID": "", "FOREMAN_LISTEN": "",
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

// adoptedVars is a valid adopted-mode setup; tests change one thing.
func adoptedVars(extra map[string]string) map[string]string {
	m := map[string]string{
		"FOREMAN_MODE":           "adopted",
		"FOREMAN_DATA_DIR":       "/srv/foreman",
		"FOREMAN_GAME_CONTAINER": "factorio",
		"FOREMAN_GAME_DIR":       "/srv/factorio",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestAdoptedSettings(t *testing.T) {
	c, err := load(adoptedVars(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.GameContainer != "factorio" || c.GameDir != "/srv/factorio" {
		t.Errorf("container %q dir %q", c.GameContainer, c.GameDir)
	}
	if c.RCONPassword.Reveal() != "" {
		t.Error("RCONPassword should be zero when unset")
	}
}

func TestAdoptedErrors(t *testing.T) {
	tests := []struct {
		name  string
		vars  map[string]string
		wants []string
	}{
		{"container missing", adoptedVars(map[string]string{"FOREMAN_GAME_CONTAINER": ""}),
			[]string{"FOREMAN_GAME_CONTAINER must be set when FOREMAN_MODE is adopted"}},
		{"dir missing", adoptedVars(map[string]string{"FOREMAN_GAME_DIR": ""}),
			[]string{"FOREMAN_GAME_DIR must be set when FOREMAN_MODE is adopted"}},
		{"dir relative", adoptedVars(map[string]string{"FOREMAN_GAME_DIR": "factorio"}),
			[]string{"FOREMAN_GAME_DIR must be an absolute path"}},
		{"dir not clean", adoptedVars(map[string]string{"FOREMAN_GAME_DIR": "/srv/factorio/"}),
			[]string{"FOREMAN_GAME_DIR must be a clean path", `"/srv/factorio"`}},
		{"dir with dots", adoptedVars(map[string]string{"FOREMAN_GAME_DIR": "/srv/x/../factorio"}),
			[]string{"FOREMAN_GAME_DIR must be a clean path"}},
		{"data dir is game dir", adoptedVars(map[string]string{"FOREMAN_DATA_DIR": "/srv/factorio"}),
			[]string{"FOREMAN_DATA_DIR", "FOREMAN_GAME_DIR", "must not be the game folder or inside it"}},
		{"data dir inside game dir", adoptedVars(map[string]string{"FOREMAN_DATA_DIR": "/srv/factorio/foreman"}),
			[]string{"FOREMAN_DATA_DIR", "FOREMAN_GAME_DIR", "must not be the game folder or inside it"}},
		{"data dir unclean inside game dir", adoptedVars(map[string]string{"FOREMAN_DATA_DIR": "/srv/factorio/a/../b/"}),
			[]string{"must not be the game folder or inside it"}},
		{"game dir inside data dir", adoptedVars(map[string]string{"FOREMAN_DATA_DIR": "/srv", "FOREMAN_GAME_DIR": "/srv/factorio"}),
			[]string{"FOREMAN_GAME_DIR", "FOREMAN_DATA_DIR", "must not be inside"}},
		{"game dir inside default data dir", map[string]string{"FOREMAN_MODE": "adopted", "FOREMAN_GAME_CONTAINER": "f", "FOREMAN_GAME_DIR": "/data/factorio"},
			[]string{"must not be inside"}},
		{"both missing reported together", map[string]string{"FOREMAN_MODE": "adopted"},
			[]string{"FOREMAN_GAME_CONTAINER must be set", "FOREMAN_GAME_DIR must be set"}},
		{"managed container", map[string]string{"FOREMAN_GAME_CONTAINER": "factorio"},
			[]string{"FOREMAN_GAME_CONTAINER is only used when FOREMAN_MODE is adopted"}},
		{"managed dir", map[string]string{"FOREMAN_GAME_DIR": "/srv/factorio"},
			[]string{"FOREMAN_GAME_DIR is only used when FOREMAN_MODE is adopted"}},
		{"managed password", map[string]string{"FOREMAN_RCON_PASSWORD": "hunter2-pw"},
			[]string{"FOREMAN_RCON_PASSWORD is only used when FOREMAN_MODE is adopted"}},
		{"password both forms", adoptedVars(map[string]string{"FOREMAN_RCON_PASSWORD": "hunter2-pw", "FOREMAN_RCON_PASSWORD_FILE": "/nope"}),
			[]string{"FOREMAN_RCON_PASSWORD and FOREMAN_RCON_PASSWORD_FILE are both set"}},
		{"password file unreadable", adoptedVars(map[string]string{"FOREMAN_RCON_PASSWORD_FILE": "/nonexistent/rconpw"}),
			[]string{"FOREMAN_RCON_PASSWORD_FILE: cannot read the file"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := load(tt.vars)
			if err == nil {
				t.Fatalf("want an error, got config %+v", c)
			}
			if c != nil {
				t.Errorf("config should be nil on error, got %+v", c)
			}
			for _, w := range tt.wants {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error = %q, want containing %q", err, w)
				}
			}
			if strings.Contains(err.Error(), "hunter2-pw") {
				t.Errorf("error leaks the password: %q", err)
			}
		})
	}
}

func TestAdoptedNestingAllowsSiblings(t *testing.T) {
	// A shared name prefix is not nesting.
	for _, dirs := range [][2]string{
		{"/srv/foreman", "/srv/foreman-game"},
		{"/srv/foreman-data", "/srv/foreman"},
		{"/data", "/games/factorio"},
	} {
		_, err := load(adoptedVars(map[string]string{"FOREMAN_DATA_DIR": dirs[0], "FOREMAN_GAME_DIR": dirs[1]}))
		if err != nil {
			t.Errorf("data %s, game %s: %v", dirs[0], dirs[1], err)
		}
	}
}

func TestRCONPasswordForms(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rconpw")
	if err := os.WriteFile(file, []byte("from-file-pw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, vars := range map[string]map[string]string{
		"value": {"FOREMAN_RCON_PASSWORD": "from-file-pw"}, //nolint:gosec // a test value
		"file":  {"FOREMAN_RCON_PASSWORD_FILE": file},
	} {
		c, err := load(adoptedVars(vars))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := c.RCONPassword.Reveal(); got != "from-file-pw" {
			t.Errorf("%s: password = %q", name, got)
		}
	}
}

func TestRCONPasswordNeverPrinted(t *testing.T) {
	const pw = "hunter2-very-secret"
	c, err := load(adoptedVars(map[string]string{"FOREMAN_RCON_PASSWORD": pw}))
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), fmt.Sprint(*c)} {
		if strings.Contains(out, pw) {
			t.Errorf("Config prints the password: %s", out)
		}
	}

	// A config that fails for another reason must not echo it either.
	_, err = load(adoptedVars(map[string]string{"FOREMAN_RCON_PASSWORD": pw, "FOREMAN_GAME_DIR": "relative", "FOREMAN_LISTEN": "nope"}))
	if err == nil || strings.Contains(err.Error(), pw) {
		t.Errorf("error = %v", err)
	}
}
