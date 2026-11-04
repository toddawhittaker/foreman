package fakegame

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/world"
)

// AdoptedAddress is the seeded adopted container's address on SelfNetwork,
// next to SelfAddress.
const AdoptedAddress = "10.250.0.3/24"

// volume is where the factoriotools image keeps everything.
const volume = "/factorio"

// Adopted seeds one game container that was already there when Foreman
// started, the way a person's own compose file would have made it (SPEC
// 4.7). It needs Config.SelfNetwork, which it joins. While it is set the
// fake Docker refuses the calls adopted mode never makes (SPEC 4.11).
//
// When the container starts, the fake does what the image's own
// /docker-entrypoint.sh does that Foreman can see: it follows the game
// folder's links to the save, the mods and the RCON password, writes the
// shell's trace of the game command, password included, to the log, and
// applies DLC_SPACE_AGE to mods/mod-list.json.
type Adopted struct {
	Name string `json:"name"`
	// GameDir is the host folder bind-mounted at /factorio.
	GameDir string `json:"gameDir"`
	// Env is added to the image's default environment; a name that is
	// already there replaces it. The image tag follows VERSION.
	Env []string `json:"env,omitempty"`
	// User is the container's user, such as "845:845"; empty leaves it to
	// the image.
	User string `json:"user,omitempty"`
	// RCONHostPort, when set, publishes the container's RCON port on that
	// host port.
	RCONHostPort string `json:"rconHostPort,omitempty"`
	// Running seeds the container already running; otherwise it is stopped.
	Running bool `json:"running,omitempty"`
}

// imageEnv is the environment of factoriotools/factorio:2.1.24, from
// docker image inspect.
var imageEnv = []string{
	"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	"PORT=34197",
	"RCON_PORT=27015",
	"SAVES=/factorio/saves",
	"PRESET=",
	"CONFIG=/factorio/config",
	"MODS=/factorio/mods",
	"SCENARIOS=/factorio/scenarios",
	"SCRIPTOUTPUT=/factorio/script-output",
	"PUID=845",
	"PGID=845",
	"DLC_SPACE_AGE=true",
	"VERSION=2.1.24",
	"SHA256=c49b66cff90aceff87b30f2973af0df7f51712cb2ee3153e0169d9bed46a964f",
}

func (a Adopted) validate(selfNetwork string) error {
	switch {
	case selfNetwork == "":
		return errors.New("Adopted needs SelfNetwork")
	case a.Name == "":
		return errors.New("Adopted needs a Name")
	case !filepath.IsAbs(a.GameDir):
		return errors.New("Adopted needs an absolute GameDir")
	}
	for _, kv := range a.Env {
		if !strings.Contains(kv, "=") {
			return fmt.Errorf("Adopted env entry %q is not NAME=value", kv)
		}
	}
	return nil
}

// mergeEnv adds extra to base, replacing a name that is already there.
func mergeEnv(base, extra []string) []string {
	out := slices.Clone(base)
	for _, kv := range extra {
		name, _, _ := strings.Cut(kv, "=")
		replaced := false
		for i, have := range out {
			if n, _, _ := strings.Cut(have, "="); n == name {
				out[i], replaced = kv, true
			}
		}
		if !replaced {
			out = append(out, kv)
		}
	}
	return out
}

func envValue(env []string, name string) string {
	for _, kv := range env {
		if n, v, _ := strings.Cut(kv, "="); n == name {
			return v
		}
	}
	return ""
}

// seedAdopted adds the container and turns the managed-call ban on.
func (g *Game) seedAdopted(a Adopted, network string) {
	env := mergeEnv(imageEnv, a.Env)
	hc := docker.HostConfig{
		RestartPolicy: docker.RestartPolicy{Name: "unless-stopped"},
		NetworkMode:   network,
	}
	if a.RCONHostPort != "" {
		hc.PortBindings = map[string][]docker.PortBinding{
			envValue(env, "RCON_PORT") + "/tcp": {{HostIP: "0.0.0.0", HostPort: a.RCONHostPort}},
		}
	}
	c := dockertest.Container{
		Name: a.Name,
		Spec: docker.ContainerSpec{
			Image: "factoriotools/factorio:" + envValue(env, "VERSION"),
			Env:   env, User: a.User, HostConfig: hc,
		},
		Status:   "exited",
		Mounts:   []docker.Mount{{Source: a.GameDir, Destination: volume}},
		Networks: map[string]string{network: ""},
	}
	if a.Running {
		c.Status, c.StartedAt = "running", time.Now().UTC()
		c.Networks[network] = AdoptedAddress
	}
	g.daemon.Seed(c)
	seeded, _ := g.daemon.Container(a.Name)
	g.adoptedID = seeded.ID
	g.daemon.ForbidManagedCalls()
	if a.Running {
		g.boot(seeded.ID)
	}
}

// entrypoint is what one run of the image's entrypoint leaves for the game
// to use, and what it printed on the way.
type entrypoint struct {
	save string // file name of the newest save
	// savesDir is the container path of the saves folder with links
	// resolved, which is what the game prints (/factorio/worlds/a/saves).
	savesDir string
	password string
	rconPort string
	log      []string
	err      error // the script stopped (it runs with set -e)
}

// runEntrypoint follows /docker-entrypoint.sh, as far as Foreman can tell,
// through the folder mounted at /factorio. Every path is opened through the
// game folder's links, as the container would.
func runEntrypoint(env []string, mounts []docker.Mount) entrypoint {
	e := entrypoint{rconPort: envValue(env, "RCON_PORT")}
	root := ""
	for _, m := range mounts {
		if m.Destination == volume {
			root = m.Source
		}
	}
	// host maps a path inside the container to the game folder.
	host := func(p string) string {
		if rel, ok := strings.CutPrefix(p, volume+"/"); ok && root != "" {
			return filepath.Join(root, rel)
		}
		return ""
	}
	fail := func(format string, args ...any) entrypoint {
		e.err = fmt.Errorf(format, args...)
		return e
	}

	saves, config, mods := envValue(env, "SAVES"), envValue(env, "CONFIG"), envValue(env, "MODS")
	for _, p := range []string{saves, config, mods} {
		dir := host(p)
		if dir == "" || os.MkdirAll(dir, 0o755) != nil { //nolint:gosec // the image's own mode
			return fail("mkdir: cannot create directory '%s'", p)
		}
	}

	pwFile := filepath.Join(host(config), "rconpw")
	b, err := os.ReadFile(pwFile) //nolint:gosec // a path inside the game folder the test chose
	if errors.Is(err, fs.ErrNotExist) {
		b, err = newPassword()
		if err == nil {
			err = os.WriteFile(pwFile, b, 0o644) //nolint:gosec // as the image writes it
		}
	}
	if err != nil {
		return fail("cat: %s/rconpw: cannot read", config)
	}
	e.password = strings.TrimRight(string(b), "\n")

	lines, err := applyDLC(filepath.Join(host(mods), world.ModListFile), envValue(env, "DLC_SPACE_AGE"))
	if err != nil {
		return fail("docker-dlc.sh: cannot change %s/%s", mods, world.ModListFile)
	}
	e.log = append(e.log, lines...)

	e.save = newestSave(host(saves))
	e.savesDir = resolvedInContainer(root, host(saves), saves)
	e.log = append(e.log, "++ cat "+config+"/rconpw", "+ exec "+gameCommand(env, e.password))
	return e
}

// resolvedInContainer maps a folder in the game folder to the path the
// container sees once links are followed. A link that leaves the game
// folder, or cannot be resolved, leaves fallback, the unresolved path.
func resolvedInContainer(root, dir, fallback string) string {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fallback
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fallback
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fallback
	}
	return path.Join(volume, rel)
}

func newPassword() ([]byte, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return []byte(hex.EncodeToString(raw)[:15] + "\n"), nil
}

// gameCommand is the line the entrypoint ends with, as the shell's trace
// prints it when the container does not start as root.
func gameCommand(env []string, password string) string {
	config := envValue(env, "CONFIG")
	return strings.Join([]string{
		"/opt/factorio/bin/x64/factorio",
		"--server-settings", config + "/server-settings.json",
		"--server-banlist", config + "/server-banlist.json",
		"--rcon-port", envValue(env, "RCON_PORT"),
		"--server-whitelist", config + "/server-whitelist.json",
		"--use-server-whitelist",
		"--server-adminlist", config + "/server-adminlist.json",
		"--rcon-password", password,
		"--server-id", config + "/server-id.json",
		"--mod-directory", envValue(env, "MODS"),
		"--port", envValue(env, "PORT"),
		"--start-server-load-latest",
	}, " ")
}

// newestSave is what --start-server-load-latest picks: the most recently
// changed .zip in the folder, links followed. A folder with none gets a
// new _autosave1.zip from the entrypoint.
func newestSave(dir string) string {
	entries, _ := os.ReadDir(dir)
	best, bestTime := "_autosave1.zip", time.Time{}
	found := false
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if !strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tmp.zip") {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil || fi.IsDir() {
			continue
		}
		if !found || fi.ModTime().After(bestTime) || (fi.ModTime().Equal(bestTime) && e.Name() > best) {
			best, bestTime, found = e.Name(), fi.ModTime(), true
		}
	}
	return best
}

type modList struct {
	Mods []modEntry `json:"mods"`
}

type modEntry struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Version string `json:"version,omitempty"`
}

var spaceAgeMods = []string{"elevated-rails", "quality", "space-age", "recycler"}

// applyDLC is docker-dlc.sh: with DLC_SPACE_AGE "true" it turns the four
// Space Age mods on where the list has them, with "false" it turns them off
// or adds them off, and any other value is a list of the ones to enable.
// It returns the lines the script prints.
func applyDLC(path, setting string) ([]string, error) {
	list := modList{Mods: []modEntry{{Name: "base", Enabled: true}}}
	b, err := os.ReadFile(path) //nolint:gosec // a path inside the game folder the test chose
	switch {
	case err == nil:
		list = modList{}
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}

	var enable, disable []string
	for _, m := range spaceAgeMods {
		if setting == "true" || (setting != "false" && slices.Contains(strings.Fields(setting), m)) {
			enable = append(enable, m)
		} else {
			disable = append(disable, m)
		}
	}
	var lines []string
	for _, m := range enable {
		lines = append(lines, "Enable mod "+m+" for DLC")
		for i := range list.Mods {
			if list.Mods[i].Name == m {
				list.Mods[i].Enabled = true
			}
		}
	}
	for _, m := range disable {
		lines = append(lines, "Disable mod "+m+" for DLC")
		found := false
		for i := range list.Mods {
			if list.Mods[i].Name == m {
				list.Mods[i].Enabled, found = false, true
			}
		}
		if !found {
			list.Mods = append(list.Mods, modEntry{Name: m})
		}
	}
	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, err
	}
	return lines, os.WriteFile(path, append(out, '\n'), 0o644) //nolint:gosec // as the image writes it
}
