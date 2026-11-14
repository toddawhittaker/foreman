package game

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// GameSubnets returns the address ranges of the networks a game can send
// from, for trusted-header sign-in to refuse peers inside them (SPEC 4.4,
// 12.6). A game with NET_RAW can claim any free address on its network, so
// the whole range counts, not only the game's own address. known is false
// until the ranges have been read once; a later failed read keeps the last
// value. It makes no call.
//
// The ranges come from container inspect (SPEC 14.3 allows no network
// call). Foreman can only answer a peer on a range it has an address in,
// or that a game it reaches is on, so these are the ones that matter.
func (c *Controller) GameSubnets() (subnets []netip.Prefix, known bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.subnets), c.subnetsKnown
}

// refreshSubnets ends the monitor's look, once it has listed and inspected
// every game container (allSeen). In a container
// the range is Foreman's own on the game network; with no game network no
// game can start (SPEC 4.4). Outside one it is what the game containers
// report (noteGameSubnets), so it is known only once all were inspected.
// With a VPN sidecar it is every range of the sidecar's networks too, read
// in either case, and known only once the sidecar was read in full and
// every game was inspected, since an older game may share the network of
// a sidecar since replaced.
func (c *Controller) refreshSubnets(ctx context.Context, allSeen bool) {
	self, inContainer, err := c.selfContainer(ctx)
	var own []netip.Prefix
	if inContainer {
		own = appendSubnets(nil, self.Networks[c.network])
	}
	switch {
	case err != nil:
	case c.sidecar != "":
		sidecar, complete := c.readSidecarSubnets(ctx)
		c.addSubnets(append(own, sidecar...), complete && allSeen)
	case inContainer:
		c.addSubnets(own, true)
	case allSeen:
		c.addSubnets(nil, true)
	}
}

// readSidecarSubnets reads the ranges of the configured sidecar's networks
// (SPEC 12.6); complete is false when they could not all be read.
func (c *Controller) readSidecarSubnets(ctx context.Context) (subnets []netip.Prefix, complete bool) {
	sc, err := c.docker.InspectContainer(ctx, c.sidecar)
	if err != nil {
		return nil, false
	}
	return sidecarSubnets(sc)
}

// noteStartSubnets adds, before a game starts, the ranges it will send
// from, so the game is never running while they are unknown. With a VPN
// sidecar they are the ranges of the sidecar the start checked, which
// were all read (SPEC 4.13). In a container they are Foreman's own range
// on the game network. Outside one Foreman has no endpoint there, and
// noteGameSubnets adds the range once the started game is inspected.
func (c *Controller) noteStartSubnets(ctx context.Context, network gameNet) {
	if network.sidecar.ID != "" {
		c.addSubnets(sidecarSubnets(network.sidecar))
		return
	}
	self, inContainer, err := c.selfContainer(ctx)
	if err != nil || !inContainer {
		return
	}
	c.addSubnets(appendSubnets(nil, self.Networks[c.network]), true)
}

// noteGameSubnets adds a game container's ranges and reports whether they
// could be read. A game in another container's network has none of its
// own: the ranges are that container's, looked up by the ID the game
// joined, and they never alone make the ranges known. A game whose
// network container is gone sends from no Docker network. Ranges are only
// added here, never dropped: a network keeps its range, and refusing an
// old one is the safe side.
func (c *Controller) noteGameSubnets(ctx context.Context, cont docker.Container) bool {
	if ref, shared := strings.CutPrefix(cont.NetworkMode, "container:"); shared {
		ns, err := c.docker.InspectContainer(ctx, ref)
		if err != nil {
			return errors.Is(err, docker.ErrNotFound)
		}
		subnets, _ := sidecarSubnets(ns)
		c.addSubnets(subnets, false)
		return true
	}
	var add []netip.Prefix
	for _, n := range cont.Networks {
		add = appendSubnets(add, n)
	}
	// With a sidecar, a game on a network of its own is left from before
	// the sidecar was set; the sidecar's ranges decide.
	c.addSubnets(add, c.sidecar == "")
	return true
}

// addSubnets adds subnets to the ranges, and marks them known when known
// is set.
func (c *Controller) addSubnets(subnets []netip.Prefix, known bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	subnets = append(subnets, c.subnets...)
	slices.SortFunc(subnets, func(a, b netip.Prefix) int {
		if d := a.Addr().Compare(b.Addr()); d != 0 {
			return d
		}
		return a.Bits() - b.Bits()
	})
	c.subnets = slices.Compact(subnets)
	c.subnetsKnown = c.subnetsKnown || known
}

// linkLocalV6 is fe80::/10, the IPv6 link-local range.
var linkLocalV6 = netip.MustParsePrefix("fe80::/10")

// appendSubnets adds the IPv4 and IPv6 ranges of one network endpoint. An
// endpoint with no address, such as a stopped container's, adds none.
func appendSubnets(out []netip.Prefix, n docker.Network) []netip.Prefix {
	for _, a := range []struct {
		ip   string
		bits int
	}{{n.IPAddress, n.IPPrefixLen}, {n.GlobalIPv6Address, n.GlobalIPv6PrefixLen}} {
		p, err := subnetOf(a.ip, a.bits)
		if err == nil {
			out = append(out, p.Masked())
		}
	}
	// An IPv6 network also carries link-local addresses, which a game can
	// send from too (SPEC 12.6).
	if n.GlobalIPv6Address != "" {
		out = append(out, linkLocalV6)
	}
	return out
}
