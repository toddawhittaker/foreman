package game

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The steps a mod change shows besides the stop, check and start steps
// (SPEC 9.2 to 9.6, 10.3).
const (
	StepDownloadMods = "Download the mods"
	StepSnapshotMods = "Take the snapshot before the mod change"
	StepApplyMods    = "Change the mods"
)

// modSteps lists a mod change's steps in the order of an update (SPEC
// 10.3): the download comes first, so a failed one costs no downtime.
var modSteps = [][]string{{StepDownloadMods}, stopSteps, {StepSnapshotMods, StepApplyMods}, {StepCheck}, launchSteps}

// ErrNoModFetcher refuses a change that needs downloads when Foreman has no
// way to fetch them, such as when no factorio.com account is set (SPEC 9.11).
var ErrNoModFetcher = errors.New("mod downloads are not set up. Enter the factorio.com username and token first")

// ModFetcher downloads one release into dst and checks it against the
// portal's checksum (SPEC 9.12). An adapter around modportal.Client fills in
// the account, so game does not handle it.
type ModFetcher interface {
	Download(ctx context.Context, r modportal.Release, dst *os.File) error
}

// SetModFetcher sets the fetcher ApplyMods downloads with. Without one, a
// change that needs a download is refused.
func (c *Controller) SetModFetcher(f ModFetcher) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.modFetcher = f
}

func (c *Controller) fetcher() ModFetcher {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.modFetcher
}

// ModDownload is one release to fetch and the mod it belongs to.
type ModDownload struct {
	Mod     string
	Release modportal.Release
}

// ModOp is one mod change (SPEC 9.2 to 9.6). Every kind of change, from an
// install to a settings upload, is one of these.
type ModOp struct {
	// Downloads are fetched first; Change.Add gets their staged names after
	// any it already holds, such as an uploaded zip.
	Downloads []ModDownload
	Change    world.ModChange
	// Fingerprint is the mods folder the plan was made against. A change to
	// the folder since is refused with world.ErrModsChanged (SPEC 17.8).
	Fingerprint string
	// Summary is what the change does, for the audit entry. It holds mod
	// names and versions, never a secret.
	Summary string
}

// ApplyMods makes one mod change to a world, whether or not it is the
// active one (SPEC 9.2 to 9.6). It is shaped like Update (SPEC 10.3): the
// downloads come first, and a failed download or checksum leaves the game
// running and the world untouched. Then a running game is warned and
// stopped, the "Before mod change" snapshot is taken, the change applied
// and the pre-start check run, and the game is started again only if it was
// running. A plan that no longer fits the mods folder is refused with
// world.ErrModsChanged before the stop. A refusal by the pre-start check,
// after the change, is a *PrecheckError; the world then stays stopped with
// the snapshot to undo. Run it with a context that outlives the request.
func (c *Controller) ApplyMods(ctx context.Context, id string, by Actor, op ModOp, stop StopOptions) error {
	countdown, err := stopCountdown(stop)
	if err != nil {
		return err
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := c.readMeta(id); err != nil {
		return err
	}
	// Whatever the outcome, no staged file outlives the operation; the
	// world layer has removed the ones it placed.
	defer func() {
		for _, name := range op.Change.Add {
			_ = c.worlds.RemoveStaged(name)
		}
	}()

	detail := op.Summary
	if got, err := c.worlds.ModsFingerprint(id); err != nil {
		return err
	} else if got != op.Fingerprint {
		return c.refused(ctx, by, AuditMods, id, detail, world.ErrModsChanged)
	}
	// A game the stop would refuse is refused before the download.
	if _, err := c.planStop(ctx, c.world(id)); err != nil && !errors.Is(err, ErrNotRunning) {
		return c.refused(ctx, by, AuditMods, id, detail, err)
	}
	// Only a game started again joins the network.
	if IsRunning(c.Status(id).State) {
		if _, err := c.gameNetwork(ctx); err != nil {
			return c.checkFailed(ctx, by, AuditMods, id, detail, err)
		}
	}
	if len(op.Downloads) > 0 && c.fetcher() == nil {
		return c.refused(ctx, by, AuditMods, id, detail, ErrNoModFetcher)
	}
	if err := c.audit(ctx, by, AuditMods, id, detail); err != nil {
		return err
	}
	ctx, run := c.beginOpOn(ctx, []*worldRun{c.world(id)}, newOperation(AuditMods, modSteps...))
	staged, err := c.modsLocked(ctx, run, by, op, countdown)
	// The downloads' staged files go too, whatever the outcome.
	for _, name := range staged {
		_ = c.worlds.RemoveStaged(name)
	}
	return run.end(err)
}

// modsLocked runs the mod change steps and returns the staged files the
// downloads made. The caller holds the world's lock.
func (c *Controller) modsLocked(ctx context.Context, o *opRun, by Actor, op ModOp, countdown time.Duration) (staged []string, err error) {
	id := o.w.id
	staged, err = c.downloadMods(ctx, o, op.Downloads)
	if err != nil {
		return staged, err
	}
	change := op.Change
	change.Add = append(slices.Clone(op.Change.Add), staged...)

	err = c.stopLocked(ctx, o, countdown)
	wasRunning := err == nil
	if err != nil && !errors.Is(err, ErrNotRunning) {
		return staged, err
	}

	o.begin(StepSnapshotMods)
	if _, err := c.worlds.AutoSnapshot(id, world.ReasonMods, world.LabelBeforeMods, by.Username); err != nil {
		return staged, fmt.Errorf("take the snapshot before the mod change: %w", err)
	}
	o.done(StepSnapshotMods)

	o.begin(StepApplyMods)
	// The game may write its mod files as it exits. The plan was compared
	// before the stop and Foreman holds the lock, so what the folder holds
	// now is what the plan was made against.
	fingerprint := op.Fingerprint
	if wasRunning {
		if fingerprint, err = c.worlds.ModsFingerprint(id); err != nil {
			return staged, err
		}
	}
	if err := c.worlds.ApplyModsHeld(id, change, fingerprint); err != nil {
		return staged, err
	}
	o.done(StepApplyMods)

	if !wasRunning {
		o.skip(StepCheck)
		o.skip(launchSteps...)
		return staged, nil
	}
	o.begin(StepCheck)
	if _, err := c.precheck(id, StartOptions{}); err != nil {
		return staged, err
	}
	o.done(StepCheck)
	return staged, c.launchLocked(ctx, o)
}

// downloadMods fetches each release into a staged file and returns the
// staged names, also when it fails, so the caller can remove them. Each zip
// must be the mod and version the plan asked for.
func (c *Controller) downloadMods(ctx context.Context, o *opRun, downloads []ModDownload) (staged []string, err error) {
	if len(downloads) == 0 {
		o.skip(StepDownloadMods)
		return nil, nil
	}
	o.begin(StepDownloadMods)
	dir := c.worlds.Dirs().Worlds
	fetcher := c.fetcher()
	for _, d := range downloads {
		// Nothing is known of a download's size before it starts, so the
		// disk is checked before and after each one (SPEC 6.11).
		if err := world.CheckSpace(dir, 0); err != nil {
			return staged, err
		}
		name, err := c.fetchOne(ctx, fetcher, d)
		if name != "" {
			staged = append(staged, name)
		}
		if err != nil {
			return staged, fmt.Errorf("download %s %s: %w", d.Mod, d.Release.Version, err)
		}
		if err := world.CheckSpace(dir, 0); err != nil {
			return staged, err
		}
	}
	o.done(StepDownloadMods)
	return staged, nil
}

// fetchOne downloads one release into a new staged file and returns its
// name, also on failure when the file was created.
func (c *Controller) fetchOne(ctx context.Context, fetcher ModFetcher, d ModDownload) (string, error) {
	f, name, err := c.worlds.StageMod()
	if err != nil {
		return "", err
	}
	err = fetcher.Download(ctx, d.Release, f)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return name, err
	}
	info, err := world.ReadModZip(c.worlds.StagedPath(name))
	if err != nil {
		return name, err
	}
	if info.Name != d.Mod || info.Version != d.Release.Version {
		return name, fmt.Errorf("the file holds %s %s, not the release asked for", info.Name, info.Version)
	}
	return name, nil
}
