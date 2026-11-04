package game

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gamedir"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// adoptedMode is what adopted mode needs (SPEC 4.7 to 4.12).
type adoptedMode struct {
	container string        // FOREMAN_GAME_CONTAINER
	dir       string        // FOREMAN_GAME_DIR
	rconPW    secret.Secret // FOREMAN_RCON_PASSWORD; zero means each world's own file
	uid, gid  int
}

// adoptedView is the last look at the container and the game folder, for
// Status. Controller.mu guards it.
type adoptedView struct {
	layout        gamedir.State
	layoutErr     string // the last error reading the folder, logged once
	version       string // the container's VERSION; "" when it has none
	rconPublished bool
	// networkWarning is the network refusal the last two checks in a row
	// found; "" when there is none (SPEC 4.4).
	networkWarning   string
	networkRefusals  int       // refusals in a row
	networkUnchecked string    // the last "could not check", logged once
	networkCleanAt   time.Time // the last check that found nothing wrong
	// netRawWarning is set while the container keeps NET_RAW; the game
	// ranges are unknown meanwhile, so no header sign-in passes (SPEC 12.6).
	netRawWarning string
}

// networkStrikes is how many network refusals in a row raise the warning,
// so one odd look at a changing network does not.
const networkStrikes = 2

func newAdoptedMode(cfg Config) (*adoptedMode, error) {
	if cfg.GameContainer == "" {
		return nil, nil
	}
	if !filepath.IsAbs(cfg.GameDir) || filepath.Clean(cfg.GameDir) != cfg.GameDir {
		return nil, fmt.Errorf("the game folder %q must be a clean absolute path", cfg.GameDir)
	}
	return &adoptedMode{
		container: cfg.GameContainer, dir: cfg.GameDir, rconPW: cfg.RCONPassword,
		uid: cfg.GameUID, gid: cfg.GameGID,
	}, nil
}

// The image's own folders for the game's files (SPEC 4.11).
var imageDirs = []struct{ name, want string }{
	{"SAVES", gameMount + "/" + world.SavesDir},
	{"MODS", gameMount + "/" + world.ModsDir},
	{"CONFIG", gameMount + "/" + world.ConfigDir},
}

// spaceAgeMods are the mods the image's DLC_SPACE_AGE turns on or off.
var spaceAgeMods = []string{"elevated-rails", "quality", "recycler", "space-age"}

// Adopted mode refusals. The text is shown to the admin.
var (
	// ErrContainerRefused matches every refusal of the container's
	// settings; each carries its own text naming what to change, never a
	// value from the container's environment.
	ErrContainerRefused = errors.New("Foreman cannot use the game container as it is set up")           //nolint:staticcheck // shown to the user as written
	ErrNoGameContainer  = errors.New("Docker has no container with the name in FOREMAN_GAME_CONTAINER") //nolint:staticcheck // shown to the user as written
	ErrNotAdopted       = errors.New("the game folder still holds its own saves, mods and config. Adopt it on the Server page first")
	ErrAlreadyAdopted   = errors.New("the game folder is not one with its own saves, mods and config, so there is nothing to adopt")
	ErrNotPresented     = errors.New("the game folder presents a different world. Switch to this world instead")
	ErrGameFolderBroken = errors.New("the game folder is not laid out as Foreman expects")
	ErrSpaceAgeMismatch = errors.New("the world's Space Age setting does not match the game container's DLC_SPACE_AGE, which the image applies to the mod list at every start")
	ErrAdoptedImages    = errors.New("in adopted mode the container's owner chooses the game image, so Foreman neither checks nor downloads it")
	ErrNotAdoptedMode   = errors.New("adoption is only for adopted mode")
	ErrAdoptRCON        = errors.New("cannot reach the game over RCON with the password in the game folder's config/rconpw, so it cannot warn players or save first. Stop the game container yourself, then adopt again")
)

type containerRefusal string

func (e containerRefusal) Error() string        { return string(e) }
func (e containerRefusal) Is(target error) bool { return target == ErrContainerRefused }

func refuseContainer(format string, args ...any) error {
	return containerRefusal(fmt.Sprintf(format, args...))
}

// LayoutError is a game folder in the Broken state (SPEC 4.8). Path is
// relative to the game folder.
type LayoutError struct {
	Path, Reason string
}

func (e *LayoutError) Error() string {
	return fmt.Sprintf("the game folder is not laid out as Foreman expects at %s: %s. Fix it by hand, then try again", e.Path, e.Reason)
}

// Is makes every LayoutError match ErrGameFolderBroken.
func (e *LayoutError) Is(target error) bool { return target == ErrGameFolderBroken }

// adoptedViewNow returns the last look for Status.
func (c *Controller) adoptedViewNow() adoptedView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.aview
}

// Layout returns the game folder's shape at the last look in adopted mode
// (SPEC 4.8), and the zero State in managed mode. It makes no call.
func (c *Controller) Layout() gamedir.State {
	if c.adopted == nil {
		return gamedir.State{}
	}
	return c.adoptedViewNow().layout
}

// inspectAdopted reads the named container and keeps what Status shows.
func (c *Controller) inspectAdopted(ctx context.Context) (docker.Container, error) {
	cont, err := c.docker.InspectContainer(ctx, c.adopted.container)
	if errors.Is(err, docker.ErrNotFound) {
		return docker.Container{}, fmt.Errorf("%w (%s). Create it, or correct the name", ErrNoGameContainer, c.adopted.container)
	}
	if err != nil {
		return docker.Container{}, err
	}
	version, _ := cont.EnvValue("VERSION")
	if _, ok := parseVersion(version); !ok {
		version = ""
	}
	published := cont.NetworkMode == "host"
	if port, err := containerRCONPort(cont); err == nil && cont.PublishesTCP(port) {
		published = true
	}
	c.mu.Lock()
	c.aview.version, c.aview.rconPublished = version, published
	c.mu.Unlock()
	return cont, nil
}

// layout reads the game folder's shape and keeps it for Status.
func (c *Controller) layout() (gamedir.State, error) {
	st, err := gamedir.Inspect(c.adopted.dir)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		if c.aview.layoutErr != err.Error() {
			c.log.Warn("cannot read the game folder", "dir", c.adopted.dir, "error", err)
		}
		c.aview.layoutErr = err.Error()
		return gamedir.State{}, err
	}
	c.aview.layout, c.aview.layoutErr = st, ""
	return st, nil
}

// checkLayout refuses server control of world id unless the game folder
// presents it, or is Fresh with id the active world (SPEC 4.8).
func (c *Controller) checkLayout(ctx context.Context, id string) error {
	st, err := c.layout()
	if err != nil {
		return err
	}
	switch st.Kind {
	case gamedir.Broken:
		return &LayoutError{Path: st.Path, Reason: st.Reason}
	case gamedir.Unadopted:
		return ErrNotAdopted
	case gamedir.Ready:
		if st.ActiveID != id {
			return ErrNotPresented
		}
	case gamedir.Fresh:
		if c.switches != nil {
			active, found, err := c.switches.ActiveWorld(ctx)
			if err != nil {
				return err
			}
			if !found || active != id {
				return ErrNotPresented
			}
		}
	}
	return nil
}

// checkContainer inspects the container and refuses one whose settings
// would move the game's files away from the world the folder presents, or
// change them with no snapshot (SPEC 4.8, 4.11). It never changes the
// container (SPEC 4.7).
func (c *Controller) checkContainer(ctx context.Context) (docker.Container, error) {
	cont, err := c.inspectAdopted(ctx)
	if err != nil {
		return docker.Container{}, err
	}
	if err := c.checkContainerEnv(cont); err != nil {
		return docker.Container{}, err
	}
	if err := c.checkContainerMounts(ctx, cont); err != nil {
		return docker.Container{}, err
	}
	if err := c.checkAdoptedNetwork(ctx, cont); err != nil {
		return docker.Container{}, err
	}
	return cont, nil
}

// checkContainerEnv names the first variable to change. Values are never
// shown: the environment can hold secrets.
func (c *Controller) checkContainerEnv(cont docker.Container) error {
	for _, d := range imageDirs {
		if v, ok := cont.EnvValue(d.name); ok && v != d.want {
			return refuseContainer("the game container sets %s to a folder other than %s, which moves the game's files away from where switching worlds puts them. Remove %s from the container's environment", d.name, d.want, d.name)
		}
	}
	names := make([]string, 0, len(cont.Env))
	for _, kv := range cont.Env {
		name, _, _ := strings.Cut(kv, "=")
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if strings.HasPrefix(name, "SERVER_") && strings.HasSuffix(name, "_FILE") && len(name) > len("SERVER__FILE") {
			return refuseContainer("the game container sets %s, which moves a game settings file away from where switching worlds puts it. Remove %s from the container's environment", name, name)
		}
	}
	if v, ok := cont.EnvValue("LOAD_LATEST_SAVE"); ok && v != "true" {
		return refuseContainer("the game container sets LOAD_LATEST_SAVE to something other than true, so the game could load a save other than the one Foreman checked. Remove LOAD_LATEST_SAVE or set it to true")
	}
	if v, _ := cont.EnvValue("GENERATE_NEW_SAVE"); v == "true" {
		return refuseContainer("the game container sets GENERATE_NEW_SAVE to true, so the game could make a new map and overwrite saves. Remove GENERATE_NEW_SAVE from the container's environment")
	}
	if v, _ := cont.EnvValue("UPDATE_MODS_ON_START"); v == "true" {
		return refuseContainer("the game container sets UPDATE_MODS_ON_START to true, so the image would change a world's mods with no snapshot. Remove UPDATE_MODS_ON_START from the container's environment")
	}
	for _, id := range []struct {
		name, setting string
		want          int
	}{{"PUID", "FOREMAN_GAME_UID", c.adopted.uid}, {"PGID", "FOREMAN_GAME_GID", c.adopted.gid}} {
		if v, _ := cont.EnvValue(id.name); v != strconv.Itoa(id.want) {
			return refuseContainer("the game container's %s is not %d, the %s Foreman uses, so the image would change the owner of every file in the game folder. Set %s to %d", id.name, id.want, id.setting, id.name, id.want)
		}
	}
	if _, err := containerRCONPort(cont); err != nil {
		return err
	}
	return nil
}

// containerRCONPort is the container's RCON_PORT, 27015 when unset.
func containerRCONPort(cont docker.Container) (int, error) {
	v, ok := cont.EnvValue("RCON_PORT")
	if !ok || v == "" {
		v = RCONPort
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return 0, refuseContainer("the game container's RCON_PORT is not a port number, so Foreman cannot reach the game. Correct RCON_PORT")
	}
	return n, nil
}

// checkContainerMounts makes sure the container sees the game folder
// Foreman changes, and only it, at /factorio, and that Foreman can reach it
// (SPEC 4.4, 4.8).
func (c *Controller) checkContainerMounts(ctx context.Context, cont docker.Container) error {
	// A game that can reach Docker could take over the host (SPEC 4.6, 14).
	if cont.Privileged {
		return refuseContainer("the game container is privileged, so a game in it could take over the host. Remove privileged mode from the container's definition")
	}
	if len(cont.CapAdd) > 0 {
		return refuseContainer("the game container adds the Linux capability %s, which a game does not need and could use against the host. Remove cap_add from the container's definition", cont.CapAdd[0])
	}
	// NET_RAW lets a game send from any address on its network, which
	// would fool the trusted-header guard's per-address checks (SPEC 12.6).
	if !dropsNetRaw(cont.CapDrop) {
		return errKeepsNetRaw()
	}
	if cont.PidMode == "host" {
		return refuseContainer("the game container uses the host's process namespace (pid: host), so a game in it could see and signal the host's processes. Remove that setting from the container's definition")
	}
	// A host-network game has no RCON address of its own, and its requests
	// look like the host's, outside the trusted-header guard (SPEC 12.6).
	if cont.NetworkMode == "host" {
		return refuseContainer("the game container uses the host's network (network_mode: host), so Foreman cannot tell a game from the host. Put the game on a Docker network it shares with Foreman")
	}
	if len(cont.Devices) > 0 {
		return refuseContainer("the game container maps host devices in (devices), which a game does not need and could use against the host. Remove devices from the container's definition")
	}
	for _, opt := range cont.SecurityOpt {
		if unconfinedOpts[opt] {
			return refuseContainer("the game container turns off a host protection with security_opt %s, so a game in it would be less confined. Remove it from the container's definition", opt)
		}
	}
	for _, ns := range []struct{ setting, mode string }{
		{"userns_mode", cont.UsernsMode}, {"ipc", cont.IpcMode}, {"uts", cont.UTSMode},
	} {
		if ns.mode == "host" {
			return refuseContainer("the game container shares a host namespace (%s: host), so a game in it would be less separated from the host. Remove that setting from the container's definition", ns.setting)
		}
	}
	src, found := "", false
	for _, m := range cont.Mounts {
		dest := path.Clean(m.Destination)
		if mountsDockerSocket(m) {
			return refuseContainer("the game container mounts the Docker socket at %s, so a game in it could control Docker. Remove that mount", dest)
		}
		switch {
		case dest == gameMount:
			src, found = m.Source, true
		case strings.HasPrefix(dest, gameMount+"/"):
			return refuseContainer("the game container mounts something at %s, inside /factorio, so the game would not see the world Foreman presents there. Remove that mount", dest)
		}
	}
	if !found {
		return refuseContainer("the game container has no mount at /factorio, so it does not use the game folder Foreman switches. Mount the game folder at /factorio")
	}
	self, inContainer, err := c.selfContainer(ctx)
	if err != nil {
		return err
	}
	if !inContainer {
		if filepath.Clean(src) != c.adopted.dir {
			return refuseContainer("the folder the game container has at /factorio is not FOREMAN_GAME_DIR, so Foreman would switch a folder the game does not use. Set FOREMAN_GAME_DIR to the folder mounted at /factorio")
		}
		return nil
	}
	own, ok := "", false
	for _, m := range self.Mounts {
		if path.Clean(m.Destination) == c.adopted.dir {
			own, ok = m.Source, true
		}
	}
	if !ok {
		return refuseContainer("FOREMAN_GAME_DIR is not a mount of the Foreman container, so Foreman cannot tell that it is the game's folder. Mount the folder the game has at /factorio at FOREMAN_GAME_DIR too")
	}
	if own != src {
		return refuseContainer("the folder mounted at FOREMAN_GAME_DIR in the Foreman container is not the one the game container has at /factorio. Mount the same folder in both")
	}
	return nil
}

// errKeepsNetRaw is the refusal of a game container that keeps NET_RAW.
func errKeepsNetRaw() error {
	return refuseContainer("the game container keeps the NET_RAW capability, so a game in it could pretend to be another machine on the network. Add `cap_drop: [NET_RAW]` to the container's definition")
}

// noteNetRaw raises or clears the NET_RAW warning from one inspect of the
// adopted container. It is a static setting, so one look is enough. It
// reports whether the warning changed.
func (c *Controller) noteNetRaw(cont docker.Container) bool {
	warning := ""
	if !dropsNetRaw(cont.CapDrop) {
		warning = strings.TrimSuffix(errKeepsNetRaw().Error(), ".") + "."
	}
	c.mu.Lock()
	changed := c.aview.netRawWarning != warning
	c.aview.netRawWarning = warning
	c.mu.Unlock()
	if changed && warning != "" {
		c.log.Error("the game container keeps NET_RAW, so trusted-header sign-in is refused", "reason", warning)
	}
	return changed
}

// checkAdoptedNetwork applies the game network rules of managed mode to the
// network Foreman shares with the adopted container (SPEC 4.4). Outside a
// container there is no shared network to check, as in managed mode.
func (c *Controller) checkAdoptedNetwork(ctx context.Context, cont docker.Container) error {
	self, inContainer, err := c.selfContainer(ctx)
	if err != nil || !inContainer {
		return err
	}
	network, err := c.adoptedNetwork(self, cont)
	if err != nil {
		return err
	}
	return c.checkSharedNetwork(ctx, self, network, func(s docker.ContainerSummary) bool { return s.ID == cont.ID })
}

// noteNetworkCheck keeps the result of the monitor's network check for
// Status (SPEC 4.4). A refusal raises the warning only when the check
// before it refused too; a check that could not be made leaves the count
// and the warning as they were. It reports whether the warning changed.
func (c *Controller) noteNetworkCheck(err error) bool {
	refused := err != nil && !errors.Is(err, errNetworkUnchecked) &&
		(errors.Is(err, ErrNetwork) || errors.Is(err, ErrContainerRefused))
	c.mu.Lock()
	v := &c.aview
	before := v.networkWarning
	logUnchecked := false
	switch {
	case err == nil:
		v.networkRefusals, v.networkWarning, v.networkUnchecked = 0, "", ""
		v.networkCleanAt = c.now()
	case refused:
		v.networkUnchecked = ""
		v.networkRefusals++
		if v.networkRefusals >= networkStrikes {
			// Shown as a sentence on the Server page.
			v.networkWarning = strings.TrimSuffix(err.Error(), ".") + "."
		}
	default:
		logUnchecked = v.networkUnchecked != err.Error()
		v.networkUnchecked = err.Error()
	}
	after := v.networkWarning
	c.mu.Unlock()
	if logUnchecked {
		c.log.Warn("could not check the game container's network", "error", err)
	}
	if after != before && after != "" {
		c.log.Error("the game container's network is refused", "reason", after)
	}
	return after != before
}

// unconfinedOpts are the security_opt entries that turn off AppArmor,
// seccomp or SELinux labels for the container (SPEC 4.6, 14).
var unconfinedOpts = map[string]bool{
	"apparmor=unconfined": true, "apparmor:unconfined": true,
	"seccomp=unconfined": true, "seccomp:unconfined": true,
	"label=disable": true, "label:disable": true,
}

// mountsDockerSocket reports whether a mount gives the container the Docker
// socket: a docker.sock at either end, or a host folder that holds it.
func mountsDockerSocket(m docker.Mount) bool {
	for _, p := range []string{path.Clean(m.Source), path.Clean(m.Destination)} {
		if path.Base(p) == "docker.sock" {
			return true
		}
	}
	// A folder that holds /run or /var/run holds the socket too.
	src := path.Clean(m.Source)
	return src == "/" || src == "/var" || src == "/run" || src == "/var/run"
}

// adoptedNetwork names the network Foreman shares with the game: the only
// one, or the one FOREMAN_GAME_NETWORK names, which is required when they
// share several (SPEC 4.4).
func (c *Controller) adoptedNetwork(self, cont docker.Container) (string, error) {
	var shared []string
	for name := range cont.Networks {
		if _, ok := self.Networks[name]; ok {
			shared = append(shared, name)
		}
	}
	slices.Sort(shared)
	switch {
	case len(shared) == 0:
		return "", refuseContainer("the Foreman container shares no Docker network with the game container, so Foreman cannot reach the game's RCON port. Put both containers on one network")
	case c.network != "":
		if !slices.Contains(shared, c.network) {
			return "", refuseNetwork("FOREMAN_GAME_NETWORK names the %q network, but the Foreman and game containers do not share it. Name a network both are on", c.network)
		}
		return c.network, nil
	case len(shared) > 1:
		return "", refuseNetwork("the Foreman container shares several Docker networks with the game container (%s), so Foreman cannot tell which one to check and reach RCON on. Set FOREMAN_GAME_NETWORK to one of them", strings.Join(shared, ", "))
	}
	return shared[0], nil
}

// adoptedRCONAddress is the container's address on the network it shares
// with Foreman, or on any network outside a container, at its RCON_PORT.
func (c *Controller) adoptedRCONAddress(ctx context.Context, cont docker.Container) (string, error) {
	port, err := containerRCONPort(cont)
	if err != nil {
		return "", err
	}
	self, inContainer, err := c.selfContainer(ctx)
	if err != nil {
		return "", err
	}
	ip := containerIP(cont)
	if inContainer {
		network, err := c.adoptedNetwork(self, cont)
		if err != nil {
			return "", err
		}
		ip = cont.Networks[network].IPAddress
	}
	if ip == "" {
		return "", nil
	}
	return net.JoinHostPort(ip, strconv.Itoa(port)), nil
}

// containerSpaceAge reads DLC_SPACE_AGE as the image does: unset or "true"
// turns the Space Age mods on, "false" off, and anything else lists the
// ones to turn on. ok is false when it turns on only some of them.
func containerSpaceAge(cont docker.Container) (on, ok bool) {
	v, set := cont.EnvValue("DLC_SPACE_AGE")
	switch {
	case !set || v == "true":
		return true, true
	case v == "false":
		return false, true
	}
	fields := strings.Fields(v)
	n := 0
	for _, m := range spaceAgeMods {
		if slices.Contains(fields, m) {
			n++
		}
	}
	switch n {
	case len(spaceAgeMods):
		return true, true
	case 0:
		return false, true
	}
	return false, false
}

// checkSpaceAge refuses a 2.x world whose Space Age flag the container's
// DLC_SPACE_AGE would overrule (SPEC 7.4). Factorio 1.1 has no Space Age.
func checkSpaceAge(cont docker.Container, gameVersion string, spaceAge bool) error {
	if !strings.HasPrefix(gameVersion, "2.") {
		return nil
	}
	on, ok := containerSpaceAge(cont)
	switch {
	case !ok:
		return fmt.Errorf("%w: it turns on only some of the Space Age mods. Set DLC_SPACE_AGE to true or false", ErrSpaceAgeMismatch)
	case on && !spaceAge:
		return fmt.Errorf("%w: this world has Space Age off and the container turns it on. Use a world with Space Age on, or set DLC_SPACE_AGE to false in the container's definition", ErrSpaceAgeMismatch)
	case !on && spaceAge:
		return fmt.Errorf("%w: this world has Space Age on and the container turns it off. Use a world with Space Age off, or set DLC_SPACE_AGE to true in the container's definition", ErrSpaceAgeMismatch)
	}
	return nil
}

// checkAdoptedStart runs every adopted check a start of id makes, inspect
// only, while the game folder presents presented: the folder, the
// container and the Space Age setting (SPEC 4.8, 4.11, 7.4).
func (c *Controller) checkAdoptedStart(ctx context.Context, id, presented string) (docker.Container, error) {
	if err := c.checkLayout(ctx, presented); err != nil {
		return docker.Container{}, err
	}
	cont, err := c.checkContainer(ctx)
	if err != nil {
		return docker.Container{}, err
	}
	meta, err := c.readMeta(id)
	if err != nil {
		return docker.Container{}, err
	}
	if err := checkSpaceAge(cont, meta.GameVersion, meta.SpaceAge); err != nil {
		return docker.Container{}, err
	}
	return cont, nil
}

// checkSaveAgainstContainer refuses a save newer than the container's
// VERSION, as ErrSaveNewerThanImage does in managed mode. An unknown
// version skips the check (SPEC 4.9).
func (c *Controller) checkSaveAgainstContainer(save savefile.Version) error {
	running, ok := parseVersion(c.adoptedViewNow().version)
	if !ok {
		return nil
	}
	saved := [3]uint16{save.Major, save.Minor, save.Patch}
	if slices.Compare(saved[:], running[:]) > 0 {
		return fmt.Errorf("%w: the save is from Factorio %s, and the game container runs Factorio %d.%d.%d",
			ErrSaveNewerThanImage, save, running[0], running[1], running[2])
	}
	return nil
}

// VersionWarning returns, in adopted mode, the warning of SPEC 4.9 for a
// world whose save that will load was made by a different Factorio from
// the one the container runs. It is "" when they agree, when either is
// unknown, and in managed mode.
func (c *Controller) VersionWarning(id string) (string, error) {
	if c.adopted == nil {
		return "", nil
	}
	running := c.adoptedViewNow().version
	if running == "" {
		return "", nil
	}
	check, err := c.worlds.PreStartCheck(id)
	if err != nil {
		return "", err
	}
	if check.SaveVersion == (savefile.Version{}) || check.SaveVersion.String() == running {
		return "", nil
	}
	return fmt.Sprintf("The save that will load was made by Factorio %s, but the game container runs Factorio %s.", check.SaveVersion, running), nil
}

// adoptedContainer is ourContainer in adopted mode: the named container is
// the world's only while the game folder presents that world.
func (c *Controller) adoptedContainer(ctx context.Context, id string) (docker.Container, bool, error) {
	st, err := c.layout()
	if err != nil {
		return docker.Container{}, false, err
	}
	if st.Kind == gamedir.Broken {
		return docker.Container{}, false, &LayoutError{Path: st.Path, Reason: st.Reason}
	}
	if st.Kind != gamedir.Ready || st.ActiveID != id {
		return docker.Container{}, false, nil
	}
	cont, err := c.inspectAdopted(ctx)
	if err != nil {
		return docker.Container{}, false, err
	}
	return cont, true, nil
}

// prepareAdopted readies the container for a start of world id. The checks
// ran with the world's (checkLocked); they run again here because the
// container may have been recreated meanwhile. A Fresh folder gets its
// links now (SPEC 4.8).
func (c *Controller) prepareAdopted(ctx context.Context, id string) (launch, error) {
	if err := c.checkLayout(ctx, id); err != nil {
		return launch{}, err
	}
	cont, err := c.checkContainer(ctx)
	if err != nil {
		return launch{}, err
	}
	switch cont.State.Status {
	case "running", "restarting":
		return launch{}, ErrAlreadyRunning
	case "paused":
		return launch{}, ErrPaused
	}
	st, err := c.layout()
	if err != nil {
		return launch{}, err
	}
	if st.Kind == gamedir.Fresh {
		if err := gamedir.Present(c.adopted.dir, id); err != nil {
			return launch{}, err
		}
		if _, err := c.layout(); err != nil {
			return launch{}, err
		}
	}
	if err := c.writeEnvRCONPassword(id); err != nil {
		return launch{}, err
	}
	// The password is read for the world presented now (SPEC 4.10).
	rconPW, secrets, err := c.secrets(id)
	if err != nil {
		return launch{}, err
	}
	meta, err := c.readMeta(id)
	if err != nil {
		return launch{}, err
	}
	save, err := c.worlds.PrepareLoad(id)
	if err != nil {
		return launch{}, err
	}
	return launch{
		cid: cont.ID, status: cont.State.Status, restarts: cont.RestartCount,
		save: save, meta: meta, image: cont.Image, rconPW: rconPW, secrets: secrets,
	}, nil
}

// rconTempFile is where writeEnvRCONPassword writes before the rename.
const rconTempFile = world.ConfigDir + "/" + world.PrefixPartial + "rconpw-foreman"

// writeEnvRCONPassword writes FOREMAN_RCON_PASSWORD, when set, into the
// world's config/rconpw, since the image's entrypoint always gives the game
// the password in that file (SPEC 4.10). The file is 0640 from the moment it
// exists and appears complete (SPEC 4.5). The caller holds the world's lock.
func (c *Controller) writeEnvRCONPassword(id string) error {
	env, ok := c.envRCONPassword()
	if !ok {
		return nil
	}
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := world.Mkdir(root, world.ConfigDir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := root.Remove(rconTempFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := root.OpenFile(rconTempFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("write FOREMAN_RCON_PASSWORD into the world: %w", err)
	}
	// The umask may have narrowed it; the game's group still needs to read.
	err = f.Chmod(0o640)
	if err == nil {
		_, err = f.WriteString(env.Reveal() + "\n")
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = world.Commit(root, rconTempFile, world.RCONPasswordFile)
	}
	if err != nil {
		_ = root.Remove(rconTempFile)
		return fmt.Errorf("write FOREMAN_RCON_PASSWORD into the world: %w", err)
	}
	return nil
}

// tickAdopted is the monitor's look in adopted mode: the named container
// belongs to the world the game folder presents, and to no world while the
// folder is Fresh, Unadopted or Broken (SPEC 4.7, 4.8).
func (c *Controller) tickAdopted(ctx context.Context) {
	cont, err := c.inspectAdopted(ctx)
	if err == nil {
		c.refreshAdoptedSubnets(ctx, cont)
		netRawChanged := c.noteNetRaw(cont)
		// The network can change while the game runs; this catches it at
		// the next look rather than preventing it (SPEC 4.4).
		// A refusal does not stop the monitor watching the game, so a
		// crash is still seen and Stop still works (SPEC 4.4, 7.1).
		nerr := c.checkAdoptedNetwork(ctx, cont)
		if ctx.Err() != nil {
			return
		}
		if c.noteNetworkCheck(nerr) || netRawChanged {
			defer func() {
				for _, w := range c.allRuns() {
					c.publish(w)
				}
			}()
		}
	}
	if ctx.Err() != nil {
		return
	}
	st, lerr := c.layout()
	c.mu.Lock()
	c.dockerErr = err
	if err == nil {
		c.listed = true
	}
	c.mu.Unlock()
	if err != nil {
		for _, w := range c.allRuns() {
			c.publish(w)
		}
		return
	}
	presented := ""
	if lerr == nil && st.Kind == gamedir.Ready {
		presented = st.ActiveID
		c.world(presented)
	}
	for _, w := range c.allRuns() {
		if c.busy(w) {
			continue
		}
		if w.id != presented {
			c.observe(w, nil)
			continue
		}
		c.observeInspect(ctx, w, cont, nil)
	}
}

// dropsNetRaw reports whether a CapDrop list removes NET_RAW, by name or
// with ALL; Docker accepts either case and an optional CAP_ prefix.
func dropsNetRaw(capDrop []string) bool {
	for _, c := range capDrop {
		c = strings.TrimPrefix(strings.ToUpper(c), "CAP_")
		if c == "NET_RAW" || c == "ALL" {
			return true
		}
	}
	return false
}
