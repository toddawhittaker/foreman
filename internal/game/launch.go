package game

import (
	"context"
	"errors"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// launch is what a start needs to run the container it prepared.
type launch struct {
	cid     string
	save    string // the save chosen to load, or ""
	meta    world.Meta
	image   string
	rconPW  secret.Secret
	secrets []secret.Secret
}

// launchLocked starts the world's container, then waits until the game is
// in game and answers over RCON (SPEC 7.3). The caller holds the world's
// lock.
func (c *Controller) launchLocked(ctx context.Context, o *opRun) error {
	p, err := c.prepareContainer(ctx, o)
	if err != nil {
		return err
	}
	o.begin(StepStartContainer)
	return c.runContainer(ctx, o, p)
}

// prepareContainer creates a fresh container for the world (ADR 0003).
func (c *Controller) prepareContainer(ctx context.Context, o *opRun) (launch, error) {
	id := o.w.id
	o.begin(StepImage)
	meta, ref, err := c.gameImage(ctx, id)
	if err != nil {
		return launch{}, err
	}
	o.done(StepImage)

	o.begin(StepCreate)
	network, err := c.gameNetwork(ctx)
	if err != nil {
		return launch{}, err
	}
	old, found, err := c.ourContainer(ctx, id)
	if err != nil {
		return launch{}, err
	}
	if found {
		if s := old.State.Status; s == "running" || s == "restarting" {
			return launch{}, ErrAlreadyRunning
		}
		// A fresh container for every start, so its settings never drift (ADR 0003).
		if err := c.docker.RemoveContainer(ctx, old.ID, false); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return launch{}, err
		}
	}
	rconPW, secrets, err := c.secrets(id)
	if err != nil {
		return launch{}, err
	}
	save, err := c.worlds.PrepareLoad(id)
	if err != nil {
		return launch{}, err
	}
	cid, err := c.docker.CreateContainer(ctx, ContainerName(id), c.containerSpec(id, ref, network, meta))
	if errors.Is(err, docker.ErrConflict) {
		return launch{}, ErrForeign
	}
	if err != nil {
		return launch{}, err
	}
	o.done(StepCreate)
	return launch{cid: cid, save: save, meta: meta, image: ref, rconPW: rconPW, secrets: secrets}, nil
}

// runContainer starts the prepared container and waits for the game.
func (c *Controller) runContainer(ctx context.Context, o *opRun, p launch) error {
	w, id, cid := o.w, o.w.id, p.cid
	w.mu.Lock()
	// A forced stop sets its own marks once this operation has ended, so
	// none may be set here after it asked.
	if err := ctx.Err(); err != nil {
		w.mu.Unlock()
		return err
	}
	stale := c.dropContainerLocked(w)
	// The container is fresh: Docker reports it created, with no restarts.
	w.containerID, w.dockerStatus, w.logMark = cid, "created", w.logs.Mark()
	w.seenRestarts, w.crashes, w.lastCrash = 0, nil, time.Time{}
	w.stopping, w.crashLoop, w.crashedRun = false, false, false
	w.run = runState{save: p.save}
	w.joins.Reset()
	w.secrets, w.gameVersion, w.image = p.secrets, p.meta.GameVersion, p.image
	w.mu.Unlock()
	closeRCON(stale)
	o.show(StateStarting)
	c.noteSelfGameSubnet(ctx)
	if err := c.docker.StartContainer(ctx, cid); err != nil {
		return err
	}
	cont, err := c.docker.InspectContainer(ctx, cid)
	if err != nil {
		return err
	}
	// Before the next monitor look, so a new game's range is refused at once.
	c.noteGameSubnets(cont)
	addr := rconAddress(cont)
	w.mu.Lock()
	if err := ctx.Err(); err != nil {
		w.mu.Unlock()
		return err
	}
	// Only a container Docker started can crash.
	w.expectRunning = true
	w.dockerStatus, w.startedAt, w.seenRestarts = cont.State.Status, cont.State.StartedAt, cont.RestartCount
	if addr != "" {
		w.rc = rcon.New(addr, p.rconPW)
	}
	c.followLocked(w, cid, time.Time{})
	c.changedLocked(w, true)
	w.mu.Unlock()
	o.done(StepStartContainer)

	deadline := time.NewTimer(c.startTimeout)
	defer deadline.Stop()
	o.begin(StepLoad)
	if err := c.waitInGame(ctx, w, cid, cont.RestartCount, deadline.C); err != nil {
		return err
	}
	// The chosen save applies once (SPEC 7.11).
	if p.save != "" {
		if err := c.worlds.ClearLoadSave(id, p.save); err != nil {
			c.log.Warn("could not reset which save loads to the newest", "world", id, "error", err)
		}
	}
	// The map exists now, so the next start loads it (SPEC 4.3, 5.6).
	if p.meta.NewMap != nil {
		if err := c.worlds.ClearNewMapHeld(id); err != nil {
			c.log.Warn("could not clear the new-map mark after the first start", "world", id, "error", err)
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

// waitInGame waits for the log to say the game is in game, or that it
// failed to load (SPEC 7.3, 8.7).
// restarts is Docker's restart count when this run began.
func (c *Controller) waitInGame(ctx context.Context, w *worldRun, cid string, restarts int, deadline <-chan time.Time) error {
	poll := time.NewTicker(c.pollInterval)
	defer poll.Stop()
	for {
		w.mu.Lock()
		run, changed := w.run, w.changed
		w.mu.Unlock()
		switch {
		case run.loadFailed:
			return &StartError{Kind: StartLoadFailed, Detail: run.loadError}
		case run.inGame:
			return nil
		}
		select {
		case <-changed:
		case <-poll.C:
			cont, err := c.docker.InspectContainer(ctx, cid)
			if err == nil && (cont.State.Status != "running" || cont.RestartCount > restarts) {
				return c.stoppedWhileLoading(ctx, w, cid)
			}
		case <-deadline:
			return &StartError{Kind: StartLoadTimeout, Timeout: c.startTimeout}
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
				return &StartError{Kind: StartRCONRefused}
			}
		} else if err := c.connectRCON(ctx, w, cid); err != nil {
			return err
		}
		t := time.NewTimer(c.rconRetry)
		select {
		case <-t.C:
		case <-deadline:
			t.Stop()
			return &StartError{Kind: StartRCONTimeout, Timeout: c.startTimeout}
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
	addr := rconAddress(cont)
	if addr == "" {
		return nil
	}
	pw, err := c.worlds.RCONPassword(w.id)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.containerID == cid && w.rc == nil {
		w.rc = rcon.New(addr, pw)
	}
	return nil
}

// The kinds of StartError.
const (
	// StartLoadFailed: the game said it could not load the save; Detail is
	// its own words, or "".
	StartLoadFailed = "load-failed"
	// StartStopped: the game stopped while loading; Reason is why, from
	// its log, when it said.
	StartStopped = "stopped"
	// StartLoadTimeout and StartRCONTimeout: the game did not finish
	// loading, or answer over RCON, within Timeout.
	StartLoadTimeout = "load-timeout"
	StartRCONTimeout = "rcon-timeout"
	// StartRCONRefused: the game refused Foreman's RCON password.
	StartRCONRefused = "rcon-refused"
)

// StartError is ErrStartFailed with the reason, so a page can word it in
// any language (SPEC 22.3). Its text is the log's.
type StartError struct {
	Kind    string
	Detail  string
	Timeout time.Duration
	Reason  LoadReason
}

func (e *StartError) Error() string {
	head := ErrStartFailed.Error()
	switch e.Kind {
	case StartLoadFailed:
		if e.Detail != "" {
			return head + ": it could not load the save (" + e.Detail + ")"
		}
		return head + ": it could not load the save"
	case StartStopped:
		if tail := e.Reason.text(); tail != "" {
			return head + tail
		}
		return head + ": the game stopped while it was loading"
	case StartLoadTimeout:
		return head + ": it did not finish loading within " + durationWords(e.Timeout)
	case StartRCONTimeout:
		return head + ": it is in game but did not answer over RCON within " + durationWords(e.Timeout)
	case StartRCONRefused:
		return head + ": the game is in game, but it refused Foreman's RCON password"
	}
	return head
}

func (e *StartError) Is(target error) bool { return target == ErrStartFailed }
