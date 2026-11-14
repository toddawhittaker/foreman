package fakegame

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

// The fake models an operator's VPN sidecar (SPEC 4.13): a container the
// operator runs on the game network, whose network namespace a shared-network
// game joins with NetworkMode "container:<ID>". Such a game has no network of
// its own, so its inspect lists none, and its RCON listens only on the
// endpoint Foreman put in gameenv.RCONBind, which must be an address of the
// sidecar's namespace. The fake seeds the sidecar at an address Foreman can
// dial on this machine, as it does for the games themselves.

// SidecarImage is the image the seeded sidecar claims. It is not added to the
// daemon's images, because Foreman never pulls or starts it.
const SidecarImage = "example/vpn-sidecar:1"

// SidecarPublishedPort is the container port the sidecar publishes when it
// is given a host port: the game's own, 34197/udp.
const SidecarPublishedPort = "34197/udp"

// SidecarSecret is the credential in the sidecar's environment. A sidecar's
// environment can hold a VPN key, so a test can look for this value in
// anything Foreman shows or logs: it must never appear.
const SidecarSecret = "canary-sidecar-credential-0000"

// sidecarPlaceholder is the address the seed gives the sidecar before the
// fake picks its loopback one; inspect never reports it.
const sidecarPlaceholder = "10.250.0.3/24"

// sidecarPrefixLen is the prefix length inspect reports for the sidecar's
// loopback address: a /24, as the self container's range is.
const sidecarPrefixLen = 24

var (
	sidecarName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	networkName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
)

// checkSidecarConfig checks the sidecar settings of a Config.
func checkSidecarConfig(cfg Config) error {
	if cfg.Sidecar == "" {
		if cfg.SidecarPort != "" {
			return errors.New("a sidecar port needs a sidecar")
		}
		return nil
	}
	if !sidecarName.MatchString(cfg.Sidecar) {
		return fmt.Errorf("%q is not a container name", cfg.Sidecar)
	}
	if cfg.SelfNetwork == "" || !networkName.MatchString(cfg.SelfNetwork) {
		return errors.New("a sidecar joins the game network, so it needs the self network")
	}
	return checkPublishedPort(cfg.SidecarPort)
}

// checkPublishedPort accepts "" (not published) or a host port number.
func checkPublishedPort(port string) error {
	if port == "" {
		return nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return fmt.Errorf("%q is not a host port", port)
	}
	return nil
}

// seedSidecar adds the operator's sidecar: running, on the game network, at
// an address on the fake's loopback range, with a credential in its
// environment and, when port is set, 34197/udp published on that host port.
func (g *Game) seedSidecar(name, network, port string) {
	g.daemon.Seed(dockertest.Container{
		Name: name, Status: "running", StartedAt: time.Now().UTC(),
		Networks: map[string]string{network: sidecarPlaceholder},
		Spec: docker.ContainerSpec{
			Image: SidecarImage,
			Env:   []string{"TS_USERSPACE=false", "TS_AUTHKEY=" + SidecarSecret},
			HostConfig: docker.HostConfig{
				NetworkMode:   network,
				RestartPolicy: docker.RestartPolicy{Name: "unless-stopped"},
			},
		},
	})
	c, ok := g.daemon.Container(name)
	if !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sidecar = c.ID
	g.sidecarName = name
	g.sidecarAddr = g.addrLocked(c.ID)
	g.sidecarPort = port
}

// SidecarAddress is the sidecar's address on the game network, where a game
// that shares its namespace binds RCON. It is "" without a sidecar.
func (g *Game) SidecarAddress() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sidecarAddr
}

// SetSidecarPort publishes 34197/udp of the sidecar on this host port, or
// stops publishing it when port is "". Like a real sidecar's, it takes effect
// for the next inspect: Docker fixes a container's ports when it is created,
// so the fake stands for the operator recreating the sidecar with the other
// setting, at the same address.
func (g *Game) SetSidecarPort(port string) error {
	if err := checkPublishedPort(port); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sidecar == "" {
		return errors.New("there is no sidecar")
	}
	g.sidecarPort = port
	return nil
}

// RestartSidecar restarts the sidecar's container, as `docker restart` does.
// The games that joined its namespace keep running but sit in the old one, so
// their RCON stops answering until Foreman starts them again (SPEC 4.13): the
// fake closes their RCON listeners and connections.
func (g *Game) RestartSidecar() error {
	g.mu.Lock()
	id := g.sidecar
	var cut []*instance
	for _, in := range g.insts {
		if id != "" && in.shared != "" && g.isSidecarLocked(in.shared) {
			cut = append(cut, in)
		}
	}
	g.mu.Unlock()
	if id == "" {
		return errors.New("there is no sidecar")
	}
	g.daemon.RestartByHand(id)
	for _, in := range cut {
		in.cutRCON()
	}
	return nil
}

// isSidecarLocked reports whether ref, a container ID or name, is the
// sidecar. g.mu is held.
func (g *Game) isSidecarLocked(ref string) bool {
	return g.sidecar != "" && (ref == g.sidecar || ref == g.sidecarName)
}

// sidecarHolder returns the container whose namespace a container joins, from
// NetworkMode "container:<ref>", and whether it joins one.
func sidecarHolder(c dockertest.Container) (string, bool) {
	return strings.CutPrefix(c.Spec.HostConfig.NetworkMode, "container:")
}

// rconHost returns the host of the RCON endpoint a game that shares a
// namespace was told to bind (gameenv.RCONBind), or "" when the value is not
// an address and port, which stops the real game from opening RCON.
func rconHost(endpoint string) string {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || net.ParseIP(host) == nil {
		return ""
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return ""
	}
	return host
}

// bindFails reports why the game cannot bind its RCON endpoint, or "" when it
// can. A game in the sidecar's namespace owns the sidecar's address and no
// other, so any other address is one the real game could not bind.
func (in *instance) bindFails() string {
	if in.ip == "" {
		return "the endpoint is not an address and port"
	}
	in.g.mu.Lock()
	defer in.g.mu.Unlock()
	if in.g.isSidecarLocked(in.shared) && in.ip != in.g.sidecarAddr {
		return "Cannot assign requested address"
	}
	return ""
}

// cutRCON closes the game's RCON listener and connections but leaves the
// game running: what the namespace's end does to a game that still runs.
func (in *instance) cutRCON() {
	in.mu.Lock()
	proxy, rcon := in.proxy, in.rcon
	in.proxy, in.rcon = nil, nil
	conns := make([]net.Conn, 0, len(in.conns))
	for c := range in.conns {
		conns = append(conns, c)
	}
	in.mu.Unlock()
	if proxy != nil {
		_ = proxy.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if rcon != nil {
		rcon.tb.runCleanups()
	}
}

// bindFailed ends a shared-namespace game that could not open its RCON
// endpoint, as the real one exits when it cannot bind.
func (in *instance) bindFailed(why string) {
	in.emitText(stamp(fmt.Sprintf("Error Util.cpp:81: cannot bind RCON to %s: %s", in.bind, why)))
	in.g.died(in, 1, false)
}

// SidecarState is what State reports about the seeded sidecar.
type SidecarState struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Status string `json:"status"`
	// IP is the sidecar's address on the game network, where a shared game
	// binds RCON.
	IP string `json:"ip"`
	// Port is the host port 34197/udp is published on, "" when it is not.
	Port string `json:"port"`
}

func (g *Game) sidecarState() *SidecarState {
	g.mu.Lock()
	id, name, ip, port := g.sidecar, g.sidecarName, g.sidecarAddr, g.sidecarPort
	g.mu.Unlock()
	if id == "" {
		return nil
	}
	s := &SidecarState{Name: name, ID: id, IP: ip, Port: port}
	if c, ok := g.daemon.Container(id); ok {
		s.Status = c.Status
	}
	return s
}

// fillInspect adds the fields Foreman reads from an inspect (SPEC 4.13) that
// the daemon's body does not carry: the environment, the network mode, and
// for the sidecar the published ports. A shared-namespace container, which
// has no network of its own, lists none.
func (g *Game) fillInspect(doc map[string]any, c dockertest.Container) {
	cfg, _ := doc["Config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
		doc["Config"] = cfg
	}
	if _, ok := cfg["Env"]; !ok {
		cfg["Env"] = c.Spec.Env
	}
	host, _ := doc["HostConfig"].(map[string]any)
	if host == nil {
		host = map[string]any{}
		doc["HostConfig"] = host
	}
	if _, ok := host["NetworkMode"]; !ok {
		host["NetworkMode"] = c.Spec.HostConfig.NetworkMode
	}
	settings, _ := doc["NetworkSettings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		doc["NetworkSettings"] = settings
	}
	if _, shared := sidecarHolder(c); shared {
		settings["Networks"] = map[string]any{}
	}
	g.mu.Lock()
	isSidecar, port := c.ID == g.sidecar, g.sidecarPort
	g.mu.Unlock()
	if isSidecar {
		settings["Ports"] = sidecarPorts(c.Status == "running", port)
	}
}

// sidecarPorts is NetworkSettings.Ports as Docker reports it: bindings for a
// running container that publishes the port, on IPv4 and IPv6 as a plain
// `-p` does, and none otherwise.
func sidecarPorts(running bool, port string) map[string]any {
	if !running || port == "" {
		return map[string]any{}
	}
	return map[string]any{SidecarPublishedPort: []map[string]string{
		{"HostIp": "0.0.0.0", "HostPort": port},
		{"HostIp": "::", "HostPort": port},
	}}
}

// setPrefixLen gives the sidecar's networks the prefix length of its
// loopback range, which is what Foreman reads its address ranges from.
func setPrefixLen(nets map[string]any) {
	for _, n := range nets {
		if nw, ok := n.(map[string]any); ok && nw["IPAddress"] != "" {
			nw["IPPrefixLen"] = sidecarPrefixLen
		}
	}
}
