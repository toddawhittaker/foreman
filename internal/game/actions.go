package game

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The steps a page shows while an operation runs (DESIGN 7.6).
const (
	StepWarn           = "Warn players"
	StepSave           = "Save the game"
	StepStopContainer  = "Stop the container"
	StepCheck          = "Check the world"
	StepSnapshot       = "Take a snapshot before starting anyway"
	StepImage          = "Check the game image"
	StepCreate         = "Create the game container"
	StepStartContainer = "Start the container"
	StepLoad           = "Wait for the game to load"
	StepRCON           = "Check RCON answers"
	StepRestore        = "Restore the snapshot"
	StepDownload       = "Download the game image"
)

var stopSteps = []string{StepWarn, StepSave, StepStopContainer}

// checkStepsFor lists the steps of checkLocked. Only a start anyway takes
// the snapshot, so only it shows that step (DESIGN 7.6).
func checkStepsFor(opts StartOptions) []string {
	if opts.Anyway {
		return []string{StepCheck, StepSnapshot}
	}
	return []string{StepCheck}
}

// launchSteps are the steps of launchLocked.
var launchSteps = []string{StepImage, StepCreate, StepStartContainer, StepLoad, StepRCON}

// startSteps lists a start's steps.
func startSteps(opts StartOptions) []string {
	return append(checkStepsFor(opts), launchSteps...)
}

// The automatic snapshot taken before "Start anyway" (SPEC 6.2, 7.4).
const (
	ReasonStartAnyway = "start-anyway"
	LabelStartAnyway  = "Before start anyway"
)

// ActionRestore names a restore in Operation.Action. Restores are not in
// this version's audit log (SPEC 12.9).
const ActionRestore = "restore"

// StartOptions changes how Start treats the pre-start check.
type StartOptions struct {
	// Anyway starts despite problems that each allow it (SPEC 7.4), after a
	// snapshot. The caller asks only after the admin confirmed.
	Anyway bool
	// Seen is the AnywayFingerprint of what the admin confirmed. The first
	// check refuses an Anyway whose fingerprint no longer matches.
	Seen string
	// running is whether the game the action stops runs now; confirmed is
	// set once the first check has compared Seen, so later checks, after
	// the stop, do not compare again.
	running, confirmed bool
}

// AnywayFingerprint names what a go-ahead anyway confirms: the kinds of
// problem and the mods they name, and whether the game runs, since that
// changes the steps. Problem texts are left out, as their wording may
// change without the plan changing.
func AnywayFingerprint(check world.PreCheck, running bool) string {
	parts := make([]string, 0, len(check.Problems)+1)
	for _, p := range check.Problems {
		mods := slices.Clone(p.Mods)
		slices.Sort(mods)
		parts = append(parts, string(p.Kind)+":"+strings.Join(mods, ","))
	}
	slices.Sort(parts)
	parts = append(parts, fmt.Sprintf("running=%t", running))
	return strings.Join(parts, ";")
}

// IsRunning reports whether a game in state s is one a stop would stop.
func IsRunning(s State) bool { return s == StateRunning || s == StateCrashed }

func (o StartOptions) auditDetail() string {
	if o.Anyway {
		return "start anyway"
	}
	return ""
}

// Start starts the world's game (SPEC 7.3, 7.4, 7.11). It holds the
// world's lock until the game is in game and answers over RCON, or the
// start has failed, which can take StartTimeout; run it with a context
// that outlives the request. A refusal by the pre-start check is a
// *PrecheckError.
func (c *Controller) Start(ctx context.Context, id string, by Actor, opts StartOptions) error {
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := c.checkActive(ctx, id); err != nil {
		return err
	}
	// The check runs as the first step, so a refusal shows in the step
	// list, and is audited as a refusal rather than a start (SPEC 12.9).
	ctx, o := c.beginOp(ctx, c.world(id), AuditStart, startSteps(opts))
	o.begin(StepCheck)
	opts.running = IsRunning(c.Status(id).State)
	check, err := c.precheck(id, opts)
	if err != nil {
		return o.end(c.checkFailed(ctx, by, AuditStart, id, opts.auditDetail(), err))
	}
	if err := c.audit(ctx, by, AuditStart, id, opts.auditDetail()); err != nil {
		return o.end(err)
	}
	o.done(StepCheck)
	if err := c.anywaySnapshot(o, by, check); err != nil {
		return o.end(err)
	}
	return o.end(c.launchLocked(ctx, o))
}

// ErrWorldNotActive matches the refusal to start a world that is not the
// active one; its text names the world.
var ErrWorldNotActive = errors.New("the world is not the active world. Switch to it first")

type notActiveError struct{ name string }

func (e notActiveError) Error() string {
	return e.name + " is not the active world. Switch to it first"
}
func (e notActiveError) Is(target error) bool { return target == ErrWorldNotActive }

// checkActive refuses a start of a world that is not the active one: a
// start runs the active world (SPEC 7.3), and only a switch changes which
// that is (SPEC 7.9). The caller holds the world's lock.
func (c *Controller) checkActive(ctx context.Context, id string) error {
	if c.switches == nil {
		return nil
	}
	active, found, err := c.switches.ActiveWorld(ctx)
	if err != nil {
		return err
	}
	if !found || active != id {
		return notActiveError{name: c.worldName(id)}
	}
	return nil
}

// startLocked runs the start steps. The caller holds the world's lock.
func (c *Controller) startLocked(ctx context.Context, o *opRun, by Actor, opts StartOptions) error {
	if err := c.checkLocked(ctx, o, by, opts); err != nil {
		return err
	}
	return c.launchLocked(ctx, o)
}

// checkLocked runs the pre-start check and takes the snapshot a start
// anyway needs (SPEC 7.4). The caller holds the world's lock.
func (c *Controller) checkLocked(ctx context.Context, o *opRun, by Actor, opts StartOptions) error {
	id := o.w.id
	o.begin(StepCheck)
	check, err := c.precheck(id, opts)
	if err != nil {
		return err
	}
	o.done(StepCheck)
	return c.anywaySnapshot(o, by, check)
}

// anywaySnapshot takes the snapshot a start anyway needs, or skips the step
// when the check found nothing (SPEC 7.4).
func (c *Controller) anywaySnapshot(o *opRun, by Actor, check world.PreCheck) error {
	if check.OK() {
		o.skip(StepSnapshot)
		return nil
	}
	o.begin(StepSnapshot)
	if _, err := c.worlds.AutoSnapshot(o.w.id, ReasonStartAnyway, LabelStartAnyway, by.Username); err != nil {
		return fmt.Errorf("take the snapshot before starting anyway: %w", err)
	}
	o.done(StepSnapshot)
	return nil
}

// gameImage returns the world's metadata and image, or
// ErrImageMissing when Docker does not have the image: a start never
// downloads it (SPEC 3.7).
func (c *Controller) gameImage(ctx context.Context, id string) (world.Meta, string, error) {
	meta, err := c.readMeta(id)
	if err != nil {
		return world.Meta{}, "", err
	}
	ref, err := c.imageFor(meta.GameVersion)
	if err != nil {
		return world.Meta{}, "", err
	}
	if _, err := c.docker.InspectImage(ctx, ref); errors.Is(err, docker.ErrNotFound) {
		return world.Meta{}, "", ErrImageMissing
	} else if err != nil {
		return world.Meta{}, "", err
	}
	return meta, ref, nil
}

// PreStartCheck runs the pre-start check of SPEC 7.4 against the world's
// pinned game version.
func (c *Controller) PreStartCheck(id string) (world.PreCheck, error) {
	return c.worlds.PreStartCheck(id)
}

// precheck runs the pre-start check (SPEC 7.4) after making sure the world
// has a mod list (SPEC 7.3). It returns a *PrecheckError unless the world may
// start, with or without "Start anyway".
func (c *Controller) precheck(id string, opts StartOptions) (world.PreCheck, error) {
	if err := c.worlds.EnsureModList(id); err != nil {
		return world.PreCheck{}, err
	}
	check, err := c.PreStartCheck(id)
	if err != nil {
		return world.PreCheck{}, err
	}
	return check, c.allowStart(check, opts)
}

// precheckReadOnly is precheck for a switch's checks before the stop, which
// may change no world file before the switch's snapshots (SPEC 6.2, 7.9). A
// missing mod list is not written: the start writes one that matches the
// Space Age flag, so the problem it would cause is left out.
func (c *Controller) precheckReadOnly(id string, opts StartOptions) error {
	check, err := c.PreStartCheck(id)
	if err != nil {
		return err
	}
	missing, err := c.modListMissing(id)
	if err != nil {
		return err
	}
	if missing {
		check.Problems = slices.DeleteFunc(check.Problems, func(p world.Problem) bool {
			return p.Kind == world.ProblemModListSpaceAge
		})
	}
	return c.allowStart(check, opts)
}

// allowStart returns a *PrecheckError unless the check lets the world start.
func (c *Controller) allowStart(check world.PreCheck, opts StartOptions) error {
	if allowed := check.OK() || (opts.Anyway && check.CanStartAnyway()); !allowed {
		return &PrecheckError{Check: check}
	}
	// A go-ahead confirms one plan; refuse it when the plan has changed, so
	// the page shows the new one to confirm (SPEC 7.4, 17.5).
	if opts.Anyway && !opts.confirmed && !check.OK() && AnywayFingerprint(check, opts.running) != opts.Seen {
		return &PrecheckError{Check: check, Changed: true}
	}
	return nil
}

// modListMissing reports whether the world has no mods/mod-list.json.
func (c *Controller) modListMissing(id string) (bool, error) {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	_, err = root.Lstat(path.Join(world.ModsDir, world.ModListFile))
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	return false, err
}

// MaxStopCountdown is the longest countdown a stop may ask for.
const MaxStopCountdown = 10 * time.Minute

// ErrCountdown refuses a countdown outside 0 to MaxStopCountdown.
var ErrCountdown = errors.New("the stop countdown must be between 0 and 10 minutes")

// StopOptions changes how a graceful stop warns players (SPEC 7.6: the
// countdown is configurable per action, SPEC 7.6).
type StopOptions struct {
	// Countdown is how long players are warned; zero warns nobody. Callers
	// pass StopCountdown for the default.
	Countdown time.Duration
}

// stopCountdown returns the countdown to use, or ErrCountdown.
func stopCountdown(o StopOptions) (time.Duration, error) {
	if o.Countdown < 0 || o.Countdown > MaxStopCountdown {
		return 0, ErrCountdown
	}
	return o.Countdown, nil
}

// ErrPaused refuses to act on a game container someone paused in Docker:
// the game is still live, so its files must not change under it.
var ErrPaused = errors.New("the game container is paused in Docker. Foreman never pauses it; unpause it with Docker, then try again")

// Stop stops the world's game gracefully (SPEC 7.5, 7.6): it warns players
// with a countdown, saves, then stops the container. When RCON cannot be
// reached it stops nothing and returns ErrRCONUnreachable, so the admin can
// choose a forced stop.
func (c *Controller) Stop(ctx context.Context, id string, by Actor, stop StopOptions) error {
	countdown, err := stopCountdown(stop)
	if err != nil {
		return err
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := c.audit(ctx, by, AuditStop, id, ""); err != nil {
		return err
	}
	ctx, o := c.beginOp(ctx, c.world(id), AuditStop, stopSteps)
	err = c.stopLocked(ctx, o, countdown)
	return o.end(err)
}

// stopLocked runs the stop steps. It returns ErrNotRunning, with the steps
// skipped, when there is no game to stop. The caller holds the world's lock.
func (c *Controller) stopLocked(ctx context.Context, o *opRun, countdown time.Duration) error {
	w := o.w
	cont, found, err := c.ourContainer(ctx, w.id)
	if err != nil {
		return err
	}
	status := cont.State.Status
	if found && status == "paused" {
		o.skip(stopSteps...)
		return ErrPaused
	}
	if !found || (status != "running" && status != "restarting") {
		o.skip(stopSteps...)
		return ErrNotRunning
	}
	w.mu.Lock()
	ours := w.containerID == cont.ID
	rc, inGame := w.rc, ours && w.run.inGame
	// A restart count above the last look is a crash the monitor has not seen yet.
	crashed := ours && (w.crashedRun || w.run.loadFailed || cont.RestartCount > w.seenRestarts)
	w.mu.Unlock()
	switch {
	case status == "restarting" || crashed:
		// Docker is restarting the game after a crash, or it crashed and is
		// not back in game: there is no game to warn or save.
		o.skip(StepWarn, StepSave)
	case ours && !inGame:
		o.skip(stopSteps...)
		return ErrNotReady
	case rc == nil || !ours:
		return ErrRCONUnreachable
	default:
		o.show(StateStopping)
		o.begin(StepWarn)
		names, err := c.online(ctx, rc)
		if err != nil {
			return err
		}
		if len(names) == 0 || countdown == 0 {
			o.skip(StepWarn)
		} else {
			if err := c.warnPlayers(ctx, rc, countdown); err != nil {
				return err
			}
			o.done(StepWarn)
		}
		o.begin(StepSave)
		if err := c.saveGame(ctx, w); err != nil {
			return err
		}
		o.done(StepSave)
	}

	o.show(StateStopping)
	o.begin(StepStopContainer)
	restore := c.markStopping(w)
	if err := c.docker.StopContainer(ctx, cont.ID, StopGraceSeconds); err != nil && !errors.Is(err, docker.ErrNotModified) {
		restore()
		return err
	}
	c.refresh(ctx, w, cont.ID)
	o.done(StepStopContainer)
	return nil
}

// warnPlayers counts down in game before a stop (SPEC 7.6).
func (c *Controller) warnPlayers(ctx context.Context, rc *rcon.Client, countdown time.Duration) error {
	marks := warningMarks(countdown)
	for i, m := range marks {
		if err := c.exec(ctx, rc, warningText(m)); err != nil {
			return err
		}
		next := time.Duration(0)
		if i+1 < len(marks) {
			next = marks[i+1]
		}
		t := time.NewTimer(m - next)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
	return nil
}

// warningMarks are the times left at which players are warned: the whole
// countdown, then 30 and 10 seconds before the end.
func warningMarks(total time.Duration) []time.Duration {
	out := []time.Duration{total}
	for _, m := range []time.Duration{30 * time.Second, 10 * time.Second} {
		if m < total {
			out = append(out, m)
		}
	}
	return out
}

// warningText is sent as plain RCON text, which the game shows as a chat
// line from the server. Nothing in it comes from a user (SPEC 14.9).
func warningText(left time.Duration) string {
	return "The server will save and stop in " + durationWords(left) + "."
}

// durationWords says a duration in whole minutes or seconds, rounding up.
func durationWords(d time.Duration) string {
	secs := int(math.Ceil(d.Seconds()))
	switch {
	case secs == 60:
		return "1 minute"
	case secs > 60 && secs%60 == 0:
		return fmt.Sprintf("%d minutes", secs/60)
	case secs == 1:
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", secs)
}

// refresh reads the container once more after Foreman changed it, while an
// operation keeps the monitor away.
func (c *Controller) refresh(ctx context.Context, w *worldRun, cid string) {
	cont, err := c.docker.InspectContainer(ctx, cid)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err == nil && w.containerID == cid {
		w.dockerStatus, w.seenRestarts = cont.State.Status, cont.RestartCount
	}
	c.changedLocked(w, true)
}

// ForceStop stops the game without saving (SPEC 7.7). It interrupts any
// start, stop, restart or restore in progress, and takes no world lock, so
// it works while one of those holds it. Docker keeps a killed container
// stopped (SPEC 7.7).
func (c *Controller) ForceStop(ctx context.Context, id string, by Actor) error {
	if err := c.audit(ctx, by, AuditForceStop, id, ""); err != nil {
		return err
	}
	w := c.world(id)
	w.mu.Lock()
	cancel, done, run := w.opCancel, w.opDone, w.opRun
	w.mu.Unlock()
	interrupted := cancel != nil
	if interrupted {
		// The operation's last marks are set before done closes, so the
		// marks set below are not overwritten.
		cancel()
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
		// A switch holds two worlds: the game to stop is the one its last
		// step acted on, whichever world was named (SPEC 7.7, 7.9).
		w = run.w
	}
	defer c.pokeMonitor()
	cont, found, err := c.ourContainer(ctx, w.id)
	if err != nil {
		return err
	}
	status := cont.State.Status
	if !found || (status != "running" && status != "restarting") {
		if interrupted {
			run.noteForceStop(fmt.Sprintf("No game of %s was running.", c.worldName(w.id)))
			// Ending what was in progress was what the admin asked for.
			return nil
		}
		return ErrNotRunning
	}
	restore := c.markStopping(w)
	if status == "running" {
		err = c.docker.KillContainer(ctx, cont.ID, "")
		if errors.Is(err, docker.ErrConflict) {
			// It exited meanwhile; make sure Docker does not restart it.
			err = c.docker.StopContainer(ctx, cont.ID, 0)
		}
	} else {
		err = c.docker.StopContainer(ctx, cont.ID, 0)
	}
	if err != nil && !errors.Is(err, docker.ErrNotModified) {
		restore()
		return err
	}
	c.refresh(ctx, w, cont.ID)
	if interrupted {
		run.noteForceStop(fmt.Sprintf("It stopped the game of %s.", c.worldName(w.id)))
	}
	return nil
}

// noteForceStop adds to the result of an operation on several worlds which
// one a forced stop acted on. The operation has ended.
func (o *opRun) noteForceStop(text string) {
	if len(o.ws) < 2 {
		return
	}
	o.lockAll()
	defer o.unlockAll()
	o.op.Error = ErrCancelled.Error() + ". " + text
	o.publishLocked()
}

// markStopping marks the game as stopped on purpose, so its exit is not a
// crash (SPEC 7.6). The returned function puts the earlier marks back, for
// when Docker refused the stop.
func (c *Controller) markStopping(w *worldRun) (restore func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	stopping, expect := w.stopping, w.expectRunning
	w.stopping, w.expectRunning = true, false
	c.changedLocked(w, true)
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.stopping, w.expectRunning = stopping, expect
		c.changedLocked(w, true)
	}
}

// Restart is a graceful stop followed by a start (SPEC 7.8). The pre-start
// check runs first too, so a start that would be refused never stops the
// game. A stopped game is just started.
func (c *Controller) Restart(ctx context.Context, id string, by Actor, stop StopOptions, opts StartOptions) error {
	countdown, err := stopCountdown(stop)
	if err != nil {
		return err
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := c.checkActive(ctx, id); err != nil {
		return err
	}
	// Compared before the stop, so a restart never refuses with the game
	// already stopped.
	opts.running = IsRunning(c.Status(id).State)
	if _, err := c.precheck(id, opts); err != nil {
		return c.checkFailed(ctx, by, AuditRestart, id, opts.auditDetail(), err)
	}
	opts.confirmed = true
	if err := c.audit(ctx, by, AuditRestart, id, opts.auditDetail()); err != nil {
		return err
	}
	ctx, o := c.beginOp(ctx, c.world(id), AuditRestart, stopSteps, startSteps(opts))
	err = c.stopLocked(ctx, o, countdown)
	if errors.Is(err, ErrNotRunning) {
		err = nil
	}
	if err == nil {
		err = c.startLocked(ctx, o, by, opts)
	}
	return o.end(err)
}

// PullImage downloads the image for the world's game version, as an
// action of its own (SPEC 3.7). It is audited, and shown as a step.
func (c *Controller) PullImage(ctx context.Context, id string, by Actor) error {
	meta, err := c.readMeta(id)
	if err != nil {
		return err
	}
	ref, err := c.imageFor(meta.GameVersion)
	if err != nil {
		return err
	}
	w := c.world(id)
	w.mu.Lock()
	if w.download != nil && !w.download.Done {
		w.mu.Unlock()
		return ErrDownloading
	}
	w.download = &Operation{Action: AuditImageDownload, Steps: []Step{{Name: StepDownload, State: StepProgress}}}
	c.changedLocked(w, true)
	w.mu.Unlock()

	err = c.audit(ctx, by, AuditImageDownload, id, ref)
	if err == nil {
		err = c.docker.PullImage(ctx, ref)
	}
	w.mu.Lock()
	w.download.Done = true
	w.download.Steps[0].State = StepDone
	if err != nil {
		w.download.Steps[0].State = StepFailed
		w.download.Error = err.Error()
	}
	c.changedLocked(w, true)
	w.mu.Unlock()
	return err
}
