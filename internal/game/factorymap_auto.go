package game

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The automatic factory map's timing (SPEC 5.16).
const (
	// factoryMapEvery is how often the loop looks.
	factoryMapEvery = time.Minute
	// FactoryMapStoppedFor is how long a world must have been stopped
	// before it is drawn automatically.
	FactoryMapStoppedFor = 2 * time.Minute
	// factoryMapsDir is the folder of the data directory that holds the
	// kept pictures.
	factoryMapsDir = "factorymaps"
)

// factoryMapLoop draws the factory map of every world whose mode is
// automatic once it has been stopped for two minutes and no game runs on this
// Foreman (SPEC 5.16). Run starts it once; it returns when the controller's
// context ends.
func (c *Controller) factoryMapLoop() {
	every := c.factoryMaps.every
	if every <= 0 {
		every = factoryMapEvery
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-tick.C:
			c.factoryMapTick()
		}
	}
}

// factoryMapTick is one look. It starts at most one run, and none while one
// is in flight: they share the one preview slot, and a later look starts the
// next.
func (c *Controller) factoryMapTick() {
	c.sweepFactoryMaps()
	ids, err := c.worldIDs()
	if err != nil {
		c.log.Warn("the factory map loop could not list the worlds", "error", err)
		return
	}
	now := c.now()
	due := c.markStopped(ids, now)
	if c.anyGameRunning() || c.anyFactoryMapDrawing() {
		return
	}
	for _, id := range due {
		if c.factoryMapDue(id) && c.startFactoryMap(id) {
			return
		}
	}
}

// markStopped notes when each world was first seen stopped, forgets the rest,
// and returns the worlds stopped for FactoryMapStoppedFor, in id order.
func (c *Controller) markStopped(ids []string, now time.Time) []string {
	stopped := map[string]bool{}
	for _, id := range ids {
		stopped[id] = c.Status(id).State == StateStopped
	}
	fm := &c.factoryMaps
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if fm.stoppedSince == nil {
		fm.stoppedSince = map[string]time.Time{}
	}
	var due []string
	for _, id := range ids {
		if !stopped[id] {
			delete(fm.stoppedSince, id)
			continue
		}
		since, ok := fm.stoppedSince[id]
		if !ok {
			fm.stoppedSince[id] = now
			continue
		}
		if now.Sub(since) >= FactoryMapStoppedFor {
			due = append(due, id)
		}
	}
	for id := range fm.stoppedSince {
		if _, ok := stopped[id]; !ok {
			delete(fm.stoppedSince, id)
		}
	}
	return due
}

// factoryMapDue is whether a stopped world is drawn now: automatic, not
// archived, a game the map reads, and a save that loads next that is neither
// the last picture's nor the last failed run's.
func (c *Controller) factoryMapDue(id string) bool {
	meta, err := c.readMeta(id)
	if err != nil || meta.Archived || !factoryMapSupported(meta.GameVersion) ||
		factoryMapModeOf(meta).Mode != world.FactoryMapAutomatic {
		return false
	}
	target, found, err := c.worlds.LoadTarget(id)
	if err != nil || !found {
		return false
	}
	basis := basisOf(target)
	fm := &c.factoryMaps
	fm.mu.Lock()
	failed, hasFailed := fm.failed[id]
	fm.mu.Unlock()
	if hasFailed && sameBasis(failed.basis, basis) {
		return false
	}
	store, err := c.factoryMapStore(id)
	if err != nil {
		return false
	}
	d, kept, err := store.Read()
	if err != nil {
		return false
	}
	return !kept || !sameBasis(d.Basis, basis)
}

// anyFactoryMapDrawing is whether a factory map run is in flight.
func (c *Controller) anyFactoryMapDrawing() bool {
	fm := &c.factoryMaps
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return len(fm.drawing) > 0
}

// anyGameRunning is whether a game container may run on this Foreman. Before
// Docker has been listed, or while listing fails, it cannot be known, so it
// says yes (SPEC 5.16, 8.16).
func (c *Controller) anyGameRunning() bool {
	listed, err := c.dockerState()
	if !listed || err != nil {
		return true
	}
	for _, w := range c.allRuns() {
		w.mu.Lock()
		busy := w.dockerStatus == "running" || w.opDone != nil
		w.mu.Unlock()
		if busy {
			return true
		}
	}
	return false
}

// worldIDs lists the worlds folder without reading the worlds, as List would.
func (c *Controller) worldIDs() ([]string, error) {
	entries, err := os.ReadDir(c.worlds.Dirs().Worlds)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && world.ValidID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// sweepFactoryMaps removes the kept pictures of worlds that no longer exist
// (SPEC 5.16).
func (c *Controller) sweepFactoryMaps() {
	entries, err := os.ReadDir(filepath.Join(c.dataDir(), factoryMapsDir))
	if err != nil {
		return
	}
	for _, e := range entries {
		id := e.Name()
		if !world.ValidID(id) {
			continue
		}
		root, err := c.worlds.OpenWorld(id)
		if err == nil {
			_ = root.Close()
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := factorymap.Remove(c.dataDir(), id); err != nil {
			c.log.Warn("the factory map of a deleted world could not be removed", "world", id, "error", err)
		}
	}
}
