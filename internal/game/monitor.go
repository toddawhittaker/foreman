package game

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Run watches every game container this install made until ctx ends: it
// re-attaches to games already running when Foreman starts, polls Docker
// (R13), follows each game's log, and stops a crash-looping game
// (SPEC 8.9). It never stops or removes a game when Foreman starts or
// stops (SPEC 3.8). Call it once.
func (c *Controller) Run(ctx context.Context) {
	defer c.shutdown()
	t := time.NewTicker(c.pollInterval)
	defer t.Stop()
	for {
		c.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.poke:
		}
	}
}

// shutdown ends the log followers and RCON connections. The games keep
// running.
func (c *Controller) shutdown() {
	c.spawnMu.Lock()
	c.closed = true
	c.spawnMu.Unlock()
	c.cancel()
	c.wg.Wait()
	for _, w := range c.allRuns() {
		w.mu.Lock()
		if w.rc != nil {
			_ = w.rc.Close()
		}
		w.mu.Unlock()
	}
}

// tick looks at every container of this install once.
func (c *Controller) tick(ctx context.Context) {
	list, err := c.docker.ListContainers(ctx, true, []string{LabelInstall + "=" + c.hostData, LabelWorld})
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	c.dockerErr = err
	if err == nil {
		c.listed = true
	}
	c.mu.Unlock()
	if err != nil {
		for _, w := range c.allRuns() {
			c.publish(w)
		}
		return
	}

	found := map[string]string{} // world id to container ID
	for _, s := range list {
		id := s.Labels[LabelWorld]
		if world.ValidID(id) && hasName(s.Names, ContainerName(id)) {
			found[id] = s.ID
			c.world(id)
		}
	}
	for _, w := range c.allRuns() {
		if c.busy(w) {
			continue
		}
		cid, ok := found[w.id]
		if !ok {
			c.observe(w, nil)
			continue
		}
		cont, err := c.docker.InspectContainer(ctx, cid)
		switch {
		case errors.Is(err, docker.ErrNotFound):
			c.observe(w, nil)
		case err != nil:
			w.mu.Lock()
			w.inspectErr = err
			c.changedLocked(w, true)
			w.mu.Unlock()
		default:
			c.observe(w, &cont)
			if cont.State.Status == "running" {
				c.readUsage(ctx, w, cont.ID)
			}
		}
	}
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if strings.TrimPrefix(n, "/") == want {
			return true
		}
	}
	return false
}

// busy reports whether an operation owns the world's container now; the
// monitor leaves it alone until the operation ends.
func (c *Controller) busy(w *worldRun) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.opDone != nil
}

// observe takes in one look at the world's container (nil when it has
// none) and acts on what changed since the last look (R13).
func (c *Controller) observe(w *worldRun, cont *docker.Container) {
	if cont != nil && c.isNewContainer(w, cont.ID) {
		c.adopt(w, *cont)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opDone != nil {
		return
	}
	w.inspectErr = nil
	if cont == nil {
		w.adoptFailed, w.adoptErr = "", nil
		if w.containerID != "" {
			c.dropContainerLocked(w)
		}
		c.changedLocked(w, true)
		return
	}
	if cont.ID != w.containerID {
		// adopt could not read the world's secrets; without them the log
		// cannot be relayed safely, so it is not followed.
		w.dockerStatus = cont.State.Status
		c.changedLocked(w, true)
		return
	}
	now := c.now()
	if n := cont.RestartCount - w.seenRestarts; n > 0 {
		// Docker restarted the game, which it only does after an exit
		// nobody asked for (R27): each restart is one crash.
		for range n {
			w.crashes = append(w.crashes, now)
		}
		w.lastCrash = now
		w.crashedRun = true
		w.run = runState{}
	}
	w.seenRestarts = cont.RestartCount
	w.crashes = keepSince(w.crashes, now.Add(-c.crashWindow))
	w.dockerStatus = cont.State.Status
	if !cont.State.StartedAt.Equal(w.startedAt) {
		w.startedAt = cont.State.StartedAt
	}
	if len(w.crashes) >= CrashLoopCount && !w.crashLoop && !w.stopping {
		w.crashLoop, w.stopping, w.expectRunning = true, true, false
		c.log.Warn("the game crashed again and again, so Foreman stopped it", "world", w.id)
		c.spawn(func() { c.stopCrashLoop(cont.ID) })
	}
	if w.dockerStatus == "running" && !w.following && !w.stopping {
		since := w.startedAt
		if w.followedRun.Equal(w.startedAt) && !w.lastLineAt.IsZero() {
			// The stream broke while the same run went on.
			since = w.lastLineAt
		}
		c.followLocked(w, cont.ID, since)
	}
	c.changedLocked(w, true)
}

func keepSince(times []time.Time, since time.Time) []time.Time {
	out := times[:0]
	for _, t := range times {
		if t.After(since) {
			out = append(out, t)
		}
	}
	return out
}

func (c *Controller) isNewContainer(w *worldRun, cid string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.containerID != cid && w.opDone == nil
}

// adopt takes on a container Foreman did not start in this run, such as a
// game left running across a Foreman restart (SPEC 3.8). The game's own
// RCON password is needed both to talk to it and to keep it out of the
// relayed log, so without it nothing is adopted.
func (c *Controller) adopt(w *worldRun, cont docker.Container) {
	// Re-attaching changes no world file (SPEC 3.8): a missing password
	// file is not made here, since the running game could not be using it.
	err := c.hasRCONPassword(w.id)
	var rconPW secret.Secret
	var secrets []secret.Secret
	if err == nil {
		rconPW, secrets, err = c.secrets(w.id)
	}
	if err != nil {
		w.mu.Lock()
		warn := w.adoptFailed != cont.ID
		w.adoptFailed, w.adoptErr = cont.ID, err
		w.mu.Unlock()
		if warn {
			c.log.Warn("cannot read the world's passwords, so its game's log is not shown", "world", w.id, "error", err)
		}
		return
	}
	meta, err := c.readMeta(w.id)
	if err != nil {
		c.log.Warn("cannot read the world's settings", "world", w.id, "error", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opDone != nil || w.containerID == cont.ID {
		return
	}
	c.dropContainerLocked(w)
	w.adoptFailed, w.adoptErr = "", nil
	w.containerID = cont.ID
	w.seenRestarts = cont.RestartCount
	w.startedAt = cont.State.StartedAt
	w.dockerStatus = cont.State.Status
	w.expectRunning = cont.State.Status == "running" || cont.State.Status == "restarting"
	w.stopping, w.crashLoop, w.crashedRun = false, false, false
	w.crashes = nil
	w.run = runState{}
	w.joins.Reset()
	w.secrets = secrets
	w.gameVersion = meta.GameVersion
	w.image = cont.Image
	if ip := containerIP(cont); ip != "" {
		w.rc = c.newRCON(ip, rconPW)
	}
}

// dropContainerLocked forgets the current container: its log follower and
// RCON connection end. w.mu is held.
func (c *Controller) dropContainerLocked(w *worldRun) {
	if w.followCancel != nil {
		w.followCancel()
	}
	w.following, w.followCancel = false, nil
	w.followedRun, w.lastLineAt = time.Time{}, time.Time{}
	if w.rc != nil {
		_ = w.rc.Close()
		w.rc = nil
	}
	w.containerID, w.dockerStatus = "", ""
	w.startedAt = time.Time{}
	w.usage = usage{}
	w.expectRunning = false
}

// stopCrashLoop stops a crash-looping game, which keeps it stopped under
// unless-stopped (SPEC 8.9, R28).
func (c *Controller) stopCrashLoop(cid string) {
	err := c.docker.StopContainer(c.ctx, cid, StopGraceSeconds)
	if err != nil && !errors.Is(err, docker.ErrNotModified) && !errors.Is(err, docker.ErrNotFound) {
		c.log.Error("could not stop the crash-looping game", "container", cid, "error", err)
	}
	c.pokeMonitor()
}

// spawn runs f in a goroutine that shutdown waits for. After shutdown it
// does nothing and reports false.
func (c *Controller) spawn(f func()) bool {
	c.spawnMu.Lock()
	defer c.spawnMu.Unlock()
	if c.closed {
		return false
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		f()
	}()
	return true
}

// followLocked starts following the container's log from since (zero for
// all of it). A follower already running is replaced. w.mu is held.
func (c *Controller) followLocked(w *worldRun, cid string, since time.Time) {
	if w.followCancel != nil {
		w.followCancel()
	}
	ctx, cancel := context.WithCancel(c.ctx)
	w.followGen++
	gen := w.followGen
	w.following, w.followCancel = true, cancel
	w.followedRun = w.startedAt
	started := c.spawn(func() {
		defer cancel()
		c.follow(ctx, w, cid, since)
		w.mu.Lock()
		if w.followGen == gen {
			w.following, w.followCancel = false, nil
		}
		w.mu.Unlock()
		// The stream ends when the game exits: look at once (R13).
		c.pokeMonitor()
	})
	if !started {
		cancel()
		w.following, w.followCancel = false, nil
	}
}

// follow relays the container's log until it ends or ctx does.
func (c *Controller) follow(ctx context.Context, w *worldRun, cid string, since time.Time) {
	s, err := c.docker.Logs(ctx, cid, docker.LogOptions{Follow: true, Since: since})
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn("cannot follow the game's log", "world", w.id, "error", err)
		}
		return
	}
	defer func() { _ = s.Close() }()
	for {
		l, err := s.Next()
		if err != nil {
			return
		}
		c.handleLine(w, cid, l.Text)
	}
}

// handleLine redacts one log line, keeps it for pages, and acts on what it
// says (SPEC 8.6). The line is added and published under w.mu, so Watch
// never misses or repeats it.
func (c *Controller) handleLine(w *worldRun, cid, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.containerID != cid {
		return
	}
	line := Redact(text, w.secrets...)
	w.lastLineAt = c.now()
	w.logs.Add(line)
	w.hub.Publish(Update{Kind: UpdateLine, Line: line})

	ev := ParseLine(line)
	publish := true
	switch ev.Kind {
	case KindOther:
		return
	case KindLoadStarted:
		// Every run of the game starts here; players from the last run are gone.
		w.run = runState{save: loadedSave(line)}
		w.joins.Reset()
	case KindInGame:
		w.run.inGame = true
		w.crashedRun = false
	case KindLoadError:
		w.run.loadError = ev.Detail
		publish = false
	case KindLoadFailed:
		w.run.loadFailed, w.run.inGame = true, false
		c.pokeMonitor()
	case KindSaveFinished:
		w.saves++
		publish = false
	case KindPlayerJoined, KindPlayerLeft:
		w.joins.Observe(ev)
		publish = false
	default:
		publish = false
	}
	c.changedLocked(w, publish)
}

// loadedSave reads the save's file name from a "Loading map" line.
func loadedSave(line string) string {
	_, after, ok := strings.Cut(line, "Loading map ")
	if !ok {
		return ""
	}
	i := strings.LastIndex(after, ": ")
	if i < 0 {
		return ""
	}
	return path.Base(after[:i])
}

// readUsage takes one CPU and memory reading (SPEC 7.2). A failure only
// leaves the figures out.
func (c *Controller) readUsage(ctx context.Context, w *worldRun, cid string) {
	s, err := c.docker.ContainerStats(ctx, cid)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.containerID != cid {
		return
	}
	if err != nil {
		w.usage = usage{}
		return
	}
	w.usage = usageFrom(s)
}

func usageFrom(s docker.Stats) usage {
	u := usage{ok: true, memory: s.MemoryUsage}
	if s.MemoryInactive < s.MemoryUsage {
		u.memory = s.MemoryUsage - s.MemoryInactive
	}
	if s.CPUTotal > s.PreCPUTotal && s.CPUSystem > s.PreCPUSystem {
		cpus := max(s.OnlineCPUs, 1)
		u.cpu = float64(s.CPUTotal-s.PreCPUTotal) / float64(s.CPUSystem-s.PreCPUSystem) * float64(cpus) * 100
	}
	return u
}
