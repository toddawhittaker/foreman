package game

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/savefile"
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

// startStepsFor lists a start's steps. Only a start anyway takes the
// snapshot, so only it shows that step (DESIGN 7.6).
func startStepsFor(opts StartOptions) []string {
	if opts.Anyway {
		return []string{StepCheck, StepSnapshot, StepImage, StepCreate, StepStartContainer, StepLoad, StepRCON}
	}
	return []string{StepCheck, StepImage, StepCreate, StepStartContainer, StepLoad, StepRCON}
}

// The automatic snapshot taken before "Start anyway" (SPEC 6.2, 7.4, R5).
const (
	ReasonStartAnyway = "start-anyway"
	LabelStartAnyway  = "Before start anyway"
)

// ActionRestore names a restore in Operation.Action. Restores are not in
// this version's audit log (R11).
const ActionRestore = "restore"

// StartOptions changes how Start treats the pre-start check.
type StartOptions struct {
	// Anyway starts despite problems that each allow it (R5, R17), after a
	// snapshot. The caller asks only after the admin confirmed.
	Anyway bool
}

func (o StartOptions) auditDetail() string {
	if o.Anyway {
		return "start anyway"
	}
	return ""
}

// opRun is one operation in progress on a world.
type opRun struct {
	c *Controller
	w *worldRun
}

// beginOp records an operation and shows its steps. The returned context
// ends when ForceStop interrupts the operation. Call end when done.
func (c *Controller) beginOp(ctx context.Context, w *worldRun, action string, steps ...[]string) (context.Context, *opRun) {
	ctx, cancel := context.WithCancel(ctx)
	op := &Operation{Action: action}
	for _, list := range steps {
		for _, s := range list {
			op.Steps = append(op.Steps, Step{Name: s, State: StepWaiting})
		}
	}
	w.mu.Lock()
	w.op, w.opState, w.opCancel, w.opDone = op, "", cancel, make(chan struct{})
	c.changedLocked(w, true)
	w.mu.Unlock()
	return ctx, &opRun{c: c, w: w}
}

func (o *opRun) set(state StepState, names ...string) {
	o.w.mu.Lock()
	defer o.w.mu.Unlock()
	for i := range o.w.op.Steps {
		for _, n := range names {
			if o.w.op.Steps[i].Name == n {
				o.w.op.Steps[i].State = state
			}
		}
	}
	o.c.changedLocked(o.w, true)
}

func (o *opRun) begin(name string)    { o.set(StepProgress, name) }
func (o *opRun) done(name string)     { o.set(StepDone, name) }
func (o *opRun) skip(names ...string) { o.set(StepSkipped, names...) }

// show sets the state the page shows while the operation runs.
func (o *opRun) show(s State) {
	o.w.mu.Lock()
	defer o.w.mu.Unlock()
	o.w.opState = s
	o.c.changedLocked(o.w, true)
}

// end finishes the operation, marking the step in progress failed when err
// is set, and lets the monitor look after the container again. It returns
// err, with an interruption by ForceStop as ErrCancelled.
func (o *opRun) end(err error) error {
	if errors.Is(err, context.Canceled) {
		err = ErrCancelled
	}
	w := o.w
	w.mu.Lock()
	for i := range w.op.Steps {
		if w.op.Steps[i].State == StepProgress {
			w.op.Steps[i].State = StepDone
			if err != nil {
				w.op.Steps[i].State = StepFailed
			}
		}
	}
	w.op.Done = true
	if err != nil {
		w.op.Error = err.Error()
	}
	done := w.opDone
	w.opCancel()
	w.opState, w.opCancel, w.opDone = "", nil, nil
	o.c.changedLocked(w, true)
	w.mu.Unlock()
	close(done)
	o.c.pokeMonitor()
	return err
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
	if err := c.audit(ctx, by, AuditStart, id, opts.auditDetail()); err != nil {
		return err
	}
	ctx, o := c.beginOp(ctx, c.world(id), AuditStart, startStepsFor(opts))
	err = c.startLocked(ctx, o, by, opts)
	return o.end(err)
}

// startLocked runs the start steps. The caller holds the world's lock.
func (c *Controller) startLocked(ctx context.Context, o *opRun, by Actor, opts StartOptions) error {
	w, id := o.w, o.w.id

	o.begin(StepCheck)
	check, err := c.precheck(id, opts)
	if err != nil {
		return err
	}
	o.done(StepCheck)
	if check.OK() {
		o.skip(StepSnapshot)
	} else {
		o.begin(StepSnapshot)
		if _, err := c.worlds.AutoSnapshot(id, ReasonStartAnyway, LabelStartAnyway, by.Username); err != nil {
			return fmt.Errorf("take the snapshot before starting anyway: %w", err)
		}
		o.done(StepSnapshot)
	}

	o.begin(StepImage)
	meta, err := c.readMeta(id)
	if err != nil {
		return err
	}
	ref, err := c.imageFor(meta.GameVersion)
	if err != nil {
		return err
	}
	// Start never downloads the image (R7, SPEC 3.7).
	if _, err := c.docker.InspectImage(ctx, ref); errors.Is(err, docker.ErrNotFound) {
		return ErrImageMissing
	} else if err != nil {
		return err
	}
	o.done(StepImage)

	o.begin(StepCreate)
	network, err := c.gameNetwork(ctx)
	if err != nil {
		return err
	}
	old, found, err := c.ourContainer(ctx, id)
	if err != nil {
		return err
	}
	if found {
		if s := old.State.Status; s == "running" || s == "restarting" {
			return ErrAlreadyRunning
		}
		// A fresh container for every start, so its settings never drift (R12).
		if err := c.docker.RemoveContainer(ctx, old.ID, false); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return err
		}
	}
	rconPW, secrets, err := c.secrets(id)
	if err != nil {
		return err
	}
	save, err := c.worlds.PrepareLoad(id)
	if err != nil {
		return err
	}
	cid, err := c.docker.CreateContainer(ctx, ContainerName(id), c.containerSpec(id, ref, network))
	if errors.Is(err, docker.ErrConflict) {
		return ErrForeign
	}
	if err != nil {
		return err
	}
	o.done(StepCreate)

	o.begin(StepStartContainer)
	w.mu.Lock()
	// A forced stop sets its own marks once this operation has ended, so
	// none may be set here after it asked.
	if err := ctx.Err(); err != nil {
		w.mu.Unlock()
		return err
	}
	stale := c.dropContainerLocked(w)
	w.containerID, w.dockerStatus = cid, "created"
	w.seenRestarts, w.crashes, w.lastCrash = 0, nil, time.Time{}
	w.stopping, w.crashLoop, w.crashedRun = false, false, false
	w.run = runState{save: save}
	w.joins.Reset()
	w.secrets, w.gameVersion, w.image = secrets, meta.GameVersion, ref
	w.mu.Unlock()
	closeRCON(stale)
	o.show(StateStarting)
	if err := c.docker.StartContainer(ctx, cid); err != nil {
		return err
	}
	cont, err := c.docker.InspectContainer(ctx, cid)
	if err != nil {
		return err
	}
	w.mu.Lock()
	if err := ctx.Err(); err != nil {
		w.mu.Unlock()
		return err
	}
	// Only a container Docker started can crash.
	w.expectRunning = true
	w.dockerStatus, w.startedAt, w.seenRestarts = cont.State.Status, cont.State.StartedAt, cont.RestartCount
	if ip := containerIP(cont); ip != "" {
		w.rc = c.newRCON(ip, rconPW)
	}
	c.followLocked(w, cid, time.Time{})
	c.changedLocked(w, true)
	w.mu.Unlock()
	o.done(StepStartContainer)

	deadline := time.NewTimer(c.startTimeout)
	defer deadline.Stop()
	o.begin(StepLoad)
	if err := c.waitInGame(ctx, w, cid, deadline.C); err != nil {
		return err
	}
	// The chosen save applies once (R4).
	if save != "" {
		if err := c.worlds.ClearLoadSave(id, save); err != nil {
			c.log.Warn("could not reset which save loads to the newest", "world", id, "error", err)
		}
	}
	o.done(StepLoad)

	o.begin(StepRCON)
	if err := c.waitRCON(ctx, w, cid, deadline.C); err != nil {
		return err
	}
	o.done(StepRCON)
	return nil
}

// precheck runs the pre-start check (SPEC 7.4) after making sure the world
// has a mod list (R34). It returns a *PrecheckError unless the world may
// start, with or without "Start anyway", and ErrSaveNewerThanImage when
// the pinned image is older than the save.
func (c *Controller) precheck(id string, opts StartOptions) (world.PreCheck, error) {
	if err := c.worlds.EnsureModList(id); err != nil {
		return world.PreCheck{}, err
	}
	check, err := c.worlds.PreStartCheck(id)
	if err != nil {
		return world.PreCheck{}, err
	}
	allowed := check.OK() || (opts.Anyway && check.CanStartAnyway())
	if !allowed {
		return world.PreCheck{}, &PrecheckError{Check: check}
	}
	if err := c.checkSaveAgainstImage(id, check.SaveVersion); err != nil {
		return world.PreCheck{}, err
	}
	return check, nil
}

// ErrSaveNewerThanImage refuses a save made by a newer game than the one
// the world's pinned image runs, which could not load it. It is a plain
// refusal, never "Start anyway".
var ErrSaveNewerThanImage = errors.New("the save was made by a newer Factorio than the game image Foreman runs for this world, so the game could not load it")

// checkSaveAgainstImage refuses a save newer than the game version in the
// pinned image's tag. A save that could not be read has no version and is
// left to the pre-start check's own refusal.
func (c *Controller) checkSaveAgainstImage(id string, save savefile.Version) error {
	if save == (savefile.Version{}) {
		return nil
	}
	meta, err := c.readMeta(id)
	if err != nil {
		return err
	}
	ref, err := c.imageFor(meta.GameVersion)
	if err != nil {
		return err
	}
	image, ok := imageVersion(ref)
	if !ok {
		return fmt.Errorf("cannot tell which Factorio version the game image %s runs", ref)
	}
	saved := [3]uint16{save.Major, save.Minor, save.Patch}
	if slices.Compare(saved[:], image[:]) > 0 {
		return fmt.Errorf("%w: the save is from Factorio %s, and the image runs Factorio %d.%d.%d",
			ErrSaveNewerThanImage, save, image[0], image[1], image[2])
	}
	return nil
}

// imageVersion reads major.minor.patch from an image reference's tag, as in
// factoriotools/factorio:2.0.77@sha256:...
func imageVersion(ref string) ([3]uint16, bool) {
	ref, _, _ = strings.Cut(ref, "@")
	i := strings.LastIndex(ref, ":")
	if i < 0 || i < strings.LastIndex(ref, "/") {
		return [3]uint16{}, false
	}
	parts := strings.Split(ref[i+1:], ".")
	if len(parts) != 3 {
		return [3]uint16{}, false
	}
	var out [3]uint16
	for k, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return [3]uint16{}, false
		}
		out[k] = uint16(n)
	}
	return out, true
}

// waitInGame waits for the log to say the game is in game, or that it
// failed to load (SPEC 7.3, 8.7).
func (c *Controller) waitInGame(ctx context.Context, w *worldRun, cid string, deadline <-chan time.Time) error {
	poll := time.NewTicker(c.pollInterval)
	defer poll.Stop()
	for {
		w.mu.Lock()
		run, changed := w.run, w.changed
		w.mu.Unlock()
		switch {
		case run.loadFailed && run.loadError != "":
			return fmt.Errorf("%w: it could not load the save (%s)", ErrStartFailed, run.loadError)
		case run.loadFailed:
			return fmt.Errorf("%w: it could not load the save", ErrStartFailed)
		case run.inGame:
			return nil
		}
		select {
		case <-changed:
		case <-poll.C:
			cont, err := c.docker.InspectContainer(ctx, cid)
			if err == nil && (cont.State.Status != "running" || cont.RestartCount > 0) {
				return fmt.Errorf("%w: the game stopped while it was loading", ErrStartFailed)
			}
		case <-deadline:
			return fmt.Errorf("%w: it did not finish loading within %s", ErrStartFailed, durationWords(c.startTimeout))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// waitRCON waits until the game answers over RCON: being in game is not
// enough, because the RCON port can fail to open (SPEC 7.3).
func (c *Controller) waitRCON(ctx context.Context, w *worldRun, cid string, deadline <-chan time.Time) error {
	for {
		w.mu.Lock()
		rc := w.rc
		w.mu.Unlock()
		if rc != nil {
			callCtx, cancel := context.WithTimeout(ctx, c.rconCallLimit)
			_, err := rc.Exec(callCtx, "/players online")
			cancel()
			if err == nil {
				return nil
			}
			if errors.Is(err, rcon.ErrLogin) {
				return fmt.Errorf("%w: the game is in game, but it refused Foreman's RCON password", ErrStartFailed)
			}
		} else if err := c.connectRCON(ctx, w, cid); err != nil {
			return err
		}
		t := time.NewTimer(c.rconRetry)
		select {
		case <-t.C:
		case <-deadline:
			t.Stop()
			return fmt.Errorf("%w: it is in game but did not answer over RCON within %s", ErrStartFailed, durationWords(c.startTimeout))
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
}

// connectRCON makes the RCON client once the container has an address.
func (c *Controller) connectRCON(ctx context.Context, w *worldRun, cid string) error {
	cont, err := c.docker.InspectContainer(ctx, cid)
	if err != nil {
		return err
	}
	ip := containerIP(cont)
	if ip == "" {
		return nil
	}
	pw, err := c.worlds.RCONPassword(w.id)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.containerID == cid && w.rc == nil {
		w.rc = c.newRCON(ip, pw)
	}
	return nil
}

// MaxStopCountdown is the longest countdown a stop may ask for.
const MaxStopCountdown = 10 * time.Minute

// ErrCountdown refuses a countdown outside 0 to MaxStopCountdown.
var ErrCountdown = errors.New("the stop countdown must be between 0 and 10 minutes")

// StopOptions changes how a graceful stop warns players (SPEC 7.6: the
// countdown is configurable per action, R40).
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
// stopped (R28).
func (c *Controller) ForceStop(ctx context.Context, id string, by Actor) error {
	if err := c.audit(ctx, by, AuditForceStop, id, ""); err != nil {
		return err
	}
	w := c.world(id)
	w.mu.Lock()
	cancel, done := w.opCancel, w.opDone
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
	}
	defer c.pokeMonitor()
	cont, found, err := c.ourContainer(ctx, id)
	if err != nil {
		return err
	}
	status := cont.State.Status
	if !found || (status != "running" && status != "restarting") {
		if interrupted {
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
	return nil
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
	if err := c.audit(ctx, by, AuditRestart, id, opts.auditDetail()); err != nil {
		return err
	}
	if _, err := c.precheck(id, opts); err != nil {
		return err
	}
	ctx, o := c.beginOp(ctx, c.world(id), AuditRestart, stopSteps, startStepsFor(opts))
	err = c.stopLocked(ctx, o, countdown)
	if errors.Is(err, ErrNotRunning) {
		err = nil
	}
	if err == nil {
		err = c.startLocked(ctx, o, by, opts)
	}
	return o.end(err)
}

// PullImage downloads the pinned image for the world's game version, as an
// action of its own (R7). It is audited, and shown as a step.
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
