package game

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// ModCheckTimeout bounds the pre-start check's question to the mod portal
// (SPEC 7.4). A portal slower than this never holds up a start.
const ModCheckTimeout = 5 * time.Second

// ModAvailability says which of a world's mods players cannot download;
// *mods.Planner has it. Its answers come from the portal client's cache
// when it has them.
type ModAvailability interface {
	Availability(ctx context.Context, worldID string) ([]mods.Flag, error)
}

// SetModCheck sets what the pre-start check of a start, restart or switch
// asks about the world's mods on the portal. Without one it asks nothing.
func (c *Controller) SetModCheck(m ModAvailability) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.modCheck = m
}

func (c *Controller) availability() ModAvailability {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.modCheck
}

// portalCheck adds to check the mods players cannot download (SPEC 7.4).
// When opts does not ask the portal, it adds what an earlier check of the
// same action found instead. A portal that cannot be asked in time never
// blocks the start: the check then says it could not ask.
func (c *Controller) portalCheck(ctx context.Context, id string, check world.PreCheck, opts StartOptions) world.PreCheck {
	if !opts.portal {
		check.Problems = append(check.Problems, opts.gone...)
		check.PortalUnchecked = opts.unchecked
		return check
	}
	m := c.availability()
	if m == nil {
		return check
	}
	ctx, cancel := context.WithTimeout(ctx, c.modCheckLimit)
	defer cancel()
	flags, err := m.Availability(ctx, id)
	if err != nil {
		c.log.Warn("could not check the world's mods on the mod portal; starting without that check", "world", id, "error", err)
		check.PortalUnchecked = true
		return check
	}
	if p, ok := goneProblem(flags); ok {
		check.Problems = append(check.Problems, p)
	}
	return check
}

// goneProblem is the problem for the mods whose installed version the
// portal no longer offers, if any. Mods not on the portal at all are left
// out: players need their files from the admin whatever Foreman does.
func goneProblem(flags []mods.Flag) (world.Problem, bool) {
	p := world.Problem{Kind: world.ProblemModGone, StartAnyway: true}
	var named []string
	for _, f := range flags {
		if f.Code != mods.FlagVersionGone {
			continue
		}
		p.Mods = append(p.Mods, f.Name)
		p.Gone = append(p.Gone, world.GoneMod{Name: f.Name, Version: f.Version, Update: f.Update})
		named = append(named, f.Name+" "+f.Version)
	}
	if len(p.Mods) == 0 {
		return world.Problem{}, false
	}
	p.Text = fmt.Sprintf("The mod portal no longer offers %s, so players' games cannot download it to join.", strings.Join(named, ", "))
	return p, true
}

// carryPortal makes the checks after the first one of an action use what
// the first found on the portal, without asking it again. A portal that
// answers differently after the stop then never refuses a start with the
// game already stopped (SPEC 7.8).
func (o *StartOptions) carryPortal(check world.PreCheck) {
	o.portal, o.unchecked, o.gone = false, check.PortalUnchecked, nil
	for _, p := range check.Problems {
		if p.Kind == world.ProblemModGone {
			o.gone = append(o.gone, p)
		}
	}
}

// notePortal shows on the operation that the check could not ask the
// portal.
func (o *opRun) notePortal(check world.PreCheck) {
	if !check.PortalUnchecked {
		return
	}
	o.lockAll()
	defer o.unlockAll()
	o.op.PortalUnchecked = true
	o.publishLocked()
}
