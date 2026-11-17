package game

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// exec runs one RCON command within the per-call limit. A failure is
// ErrRCONUnreachable, with RCON's own reason, which never holds the
// command or the password.
func (c *Controller) exec(ctx context.Context, rc *rcon.Client, command string) error {
	_, err := c.execReply(ctx, rc, command)
	return err
}

func (c *Controller) execReply(ctx context.Context, rc *rcon.Client, command string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.rconCallLimit)
	defer cancel()
	reply, err := rc.Exec(callCtx, command)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w (%w)", ErrRCONUnreachable, err)
	}
	return reply, nil
}

// online asks the game who is on (SPEC 8.1).
func (c *Controller) online(ctx context.Context, rc *rcon.Client) ([]string, error) {
	reply, err := c.execReply(ctx, rc, "/players online")
	if err != nil {
		return nil, err
	}
	return ParseOnline(reply)
}

// liveRCON returns the RCON client of a game that is in game. A game that
// is not running, or not yet in game, is ErrNotRunning. A game that is in
// game but has no RCON client, such as after Foreman restarted and the
// sidecar's network is gone (SPEC 4.13), is ErrRCONUnreachable: it runs, so
// "not running" would be wrong (SPEC 8.4).
func (c *Controller) liveRCON(w *worldRun) (*rcon.Client, []secret.Secret, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dockerStatus != "running" || !w.run.inGame {
		return nil, nil, ErrNotRunning
	}
	if w.rc == nil {
		return nil, nil, ErrRCONUnreachable
	}
	return w.rc, w.secrets, nil
}

// Players lists who is online now and for how long (SPEC 8.1). RCON
// decides who is on; the log's join lines only say since when (SPEC 8.1).
func (c *Controller) Players(ctx context.Context, id string) ([]Player, error) {
	w := c.world(id)
	rc, _, err := c.liveRCON(w)
	if err != nil {
		return nil, err
	}
	names, err := c.online(ctx, rc)
	if err != nil {
		return nil, err
	}
	return w.joins.Online(names, c.now()), nil
}

// Console sends one command the admin typed and returns the game's reply
// (SPEC 8.4). The command is recorded in the audit log before it is sent,
// and is not sent if it cannot be recorded (SPEC 12.9). auditText is the
// command with any secret argument already replaced with Mask by the
// caller (SPEC 8.4, 14.8). The world's known passwords are also removed
// from it here, and from the reply. shown is auditText as recorded, for
// the page's history.
func (c *Controller) Console(ctx context.Context, id string, by Actor, command, auditText string) (shown, reply string, err error) {
	w := c.world(id)
	rc, secrets, err := c.liveRCON(w)
	if err != nil {
		return "", "", err
	}
	shown = Redact(auditText, secrets...)
	if err := c.audit(ctx, by, AuditConsole, id, shown); err != nil {
		return "", "", err
	}
	reply, err = c.execReply(ctx, rc, command)
	if err != nil {
		return "", "", err
	}
	return shown, Redact(reply, secrets...), nil
}

// saveCount follows the saves the game logs. The game writes one save at
// a time, autosaves and /server-save alike, so the nth save to end is the
// nth to begin. With fork saving a /server-save asked while another save is
// still being written waits behind it, and the first "Saving finished"
// after the command ends that earlier save.
type saveCount struct {
	starts, ends int // saves begun, and ended by a finish or a failure
	server       int // /server-save starts
	lastServer   int // which start, counted in starts, was the newest /server-save
	// lastServerAt is Docker's time of the newest /server-save start, zero
	// when the line came without one.
	lastServerAt time.Time
	failed       int // saves whose writing process died or failed
}

func (s *saveCount) observe(ev Event) {
	k := ev.Kind
	switch k {
	case KindSaveStarted:
		s.starts++
	case KindServerSaveStarted:
		s.starts++
		s.server++
		s.lastServer = s.starts
		s.lastServerAt = ev.Time
	case KindSaveFinished, KindSaveFailed:
		s.ends++
		if k == KindSaveFailed {
			s.failed++
		}
		// A save begun before the log was followed was never counted.
		s.starts = max(s.starts, s.ends)
	}
}

// runEnded ends, as failed, the saves the game was writing when its run
// ended: the game logs nothing more for them.
func (s *saveCount) runEnded() {
	if s.starts > s.ends {
		s.failed += s.starts - s.ends
		s.ends = s.starts
	}
}

// saveMark is what a save waits from: the /server-save starts and failed
// saves seen before the command was sent, and when it was sent. A start
// the follower reads late, from a /server-save sent just before, counts
// as read only when Docker stamped it at or after sent.
type saveMark struct {
	server, failed int
	sent           time.Time
}

func (w *worldRun) saveMarkLocked(sent time.Time) saveMark {
	return saveMark{server: w.save.server, failed: w.save.failed, sent: sent}
}

// saveGame sends /server-save and waits for the log to say the save
// finished (SPEC 6.10, 7.6).
func (c *Controller) saveGame(ctx context.Context, w *worldRun) error {
	w.mu.Lock()
	// Docker stamps lines with the host's wall clock, so the send time is
	// read from that clock, not the controller's, with no margin: a daemon
	// clock behind this one can only make the save time out, never end the
	// wait on an earlier save.
	rc, mark := w.rc, w.saveMarkLocked(time.Now())
	w.mu.Unlock()
	if rc == nil {
		return ErrRCONUnreachable
	}
	if err := c.exec(ctx, rc, "/server-save"); err != nil {
		return err
	}
	return c.waitSaved(ctx, w, mark)
}

// waitSaved waits until a /server-save begun after mark has finished. A
// save that fails meanwhile ends the wait with ErrSaveFailed: the game
// quits when its save process dies.
func (c *Controller) waitSaved(ctx context.Context, w *worldRun, mark saveMark) error {
	deadline := time.NewTimer(c.saveTimeout)
	defer deadline.Stop()
	for {
		w.mu.Lock()
		s, changed := w.save, w.changed
		w.mu.Unlock()
		switch {
		case s.failed > mark.failed:
			return ErrSaveFailed
		case s.server > mark.server && s.ends >= s.lastServer &&
			(s.lastServerAt.IsZero() || !s.lastServerAt.Before(mark.sent)):
			return nil
		}
		select {
		case <-changed:
		case <-deadline.C:
			return ErrSaveTimeout
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// saveRunning saves the world's game when it runs, so the files on disk hold
// the current game (SPEC 6.10), and reports whether it ran. A game that is
// starting or restarting is ErrNotReady. The caller holds the world's lock.
func (c *Controller) saveRunning(ctx context.Context, id string) (running bool, err error) {
	cont, found, err := c.ourContainer(ctx, id)
	if err != nil || !found {
		return false, err
	}
	switch cont.State.Status {
	case "running":
		w := c.world(id)
		w.mu.Lock()
		ready := w.containerID == cont.ID && w.run.inGame
		w.mu.Unlock()
		if !ready {
			return false, ErrNotReady
		}
		return true, c.saveGame(ctx, w)
	case "restarting":
		return false, ErrNotReady
	}
	return false, nil
}

// Snapshot takes a manual snapshot of the world (SPEC 6.3). When the game
// is running it saves first, so the snapshot holds the current game
// (SPEC 6.10). It is recorded in the audit log first, and not taken when
// the entry cannot be written (SPEC 12.9).
func (c *Controller) Snapshot(ctx context.Context, id string, by Actor, label string) (world.Snapshot, error) {
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return world.Snapshot{}, err
	}
	defer unlock()
	if err := c.audit(ctx, by, AuditSnapshot, id, label); err != nil {
		return world.Snapshot{}, err
	}
	if _, err := c.saveRunning(ctx, id); err != nil {
		return world.Snapshot{}, err
	}
	return c.worlds.TakeSnapshotHeld(id, label, by.Username)
}

// Restore puts the world back as a snapshot recorded it (SPEC 6.5). A
// running game is stopped gracefully first and started again after; the
// world package takes the "Before restore" snapshot, which is returned as
// the undo, with any error.
func (c *Controller) Restore(ctx context.Context, id string, by Actor, name string, stop StopOptions) (world.Snapshot, error) {
	countdown, err := stopCountdown(stop)
	if err != nil {
		return world.Snapshot{}, err
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return world.Snapshot{}, err
	}
	defer unlock()
	// A missing snapshot is refused before the game is stopped.
	if _, err := c.worlds.Snapshot(id, name); err != nil {
		return world.Snapshot{}, err
	}
	ctx, o := c.beginOp(ctx, c.world(id), ActionRestore, stopSteps, []string{StepRestore}, startSteps(StartOptions{}))
	before, err := c.restoreLocked(ctx, o, by, name, countdown)
	return before, o.end(err)
}

func (c *Controller) restoreLocked(ctx context.Context, o *opRun, by Actor, name string, countdown time.Duration) (world.Snapshot, error) {
	err := c.stopLocked(ctx, o, countdown)
	wasRunning := err == nil
	if err != nil && !errors.Is(err, ErrNotRunning) {
		return world.Snapshot{}, err
	}
	o.begin(StepRestore)
	// Once the files are being swapped the restore runs to its end, even if
	// a forced stop arrives.
	before, err := c.worlds.RestoreHeld(context.WithoutCancel(ctx), o.w.id, name, by.Username)
	if err != nil {
		return before, err
	}
	o.done(StepRestore)
	if !wasRunning {
		o.skip(startSteps(StartOptions{})...)
		return before, nil
	}
	return before, c.startLocked(ctx, o, by, StartOptions{})
}
