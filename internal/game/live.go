package game

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

func (c *Controller) newRCON(ip string, pw secret.Secret) *rcon.Client {
	return rcon.New(net.JoinHostPort(ip, RCONPort), pw)
}

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

// liveRCON returns the RCON client of a game that is in game, or
// ErrNotRunning.
func (c *Controller) liveRCON(w *worldRun) (*rcon.Client, []secret.Secret, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.rc == nil || w.dockerStatus != "running" || !w.run.inGame {
		return nil, nil, ErrNotRunning
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

// saveGame sends /server-save and waits for the log to say the save
// finished (SPEC 6.10, 7.6).
func (c *Controller) saveGame(ctx context.Context, w *worldRun) error {
	w.mu.Lock()
	rc, before := w.rc, w.saves
	w.mu.Unlock()
	if rc == nil {
		return ErrRCONUnreachable
	}
	if err := c.exec(ctx, rc, "/server-save"); err != nil {
		return err
	}
	deadline := time.NewTimer(c.saveTimeout)
	defer deadline.Stop()
	for {
		w.mu.Lock()
		saved, changed := w.saves > before, w.changed
		w.mu.Unlock()
		if saved {
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

// Snapshot takes a manual snapshot of the world (SPEC 6.3). When the game
// is running it saves first, so the snapshot holds the current game
// (SPEC 6.10).
func (c *Controller) Snapshot(ctx context.Context, id string, by Actor, label string) (world.Snapshot, error) {
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return world.Snapshot{}, err
	}
	defer unlock()
	cont, found, err := c.ourContainer(ctx, id)
	if err != nil {
		return world.Snapshot{}, err
	}
	if found {
		switch cont.State.Status {
		case "running":
			w := c.world(id)
			w.mu.Lock()
			ready := w.containerID == cont.ID && w.run.inGame
			w.mu.Unlock()
			if !ready {
				return world.Snapshot{}, ErrNotReady
			}
			if err := c.saveGame(ctx, w); err != nil {
				return world.Snapshot{}, err
			}
		case "restarting":
			return world.Snapshot{}, ErrNotReady
		}
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
	ctx, o := c.beginOp(ctx, c.world(id), ActionRestore, stopSteps, []string{StepRestore}, startStepsFor(StartOptions{}))
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
		o.skip(startStepsFor(StartOptions{})...)
		return before, nil
	}
	return before, c.startLocked(ctx, o, by, StartOptions{})
}
