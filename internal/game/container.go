package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Labels on every game container (SPEC 4.3). Foreman finds its containers by
// them after a restart, with no database row.
const (
	labelPrefix  = "io.github.toddawhittaker.foreman."
	LabelWorld   = labelPrefix + "world"   // the world id
	LabelInstall = labelPrefix + "install" // the host data path, so two installs never share a container
	// LabelWhitelist is "1" on a container started with the whitelist
	// enforced, so the game keeps the whitelist file (SPEC 8.3).
	LabelWhitelist = labelPrefix + "whitelist"
)

// The container's fixed parts (SPEC 4.3 to 4.6).
const (
	gameMount      = "/factorio"
	gameBinary     = "/opt/factorio/bin/x64/factorio"
	gamePort       = "34197/udp"
	gameHostPort   = "34197"
	RCONPort       = "27015"
	restartPolicy  = "unless-stopped"
	serverSettings = world.ConfigDir + "/" + world.ServerSettingsFile
	maxSettings    = 1 << 20
)

// configPath is a config file's path inside the container.
func configPath(file string) string { return gameMount + "/" + world.ConfigDir + "/" + file }

// passIfPresent is a script line that adds flag with the config file to the
// game's arguments only when the file exists.
func passIfPresent(flag, file string) string {
	return `if [ -f ` + configPath(file) + ` ]; then set -- "$@" ` + flag + ` ` + configPath(file) + `; fi`
}

// startScript runs the game with Foreman's flags. It reads the RCON
// password from the world's file inside the container, so the password is
// never in the container's settings (SPEC 4.5). The admin and ban lists are
// always passed: the game makes them when missing, so /promote and /ban
// made in game persist in the world's folder (SPEC 5.1). Server settings
// are passed when the world has them.
//
// The whitelist is passed only when useWhitelist is set, and then with
// --use-server-whitelist too: the game refuses --server-whitelist alone, and
// an in-game /whitelist enable is lost on restart, so enforcement comes from
// the world's switch (SPEC 8.3). Without the switch the whitelist file is
// left alone for when it is turned on.
//
// A world made by Generate has no save. When the container is told so
// (gameenv.NewMap) and the folder holds no save, the script first makes
// one with --create (SPEC 4.3, 5.6). The check is in the script, not in
// Foreman, so Docker's own restart of a container that still carries the
// variable can never overwrite a save. A failed create exits the container,
// which is how a failed start is reported. Values reach the script by
// environment variables, never through its text.
func startScript(useWhitelist bool) string {
	server := `set -- --start-server-load-latest --mod-directory ` + gameMount + "/" + world.ModsDir +
		` --rcon-port ` + RCONPort + ` --rcon-password "$pw"` +
		` --server-adminlist ` + configPath(world.ServerAdminListFile) +
		` --server-banlist ` + configPath(world.ServerBanListFile)
	if useWhitelist {
		server += ` --use-server-whitelist --server-whitelist ` + configPath(world.ServerWhiteListFile)
	}
	return strings.Join([]string{
		`pw=$(cat ` + gameMount + "/" + world.RCONPasswordFile + `) || exit 1`,
		`if [ -n "$` + gameenv.NewMap + `" ]; then`,
		`have=`,
		`for f in ` + gameMount + "/" + world.SavesDir + `/*.zip; do [ -f "$f" ] && have=1; done`,
		`if [ -z "$have" ]; then`,
		`set -- --create ` + gameMount + "/" + world.SavesDir + `/"$` + gameenv.WorldID + `".zip --mod-directory ` + gameMount + "/" + world.ModsDir,
		`if [ -n "$` + gameenv.Preset + `" ]; then set -- "$@" --preset "$` + gameenv.Preset + `"; fi`,
		`if [ -n "$` + gameenv.Seed + `" ]; then set -- "$@" --map-gen-seed "$` + gameenv.Seed + `"; fi`,
		passIfPresent("--map-gen-settings", world.MapGenSettingsFile),
		passIfPresent("--map-settings", world.MapSettingsFile),
		gameBinary + ` "$@" || exit 1`,
		`fi`,
		`fi`,
		server,
		passIfPresent("--server-settings", world.ServerSettingsFile),
		`exec ` + gameBinary + ` "$@"`,
	}, "\n")
}

// ContainerName is the name of a world's game container (SPEC 4.3).
func ContainerName(id string) string { return "foreman-" + id }

// containerSpec builds the game container for a world (SPEC 4.3 to 4.6).
// Nothing in it comes from what an admin types, apart from the world id,
// which world.ValidID has checked.
//
// meta's new_map, when set, adds the create step's environment (SPEC 4.3);
// its whitelist switch decides the whitelist flags (SPEC 8.3).
func (c *Controller) containerSpec(id, image, network string, meta world.Meta) docker.ContainerSpec {
	grace := StopGraceSeconds
	labels := map[string]string{LabelWorld: id, LabelInstall: c.hostData}
	if meta.UseWhitelist {
		labels[LabelWhitelist] = "1"
	}
	return docker.ContainerSpec{
		Image:        image,
		Labels:       labels,
		Env:          createEnv(id, meta.NewMap),
		Entrypoint:   []string{"/bin/sh"},
		Cmd:          []string{"-c", startScript(meta.UseWhitelist)},
		User:         c.user,
		OpenStdin:    true,
		StopTimeout:  &grace,
		ExposedPorts: map[string]struct{}{gamePort: {}},
		HostConfig: docker.HostConfig{
			Binds:         []string{filepath.Join(datadir.Worlds(c.hostData), id) + ":" + gameMount},
			PortBindings:  map[string][]docker.PortBinding{gamePort: {{HostPort: gameHostPort}}},
			CapDrop:       []string{"ALL"},
			SecurityOpt:   []string{"no-new-privileges"},
			RestartPolicy: docker.RestartPolicy{Name: restartPolicy},
			LogConfig: docker.LogConfig{Type: "json-file", Config: map[string]string{
				"max-size": "10m", "max-file": "3",
			}},
			NetworkMode: network,
		},
	}
}

// createEnv is the environment that makes the start script create the
// world's map, or nil for a world with a save (SPEC 4.3). Only the variables
// that are set are given. The preset and the seed were checked when the
// world was made.
func createEnv(id string, newMap *world.NewMap) []string {
	if newMap == nil {
		return nil
	}
	env := []string{gameenv.NewMap + "=1", gameenv.WorldID + "=" + id}
	if newMap.Preset != "" {
		env = append(env, gameenv.Preset+"="+newMap.Preset)
	}
	if newMap.Seed != nil {
		env = append(env, gameenv.Seed+"="+strconv.FormatUint(uint64(*newMap.Seed), 10))
	}
	return env
}

// ourContainer reads the world's container. found is false when there is
// none. A container with the name that Foreman did not make for this world
// and this install is ErrForeign (SPEC 4.3).
func (c *Controller) ourContainer(ctx context.Context, id string) (cont docker.Container, found bool, err error) {
	cont, err = c.docker.InspectContainer(ctx, ContainerName(id))
	if errors.Is(err, docker.ErrNotFound) {
		return docker.Container{}, false, nil
	}
	if err != nil {
		return docker.Container{}, false, err
	}
	if cont.Labels[LabelWorld] != id || cont.Labels[LabelInstall] != c.hostData {
		return docker.Container{}, false, ErrForeign
	}
	return cont, true, nil
}

// checkNetworkName refuses a configured network that would break SPEC 4.4
// or 14.4.
func checkNetworkName(n string) error {
	if n == "host" || n == "none" || strings.HasPrefix(n, "container:") {
		return fmt.Errorf("FOREMAN_GAME_NETWORK cannot be %q: the game needs its own place on a Docker network shared with Foreman", n)
	}
	return nil
}

// ErrNetwork matches every refusal of the game network. Each carries its
// own text, which says what to change.
var ErrNetwork = errors.New("the game network cannot be used")

// errNetworkUnchecked matches a network check that could not be made, such
// as a failed DNS lookup. It refuses an action as ErrNetwork does, but the
// monitor does not count it as a refusal.
var errNetworkUnchecked = errors.New("the game network could not be checked")

// The kinds of NetworkError.
const (
	NetworkUnset        = "unset"         // FOREMAN_GAME_NETWORK is not set
	NetworkNotJoined    = "not-joined"    // Foreman is not on Network
	NetworkNoRange      = "no-range"      // Network's address range cannot be read
	NetworkProxyOverlap = "proxy-overlap" // Network's Subnet overlaps Proxies
	NetworkShared       = "shared"        // Members are on Network too
	NetworkHostLookup   = "host-lookup"   // Host cannot be looked up
	NetworkHostAddress  = "host-address"  // Host gave Address, which cannot be read
	NetworkHostOnGame   = "host-on-game"  // Host's Address is on Network's Subnet
	NetworkHostOutside  = "host-outside"  // Host's Address is on none of Foreman's networks
	NetworkSelfFile     = "self-file"     // File says Foreman is in a container Docker does not know
	NetworkSelfTCP      = "self-tcp"      // a tcp:// DOCKER_HOST says so
)

// NetworkError is a refusal of the game network (ErrNetwork), with the
// facts its text is made from, so a page can word it (SPEC 22.3).
type NetworkError struct {
	Kind                                          string
	Network, Subnet, Proxies, Host, Address, File string
	// Members are the other containers on a shared network.
	Members []NetworkMember
	// unchecked marks a check that could not be made (errNetworkUnchecked).
	unchecked bool
}

// NetworkMember is a container other than Foreman and its games on the
// game network. State is Docker's, or "stopped"; Install is true for a game
// of another Foreman installation.
type NetworkMember struct {
	Name, State string
	Install     bool
}

// Stopped reports whether the member can be removed rather than
// disconnected.
func (m NetworkMember) Stopped() bool { return m.State == "stopped" }

func (e *NetworkError) Is(target error) bool {
	return target == ErrNetwork || e.unchecked && target == errNetworkUnchecked
}

// Removable and Keep split the members into those to remove and those to
// disconnect.
func (e *NetworkError) Removable() []string { return e.members(true) }
func (e *NetworkError) Keep() []string      { return e.members(false) }

func (e *NetworkError) members(stopped bool) []string {
	var out []string
	for _, m := range e.Members {
		if m.Stopped() == stopped {
			out = append(out, m.Name)
		}
	}
	return out
}

func (e *NetworkError) Error() string {
	switch e.Kind {
	case NetworkUnset:
		return "FOREMAN_GAME_NETWORK must be set when Foreman runs in a container: it names the Docker network the game joins. Choose a network Foreman is on that the Docker socket proxy is not on"
	case NetworkNotJoined:
		return fmt.Sprintf("FOREMAN_GAME_NETWORK names the %q network, but the Foreman container is not on it, so Foreman could not reach the game. Connect Foreman to that network, or name one it is on", e.Network)
	case NetworkNoRange:
		return fmt.Sprintf("cannot read the address range of the %q network from Docker, so Foreman cannot check that a game on it stays away from the Docker socket proxy and FOREMAN_TRUSTED_PROXIES. Check that Docker answers, then try again", e.Network)
	case NetworkProxyOverlap:
		return fmt.Sprintf("the game network %q (%s) overlaps FOREMAN_TRUSTED_PROXIES (%s), so a game could pose as the proxy that signs users in. Use a game network outside those ranges, or narrow FOREMAN_TRUSTED_PROXIES", e.Network, e.Subnet, e.Proxies)
	case NetworkShared:
		others := make([]string, len(e.Members))
		for i, m := range e.Members {
			kind := "container that is not a Foreman game"
			if m.Install {
				kind = "game from another Foreman installation"
			}
			others[i] = fmt.Sprintf("%s (a %s %s)", m.Name, m.State, kind)
		}
		fixes := ""
		if removable := e.Removable(); len(removable) > 0 {
			fixes += fmt.Sprintf(" Remove the stopped ones you no longer need with %q.", "docker rm "+strings.Join(removable, " "))
		}
		if keep := e.Keep(); len(keep) > 0 {
			cmds := make([]string, len(keep))
			for i, n := range keep {
				cmds[i] = fmt.Sprintf("%q", "docker network disconnect "+e.Network+" "+n)
			}
			fixes += " Disconnect the others with " + strings.Join(cmds, " and ") + "."
		}
		return fmt.Sprintf("Foreman will not start a game on the network %q while other containers are on it, because a game could reach them. On it now: %s.%s Then try again. If they must stay, give Foreman a game network of its own with FOREMAN_GAME_NETWORK", e.Network, strings.Join(others, ", "), fixes)
	case NetworkHostLookup:
		return fmt.Sprintf("DOCKER_HOST uses tcp://, but Foreman cannot look up the address of %q, so it cannot check that a game on the %q network could not reach the Docker socket proxy. Check DOCKER_HOST, then try again", e.Host, e.Network)
	case NetworkHostAddress:
		return fmt.Sprintf("DOCKER_HOST uses tcp://, but %q gave the address %q, which Foreman cannot read, so it cannot check that a game could not reach the Docker socket proxy. Check DOCKER_HOST, then try again", e.Host, e.Address)
	case NetworkHostOnGame:
		return fmt.Sprintf("DOCKER_HOST reaches the Docker socket proxy at %s, which is on the game network %q (%s), so a game could reach it. Put the game on a network of its own that Foreman also joins and the proxy does not", e.Address, e.Network, e.Subnet)
	case NetworkHostOutside:
		return fmt.Sprintf("DOCKER_HOST reaches Docker at %s, which is not on any of Foreman's networks other than the game network %q, so a game might reach it through that network's gateway. Reach Docker over a network the game is not on, such as a socket proxy on a network Foreman shares with it", e.Address, e.Network)
	case NetworkSelfFile:
		return fmt.Sprintf("Foreman seems to run in a container (%s exists), but Docker has no container with Foreman's ID or hostname, so Foreman cannot tell which networks it is on. Keep the hostname Docker gave the Foreman container, and make sure DOCKER_HOST reaches the Docker that runs Foreman", e.File)
	case NetworkSelfTCP:
		return "Foreman seems to run in a container (DOCKER_HOST uses tcp://), but Docker has no container with Foreman's ID or hostname, so Foreman cannot tell which networks it is on. Keep the hostname Docker gave the Foreman container, and make sure DOCKER_HOST reaches the Docker that runs Foreman"
	}
	return ErrNetwork.Error()
}

// gameNetwork returns the Docker network the game joins (SPEC 4.4).
// Outside a container it is FOREMAN_GAME_NETWORK, or Docker's default
// bridge. Inside one FOREMAN_GAME_NETWORK is required, and refused when a
// game on it could reach the Docker socket proxy or pose as a trusted
// proxy (SPEC 12.6, 14.4).
func (c *Controller) gameNetwork(ctx context.Context) (string, error) {
	self, inContainer, err := c.selfContainer(ctx)
	if err != nil {
		return "", err
	}
	if !inContainer {
		if c.network != "" {
			return c.network, nil
		}
		return "bridge", nil
	}
	return c.checkGameNetwork(ctx, self)
}

// checkGameNetwork checks FOREMAN_GAME_NETWORK against Foreman's own
// container, self.
func (c *Controller) checkGameNetwork(ctx context.Context, self docker.Container) (string, error) {
	if c.network == "" {
		return "", &NetworkError{Kind: NetworkUnset}
	}
	if _, ok := self.Networks[c.network]; !ok {
		return "", &NetworkError{Kind: NetworkNotJoined, Network: c.network}
	}
	isGame := func(s docker.ContainerSummary) bool {
		return s.Labels[LabelInstall] == c.hostData && s.Labels[LabelWorld] != ""
	}
	if err := c.checkSharedNetwork(ctx, self, c.network, isGame); err != nil {
		return "", err
	}
	return c.network, nil
}

// checkSharedNetwork refuses a network Foreman shares with the game when a
// game on it could reach the Docker socket proxy or pose as a trusted proxy,
// or when a container other than Foreman and those isGame accepts is on it
// (SPEC 4.4, 12.6, 14.4). self is on the network.
func (c *Controller) checkSharedNetwork(ctx context.Context, self docker.Container, network string, isGame func(docker.ContainerSummary) bool) error {
	if c.dockerTCP || len(c.proxies) > 0 {
		subnets, err := networkSubnets(self.Networks[network])
		if err != nil {
			return &NetworkError{Kind: NetworkNoRange, Network: network}
		}
		if c.dockerTCP {
			if err := c.checkDockerHost(ctx, self, network, subnets); err != nil {
				return err
			}
		}
		for _, subnet := range subnets {
			for _, p := range c.proxies {
				if p.Overlaps(subnet) {
					return &NetworkError{Kind: NetworkProxyOverlap, Network: network, Subnet: subnet.String(), Proxies: p.String()}
				}
			}
		}
	}
	return c.checkNetworkMembers(ctx, self, network, isGame)
}

// checkNetworkMembers refuses when a container other than Foreman and the
// games isGame accepts is on the game network: the network is shared only
// by Foreman and the game (SPEC 4.4).
func (c *Controller) checkNetworkMembers(ctx context.Context, self docker.Container, network string, isGame func(docker.ContainerSummary) bool) error {
	list, err := c.docker.ListContainersOnNetwork(ctx, network)
	if err != nil {
		return err
	}
	var others []NetworkMember
	for _, s := range list {
		if s.ID == self.ID || isGame(s) {
			continue
		}
		name := s.ID
		if len(s.Names) > 0 {
			name = strings.TrimPrefix(s.Names[0], "/")
		}
		state := s.State
		if slices.Contains([]string{"exited", "created", "dead"}, s.State) {
			state = "stopped"
		}
		// isGame refused it, so a world label means another install's game.
		others = append(others, NetworkMember{Name: name, State: state, Install: s.Labels[LabelWorld] != ""})
	}
	if len(others) == 0 {
		return nil
	}
	return &NetworkError{Kind: NetworkShared, Network: network, Members: others}
}

// checkDockerHost accepts a tcp:// DOCKER_HOST only when every address it
// resolves to is inside one of Foreman's networks other than the game
// network (SPEC 14.4). An address elsewhere (the host, the LAN, loopback)
// may be reachable from the game through its network's gateway. A proxy
// container that also joined the game network is caught here by its
// address when DOCKER_HOST resolves to it, and by checkNetworkMembers
// whatever address DOCKER_HOST names.
func (c *Controller) checkDockerHost(ctx context.Context, self docker.Container, network string, subnets []netip.Prefix) error {
	var addrs []string
	err := errors.New("DOCKER_HOST names no host")
	if c.dockerHost != "" {
		addrs, err = c.lookupHost(ctx, c.dockerHost)
	}
	if err != nil || len(addrs) == 0 {
		return &NetworkError{Kind: NetworkHostLookup, Host: c.dockerHost, Network: network, unchecked: true}
	}
	ips := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		ip, err := netip.ParseAddr(a)
		if err != nil {
			return &NetworkError{Kind: NetworkHostAddress, Host: c.dockerHost, Address: a}
		}
		ip = ip.WithZone("").Unmap()
		for _, subnet := range subnets {
			if subnet.Contains(ip) {
				return &NetworkError{Kind: NetworkHostOnGame, Address: ip.String(), Network: network, Subnet: subnet.String()}
			}
		}
		ips = append(ips, ip)
	}
	for _, ip := range ips {
		if !onOtherNetwork(self, network, ip) {
			return &NetworkError{Kind: NetworkHostOutside, Address: ip.String(), Network: network}
		}
	}
	return nil
}

// onOtherNetwork reports whether ip lies in the range of one of self's
// networks other than game. A network whose range cannot be read counts
// for nothing.
func onOtherNetwork(self docker.Container, game string, ip netip.Addr) bool {
	for name, n := range self.Networks {
		if name == game {
			continue
		}
		subnets, err := networkSubnets(n)
		if err != nil {
			continue
		}
		for _, s := range subnets {
			if s.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// networkSubnets returns the address ranges of Foreman's place on a
// network: IPv4 always, and IPv6 when the network has it.
func networkSubnets(n docker.Network) ([]netip.Prefix, error) {
	v4, err := subnetOf(n.IPAddress, n.IPPrefixLen)
	if err != nil {
		return nil, err
	}
	out := []netip.Prefix{v4}
	if n.GlobalIPv6Address != "" {
		v6, err := subnetOf(n.GlobalIPv6Address, n.GlobalIPv6PrefixLen)
		if err != nil {
			return nil, err
		}
		out = append(out, v6)
	}
	return out, nil
}

func subnetOf(ip string, bits int) (netip.Prefix, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Prefix{}, err
	}
	if bits <= 0 {
		return netip.Prefix{}, errors.New("no prefix length")
	}
	return addr.Prefix(bits)
}

// selfContainer reads Foreman's own container. inContainer is false when
// Foreman does not run in one.
func (c *Controller) selfContainer(ctx context.Context) (self docker.Container, inContainer bool, err error) {
	if c.selfID != "" {
		self, err = c.docker.InspectContainer(ctx, c.selfID)
		if err == nil {
			return self, true, nil
		}
		if !errors.Is(err, docker.ErrNotFound) {
			return docker.Container{}, false, err
		}
	}
	// No container is Foreman's. Fail safe: when Foreman may still be in
	// one, its networks are unknown, so none can be checked (SPEC 14.4).
	for _, f := range c.containerEnvs {
		if exists(f) {
			return docker.Container{}, false, &NetworkError{Kind: NetworkSelfFile, File: f}
		}
	}
	if c.dockerTCP {
		return docker.Container{}, false, &NetworkError{Kind: NetworkSelfTCP}
	}
	return docker.Container{}, false, nil
}

// exists reports whether path may exist; an error other than "not there"
// counts as there.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// rconAddress is where the game's RCON listens, or "" while the container
// has no address. The container is on one network only (SPEC 4.4).
func rconAddress(cont docker.Container) string {
	ip := containerIP(cont)
	if ip == "" {
		return ""
	}
	return net.JoinHostPort(ip, RCONPort)
}

// containerIP is the game's address on the first network that gave it one.
func containerIP(cont docker.Container) string {
	for _, n := range cont.Networks {
		if n.IPAddress != "" {
			return n.IPAddress
		}
	}
	return ""
}

// readMeta reads a world's world.json.
func (c *Controller) readMeta(id string) (world.Meta, error) {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return world.Meta{}, err
	}
	defer func() { _ = root.Close() }()
	return world.ReadMeta(root)
}

// ImageRef is the image that runs a game version: the exact tag, as the
// images are named on Docker Hub (SPEC 4.2, 10.1; ADR 0005).
func ImageRef(version string) string { return "factoriotools/factorio:" + version }

// imageFor returns the image for a supported game version.
func (c *Controller) imageFor(gameVersion string) (string, error) {
	if !world.SupportedGameVersion(gameVersion) {
		return "", fmt.Errorf("there is no game image for Factorio %s", gameVersion)
	}
	return ImageRef(gameVersion), nil
}

// ImageFor returns the image reference a world runs, and whether
// Docker has it (SPEC 3.7). The server page uses it to offer the download.
func (c *Controller) ImageFor(ctx context.Context, id string) (ref string, present bool, err error) {
	meta, err := c.readMeta(id)
	if err != nil {
		return "", false, err
	}
	ref, err = c.imageFor(meta.GameVersion)
	if err != nil {
		return "", false, err
	}
	_, err = c.docker.InspectImage(ctx, ref)
	if errors.Is(err, docker.ErrNotFound) {
		return ref, false, nil
	}
	if err != nil {
		return ref, false, err
	}
	return ref, true, nil
}

// secrets returns the world's RCON password and, with it, every secret the
// game might print: the passwords and tokens its server settings set
// (SPEC 8.6, 14.8). It creates the RCON password file when missing (SPEC 4.5).
func (c *Controller) secrets(id string) (rconPW secret.Secret, all []secret.Secret, err error) {
	rconPW, err = c.worlds.RCONPassword(id)
	if err != nil {
		return secret.Secret{}, nil, fmt.Errorf("read the world's RCON password: %w", err)
	}
	fromSettings, err := c.settingsSecrets(id)
	if err != nil {
		return secret.Secret{}, nil, err
	}
	return rconPW, append([]secret.Secret{rconPW}, fromSettings...), nil
}

// hasRCONPassword reports, as an error, whether the world lacks its RCON
// password file.
func (c *Controller) hasRCONPassword(id string) error {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	_, err = root.Lstat(world.RCONPasswordFile)
	return err
}

// settingsSecrets reads the secrets in the world's server settings: the
// game password and the factorio.com username, password and token
// (gamesettings.SecretKeys, SPEC 14.8). It returns none when the file or
// the fields are missing or empty.
func (c *Controller) settingsSecrets(id string) ([]secret.Secret, error) {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	b, err := world.ReadSmallFile(root, serverSettings, maxSettings)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	// Fail safe: a file Foreman cannot read may still hold a secret the
	// game reads, which Foreman could then not hide (SPEC 14.8).
	if json.Unmarshal(b, &doc) != nil {
		return nil, fmt.Errorf("%s is not valid JSON, so Foreman cannot tell whether it sets a password or token to keep out of the log. Fix the file, then start again", path.Clean(serverSettings))
	}
	var out []secret.Secret
	for _, key := range gamesettings.SecretKeys {
		var value string
		// A key of another type is not a string the game could print.
		if json.Unmarshal(doc[key], &value) == nil && value != "" {
			out = append(out, secret.New(value))
		}
	}
	return out, nil
}
