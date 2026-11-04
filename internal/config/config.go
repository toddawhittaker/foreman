// Package config loads and validates Foreman's settings from environment
// variables (SPEC 3.2). Load reports every problem at once, each as a
// sentence that names the variable.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// Defaults for variables that have one.
const (
	DefaultDataDir     = "/data"
	DefaultListen      = ":8080"
	DefaultGameID      = 845
	DefaultSessionIdle = 7 * 24 * time.Hour
	DefaultDockerHost  = "unix:///var/run/docker.sock"
)

// Run modes (SPEC 4.1).
const (
	ModeManaged = "managed"
	ModeAdopted = "adopted"
)

// Variable names.
const (
	varMode        = "FOREMAN_MODE"
	varDataDir     = "FOREMAN_DATA_DIR"
	varHostDataDir = "FOREMAN_HOST_DATA_DIR"
	varGameUID     = "FOREMAN_GAME_UID"
	varGameGID     = "FOREMAN_GAME_GID"
	varListen      = "FOREMAN_LISTEN"
	varPublicURL   = "FOREMAN_PUBLIC_URL"
	varAuthMethods = "FOREMAN_AUTH_METHODS"
	varUserHeader  = "FOREMAN_AUTH_USER_HEADER"
	varGroupHeader = "FOREMAN_AUTH_GROUPS_HEADER"
	varGroup       = "FOREMAN_AUTH_REQUIRED_GROUP"
	varProxies     = "FOREMAN_TRUSTED_PROXIES"
	varSessionIdle = "FOREMAN_SESSION_IDLE"
	varDockerHost  = "DOCKER_HOST"
	varGameNetwork = "FOREMAN_GAME_NETWORK"
	varContainer   = "FOREMAN_GAME_CONTAINER"
	varGameDir     = "FOREMAN_GAME_DIR"
	varRCONPass    = "FOREMAN_RCON_PASSWORD" //nolint:gosec // a variable name, not a credential
)

// Config is Foreman's validated configuration.
type Config struct {
	Mode             string // ModeManaged or ModeAdopted
	DataDir          string
	HostDataDir      string // empty means discover it from the container
	GameUID, GameGID int
	Listen           string
	PublicURL        *url.URL // nil when unset
	AuthLocal        bool
	AuthHeaders      bool
	UserHeader       string // canonical header name
	GroupsHeader     string // canonical header name, may be empty
	RequiredGroup    string
	TrustedProxies   []netip.Prefix
	SessionIdle      time.Duration
	DockerHost       string
	GameNetwork      string // the Docker network the game joins; empty means work it out (SPEC 4.4)

	// Adopted mode only (SPEC 4.7, 4.10).
	GameContainer string        // name or id of the container Foreman controls
	GameDir       string        // the game folder as mounted inside Foreman; absolute and clean
	RCONPassword  secret.Secret // zero when unset: the world's own config/rconpw is used
}

// SecureCookies reports whether session cookies carry the Secure flag,
// which is when the public URL is https (SPEC 12.8).
func (c *Config) SecureCookies() bool {
	return c.PublicURL != nil && c.PublicURL.Scheme == "https"
}

// Load reads the configuration. getenv is os.LookupEnv in production.
func Load(getenv func(string) (string, bool)) (*Config, error) {
	return load(env{getenv: getenv, readFile: os.ReadFile})
}

func load(e env) (*Config, error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	str := func(name, def string) string {
		if v, ok := e.get(name); ok {
			return v
		}
		return def
	}

	c := &Config{
		DataDir:       str(varDataDir, DefaultDataDir),
		HostDataDir:   str(varHostDataDir, ""),
		Listen:        str(varListen, DefaultListen),
		UserHeader:    canonicalHeader(str(varUserHeader, "")),
		GroupsHeader:  canonicalHeader(str(varGroupHeader, "")),
		RequiredGroup: str(varGroup, ""),
		DockerHost:    str(varDockerHost, DefaultDockerHost),
		GameNetwork:   str(varGameNetwork, ""),
		GameContainer: str(varContainer, ""),
		GameDir:       str(varGameDir, ""),
	}

	switch mode := strings.ToLower(strings.TrimSpace(str(varMode, ModeManaged))); mode {
	case ModeManaged, ModeAdopted:
		c.Mode = mode
	default:
		fail("%s must be %q or %q, not %q", varMode, ModeManaged, ModeAdopted, mode)
	}

	var err error
	if c.GameUID, err = parseID(e, varGameUID); err != nil {
		errs = append(errs, err)
	}
	if c.GameGID, err = parseID(e, varGameGID); err != nil {
		errs = append(errs, err)
	}

	if err := validateListen(c.Listen); err != nil {
		fail("%s is not a valid listen address (use host:port, for example :8080): %v", varListen, err)
	}

	if raw, ok := e.get(varPublicURL); ok {
		if c.PublicURL, err = parsePublicURL(raw); err != nil {
			fail("%s %v", varPublicURL, err)
		}
	}

	if raw, ok := e.get(varAuthMethods); !ok {
		c.AuthLocal = true
	} else if c.AuthLocal, c.AuthHeaders, err = parseAuthMethods(raw); err != nil {
		errs = append(errs, err)
	}

	if raw, ok := e.get(varProxies); ok {
		if c.TrustedProxies, err = parseProxies(raw); err != nil {
			errs = append(errs, err)
		}
	}

	if c.AuthHeaders {
		if c.UserHeader == "" {
			fail("%s must be set when %s includes headers", varUserHeader, varAuthMethods)
		}
		if c.GroupsHeader == "" {
			fail("%s must be set when %s includes headers", varGroupHeader, varAuthMethods)
		}
		if c.RequiredGroup == "" {
			fail("%s must be set when %s includes headers; without it anyone the proxy lets through could sign in", varGroup, varAuthMethods)
		}
		if len(c.TrustedProxies) == 0 {
			fail("%s must list at least one address range when %s includes headers", varProxies, varAuthMethods)
		}
	}

	if pw, ok, err := e.lookupSecret(varRCONPass); err != nil {
		errs = append(errs, err)
	} else if ok {
		c.RCONPassword = pw
	}
	errs = append(errs, validateAdopted(c, e)...)

	c.SessionIdle = DefaultSessionIdle
	if raw, ok := e.get(varSessionIdle); ok {
		if d, err := time.ParseDuration(raw); err != nil || d <= 0 {
			fail("%s must be a positive duration such as 168h, not %q", varSessionIdle, raw)
		} else {
			c.SessionIdle = d
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// validateAdopted checks the settings that belong to adopted mode (SPEC 4.7,
// 4.10) and reports the ones set in managed mode, so a misplaced setting is
// visible instead of silently ignored.
func validateAdopted(c *Config, e env) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	switch c.Mode {
	case ModeManaged:
		for _, name := range []string{varContainer, varGameDir, varRCONPass, varRCONPass + "_FILE"} {
			if _, ok := e.get(name); ok {
				fail("%s is only used when %s is %s; remove it or switch modes", name, varMode, ModeAdopted)
			}
		}
		return errs
	case ModeAdopted:
	default:
		return errs // the mode error is already reported
	}

	if c.GameContainer == "" {
		fail("%s must be set when %s is %s: it names the game container Foreman controls", varContainer, varMode, ModeAdopted)
	}
	switch {
	case c.GameDir == "":
		fail("%s must be set when %s is %s: it is the game folder as Foreman sees it", varGameDir, varMode, ModeAdopted)
	case !filepath.IsAbs(c.GameDir):
		fail("%s must be an absolute path, not %q", varGameDir, c.GameDir)
	case filepath.Clean(c.GameDir) != c.GameDir:
		fail("%s must be a clean path with no trailing slash, dots or doubled slashes; use %q", varGameDir, filepath.Clean(c.GameDir))
	default:
		data, err := filepath.Abs(c.DataDir)
		if err != nil {
			break
		}
		if msg := NestingProblem(data, c.GameDir); msg != "" {
			fail("%s", msg)
		}
	}
	return errs
}

// NestingProblem returns the sentence refusing a data directory and a game
// folder when one is the other or inside it, or "" when they are apart
// (SPEC 3.2). It compares the paths as given and never touches the
// filesystem; a caller that can resolve links does so first.
func NestingProblem(dataDir, gameDir string) string {
	switch {
	case within(dataDir, gameDir):
		return fmt.Sprintf("%s must not be the game folder or inside it (%s): the game's files and Foreman's would mix", varDataDir, varGameDir)
	case within(gameDir, dataDir):
		return fmt.Sprintf("%s must not be inside %s: snapshots of the data directory would include the game folder", varGameDir, varDataDir)
	}
	return ""
}

// within reports whether path is dir or inside it. It compares cleaned
// paths and never touches the filesystem.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func canonicalHeader(name string) string {
	if name == "" {
		return ""
	}
	return http.CanonicalHeaderKey(strings.TrimSpace(name))
}

func parseID(e env, name string) (int, error) {
	raw, ok := e.get(name)
	if !ok {
		return DefaultGameID, nil
	}
	n, err := strconv.ParseUint(raw, 10, 31)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number above zero, not %q", name, raw)
	}
	if n == 0 {
		return 0, fmt.Errorf("%s must not be 0, because the game would run as root. Use the id of an unprivileged user", name)
	}
	return int(n), nil
}

func validateListen(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("port %q is not a number from 0 to 65535", port)
	}
	return nil
}

func parsePublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("must be an absolute http or https URL, for example https://foreman.example.com, not %q", raw)
	}
	return u, nil
}

func parseAuthMethods(raw string) (local, headers bool, err error) {
	var unknown []string
	count := 0
	for _, m := range strings.Split(raw, ",") {
		m = strings.ToLower(strings.TrimSpace(m))
		switch m {
		case "":
			continue
		case "local":
			local = true
		case "headers":
			headers = true
		default:
			unknown = append(unknown, m)
		}
		count++
	}
	switch {
	case len(unknown) > 0:
		return false, false, fmt.Errorf("%s has unknown methods %q; use local, headers or both", varAuthMethods, strings.Join(unknown, ","))
	case count == 0:
		return false, false, fmt.Errorf("%s lists no methods; use local, headers or both", varAuthMethods)
	}
	return local, headers, nil
}

// parseProxies accepts address ranges (10.0.0.0/8) and single addresses,
// which become /32 or /128 ranges.
func parseProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	var bad []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(item); err == nil && a.Zone() == "" {
			a = a.Unmap()
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		bad = append(bad, item)
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("%s has entries that are not an address or range (like 10.0.0.0/8): %q", varProxies, strings.Join(bad, ","))
	}
	return out, nil
}
