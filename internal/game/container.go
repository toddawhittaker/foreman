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
	"strings"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Labels on every game container (SPEC 4.3). Foreman finds its containers by
// them after a restart, with no database row.
const (
	labelPrefix  = "io.github.toddawhittaker.foreman."
	LabelWorld   = labelPrefix + "world"   // the world id
	LabelInstall = labelPrefix + "install" // the host data path, so two installs never share a container
)

// The container's fixed parts (SPEC 4.3 to 4.6).
const (
	gameMount      = "/factorio"
	gameBinary     = "/opt/factorio/bin/x64/factorio"
	gamePort       = "34197/udp"
	gameHostPort   = "34197"
	RCONPort       = "27015"
	restartPolicy  = "unless-stopped"
	serverSettings = world.ConfigDir + "/server-settings.json"
	maxSettings    = 1 << 20
)

// startScript runs the game with Foreman's flags. It reads the RCON
// password from the world's file inside the container, so the password is
// never in the container's settings (SPEC 4.5). Server settings are passed when
// the world has them, as the spike's command line does.
var startScript = strings.Join([]string{
	`pw=$(cat ` + gameMount + "/" + world.RCONPasswordFile + `) || exit 1`,
	`set -- --start-server-load-latest --mod-directory ` + gameMount + "/" + world.ModsDir +
		` --rcon-port ` + RCONPort + ` --rcon-password "$pw"`,
	`if [ -f ` + gameMount + "/" + serverSettings + ` ]; then set -- "$@" --server-settings ` + gameMount + "/" + serverSettings + `; fi`,
	`exec ` + gameBinary + ` "$@"`,
}, "\n")

// ContainerName is the name of a world's game container (SPEC 4.3).
func ContainerName(id string) string { return "foreman-" + id }

// containerSpec builds the game container for a world (SPEC 4.3 to 4.6).
// Nothing in it comes from what an admin types, apart from the world id,
// which world.ValidID has checked.
func (c *Controller) containerSpec(id, image, network string) docker.ContainerSpec {
	grace := StopGraceSeconds
	return docker.ContainerSpec{
		Image:        image,
		Labels:       map[string]string{LabelWorld: id, LabelInstall: c.hostData},
		Entrypoint:   []string{"/bin/sh"},
		Cmd:          []string{"-c", startScript},
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

type networkRefusal string

func (e networkRefusal) Error() string        { return string(e) }
func (e networkRefusal) Is(target error) bool { return target == ErrNetwork }

func refuseNetwork(format string, args ...any) error {
	return networkRefusal(fmt.Sprintf(format, args...))
}

// errNetworkUnchecked matches a network check that could not be made, such
// as a failed DNS lookup. It refuses an action as ErrNetwork does, but the
// monitor does not count it as a refusal.
var errNetworkUnchecked = errors.New("the game network could not be checked")

type networkUnchecked string

func (e networkUnchecked) Error() string { return string(e) }
func (e networkUnchecked) Is(target error) bool {
	return target == ErrNetwork || target == errNetworkUnchecked
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
		return "", refuseNetwork("FOREMAN_GAME_NETWORK must be set when Foreman runs in a container: it names the Docker network the game joins. Choose a network Foreman is on that the Docker socket proxy is not on")
	}
	if _, ok := self.Networks[c.network]; !ok {
		return "", refuseNetwork("FOREMAN_GAME_NETWORK names the %q network, but the Foreman container is not on it, so Foreman could not reach the game. Connect Foreman to that network, or name one it is on", c.network)
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
			return refuseNetwork("cannot read the address range of the %q network from Docker, so Foreman cannot check that a game on it stays away from the Docker socket proxy and FOREMAN_TRUSTED_PROXIES", network)
		}
		if c.dockerTCP {
			if err := c.checkDockerHost(ctx, self, network, subnets); err != nil {
				return err
			}
		}
		for _, subnet := range subnets {
			for _, p := range c.proxies {
				if p.Overlaps(subnet) {
					return refuseNetwork("the game network %q (%s) overlaps FOREMAN_TRUSTED_PROXIES (%s), so a game could pose as the proxy that signs users in. Use a game network outside those ranges, or narrow FOREMAN_TRUSTED_PROXIES", network, subnet, p)
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
	var others []string
	for _, s := range list {
		if s.ID == self.ID || isGame(s) {
			continue
		}
		name := s.ID
		if len(s.Names) > 0 {
			name = strings.TrimPrefix(s.Names[0], "/")
		}
		others = append(others, name)
	}
	if len(others) > 0 {
		return refuseNetwork("the game network %q also holds %s, which Foreman did not make, so a game could reach them. Give the game a network that only Foreman and its games are on", network, strings.Join(others, ", "))
	}
	return nil
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
		return networkUnchecked(fmt.Sprintf("DOCKER_HOST uses tcp://, but Foreman cannot look up the address of %q, so it cannot check that a game on the %q network could not reach the Docker socket proxy. Check DOCKER_HOST, then try again", c.dockerHost, network))
	}
	ips := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		ip, err := netip.ParseAddr(a)
		if err != nil {
			return refuseNetwork("DOCKER_HOST uses tcp://, but %q gave the address %q, which Foreman cannot read, so it cannot check that a game could not reach the Docker socket proxy. Check DOCKER_HOST, then try again", c.dockerHost, a)
		}
		ip = ip.WithZone("").Unmap()
		for _, subnet := range subnets {
			if subnet.Contains(ip) {
				return refuseNetwork("DOCKER_HOST reaches the Docker socket proxy at %s, which is on the game network %q (%s), so a game could reach it. Put the game on a network of its own that Foreman also joins and the proxy does not", ip, network, subnet)
			}
		}
		ips = append(ips, ip)
	}
	for _, ip := range ips {
		if !onOtherNetwork(self, network, ip) {
			return refuseNetwork("DOCKER_HOST reaches Docker at %s, which is not on any of Foreman's networks other than the game network %q, so a game might reach it through that network's gateway. Reach Docker over a network the game is not on, such as a socket proxy on a network Foreman shares with it", ip, network)
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
			return docker.Container{}, false, refuseNetwork("Foreman seems to run in a container (%s exists), but Docker has no container with Foreman's ID or hostname, so Foreman cannot tell which networks it is on. Keep the hostname Docker gave the Foreman container, and make sure DOCKER_HOST reaches the Docker that runs Foreman", f)
		}
	}
	if c.dockerTCP {
		return docker.Container{}, false, refuseNetwork("Foreman seems to run in a container (DOCKER_HOST uses tcp://), but Docker has no container with Foreman's ID or hostname, so Foreman cannot tell which networks it is on. Keep the hostname Docker gave the Foreman container, and make sure DOCKER_HOST reaches the Docker that runs Foreman")
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

// imageFor returns the pinned image for a game version, by its major and
// minor number (SPEC 4.2).
func (c *Controller) imageFor(gameVersion string) (string, error) {
	parts := strings.SplitN(gameVersion, ".", 3)
	if len(parts) >= 2 {
		if ref, ok := c.images[parts[0]+"."+parts[1]]; ok {
			return ref, nil
		}
	}
	return "", fmt.Errorf("there is no game image for Factorio %s", gameVersion)
}

// ImageFor returns the pinned image reference a world runs, and whether
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

// secrets returns the world's RCON password and, when its server settings
// set one, the game password: every secret the game might print
// (SPEC 8.6, 14.8). It creates the RCON password file when missing (SPEC 4.5).
func (c *Controller) secrets(id string) (rconPW secret.Secret, all []secret.Secret, err error) {
	rconPW, err = c.worlds.RCONPassword(id)
	if err != nil {
		return secret.Secret{}, nil, fmt.Errorf("read the world's RCON password: %w", err)
	}
	all = []secret.Secret{rconPW}
	gamePW, err := c.gamePassword(id)
	if err != nil {
		return secret.Secret{}, nil, err
	}
	if gamePW.Reveal() != "" {
		all = append(all, gamePW)
	}
	return rconPW, all, nil
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

// gamePassword reads game_password from the world's server settings; it is
// empty when the file or the field is missing.
func (c *Controller) gamePassword(id string) (secret.Secret, error) {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return secret.Secret{}, err
	}
	defer func() { _ = root.Close() }()
	b, err := world.ReadSmallFile(root, serverSettings, maxSettings)
	if errors.Is(err, fs.ErrNotExist) {
		return secret.Secret{}, nil
	}
	if err != nil {
		return secret.Secret{}, err
	}
	var doc struct {
		GamePassword string `json:"game_password"`
	}
	// Fail safe: a file Foreman cannot read may still hold a password the
	// game reads, which Foreman could then not hide (SPEC 14.8).
	if json.Unmarshal(b, &doc) != nil {
		return secret.Secret{}, fmt.Errorf("%s is not valid JSON, so Foreman cannot tell whether it sets a game password to keep out of the log. Fix the file, then start again", path.Clean(serverSettings))
	}
	return secret.New(doc.GamePassword), nil
}
