package game

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gamedir"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// oldAdoptionMarker is a file an older build wrote in the game folder. The
// database row is the only record now (SPEC 4.8); startup removes a
// leftover one and nothing reads it.
const oldAdoptionMarker = ".foreman-adoption"

// Steps of a first adoption besides the stop and start steps (SPEC 4.8).
const (
	StepContainerCheck = "Check the game container"
	StepAdoptWorld     = "Copy the game folder into a new world and snapshot it"
	StepKeepOriginals  = "Move the original folders into a pre-foreman folder"
)

// ErrAdoptionInProgress refuses a first adoption while another runs, or
// while one that Foreman stopped during is still recorded (SPEC 4.8).
var ErrAdoptionInProgress = errors.New("an adoption of the game folder is already in progress. Wait for it to finish, or restart Foreman to recover one it stopped during")

// adoptionWaitsForStop is ErrAdoptionInProgress when startup found a
// cut-over left part way but could not finish it with the game running.
type adoptionWaitsForStop struct{}

func (adoptionWaitsForStop) Error() string {
	return "an adoption Foreman stopped during is still recorded, and the game container was running when Foreman started, so it could not be finished. Stop the game container, then restart Foreman to finish the adoption"
}

func (adoptionWaitsForStop) Is(target error) bool { return target == ErrAdoptionInProgress }

// AdoptionDefaults is what the adoption form starts from (SPEC 4.8).
type AdoptionDefaults struct {
	// Unadopted is true when the game folder still has its own saves, mods
	// and config, so the form applies.
	Unadopted bool
	// GameVersion is the container's VERSION, or "" when it has none.
	GameVersion string
	// SpaceAge is what the container's DLC_SPACE_AGE turns on.
	SpaceAge bool
	// Running is true when the adoption would stop the game first.
	Running bool
}

// AdoptionDefaults reads the defaults for the adoption form. It only
// inspects.
func (c *Controller) AdoptionDefaults(ctx context.Context) (AdoptionDefaults, error) {
	if c.adopted == nil {
		return AdoptionDefaults{}, ErrNotAdoptedMode
	}
	st, err := c.layout()
	if err != nil {
		return AdoptionDefaults{}, err
	}
	cont, err := c.inspectAdopted(ctx)
	if err != nil {
		return AdoptionDefaults{}, err
	}
	out := AdoptionDefaults{Unadopted: st.Kind == gamedir.Unadopted, GameVersion: c.adoptedViewNow().version}
	out.SpaceAge, _ = containerSpaceAge(cont)
	switch cont.State.Status {
	case "running", "restarting", "paused":
		out.Running = true
	}
	return out, nil
}

// Adoption is a first adoption that ReserveAdoption checked and gave an id.
type Adoption struct {
	ID, Name, GameVersion string
	SpaceAge              bool
}

// ReserveAdoption checks a first adoption, inspect only, and reserves the
// new world's id (SPEC 4.8). Nothing is stopped or moved. Pass the result
// to Adopt; the id lets a page follow the adoption as it runs.
func (c *Controller) ReserveAdoption(ctx context.Context, name, gameVersion string, spaceAge bool) (Adoption, error) {
	if c.adopted == nil {
		return Adoption{}, ErrNotAdoptedMode
	}
	if c.switches == nil {
		return Adoption{}, ErrSwitchUnavailable
	}
	if !world.ValidGameVersion(gameVersion) {
		return Adoption{}, world.ErrBadGameVersion
	}
	if err := c.adoptionBusy(ctx); err != nil {
		return Adoption{}, err
	}
	if _, err := c.checkUnadopted(ctx, gameVersion, spaceAge); err != nil {
		return Adoption{}, err
	}
	id, err := c.worlds.ReserveID(ctx, name)
	if err != nil {
		return Adoption{}, err
	}
	return Adoption{ID: id, Name: name, GameVersion: gameVersion, SpaceAge: spaceAge}, nil
}

// checkUnadopted refuses an adoption the folder or container does not
// allow.
func (c *Controller) checkUnadopted(ctx context.Context, gameVersion string, spaceAge bool) (docker.Container, error) {
	st, err := c.layout()
	if err != nil {
		return docker.Container{}, err
	}
	switch st.Kind {
	case gamedir.Broken:
		return docker.Container{}, &LayoutError{Path: st.Path, Reason: st.Reason}
	case gamedir.Unadopted:
	default:
		return docker.Container{}, ErrAlreadyAdopted
	}
	cont, err := c.checkContainer(ctx)
	if err != nil {
		return docker.Container{}, err
	}
	if err := checkSpaceAge(cont, gameVersion, spaceAge); err != nil {
		return docker.Container{}, err
	}
	return cont, nil
}

func (c *Controller) adoptSteps() [][]string {
	return [][]string{{StepContainerCheck}, stopSteps, {StepAdoptWorld, StepKeepOriginals, StepPresent}, c.startSteps(StartOptions{})}
}

// Adopt turns the game folder's own saves, mods and config into the world
// a names, and makes it active (SPEC 4.8): it checks the container, stops
// a running game gracefully, builds the world with a "Before adoption"
// snapshot, moves the originals into pre-foreman-<time>/, and starts the
// game again if it was running. It is shown as an operation on the new
// world. Like Start it can take StartTimeout; run it with a context that
// outlives the request.
func (c *Controller) Adopt(ctx context.Context, a Adoption, by Actor, stop StopOptions) error {
	if c.adopted == nil {
		return ErrNotAdoptedMode
	}
	if c.switches == nil {
		return ErrSwitchUnavailable
	}
	countdown, err := stopCountdown(stop)
	if err != nil {
		return err
	}
	unlock, err := c.worlds.Lock(a.ID)
	if err != nil {
		return err
	}
	defer unlock()
	release, err := c.claimAdoption(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := c.audit(ctx, by, AuditAdopt, a.ID, adoptDetail(a)); err != nil {
		return err
	}
	ctx, o := c.beginOp(ctx, c.world(a.ID), AuditAdopt, c.adoptSteps()...)
	err = c.adoptLocked(ctx, o, a, by, countdown)
	return o.end(err)
}

// adoptionBusy refuses while an adoption runs in this Foreman or the
// database records one, before anything is stopped (SPEC 4.8).
func (c *Controller) adoptionBusy(ctx context.Context) error {
	c.mu.Lock()
	running := c.adopting
	c.mu.Unlock()
	if running {
		return ErrAdoptionInProgress
	}
	if _, found, err := c.switches.Adoption(ctx); err != nil {
		return err
	} else if found {
		c.mu.Lock()
		waits := c.adoptionWaits
		c.mu.Unlock()
		if waits {
			return adoptionWaitsForStop{}
		}
		return ErrAdoptionInProgress
	}
	return nil
}

// claimAdoption marks an adoption as running, or refuses when one is.
// Call release when it ends.
func (c *Controller) claimAdoption(ctx context.Context) (release func(), err error) {
	if err := c.adoptionBusy(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.adopting {
		return nil, ErrAdoptionInProgress
	}
	c.adopting = true
	return func() {
		c.mu.Lock()
		c.adopting = false
		c.mu.Unlock()
	}, nil
}

func adoptDetail(a Adoption) string {
	d := a.Name + ", Factorio " + a.GameVersion
	if a.SpaceAge {
		d += ", Space Age"
	}
	return d
}

func (c *Controller) adoptLocked(ctx context.Context, o *opRun, a Adoption, by Actor, countdown time.Duration) error {
	o.begin(StepContainerCheck)
	cont, err := c.checkUnadopted(ctx, a.GameVersion, a.SpaceAge)
	if err != nil {
		return err
	}
	o.done(StepContainerCheck)

	wasRunning, err := c.stopUnadopted(ctx, o, cont, countdown)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Once the folder starts to change, the adoption runs to its end even
	// if a forced stop arrives, as a restore does.
	work := context.WithoutCancel(ctx)

	o.begin(StepAdoptWorld)
	if err := c.recordAdoption(work, a.ID); err != nil {
		return err
	}
	if err := c.buildAdopted(work, a.ID, a.Name, a.GameVersion, a.SpaceAge, by.Username); err != nil {
		c.forgetAdoption(work)
		return err
	}
	o.done(StepAdoptWorld)

	o.begin(StepKeepOriginals)
	pre, err := gamedir.CutOver(c.adopted.dir, a.ID, c.now())
	if err != nil {
		// The admin sees this adoption fail, so startup must not finish
		// it; gamedir.Recover still repairs a half-moved folder (SPEC 4.8).
		c.forgetAdoption(work)
		return err
	}
	c.log.Info("adopted the game folder; its original folders are kept", "world", a.ID, "kept", pre)
	o.done(StepKeepOriginals)

	o.begin(StepPresent)
	if err := c.finishAdoption(work, a.ID); err != nil {
		return err
	}
	o.done(StepPresent)

	if !wasRunning {
		o.skip(c.startSteps(StartOptions{})...)
		return nil
	}
	return c.startLocked(ctx, o, by, StartOptions{})
}

// buildAdopted builds worlds/<id> from the game folder, with its snapshot.
func (c *Controller) buildAdopted(ctx context.Context, id, name, gameVersion string, spaceAge bool, by string) error {
	root, err := os.OpenRoot(c.adopted.dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	_, err = c.worlds.Adopt(ctx, root, id, name, gameVersion, spaceAge, by)
	return err
}

// finishAdoption makes the adopted world active and forgets the adoption.
func (c *Controller) finishAdoption(ctx context.Context, id string) error {
	if err := c.switches.SetActiveWorld(ctx, id, c.now()); err != nil {
		return err
	}
	if _, err := c.layout(); err != nil {
		c.log.Warn("cannot read the game folder after adoption", "error", err)
	}
	c.forgetAdoption(ctx)
	return nil
}

// recordAdoption records the world being adopted in the app database, which
// the game container cannot reach (SPEC 4.8, 14).
func (c *Controller) recordAdoption(ctx context.Context, id string) error {
	if err := c.switches.SetAdoption(ctx, id, c.now()); errors.Is(err, store.ErrAdoptionExists) {
		return ErrAdoptionInProgress
	} else if err != nil {
		return err
	}
	return nil
}

// forgetAdoption clears the database record. A failure is logged: startup
// forgets an adoption that never began.
func (c *Controller) forgetAdoption(ctx context.Context) {
	if err := c.switches.ClearAdoption(ctx); err != nil {
		c.log.Warn("could not clear the adoption record from the database", "error", err)
	}
}

// stopUnadopted stops the game the unadopted folder runs, gracefully, with
// the password in the folder's own config/rconpw (SPEC 4.8, 7.6). It
// reports whether the game was running, so the adoption starts it again.
func (c *Controller) stopUnadopted(ctx context.Context, o *opRun, cont docker.Container, countdown time.Duration) (bool, error) {
	switch cont.State.Status {
	case "paused":
		o.skip(stopSteps...)
		return false, ErrPaused
	case "running":
	case "restarting":
		// Docker is restarting it after a crash: there is no game to save,
		// and it is not started again after.
		o.skip(StepWarn, StepSave)
		return false, c.stopAdoptedContainer(ctx, o, cont.ID)
	default:
		o.skip(stopSteps...)
		return false, nil
	}
	pw, err := c.unadoptedPassword()
	if err != nil {
		return false, fmt.Errorf("%w (%w)", ErrAdoptRCON, err)
	}
	addr, err := c.rconAddress(ctx, cont)
	if err != nil {
		return false, err
	}
	if addr == "" {
		return false, ErrAdoptRCON
	}
	rc := rcon.New(addr, pw)
	defer closeRCON(rc)

	o.show(StateStopping)
	o.begin(StepWarn)
	names, err := c.online(ctx, rc)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, ErrAdoptRCON
	}
	if len(names) == 0 || countdown == 0 {
		o.skip(StepWarn)
	} else {
		if err := c.warnPlayers(ctx, rc, countdown); err != nil {
			return false, err
		}
		o.done(StepWarn)
	}
	o.begin(StepSave)
	if err := c.saveUnadopted(ctx, cont.ID, rc); err != nil {
		return false, err
	}
	o.done(StepSave)
	return true, c.stopAdoptedContainer(ctx, o, cont.ID)
}

func (c *Controller) stopAdoptedContainer(ctx context.Context, o *opRun, cid string) error {
	o.show(StateStopping)
	o.begin(StepStopContainer)
	if err := c.docker.StopContainer(ctx, cid, StopGraceSeconds); err != nil && !errors.Is(err, docker.ErrNotModified) {
		return err
	}
	o.done(StepStopContainer)
	return nil
}

// saveUnadopted sends /server-save and waits for the log to say it
// finished. No world follows this game's log, so it reads the log itself,
// from just before the command.
func (c *Controller) saveUnadopted(ctx context.Context, cid string, rc *rcon.Client) error {
	saveCtx, cancel := context.WithTimeout(ctx, c.saveTimeout)
	defer cancel()
	s, err := c.docker.Logs(saveCtx, cid, docker.LogOptions{Follow: true, Since: time.Now()})
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	if err := c.exec(saveCtx, rc, "/server-save"); err != nil {
		if ctx.Err() == nil && saveCtx.Err() != nil {
			return ErrSaveTimeout
		}
		return err
	}
	for {
		l, err := s.Next()
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return ctx.Err()
			case saveCtx.Err() != nil:
				return ErrSaveTimeout
			}
			return err
		}
		if ParseLine(l.Text).Kind == KindSaveFinished {
			return nil
		}
	}
}

// unadoptedPassword is FOREMAN_RCON_PASSWORD, else the game folder's own
// config/rconpw, which the image made (SPEC 4.10).
func (c *Controller) unadoptedPassword() (secret.Secret, error) {
	if env, ok := c.envRCONPassword(); ok {
		return env, nil
	}
	root, err := os.OpenRoot(c.adopted.dir)
	if err != nil {
		return secret.Secret{}, err
	}
	defer func() { _ = root.Close() }()
	return world.ReadRCONPassword(root)
}

// removeOldAdoptionMarker removes the marker file an older build left in
// the game folder when it is a regular file; anything else of that name is
// the game's and is left alone.
func (c *Controller) removeOldAdoptionMarker() {
	root, err := os.OpenRoot(c.adopted.dir)
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(oldAdoptionMarker)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if err := root.Remove(oldAdoptionMarker); err != nil {
		c.log.Warn("could not remove an adoption marker an older Foreman left in the game folder", "error", err)
	}
}

// Recover finishes or reports what a stop of Foreman left part way. Call it
// once at startup, before Run. It never starts or stops a game
// (SPEC 3.8). In adopted mode, while the game is stopped, it first repairs
// the game folder and finishes a first adoption (SPEC 4.8); then it
// recovers switches (SPEC 7.10); then it makes the active world in the
// database match the world the folder presents, and logs the correction.
//
// Each fault is logged and recovery goes on: none of them stops Foreman
// from serving pages (SPEC 7.1), and the world it affects is left
// unstarted, with a notice where there is one. It returns an error only
// when ctx ends, which means startup itself was cancelled.
func (c *Controller) Recover(ctx context.Context) error {
	if c.adopted != nil {
		if err := c.recoverGameDir(ctx); err != nil {
			c.log.Error("could not recover the game folder; Foreman serves pages, and server control may refuse until it is fixed", "error", err)
		}
	}
	c.recoverSwitches(ctx)
	if c.adopted != nil {
		if err := c.correctActive(ctx); err != nil {
			c.log.Error("could not make the active world match the game folder", "error", err)
		}
	}
	return ctx.Err()
}

// recoverGameDir forgets an adoption that never began whatever the
// container's state, since that changes only the database record. It
// changes the game folder only while the container is stopped, since the
// game may be reading it.
func (c *Controller) recoverGameDir(ctx context.Context) error {
	c.removeOldAdoptionMarker()
	rec, haveRecord, err := c.switches.Adoption(ctx)
	if err != nil {
		return err
	}
	// Read before gamedir.Recover, which removes a leftover current.new.
	begun := false
	if haveRecord {
		if begun, err = c.cutOverBegun(rec.StartedAt); err != nil {
			return err
		}
		if !begun {
			c.log.Warn("an adoption stopped before the game folder was changed; the folder is unchanged and the world it built is left inactive", "world", rec.WorldID)
			c.forgetAdoption(ctx)
		}
	}
	cont, err := c.inspectAdopted(ctx)
	if err != nil {
		c.log.Warn("cannot read the game container, so the game folder was not checked for a change left part way", "error", err)
		return nil
	}
	switch cont.State.Status {
	case "running", "restarting", "paused":
		// A normal start finds nothing left part way; warn only on evidence.
		if haveRecord || c.changeLeftOver() {
			c.log.Warn("the game container is running, so the game folder was not checked for a change left part way")
		} else {
			c.log.Debug("the game container is running and the game folder shows no change left part way")
		}
		if begun {
			c.mu.Lock()
			c.adoptionWaits = true
			c.mu.Unlock()
		}
		return nil
	}
	if err := gamedir.Recover(c.adopted.dir); err != nil {
		c.log.Warn("could not finish a change to the game folder that was left part way", "error", err)
		return nil
	}
	if !begun {
		return nil
	}
	return c.recoverAdoption(ctx, rec.WorldID)
}

// cutOverBegun reports whether the folder shows that the cut-over of an
// adoption recorded at started had begun: current.new exists, or a
// pre-foreman folder is no older than the record (SPEC 4.8). The record is
// kept to the second, so a folder made in that second counts.
func (c *Controller) cutOverBegun(started time.Time) (bool, error) {
	root, err := os.OpenRoot(c.adopted.dir)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	if _, err := root.Lstat(gamedir.CurrentDir + ".new"); err == nil {
		return true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	d, err := root.Open(".")
	if err != nil {
		return false, err
	}
	names, err := d.Readdirnames(-1)
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if !strings.HasPrefix(n, gamedir.PreForemanPrefix) {
			continue
		}
		info, err := root.Lstat(n)
		if err != nil {
			return false, err
		}
		if info.IsDir() && !info.ModTime().Before(started.Truncate(time.Second)) {
			return true, nil
		}
	}
	return false, nil
}

// recoverAdoption finishes the first adoption of world id, whose cut-over
// had begun, from what is on disk: the snapshot is taken if it is missing
// and the originals are moved aside, as the admin confirmed (SPEC 3.8,
// 4.8).
func (c *Controller) recoverAdoption(ctx context.Context, id string) error {
	st, err := c.layout()
	if err != nil {
		return err
	}
	switch st.Kind {
	case gamedir.Ready:
		c.log.Warn("finished an adoption that Foreman stopped during", "world", id)
		return c.finishAdoption(ctx, id)
	case gamedir.Unadopted:
	default:
		c.log.Warn("an adoption was left part way, but the game folder is not in a state Foreman can finish it from", "world", id, "state", st.Kind.String())
		return nil
	}
	if _, err := c.readMeta(id); errors.Is(err, fs.ErrNotExist) {
		c.log.Warn("an adoption stopped before its world was made; the game folder is unchanged", "world", id)
		c.forgetAdoption(ctx)
		return nil
	} else if err != nil {
		return err
	}
	snaps, err := c.worlds.Snapshots(id)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(snaps, func(s world.Snapshot) bool { return s.Reason == world.ReasonAdoption }) {
		if _, err := c.worlds.AutoSnapshot(id, world.ReasonAdoption, world.LabelBeforeAdoption, ""); err != nil {
			return fmt.Errorf("take the snapshot before adoption of %s: %w", id, err)
		}
	}
	pre, err := gamedir.CutOver(c.adopted.dir, id, c.now())
	if err != nil {
		return err
	}
	c.log.Warn("finished an adoption that Foreman stopped during; the game was not started", "world", id, "kept", pre)
	return c.finishAdoption(ctx, id)
}

// correctActive makes the active world in the database the one the game
// folder presents: the link decides (SPEC 4.8).
func (c *Controller) correctActive(ctx context.Context) error {
	if c.switches == nil {
		return nil
	}
	st, err := c.layout()
	if err != nil || st.Kind != gamedir.Ready {
		return nil
	}
	active, found, err := c.switches.ActiveWorld(ctx)
	if err != nil {
		return err
	}
	if found && active == st.ActiveID {
		return nil
	}
	c.log.Warn("the active world in the database did not match the game folder; it now matches the folder", "was", active, "now", st.ActiveID)
	return c.switches.SetActiveWorld(ctx, st.ActiveID, c.now())
}

// changeLeftOver reports whether the game folder holds a .new link, which
// only a change to the folder stopped part way leaves (gamedir.Recover).
func (c *Controller) changeLeftOver() bool {
	root, err := os.OpenRoot(c.adopted.dir)
	if err != nil {
		return true // cannot tell, so say so
	}
	defer func() { _ = root.Close() }()
	for _, name := range append([]string{gamedir.CurrentDir}, gamedir.FixedLinks...) {
		if _, err := root.Lstat(name + ".new"); !errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}
