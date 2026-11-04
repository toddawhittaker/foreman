package game

import (
	"context"
	"net/netip"
	"slices"

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

// refreshAdoptedSubnets sets the ranges from the adopted container at this
// look: those of every network Foreman shares with it, from both ends, or
// of all its networks when Foreman is not in a container.
func (c *Controller) refreshAdoptedSubnets(ctx context.Context, cont docker.Container) {
	self, inContainer, err := c.selfContainer(ctx)
	if err != nil {
		return
	}
	var all []netip.Prefix
	for name, n := range cont.Networks {
		mine, shared := self.Networks[name]
		if inContainer && !shared {
			continue
		}
		all = appendSubnets(all, n)
		if shared {
			all = appendSubnets(all, mine)
		}
	}
	c.setSubnets(all, true)
}

// refreshManagedSubnets ends the monitor's look in managed mode, once it
// has listed and inspected every game container (allSeen). In a container
// the range is Foreman's own on the game network; with no game network no
// game can start (SPEC 4.4). Outside one it is what the game containers
// report (noteGameSubnets), so it is known only once all were inspected.
func (c *Controller) refreshManagedSubnets(ctx context.Context, allSeen bool) {
	self, inContainer, err := c.selfContainer(ctx)
	switch {
	case err != nil:
	case inContainer:
		c.setSubnets(appendSubnets(nil, self.Networks[c.network]), false)
	case allSeen:
		c.setSubnets(nil, false)
	}
}

// noteGameSubnets adds a managed game container's range on its network.
// Ranges are only added in managed mode, never dropped: a network keeps
// its range, and refusing an old one is the safe side.
func (c *Controller) noteGameSubnets(cont docker.Container) {
	if c.adopted != nil {
		return // refreshAdoptedSubnets owns the ranges there
	}
	var add []netip.Prefix
	for _, n := range cont.Networks {
		add = appendSubnets(add, n)
	}
	c.setSubnets(add, false)
}

// setSubnets marks the ranges known and either replaces them or adds to
// them.
func (c *Controller) setSubnets(subnets []netip.Prefix, replace bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !replace {
		subnets = append(subnets, c.subnets...)
	}
	slices.SortFunc(subnets, func(a, b netip.Prefix) int {
		if d := a.Addr().Compare(b.Addr()); d != 0 {
			return d
		}
		return a.Bits() - b.Bits()
	})
	c.subnets, c.subnetsKnown = slices.Compact(subnets), true
}

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
	return out
}
