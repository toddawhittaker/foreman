package game

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The steps a switch shows besides the stop and start steps (SPEC 7.9).
const (
	StepSnapshotBoth = "Take a snapshot of both worlds"
	StepPresent      = "Make the new world active"
)

// SwitchStore records switches in progress and which world is active
// (SPEC 5.3, 7.10). *store.Store satisfies it.
type SwitchStore interface {
	ActiveWorld(ctx context.Context) (id string, found bool, err error)
	BeginSwitch(ctx context.Context, from, to, byUser string, now time.Time) error
	SetSwitchStep(ctx context.Context, from, step string) error
	ListSwitches(ctx context.Context) ([]store.Switch, error)
	EndSwitch(ctx context.Context, from string) error
	ReplaceActiveWorldForSwitch(ctx context.Context, from, to string, now time.Time) error
}

// SwitchInfo says which worlds a switch is between and whether the new one
// became active. When a switch fails, Presented decides the offer: a switch
// back to From when it is set, otherwise just Start of From (SPEC 7.9).
type SwitchInfo struct {
	From, To  string // world ids
	Presented bool
}

// SwitchNotice reports a switch that startup found unfinished (SPEC 7.10).
// When Presented is set, To is active and the page offers Start of To and a
// switch back to From; otherwise From is still active and the page offers
// Start of From.
type SwitchNotice struct {
	SwitchInfo
	Text string
	// Step is the last step the switch recorded (store.Step*), so a page
	// can say where it stopped.
	Step string
}

// Switch refusals. The text is shown to the admin.
var (
	ErrSwitchUnavailable   = errors.New("switching worlds is not set up in this Foreman")
	ErrSwitchSame          = errors.New("this is already the active world")
	ErrSwitchNoActive      = errors.New("no world is active, so there is nothing to switch from")
	ErrSwitchNotActive     = errors.New("the world to switch from is no longer the active world. Reload the page, then try again")
	ErrSwitchNoTarget      = errors.New("the world to switch to does not exist")
	ErrSwitchUnreadable    = errors.New("the world.json of the world to switch to cannot be read, so that world cannot start")
	ErrSwitchNewer         = errors.New("a newer version of Foreman wrote the world to switch to, so this version cannot start it")
	ErrSwitchBusy          = errors.New("another operation is running on one of these worlds. Wait for it to finish, then try again")
	ErrSwitchUnsettled     = errors.New("the game is starting or stopping, or Foreman cannot tell its state. Wait until it is running or stopped, or use Force stop")
	ErrSwitchTargetRunning = errors.New("the world to switch to already has a game running. Stop it first")
)

// isSwitchRefusal reports whether err is one of the switch refusals that
// IsRefusal does not know, so it is audited as a refusal (SPEC 12.9).
// ErrSwitchUnsettled is left out: like a stop the game is not ready for, it
// is audited as the switch, which then failed.
func isSwitchRefusal(err error) bool {
	for _, r := range []error{ErrSwitchNoActive, ErrSwitchUnreadable, ErrSwitchNewer, ErrSwitchBusy} {
		if errors.Is(err, r) {
			return true
		}
	}
	return false
}

// SwitchPlan is what a switch would do, from the checks that run before
// anything changes (SPEC 7.9, 17.8). The confirmation page shows it, and the
// switch acts on it.
type SwitchPlan struct {
	// Running is true when the game runs on the world switched from, so the
	// switch stops it first.
	Running bool
	// Image is the game image the new world runs. Download is true when
	// Docker does not have it, so the switch downloads it before anything
	// else (SPEC 3.7).
	Image    string
	Download bool
	// Check is the new world's pre-start check (SPEC 7.4), read without
	// writing anything. Its problems refuse only a switch that starts the
	// new world; a switch that leaves the game stopped leaves them to the
	// next Start.
	Check world.PreCheck
}

// PlanSwitch runs every check that could refuse a switch from from to to,
// and changes nothing (SPEC 7.8, 7.9). The confirmation page calls it, so
// problems show before the admin confirms (SPEC 17.8), and Switch calls it
// again under both worlds' locks, so the two never disagree on the rules.
// A refusal is returned as an error. The new world's pre-start problems are
// in the plan instead, because the admin still chooses what to do about
// them.
func (c *Controller) PlanSwitch(ctx context.Context, from, to string) (SwitchPlan, error) {
	return c.planSwitch(ctx, from, to, true)
}

// planSwitch asks the portal only when portal is set: a switch that will
// not start the world has no use for its answer (SPEC 7.9).
func (c *Controller) planSwitch(ctx context.Context, from, to string, portal bool) (SwitchPlan, error) {
	if c.switches == nil {
		return SwitchPlan{}, ErrSwitchUnavailable
	}
	active, found, err := c.switches.ActiveWorld(ctx)
	if err != nil {
		return SwitchPlan{}, err
	}
	switch {
	case !found:
		return SwitchPlan{}, ErrSwitchNoActive
	case from != active:
		return SwitchPlan{}, ErrSwitchNotActive
	case to == active:
		return SwitchPlan{}, ErrSwitchSame
	}
	meta, err := c.readMeta(to)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return SwitchPlan{}, ErrSwitchNoTarget
	case err != nil:
		return SwitchPlan{}, ErrSwitchUnreadable
	case meta.Newer():
		return SwitchPlan{}, ErrSwitchNewer
	}
	cur, next := c.Status(from), c.Status(to)
	if busy(cur.Operation) || busy(next.Operation) {
		return SwitchPlan{}, ErrSwitchBusy
	}
	var plan SwitchPlan
	switch cur.State {
	case StateRunning, StateCrashed:
		plan.Running = true
	case StateStopped, StateCrashLooping:
	default:
		return SwitchPlan{}, ErrSwitchUnsettled
	}
	check, err := c.precheckReadOnly(ctx, to, "", StartOptions{portal: portal})
	if _, refused := errors.AsType[*PrecheckError](err); err != nil && !refused {
		return SwitchPlan{}, err
	}
	plan.Check = check
	ref, present, err := c.ImageFor(ctx, to)
	if err != nil {
		return SwitchPlan{}, err
	}
	plan.Image, plan.Download = ref, !present
	if _, err := c.gameNetwork(ctx); err != nil {
		return SwitchPlan{}, err
	}
	cont, found, err := c.ourContainer(ctx, to)
	if err != nil {
		return SwitchPlan{}, err
	}
	if found {
		switch cont.State.Status {
		case "running", "restarting", "paused":
			return SwitchPlan{}, ErrSwitchTargetRunning
		}
	}
	return plan, nil
}

func busy(op *Operation) bool { return op != nil && !op.Done }

// switchSteps lists a switch's steps in the order of SPEC 7.9. A missing
// image is downloaded first, before the stop, as an update does (SPEC 10.3).
// A switch that leaves the game stopped neither checks nor starts the new
// world, so it shows neither.
func switchSteps(plan SwitchPlan, opts StartOptions, start bool) [][]string {
	var steps [][]string
	if plan.Download {
		steps = append(steps, []string{StepDownload})
	}
	steps = append(steps, stopSteps, []string{StepSnapshotBoth})
	if start {
		steps = append(steps, checkStepsFor(opts))
	}
	steps = append(steps, []string{StepPresent})
	if start {
		steps = append(steps, launchSteps)
	}
	return steps
}

// Switch makes to the active world in place of from (SPEC 7.9, 7.10), and
// starts it when start is set; otherwise the game ends stopped. Everything
// that could refuse is checked before from is stopped (SPEC 7.8). A missing
// game image for to is downloaded before the switch record is written and
// before the stop, so a failed download leaves from running and untouched.
// The pre-start problems of to refuse only a switch that starts it, unless
// opts goes ahead anyway. Both worlds are locked, show the operation and are
// busy until it ends; a forced stop on either interrupts it. It can take
// StartTimeout after the stop; run it with a context that outlives the
// request.
func (c *Controller) Switch(ctx context.Context, from, to string, by Actor, stop StopOptions, opts StartOptions, start bool) error {
	if c.switches == nil {
		return ErrSwitchUnavailable
	}
	if from == to {
		return ErrSwitchSame
	}
	countdown, err := stopCountdown(stop)
	if err != nil {
		return err
	}
	unlock, err := c.lockWorlds(from, to)
	if err != nil {
		return err
	}
	defer unlock()
	if !start {
		opts.Anyway = false
	}
	plan, err := c.planSwitch(ctx, from, to, start)
	detail := switchDetail(from, to, plan, opts, start)
	if err != nil {
		if isSwitchRefusal(err) {
			return c.refused(ctx, by, AuditSwitch, from, detail, err)
		}
		return c.checkFailed(ctx, by, AuditSwitch, from, detail, err)
	}
	opts.running = plan.Running
	if start {
		if err := c.allowStart(plan.Check, opts); err != nil {
			return c.checkFailed(ctx, by, AuditSwitch, from, detail, err)
		}
	}
	// Compared before the stop; the check after the snapshots does not.
	opts.confirmed = true
	opts.carryPortal(plan.Check)
	if err := c.audit(ctx, by, AuditSwitch, from, detail); err != nil {
		return err
	}
	ws := []*worldRun{c.world(from), c.world(to)}
	slices.SortFunc(ws, func(a, b *worldRun) int { return strings.Compare(a.id, b.id) })
	op := newOperation(AuditSwitch, switchSteps(plan, opts, start)...)
	op.Switch = &SwitchInfo{From: from, To: to}
	ctx, o := c.beginOpOn(ctx, ws, op)
	if plan.Download {
		if err := c.switchDownload(ctx, o, plan.Image); err != nil {
			return o.end(err)
		}
	}
	// A record from an earlier switch from this world is stale: from's lock
	// is held, so no switch from it is running (SPEC 7.10).
	if err := c.switches.EndSwitch(ctx, from); err != nil {
		return o.end(err)
	}
	if err := c.switches.BeginSwitch(ctx, from, to, by.Username, c.now()); err != nil {
		return o.end(err)
	}
	err = c.switchLocked(ctx, o, from, to, by, countdown, opts, start)
	// The record goes whether the switch worked or failed (SPEC 7.10), even
	// after a forced stop.
	if endErr := c.switches.EndSwitch(context.WithoutCancel(ctx), from); endErr != nil {
		c.log.Error("could not clear the switch record; startup will report it", "from", from, "to", to, "error", endErr)
	}
	return o.end(err)
}

// switchDownload downloads the new world's game image, which the admin
// confirmed on the switch page (SPEC 3.7). An image that arrived since the
// plan is not downloaded again.
func (c *Controller) switchDownload(ctx context.Context, o *opRun, ref string) error {
	o.begin(StepDownload)
	_, err := c.docker.InspectImage(ctx, ref)
	if err == nil {
		o.skip(StepDownload)
		return nil
	}
	if !errors.Is(err, docker.ErrNotFound) {
		return err
	}
	if err := c.docker.PullImage(ctx, ref); err != nil {
		return pullError(ref, err)
	}
	o.done(StepDownload)
	return nil
}

func switchDetail(from, to string, plan SwitchPlan, opts StartOptions, start bool) string {
	d := "from " + from + " to " + to
	if plan.Download {
		d += ", download " + plan.Image
	}
	switch {
	case !start:
		d += ", leave stopped"
	case opts.Anyway:
		d += ", start anyway"
	}
	return d
}

// lockWorlds takes each world's lock in id order, or none of them.
func (c *Controller) lockWorlds(ids ...string) (unlock func(), err error) {
	ids = slices.Sorted(slices.Values(ids))
	var held []func()
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i]()
		}
	}
	for _, id := range ids {
		u, err := c.worlds.Lock(id)
		if err != nil {
			release()
			return nil, err
		}
		held = append(held, u)
	}
	return release, nil
}

// switchLocked runs the switch steps. The caller holds both worlds' locks
// and the switch record. The order alone keeps SPEC 7.10: the old game is
// stopped, and a new world that will start is checked, before the new world
// is made active.
func (c *Controller) switchLocked(ctx context.Context, o *opRun, from, to string, by Actor, countdown time.Duration, opts StartOptions, start bool) error {
	o.w = c.world(from)
	if err := c.stopLocked(ctx, o, countdown); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}

	// Each record write happens while a step shows in progress, so a failed
	// write still shows as a failed step.
	o.begin(StepSnapshotBoth)
	if err := c.switches.SetSwitchStep(ctx, from, store.StepStopped); err != nil {
		return err
	}
	for _, id := range []string{from, to} {
		if _, err := c.worlds.AutoSnapshot(id, world.ReasonSwitch, world.LabelBeforeSwitch, by.Username); err != nil {
			return fmt.Errorf("take the snapshot of %s before the switch: %w", c.worldName(id), err)
		}
	}
	if err := c.switches.SetSwitchStep(ctx, from, store.StepSnapshotted); err != nil {
		return err
	}
	o.done(StepSnapshotBoth)

	o.w = c.world(to)
	if start {
		if err := c.checkLocked(ctx, o, by, opts); err != nil {
			return err
		}
	}

	o.begin(StepPresent)
	if err := c.switches.SetSwitchStep(ctx, from, store.StepChecked); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.switches.ReplaceActiveWorldForSwitch(ctx, from, to, c.now()); err != nil {
		return err
	}
	o.lockAll()
	o.op.Switch.Presented = true
	o.unlockAll()
	o.done(StepPresent)

	if !start {
		return nil
	}
	if err := c.launchLocked(ctx, o); err != nil {
		return err
	}
	// The record is deleted next, so a failed write here changes nothing
	// recovery would do.
	if err := c.switches.SetSwitchStep(context.WithoutCancel(ctx), from, store.StepStarted); err != nil {
		c.log.Warn("could not record the switch's last step", "from", from, "to", to, "error", err)
	}
	return nil
}

// worldName returns the world's name for a message, or its id when the
// name cannot be read.
func (c *Controller) worldName(id string) string {
	meta, err := c.readMeta(id)
	if err != nil || meta.Name == "" {
		return id
	}
	return meta.Name
}

// Recover reports what a stop of Foreman left part way. Call it once at
// startup, before Run. It never starts or stops a game (SPEC 3.8). Each
// fault is logged and recovery goes on, so none stops Foreman from serving
// pages (SPEC 7.1). It returns an error only when ctx ends, which means
// startup itself was cancelled.
func (c *Controller) Recover(ctx context.Context) error {
	c.recoverSwitches(ctx)
	return ctx.Err()
}

// recoverSwitches finds every switch left unfinished by a stop of Foreman
// and reports it (SPEC 7.10). It never starts or stops a game (SPEC 3.8):
// it clears each record, logs what it found, and keeps a notice on the
// world that is active, which Status shows until the next operation on that
// world. A record it cannot recover is logged and left for the next switch
// from that world to replace.
func (c *Controller) recoverSwitches(ctx context.Context) {
	if c.switches == nil {
		return
	}
	list, err := c.switches.ListSwitches(ctx)
	if err != nil {
		c.log.Error("cannot read the switch records, so an unfinished switch is not reported", "error", err)
		return
	}
	for _, sw := range list {
		if err := c.recoverSwitch(ctx, sw); err != nil {
			c.log.Error("could not recover an unfinished world switch", "from", sw.From, "to", sw.To, "step", sw.Step, "error", err)
		}
	}
}

func (c *Controller) recoverSwitch(ctx context.Context, sw store.Switch) error {
	fromName, toName := c.worldName(sw.From), c.worldName(sw.To)
	// The active row and the presented step change in one transaction, so
	// the step alone says which world is active.
	n := &SwitchNotice{SwitchInfo: SwitchInfo{From: sw.From, To: sw.To}, Step: sw.Step}
	active := sw.From
	switch sw.Step {
	case store.StepPresented, store.StepStarted:
		n.Presented = true
		active = sw.To
		n.Text = fmt.Sprintf("%s is active; the switch did not confirm it running.", toName)
	default:
		n.Text = fmt.Sprintf("Switch from %s to %s stopped at %s. %s is still active and was not started again.",
			fromName, toName, unfinishedStep(sw.Step, fromName, toName), fromName)
	}
	c.log.Warn("found an unfinished world switch", "from", sw.From, "to", sw.To, "step", sw.Step, "notice", n.Text)
	w := c.world(active)
	w.mu.Lock()
	w.notice = n
	c.changedLocked(w, true)
	w.mu.Unlock()
	return c.switches.EndSwitch(ctx, sw.From)
}

// unfinishedStep names the step that came after the last one recorded.
func unfinishedStep(last, fromName, toName string) string {
	switch last {
	case store.StepBegun:
		return "stopping " + fromName
	case store.StepStopped:
		return "the snapshots"
	case store.StepSnapshotted:
		return "the check of " + toName
	case store.StepChecked:
		return "making " + toName + " active"
	}
	return "step " + last
}
