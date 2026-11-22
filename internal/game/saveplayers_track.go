package game

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// gameTimeEvery is how often a page may make Foreman ask a running game for
// its play time (SPEC 8.15).
const gameTimeEvery = time.Minute

// recordState is the players records' mutexes, one per world, with what
// tracking knows of each world's run and its last /time reading (SPEC 8.14,
// 8.15).
type recordState struct {
	mu     sync.Mutex
	worlds map[string]*trackedWorld
}

// trackedWorld is one world's tracking. mu is held while the world's
// players.json is read and written, and guards every field. It is never
// held while taking a worldRun's mu.
type trackedWorld struct {
	mu sync.Mutex
	// open is each player online now, by name, with the time of their join
	// line.
	open map[string]time.Time
	// target is the save the running game continues from: the save it
	// loaded, then each save it finished. Zero when that is not known, as
	// for a run Foreman did not see begin; a record is moved to a new save
	// only from its target (SPEC 8.14).
	target saveplayers.Basis
	// endedAt is the newest run end taken in, so a stopped container seen
	// at every poll reads the record once. lineAt is Docker's time of the
	// newest line taken in.
	endedAt time.Time
	lineAt  time.Time
	// The last /time reading of the running game, true at gameTimeAt;
	// asked is when the last ask began, and asking is set while it runs.
	gameTime   time.Duration
	gameTimeAt time.Time
	asked      time.Time
	asking     bool
}

// tracked returns the world's tracking, making it on first use.
func (c *Controller) tracked(id string) *trackedWorld {
	c.records.mu.Lock()
	defer c.records.mu.Unlock()
	if c.records.worlds == nil {
		c.records.worlds = map[string]*trackedWorld{}
	}
	t := c.records.worlds[id]
	if t == nil {
		t = &trackedWorld{open: map[string]time.Time{}}
		c.records.worlds[id] = t
	}
	return t
}

// updatePlayersRecord reads the world's players record, calls f on it, and
// writes it back, under one mutex per world (SPEC 8.14). A world without a
// record, or with one that cannot be read, gives f a zero Record. Nothing
// is written when f returns an error, leaves the record without a format
// version, or changes nothing. A write tells the world's pages.
func (c *Controller) updatePlayersRecord(id string, f func(*saveplayers.Record) error) error {
	t := c.tracked(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	return c.updateRecordLocked(id, f)
}

// updateRecordLocked is updatePlayersRecord with the world's tracking mu
// held.
func (c *Controller) updateRecordLocked(id string, f func(*saveplayers.Record) error) error {
	old, err := c.worlds.ReadPlayersRecord(id)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var r saveplayers.Record
	if len(old) > 0 {
		if r, err = saveplayers.Parse(old, c.now()); err != nil {
			// The game can write the world folder, so a bad record is
			// read afresh rather than trusted (SPEC 14.6).
			c.log.Warn("cannot read the players record, so it is read from the save again", "world", id, "error", err)
			r = saveplayers.Record{}
		}
	}
	if err := f(&r); err != nil {
		return err
	}
	if r.FormatVersion == 0 {
		return nil
	}
	b, err := r.Marshal()
	if err != nil {
		return err
	}
	if bytes.Equal(b, old) {
		return nil
	}
	if err := c.worlds.WritePlayersRecord(id, b); err != nil {
		return err
	}
	c.world(id).hub.Publish(Update{Kind: UpdateSavePlayers})
	return nil
}

// trackRecord changes the world's record when it has one, and logs a
// failure: tracking never stops the game or its log.
func (c *Controller) trackRecord(id string, f func(*saveplayers.Record)) {
	err := c.updateRecordLocked(id, func(r *saveplayers.Record) error {
		if r.FormatVersion != 0 {
			f(r)
		}
		return nil
	})
	if err != nil {
		c.log.Warn("cannot update the players record", "world", id, "error", err)
	}
}

// foldOpen counts every open session up to at, all from the same AsOf,
// then makes the totals true at at. A moment no later than AsOf was counted
// already, so a replayed line changes nothing (SPEC 8.14).
func foldOpen(r *saveplayers.Record, open map[string]time.Time, at time.Time) {
	if !at.After(r.AsOf) {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(open)) {
		r.AddSession(name, open[name], at)
	}
	r.AsOf = at
}

// trackLine takes in a join, leave or load line of the world's log, or a
// finished save (SPEC 8.14). live is false for a line replayed after a
// re-attach. loaded is the save a load line names.
func (c *Controller) trackLine(id string, ev Event, loaded string, live bool) {
	at := ev.Time
	if at.IsZero() {
		return
	}
	t := c.tracked(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	if !at.After(t.endedAt) || !at.After(t.lineAt) {
		// A line read again as the log is followed anew: it was taken in
		// already, or its run has ended and its sessions with it.
		return
	}
	t.lineAt = at
	switch ev.Kind {
	case KindPlayerJoined:
		c.trackRecord(id, func(r *saveplayers.Record) {
			foldOpen(r, t.open, at)
			r.AddSession(ev.Player, at, at)
		})
		t.open[ev.Player] = at
	case KindPlayerLeft:
		c.trackRecord(id, func(r *saveplayers.Record) { foldOpen(r, t.open, at) })
		delete(t.open, ev.Player)
	case KindLoadStarted:
		// Every run begins here: players of a run before it are gone.
		c.trackRecord(id, func(r *saveplayers.Record) { foldOpen(r, t.open, at) })
		clear(t.open)
		t.target = saveplayers.Basis{}
		if live {
			if s, ok := c.loadTarget(id); ok && s.Save == loaded {
				t.target = s
			}
		}
	case KindSaveFinished:
		c.trackSaved(id, t, at, live)
	}
}

// trackSaved takes in a save the game finished at at. A record that
// matched the save the game went on from is moved to the new one, so it
// still matches the save that loads next (SPEC 8.14). The game is asked
// for its play time (SPEC 8.15). t.mu is held.
func (c *Controller) trackSaved(id string, t *trackedWorld, at time.Time, live bool) {
	var next saveplayers.Basis
	if live && t.target != (saveplayers.Basis{}) {
		next, _ = c.loadTarget(id)
	}
	c.trackRecord(id, func(r *saveplayers.Record) {
		foldOpen(r, t.open, at)
		if next != (saveplayers.Basis{}) && !r.Inexact && r.Basis.Same(t.target) {
			r.Rebase(next, at)
		}
	})
	t.target = next
	if live {
		c.askGameTimeLocked(id, t)
	}
}

// loadTarget is the save the world would load now, as a basis.
func (c *Controller) loadTarget(id string) (saveplayers.Basis, bool) {
	s, ok, err := c.worlds.LoadTarget(id)
	if err != nil || !ok {
		return saveplayers.Basis{}, false
	}
	return basisOfSave(s), true
}

// basisOfSave is the basis of a world's save.
func basisOfSave(s world.Save) saveplayers.Basis {
	return saveplayers.Basis{Save: s.Name, Size: s.Size, Modified: s.ModTime}
}

// runEnd is a run of the game that ended, as a look at Docker found it.
type runEnd struct {
	at time.Time // when it ended; zero for no end
	// started is when the ended run began, zero when not known.
	started time.Time
	// unseen marks the record inexact: a crash, or an end Foreman could
	// not time (SPEC 8.14).
	unseen bool
}

// trackRunEnd closes the open sessions when a run ends (SPEC 8.14). A
// crash marks the record inexact, and so does a run that ended with
// nothing tracked since it began: Foreman was down during it.
func (c *Controller) trackRunEnd(id string, e runEnd) {
	if e.at.IsZero() {
		return
	}
	t := c.tracked(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	// A look between a crash and Docker's restart sees the same end as a
	// plain exit, before the restart shows it was a crash.
	if e.at.Before(t.endedAt) || (e.at.Equal(t.endedAt) && !e.unseen) {
		return
	}
	t.endedAt = e.at
	c.trackRecord(id, func(r *saveplayers.Record) {
		missed := !e.started.IsZero() && r.AsOf.Before(e.started)
		foldOpen(r, t.open, e.at)
		if e.unseen || missed {
			r.Inexact = true
		}
	})
	clear(t.open)
	t.target = saveplayers.Basis{}
	t.gameTime, t.gameTimeAt = 0, time.Time{}
}

// trackReattach marks the record inexact when Foreman takes on a game that
// was running while Foreman was down (SPEC 8.14).
func (c *Controller) trackReattach(id string) {
	t := c.tracked(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.target = saveplayers.Basis{}
	c.trackRecord(id, func(r *saveplayers.Record) { r.Inexact = true })
}

// liveGameTime returns the last /time reading of the world's running game
// and when it was true. When it is a minute old, or there is none, the game
// is asked again in the background, and the record follows (SPEC 8.15).
func (c *Controller) liveGameTime(id string) (time.Duration, time.Time, bool) {
	t := c.tracked(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	if c.now().Sub(t.asked) >= gameTimeEvery {
		c.askGameTimeLocked(id, t)
	}
	return t.gameTime, t.gameTimeAt, !t.gameTimeAt.IsZero()
}

// askGameTimeLocked asks the game for /time in the background, unless an
// ask runs already. t.mu is held.
func (c *Controller) askGameTimeLocked(id string, t *trackedWorld) {
	if t.asking {
		return
	}
	if c.spawn(func() { c.readGameTime(id) }) {
		t.asking, t.asked = true, c.now()
	}
}

// readGameTime asks the running game for its play time with plain /time,
// never Lua, so achievements stay on (SPEC 8.15), and keeps the answer.
func (c *Controller) readGameTime(id string) {
	t := c.tracked(id)
	d, at, err := c.askGameTime(id)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.asking = false
	if err != nil {
		c.log.Debug("cannot read the game's play time", "world", id, "error", err)
		return
	}
	t.gameTime, t.gameTimeAt = d, at
	c.trackRecord(id, func(r *saveplayers.Record) {
		r.SetGameTicks(int64(d/time.Second)*saveplayers.TicksPerSecond, at)
	})
	c.world(id).hub.Publish(Update{Kind: UpdateSavePlayers})
}

// askGameTime sends /time to the world's running game.
func (c *Controller) askGameTime(id string) (time.Duration, time.Time, error) {
	rc, _, err := c.liveRCON(c.world(id))
	if err != nil {
		return 0, time.Time{}, err
	}
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	reply, err := c.execReply(ctx, rc, "/time")
	if err != nil {
		return 0, time.Time{}, err
	}
	at := c.now()
	d, err := ParseTimeReply(reply)
	return d, at, err
}
