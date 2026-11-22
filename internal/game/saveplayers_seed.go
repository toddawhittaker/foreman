package game

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"

	saveplayersmod "github.com/toddawhittaker/foreman/internal/game/saveplayers"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The "players in this save" the world page shows (SPEC 8.14), and the read
// of a copy of the save that seeds them (SPEC 8.16).

// The states of a SavePlayersView.
const (
	SavePlayersReady        = "ready"         // a record matches the save that loads
	SavePlayersReading      = "reading"       // a read is running
	SavePlayersTrackedOnly  = "tracked-only"  // a read failed; see Failure
	SavePlayersNone         = "none"          // no save to read
	SavePlayersNotSupported = "not-supported" // the world's game cannot be read
)

// SavePlayersMemory is the memory limit of a players read: a read takes
// about as much as running the save (SPEC 8.16).
const SavePlayersMemory = 12 << 30

// savePlayersControl is the helper mod a players read runs with.
var savePlayersControl = controlMod{
	kind:      gameenv.KindSavePlayers,
	what:      "save players",
	name:      saveplayersmod.ModName,
	dir:       saveplayersmod.ModDir,
	control:   saveplayersmod.Control,
	params:    ":",
	output:    gameenv.SavePlayersFile,
	maxOutput: saveplayers.MaxOutputBytes,
	memory:    SavePlayersMemory,
}

// savePlayersMod is the players read: its one small output file is read from
// the tmpfs, and lists the mods the game loaded for the check after the run.
var savePlayersMod = saveCopyMod{controlMod: savePlayersControl, activeMods: savePlayersActiveMods}

// savePlayersActiveMods reads the mods the game loaded from the helper's
// output in the work folder dir.
func savePlayersActiveMods(base *os.Root, dir string) ([]saveplayers.Mod, error) {
	b, err := readControlModOutput(base, dir, savePlayersControl)
	if err != nil {
		return nil, err
	}
	seed, err := saveplayers.ParseSeed(b)
	if err != nil {
		return nil, err
	}
	return seed.Mods, nil
}

// seedState is the reads of players records in flight, by world id. A read
// that failed is remembered in the record itself, so it is not tried again
// until the save changes or an admin asks.
type seedState struct {
	mu      sync.Mutex
	running map[string]bool
	// read runs the save and returns the save it read and the helper's
	// output. Nil means readSavePlayers; tests replace it.
	read func(ctx context.Context, id string) (saveplayers.Basis, []byte, error)
	// gameTime is the running world's last /time reading (SPEC 8.15). Nil
	// means liveGameTime; tests replace it.
	gameTime func(id string) (d time.Duration, at time.Time, ok bool)
	// noAuto is Config.NoAutoPlayersRead: only a click starts a read.
	noAuto bool
}

// SavePlayersView is what the web layer shows of a world's players record.
type SavePlayersView struct {
	// State is one of the SavePlayers* constants.
	State string
	// Failure is the failure id when State is "tracked-only".
	Failure      string
	Save         string
	TrackedSince time.Time
	// GameTime is the save's total play time, true when GameTimeKnown.
	GameTime      time.Duration
	GameTimeKnown bool
	// Inexact is true when Foreman may have missed play time (a crash, or
	// Foreman down during a run) since the record was read.
	Inexact bool
	Players []SavePlayer
}

// SavePlayer is one player of the save.
type SavePlayer struct {
	Name     string
	PlayTime time.Duration
	Online   bool
	LastSeen time.Time
	// BeforeForeman is true when the play time is from before Foreman
	// tracked this world.
	BeforeForeman bool
}

// SavePlayers returns the view and starts a read if one is due and allowed
// (SPEC 8.16). It never waits for a run: a read's result arrives as an
// UpdateSavePlayers.
//
// A read is due when the record is missing, unreadable, made for another
// save than the one that loads next, or inexact. It starts by itself only
// while no game runs on this Foreman.
func (c *Controller) SavePlayers(ctx context.Context, id string) (SavePlayersView, error) {
	target, found, err := c.worlds.LoadTarget(id)
	if err != nil {
		return SavePlayersView{}, err
	}
	if !found {
		return SavePlayersView{State: SavePlayersNone}, nil
	}
	rec, have, newer := c.readPlayersRecord(id)
	matches := have && sameSave(rec.Basis, target)
	due := !matches || rec.Inexact
	reading := c.seedRunning(id)
	// A record from a newer Foreman is replaced only on a click, so a
	// downgrade does not lose it.
	if !reading && due && !newer && !c.seeds.noAuto && c.noGameRuns() {
		reading = c.startSeed(id)
	}
	v := SavePlayersView{State: SavePlayersNone, Save: target.Name}
	if matches {
		v = c.savePlayersView(id, rec)
	}
	if reading {
		v.State = SavePlayersReading
	}
	// While the world runs, total play time is the game's own /time
	// reading (SPEC 8.15).
	if c.Status(id).State == StateRunning {
		gameTime := c.seeds.gameTime
		if gameTime == nil {
			gameTime = c.liveGameTime
		}
		if d, _, ok := gameTime(id); ok {
			v.GameTime, v.GameTimeKnown = d, true
		}
	}
	return v, nil
}

// ReadSavePlayersAgain reads the newest save again, on an admin's click. It
// is allowed beside a running game (SPEC 8.16). A read already running is
// the read asked for. A read changes no file the game uses, so it is not
// audited (SPEC 8.14).
func (c *Controller) ReadSavePlayersAgain(ctx context.Context, id string, by Actor) error {
	if _, err := c.worlds.Get(ctx, id); err != nil {
		return err
	}
	if !c.seedRunning(id) && !c.startSeed(id) {
		return context.Canceled
	}
	return nil
}

// readPlayersRecord returns the world's record, and false when there is none
// or it is refused; a read by itself may then replace it. newer is true when
// the record is from a newer Foreman, which only a click replaces.
func (c *Controller) readPlayersRecord(id string) (rec saveplayers.Record, ok, newer bool) {
	b, err := c.worlds.ReadPlayersRecord(id)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Warn("cannot read the players record", "world", id, "err", err)
		}
		return saveplayers.Record{}, false, false
	}
	rec, err = saveplayers.Parse(b, c.now())
	if err != nil {
		newer = errors.Is(err, saveplayers.ErrNewerFormat)
		c.log.Warn("the players record is refused", "world", id, "newer", newer, "err", err)
		return saveplayers.Record{}, false, newer
	}
	return rec, true, false
}

// savePlayersView turns a record that matches the save into the page's view.
func (c *Controller) savePlayersView(id string, rec saveplayers.Record) SavePlayersView {
	v := SavePlayersView{
		State: SavePlayersReady, Save: rec.Basis.Save, TrackedSince: rec.TrackedSince,
		GameTime:      time.Duration(rec.GameSeconds()) * time.Second,
		GameTimeKnown: rec.GameTicks > 0,
		Inexact:       rec.Inexact,
	}
	if rec.Seed.Result != saveplayers.ResultOK {
		v.State, v.Failure = SavePlayersTrackedOnly, rec.Seed.Result
	}
	online := map[string]bool{}
	if c.Status(id).State == StateRunning {
		names := make([]string, len(rec.Players))
		for i, p := range rec.Players {
			names[i] = p.Name
		}
		for _, p := range c.world(id).joins.Online(names, c.now()) {
			online[p.Name] = !p.Joined.IsZero()
		}
	}
	for _, p := range rec.Players {
		sp := SavePlayer{
			Name:          p.Name,
			PlayTime:      time.Duration(p.PlayedSeconds()) * time.Second,
			Online:        online[p.Name],
			BeforeForeman: p.SeededTicks > 0,
		}
		if p.LastSeen != nil {
			sp.LastSeen = *p.LastSeen
		}
		v.Players = append(v.Players, sp)
	}
	return v
}

// noGameRuns reports whether no world's game runs on this Foreman, so a read
// may start by itself. A world whose state is not known counts as running.
func (c *Controller) noGameRuns() bool {
	for _, w := range c.allRuns() {
		if c.Status(w.id).State != StateStopped {
			return false
		}
	}
	return true
}

func (c *Controller) seedRunning(id string) bool {
	c.seeds.mu.Lock()
	defer c.seeds.mu.Unlock()
	return c.seeds.running[id]
}

// startSeed starts a read of the world's save unless one runs. It reports
// whether a read is running; false only after shutdown.
func (c *Controller) startSeed(id string) bool {
	s := &c.seeds
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[id] {
		return true
	}
	if s.running == nil {
		s.running = map[string]bool{}
	}
	s.running[id] = true
	if !c.spawn(func() { c.runSeed(id) }) {
		delete(s.running, id)
		return false
	}
	return true
}

// runSeed reads the save, writes the record when the save that loads next is
// still the one read, and tells the world's pages. It runs in the
// controller's context, so a page that goes away does not stop it; a start
// of the world does (cancelSaveCopies).
func (c *Controller) runSeed(id string) {
	defer func() {
		c.seeds.mu.Lock()
		delete(c.seeds.running, id)
		c.seeds.mu.Unlock()
		c.world(id).hub.Publish(Update{Kind: UpdateSavePlayers})
	}()
	rec, ok := c.seedRecord(c.ctx, id)
	if !ok {
		return
	}
	after, found, err := c.worlds.LoadTarget(id)
	if err != nil || !found || !sameSave(rec.Basis, after) {
		c.log.Info("the save changed during the players read, so the result is not kept", "world", id)
		return
	}
	err = c.updatePlayersRecord(id, func(r *saveplayers.Record) error {
		keepLastSeen(&rec, *r)
		*r = rec
		return nil
	})
	if err != nil {
		c.log.Warn("cannot write the players record", "world", id, "err", err)
	}
}

// seedRecord makes the record a read gives: from the save's figures, or a
// failed seed. It reports false when nothing should be written: no save, or
// a run that did not finish for a reason that is not the save's (the slot
// was busy, a start cancelled it), so the next view tries again.
func (c *Controller) seedRecord(ctx context.Context, id string) (saveplayers.Record, bool) {
	target, found, err := c.worlds.LoadTarget(id)
	if err != nil || !found {
		return saveplayers.Record{}, false
	}
	basis := basisOfSave(target)
	read := c.seeds.read
	if read == nil {
		read = c.readSavePlayers
	}
	readBasis, out, err := read(ctx, id)
	if readBasis.Save != "" {
		basis = readBasis
	}
	if err != nil {
		result := seedFailure(err)
		if result == "" {
			c.log.Info("the players read did not finish", "world", id, "err", err)
			return saveplayers.Record{}, false
		}
		c.log.Info("the players read failed", "world", id, "result", result, "err", err)
		return saveplayers.Failed(result, basis, c.now()), true
	}
	seed, err := saveplayers.ParseSeed(out)
	if err != nil {
		c.log.Info("the players read made no usable output", "world", id, "err", err)
		return saveplayers.Failed(saveplayers.ResultBadOutput, basis, c.now()), true
	}
	return saveplayers.FromSeed(seed, basis, c.now()), true
}

// readSavePlayers runs the save with the players helper and returns its
// output.
func (c *Controller) readSavePlayers(ctx context.Context, id string) (saveplayers.Basis, []byte, error) {
	var out []byte
	basis, err := c.runSaveCopy(ctx, id, savePlayersMod, func(base *os.Root, dir string) error {
		b, err := readControlModOutput(base, dir, savePlayersControl)
		out = b
		return err
	})
	return basis, out, err
}

// seedFailure maps a run's error to its failure id, or "" for an error that
// is not the save's.
func seedFailure(err error) string {
	switch {
	case errors.Is(err, ErrSaveCopyImageMissing):
		return saveplayers.ResultImageMissing
	case errors.Is(err, ErrSaveCopyOutOfMemory):
		return saveplayers.ResultOutOfMemory
	case errors.Is(err, ErrSaveCopyTimeout):
		return saveplayers.ResultTimeout
	case errors.Is(err, ErrSaveCopyModsDiffer):
		return saveplayers.ResultModsDiffer
	case errors.Is(err, ErrSaveCopySaveNewer):
		return saveplayers.ResultSaveNewer
	case errors.Is(err, ErrSaveCopyBadOutput):
		return saveplayers.ResultBadOutput
	}
	return ""
}

// keepLastSeen copies the last seen dates of the old record into the new
// one, by name, since a read of the save cannot know them (SPEC 8.14).
func keepLastSeen(rec *saveplayers.Record, old saveplayers.Record) {
	seen := map[string]*time.Time{}
	for _, p := range old.Players {
		if p.LastSeen != nil && !p.LastSeen.After(rec.AsOf) {
			seen[asciiLower(p.Name)] = p.LastSeen
		}
	}
	for i := range rec.Players {
		if rec.Players[i].LastSeen == nil {
			rec.Players[i].LastSeen = seen[asciiLower(rec.Players[i].Name)]
		}
	}
}

// asciiLower lowers ASCII letters only, as player names compare.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// sameSave reports whether a record's basis is the save s.
func sameSave(b saveplayers.Basis, s world.Save) bool {
	return b.Save == s.Name && b.Size == s.Size && b.Modified.Equal(s.ModTime)
}
