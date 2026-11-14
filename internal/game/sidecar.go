package game

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
)

// gameNet is where a start puts the game (SPEC 4.4, 4.13).
type gameNet struct {
	// mode is Docker's network mode: the game network, or
	// "container:<sidecar ID>" with a VPN sidecar.
	mode string
	// rconBind is the endpoint RCON listens on in the sidecar's network;
	// "" without a sidecar, when the game's own address is used.
	rconBind string
	// sidecar is the sidecar as inspected for this start; zero without one.
	sidecar docker.Container
}

// sharedMode is the network mode that joins the network of the container
// with this ID.
func sharedMode(id string) string { return "container:" + id }

// sidecarNetwork checks the VPN sidecar for a start and returns where the
// game goes (SPEC 4.13, 14.16). The sidecar is looked up by name now and
// the game joins the ID that was checked, so a replacement made later is
// never joined unchecked. Each Docker network it is on may also hold other
// services; only ranges that could let a game pose as a trusted proxy or
// reach the Docker socket proxy refuse it. Inside a container, the game
// network keeps the checks of SPEC 4.4, with the sidecar allowed on it.
func (c *Controller) sidecarNetwork(ctx context.Context, self docker.Container, inContainer bool) (gameNet, error) {
	if inContainer {
		if _, ok := self.Networks[c.network]; !ok {
			return gameNet{}, &NetworkError{Kind: NetworkNotJoined, Network: c.network}
		}
	}
	sc, err := c.inspectSidecar(ctx)
	if err != nil {
		return gameNet{}, err
	}
	if inContainer {
		allowed := func(s docker.ContainerSummary) bool {
			return s.ID == sc.ID || s.Labels[LabelInstall] == c.hostData && s.Labels[LabelWorld] != ""
		}
		if err := c.checkSharedNetwork(ctx, self, c.network, allowed); err != nil {
			return gameNet{}, err
		}
	}
	if err := c.checkSidecarRanges(ctx, sc); err != nil {
		return gameNet{}, err
	}
	bind := net.JoinHostPort(sc.Networks[c.network].IPAddress, RCONPort)
	return gameNet{mode: sharedMode(sc.ID), rconBind: bind, sidecar: sc}, nil
}

// inspectSidecar reads the sidecar by its configured name and refuses one
// a game cannot share (SPEC 4.13). A failed inspect is returned as is, so
// nothing starts on what Foreman could not see.
func (c *Controller) inspectSidecar(ctx context.Context) (docker.Container, error) {
	sc, err := c.docker.InspectContainer(ctx, c.sidecar)
	if errors.Is(err, docker.ErrNotFound) {
		return docker.Container{}, &NetworkError{Kind: NetworkSidecarMissing, Container: c.sidecar}
	}
	if err != nil {
		return docker.Container{}, err
	}
	refuse := func(kind string) (docker.Container, error) {
		return docker.Container{}, &NetworkError{Kind: kind, Container: c.sidecar, Network: c.network}
	}
	switch {
	case sc.State.Status != "running":
		return refuse(NetworkSidecarStopped)
	case sc.NetworkMode == "host":
		return refuse(NetworkSidecarHost)
	case strings.HasPrefix(sc.NetworkMode, "container:"):
		return refuse(NetworkSidecarNested)
	}
	if _, ok := sc.Networks[c.network]; !ok {
		return refuse(NetworkSidecarOffNetwork)
	}
	return sc, nil
}

// checkSidecarRanges refuses the sidecar when the range of any Docker
// network it is on cannot be read, overlaps FOREMAN_TRUSTED_PROXIES, or
// holds the address DOCKER_HOST reaches (SPEC 4.13, 12.6, 14.4).
func (c *Controller) checkSidecarRanges(ctx context.Context, sc docker.Container) error {
	var proxy []netip.Addr
	if c.dockerTCP {
		var err error
		if proxy, err = c.dockerHostAddrs(ctx, c.network); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(sc.Networks)) {
		subnets, err := networkSubnets(sc.Networks[name])
		if err != nil {
			return &NetworkError{Kind: NetworkSidecarNoRange, Container: c.sidecar, Network: name}
		}
		for _, subnet := range subnets {
			for _, p := range c.proxies {
				if p.Overlaps(subnet) {
					return &NetworkError{Kind: NetworkSidecarProxyOverlap, Container: c.sidecar, Network: name, Subnet: subnet.String(), Proxies: p.String()}
				}
			}
			for _, ip := range proxy {
				if subnet.Contains(ip) {
					return &NetworkError{Kind: NetworkSidecarHostOnNetwork, Container: c.sidecar, Network: name, Subnet: subnet.String(), Address: ip.String()}
				}
			}
		}
	}
	return nil
}

// sidecarSubnets returns the ranges of every Docker network ns is on, and
// whether they are complete: ns runs and every range could be read
// (SPEC 12.6). Docker cannot show a VPN interface's ranges.
func sidecarSubnets(ns docker.Container) (subnets []netip.Prefix, complete bool) {
	complete = ns.State.Status == "running" && len(ns.Networks) > 0
	for _, n := range ns.Networks {
		if _, err := networkSubnets(n); err != nil {
			complete = false
		}
		subnets = appendSubnets(subnets, n)
	}
	return subnets, complete
}

// endpoint is how Foreman reaches a game, as read from Docker.
type endpoint struct {
	// rcon is where its RCON listens; "" while it has no address.
	rcon string
	// lanUnpublished is the sidecar's name when the game shares a sidecar's
	// network and the sidecar publishes no host port for the game's UDP
	// port (SPEC 4.13); "" otherwise.
	lanUnpublished string
}

// errNamespaceGone says the container whose network a game joined is gone.
var errNamespaceGone = errors.New("the VPN sidecar this game shares a network with is gone, so Foreman cannot reach the game over RCON. Restart the game from Foreman")

// errBindMismatch says a game's RCON address is not its sidecar's.
var errBindMismatch = errors.New("the game's RCON address is not its VPN sidecar's address on the game network, so Foreman will not connect to it. Restart the game from Foreman")

// gameEndpoint finds where Foreman reaches the game in cont (SPEC 4.4,
// 4.13). It is the one path for a start, a reconnect and taking on a game
// after a Foreman restart. A game in a sidecar's network is reached at the
// endpoint it was created with, read from its environment, and only when
// that is the address, on the game network, of the very container whose
// network it joined: neither a sidecar recreated under the same name nor
// an edited environment can send Foreman's RCON login elsewhere.
func (c *Controller) gameEndpoint(ctx context.Context, cont docker.Container) (endpoint, error) {
	ref, shared := strings.CutPrefix(cont.NetworkMode, "container:")
	if !shared {
		return endpoint{rcon: rconAddress(cont)}, nil
	}
	ns, err := c.docker.InspectContainer(ctx, ref)
	if errors.Is(err, docker.ErrNotFound) {
		return endpoint{}, errNamespaceGone
	}
	if err != nil {
		return endpoint{}, err
	}
	// A name in the mode could now name a replacement; Foreman joins IDs.
	if ns.ID != ref {
		return endpoint{}, errBindMismatch
	}
	addr, err := sharedRCON(cont, ns, c.network)
	if err != nil {
		return endpoint{}, err
	}
	out := endpoint{rcon: addr}
	if !publishesGamePort(ns) {
		out.lanUnpublished = ns.Name
	}
	return out, nil
}

// rconClient returns the RCON client for the game in cont, reached at addr
// (SPEC 4.13, 14.16). For a game in a sidecar's network, each new
// connection is checked again with gameEndpoint before the login: the
// sidecar the game joined must still exist, under its ID, at addr on the
// game network. So a dropped connection is never made again, with the
// world's password, to a replacement that took the sidecar's address. The
// check is Docker's word at one moment, not proof that nothing changed
// between it and the login. It uses Docker only, never a world's lock, so
// holding the client while it runs cannot deadlock with w.mu.
func (c *Controller) rconClient(cont docker.Container, addr string, pw secret.Secret) *rcon.Client {
	if !strings.HasPrefix(cont.NetworkMode, "container:") {
		return rcon.New(addr, pw)
	}
	return rcon.NewChecked(addr, pw, func(ctx context.Context) error {
		ep, err := c.gameEndpoint(ctx, cont)
		if err != nil {
			return err
		}
		if ep.rcon != addr {
			return errBindMismatch
		}
		return nil
	})
}

// sharedRCON checks the RCON endpoint in the game's environment against the
// address ns has on network, and returns it.
func sharedRCON(cont, ns docker.Container, network string) (string, error) {
	raw, _ := cont.EnvValue(gameenv.RCONBind)
	bind, err := netip.ParseAddrPort(raw)
	if err != nil || strconv.Itoa(int(bind.Port())) != RCONPort {
		return "", errBindMismatch
	}
	n, ok := ns.Networks[network]
	if !ok {
		return "", errBindMismatch
	}
	want, err := netip.ParseAddr(n.IPAddress)
	if err != nil || want != bind.Addr() {
		return "", errBindMismatch
	}
	return bind.String(), nil
}

// publishesGamePort reports whether ns publishes the game's UDP port on
// some host port. It is what Docker reports, not proof that LAN players
// can reach it (SPEC 4.13).
func publishesGamePort(ns docker.Container) bool {
	for _, b := range ns.Ports[gamePort] {
		if b.HostPort != "" {
			return true
		}
	}
	return false
}

// sidecarText words the sidecar refusals of NetworkError.
func (e *NetworkError) sidecarText() string {
	switch e.Kind {
	case NetworkSidecarMissing:
		return fmt.Sprintf("FOREMAN_GAME_NETWORK_CONTAINER names %q, but Docker has no container by that name, so the game has no VPN sidecar to share a network with. Start the sidecar, or unset FOREMAN_GAME_NETWORK_CONTAINER", e.Container)
	case NetworkSidecarStopped:
		return fmt.Sprintf("the VPN sidecar %q is not running, so the game cannot share its network. Start the sidecar, then try again", e.Container)
	case NetworkSidecarHost:
		return fmt.Sprintf("the VPN sidecar %q uses the host's network, so a game sharing it would be on the host's network too. Give the sidecar a Docker network of its own and put it on %q", e.Container, e.Network)
	case NetworkSidecarNested:
		return fmt.Sprintf("the VPN sidecar %q uses another container's network, so Foreman cannot tell what the game would share. Name the container that owns the network instead", e.Container)
	case NetworkSidecarOffNetwork:
		return fmt.Sprintf("the VPN sidecar %q is not on the game network %q, so Foreman could not reach the game over RCON. Connect the sidecar to that network, then try again", e.Container, e.Network)
	case NetworkSidecarNoRange:
		return fmt.Sprintf("cannot read the address range of the %q network that the VPN sidecar %q is on, so Foreman cannot check that a game sharing it stays away from the Docker socket proxy and FOREMAN_TRUSTED_PROXIES. Check that Docker answers, then try again", e.Network, e.Container)
	case NetworkSidecarProxyOverlap:
		return fmt.Sprintf("the VPN sidecar %q is on the network %q (%s), which overlaps FOREMAN_TRUSTED_PROXIES (%s), so a game sharing the sidecar's network could pose as the proxy that signs users in. Take the sidecar off that network, or narrow FOREMAN_TRUSTED_PROXIES", e.Container, e.Network, e.Subnet, e.Proxies)
	case NetworkSidecarHostOnNetwork:
		return fmt.Sprintf("DOCKER_HOST reaches the Docker socket proxy at %s, which is on the network %q (%s) that the VPN sidecar %q is on, so a game sharing the sidecar's network could reach it. Take the sidecar or the proxy off that network", e.Address, e.Network, e.Subnet, e.Container)
	}
	return ""
}
