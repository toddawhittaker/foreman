package game

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The steps an update shows besides the download, stop and start steps
// (SPEC 10.3).
const (
	StepSnapshotUpdate = "Take the snapshot before the update"
	StepSetVersion     = "Set the game version"
)

// updateSteps lists an update's steps in the order of SPEC 10.3. The
// "Before update" snapshot stands in for the start-anyway one, so the start
// shows no snapshot step of its own.
var updateSteps = [][]string{{StepDownload}, stopSteps, {StepSnapshotUpdate, StepSetVersion}, {StepCheck}, launchSteps}

// Update moves the active world to a newer game version (SPEC 10.3). Every
// check that could refuse runs before the game is stopped; a missing image
// is downloaded before the stop too. The game is stopped gracefully, the
// "Before update" snapshot taken, the version set, and the game started
// again only if it was running. A refusal by the pre-start check is a
// *PrecheckError. Run it with a context that outlives the request.
func (c *Controller) Update(ctx context.Context, id, version string, by Actor, stop StopOptions, opts StartOptions) error {
	countdown, err := stopCountdown(stop)
	if err != nil {
		return err
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	meta, err := c.readMeta(id)
	if err != nil {
		return err
	}
	detail := updateDetail(meta.GameVersion, version, opts)
	// Not IsRefusal errors, but still refusals: nothing was done.
	if err := world.CheckUpdateTarget(meta, version); err != nil {
		return c.refused(ctx, by, AuditUpdate, id, detail, err)
	}
	if err := c.checkActive(ctx, id); err != nil {
		return c.refused(ctx, by, AuditUpdate, id, detail, err)
	}
	// A game the stop would refuse is refused before the download.
	if _, err := c.planStop(ctx, c.world(id)); err != nil && !errors.Is(err, ErrNotRunning) {
		return c.refused(ctx, by, AuditUpdate, id, detail, err)
	}
	opts.running = IsRunning(c.Status(id).State)
	if err := c.precheckReadOnly(id, version, opts); err != nil {
		return c.checkFailed(ctx, by, AuditUpdate, id, detail, err)
	}
	if _, err := c.gameNetwork(ctx); err != nil {
		return c.checkFailed(ctx, by, AuditUpdate, id, detail, err)
	}
	opts.confirmed = true
	ref, err := c.imageFor(version)
	if err != nil {
		return err
	}
	if err := c.audit(ctx, by, AuditUpdate, id, detail); err != nil {
		return err
	}
	ctx, o := c.beginOp(ctx, c.world(id), AuditUpdate, updateSteps...)
	return o.end(c.updateLocked(ctx, o, by, version, ref, countdown, opts))
}

func updateDetail(from, to string, opts StartOptions) string {
	d := "from " + from + " to " + to
	if opts.Anyway {
		d += ", start anyway"
	}
	return d
}

// updateLocked runs the update steps. The caller holds the world's lock.
func (c *Controller) updateLocked(ctx context.Context, o *opRun, by Actor, version, ref string, countdown time.Duration, opts StartOptions) error {
	id := o.w.id
	o.begin(StepDownload)
	if _, err := c.docker.InspectImage(ctx, ref); errors.Is(err, docker.ErrNotFound) {
		if err := c.docker.PullImage(ctx, ref); err != nil {
			return fmt.Errorf("download %s: %w", ref, err)
		}
		o.done(StepDownload)
	} else if err != nil {
		return err
	} else {
		o.skip(StepDownload)
	}

	err := c.stopLocked(ctx, o, countdown)
	wasRunning := err == nil
	if err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}

	o.begin(StepSnapshotUpdate)
	if _, err := c.worlds.AutoSnapshot(id, world.ReasonGameUpdate, world.LabelBeforeUpdate, by.Username); err != nil {
		return fmt.Errorf("take the snapshot before the update: %w", err)
	}
	o.done(StepSnapshotUpdate)

	o.begin(StepSetVersion)
	// Once the snapshot is taken the version is set, even if a forced stop
	// arrives, so the world never sits between versions.
	if err := c.worlds.SetGameVersionHeld(id, version); err != nil {
		return err
	}
	o.done(StepSetVersion)

	if !wasRunning {
		o.skip(StepCheck)
		o.skip(launchSteps...)
		return nil
	}
	o.begin(StepCheck)
	if _, err := c.precheck(id, opts); err != nil {
		return err
	}
	o.done(StepCheck)
	return c.launchLocked(ctx, o)
}

// Rollback restores the newest "Before update" snapshot from an older game
// version than the world's (SPEC 10.4), as a restore does, and returns the
// "Before restore" snapshot as the undo. A running game is stopped first
// and started again after; a missing old image refuses that start (SPEC
// 3.7), with the files already restored and nothing lost.
func (c *Controller) Rollback(ctx context.Context, id string, by Actor, stop StopOptions) (world.Snapshot, error) {
	countdown, err := stopCountdown(stop)
	if err != nil {
		return world.Snapshot{}, err
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return world.Snapshot{}, err
	}
	defer unlock()
	snap, version, err := c.worlds.RollbackTarget(id)
	if err != nil {
		return world.Snapshot{}, err
	}
	if err := c.audit(ctx, by, AuditRollback, id, "to "+version+" from snapshot "+snap.Name); err != nil {
		return world.Snapshot{}, err
	}
	ctx, o := c.beginOp(ctx, c.world(id), AuditRollback, stopSteps, []string{StepRestore}, startSteps(StartOptions{}))
	before, err := c.restoreLocked(ctx, o, by, snap.Name, countdown)
	return before, o.end(err)
}
