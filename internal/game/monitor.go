package game

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Run watches every game container this install made until ctx ends: it
// re-attaches to games already running when Foreman starts, polls Docker
// and follows its out-of-memory events where the proxy allows them
// (SPEC 8.7), follows each game's log, and stops a crash-looping game
// (SPEC 8.9). It never stops or removes a game when Foreman starts or
// stops (SPEC 3.8). Call it once.
func (c *Controller) Run(ctx context.Context) {
	defer c.shutdown()
	// Not waited for at shutdown: a notifier that hangs must not hold
	// Foreman up (SPEC 8.12).
	go c.deliverEvents()
	c.spawn(func() { c.watchOOM(c.ctx) })
	c.spawn(c.factoryMapLoop) // returns when c.ctx ends (SPEC 5.16)
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
		rc := w.rc
		w.mu.Unlock()
		closeRCON(rc)
	}
}

// closeRCON closes an RCON connection, if there is one. Closing waits for a
// command in flight, so it is never done while holding a world's mu.
func closeRCON(rc *rcon.Client) {
	if rc != nil {
		_ = rc.Close()
	}
}

// tick looks at every container of this install once.
func (c *Controller) tick(ctx context.Context) {
	// Counted before Docker is read: a world whose operation begins after
	// this keeps what that operation did, not this older look.
	ops := map[*worldRun]uint64{}
	for _, w := range c.allRuns() {
		w.mu.Lock()
		ops[w] = w.ops
		w.mu.Unlock()
	}
	list, err := c.docker.ListContainers(ctx, true, []string{LabelInstall + "=" + c.hostData, LabelWorld})
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	before := c.dockerErr
	c.dockerErr = err
	if err == nil {
		c.listed = true
	}
	c.mu.Unlock()
	if err != nil {
		c.logDockerFault(before, err)
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
	allSeen := true
	for _, w := range c.allRuns() {
		if c.busy(w) {
			_, exists := found[w.id]
			allSeen = allSeen && !exists
			continue
		}
		cid, ok := found[w.id]
		if !ok {
			c.observe(w, nil, ops[w], nil)
			continue
		}
		cont, err := c.docker.InspectContainer(ctx, cid)
		if err != nil || !c.noteGameSubnets(ctx, cont) {
			allSeen = false
		}
		c.observeInspect(ctx, w, cont, err, ops[w])
	}
	c.refreshSubnets(ctx, allSeen)
}

// observeInspect takes in one inspect of the world's container, read
// when the world had begun ops operations.
func (c *Controller) observeInspect(ctx context.Context, w *worldRun, cont docker.Container, err error, ops uint64) {
	switch {
	case errors.Is(err, docker.ErrNotFound):
		c.observe(w, nil, ops, nil)
	case err != nil:
		w.mu.Lock()
		if w.opDone != nil || w.ops != ops {
			w.mu.Unlock()
			return
		}
		c.logFault("cannot read the game container", w.inspectErr, err, "world", w.id)
		w.inspectErr = err
		c.changedLocked(w, true)
		w.mu.Unlock()
	default:
		if !c.observe(w, &cont, ops, c.readCrashLog(ctx, w, cont, ops)) {
			return
		}
		if cont.State.Status == "running" {
			c.readUsage(ctx, w, cont.ID)
			c.checkDisk(w, cont.ID)
		}
	}
}

// readCrashLog reads the container's log once when the look finds Docker
// restarted the game since the last look. Each run can end, and Docker
// start it again, before the follower reads it, and the follower is not
// started again once a crash loop is being stopped, so the words that name
// the crash may be only in Docker's log (SPEC 8.7, ADR 0016). It returns
// nil when there is no crash to read for, the world's secrets are not
// known to mask what it reads, or the read did not reach the log's end.
func (c *Controller) readCrashLog(ctx context.Context, w *worldRun, cont docker.Container, ops uint64) *crashLog {
	w.mu.Lock()
	ok := w.opDone == nil && w.ops == ops && w.containerID == cont.ID && !w.noSecrets && cont.RestartCount > w.seenRestarts
	mark := w.logs.Mark()
	w.mu.Unlock()
	if !ok {
		return nil
	}
	f, whole := c.readLoadLog(ctx, cont.ID)
	if !whole {
		return nil
	}
	// Docker restarted the game before this read, so every run before the
	// newest it holds had ended and said all it would (see crashReason).
	f.runWhole = true
	return &crashLog{f: f, mark: mark}
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
// none), read when the world had begun ops operations, and acts on what
// changed since the last look (SPEC 8.7). read is the container's log read
// for a restart this look shows, or nil. It reports false when the look
// was dropped: an operation runs, or one began since the look.
func (c *Controller) observe(w *worldRun, cont *docker.Container, ops uint64, read *crashLog) bool {
	if !c.lookStillHolds(w, ops) {
		return false
	}
	if cont != nil && c.needsAttach(w, *cont, ops) {
		c.attach(w, *cont, ops)
	}
	var old, stale *rcon.Client
	defer func() { closeRCON(old); closeRCON(stale) }() // runs after the unlock below
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opDone != nil || w.ops != ops {
		return false
	}
	w.inspectErr = nil
	if cont == nil {
		w.attachFailed, w.attachErr = "", nil
		if w.containerID != "" {
			old = c.dropContainerLocked(w)
		}
		c.changedLocked(w, true)
		return true
	}
	if cont.ID != w.containerID {
		// attach could not read the world's secrets; without them the log
		// cannot be relayed safely, so it is not followed.
		w.dockerStatus = cont.State.Status
		c.changedLocked(w, true)
		return true
	}
	stale = c.takeRunLocked(w, cont, read)
	c.crashLoopLocked(w, cont.ID)
	if w.dockerStatus == "running" && !w.following && !w.stopping && !w.noSecrets {
		since := w.startedAt
		if w.followedRun.Equal(w.startedAt) && !w.lastLineAt.IsZero() {
			// The stream broke while the same run went on: resume after the
			// last line by Docker's own time, since Docker keeps a line
			// written at the since time itself.
			since = w.lastLineAt.Add(time.Nanosecond)
		}
		c.followLocked(w, cont.ID, since)
	}
	c.changedLocked(w, true)
	return true
}

// takeRunLocked takes in what a look at the world's own container says
// about its run: each Docker restart is a crash, and a new run resets what
// the last one knew (SPEC 8.7). It returns the RCON connection to a run
// that ended, for the caller to close after the unlock, or nil. w.mu is
// held.
func (c *Controller) takeRunLocked(w *worldRun, cont *docker.Container, read *crashLog) (stale *rcon.Client) {
	now := c.now()
	if n := cont.RestartCount - w.seenRestarts; n > 0 {
		// Docker restarted the game, which it only does after an exit
		// nobody asked for (SPEC 8.7): each restart is one crash.
		for range n {
			w.crashes = append(w.crashes, now)
		}
		w.lastCrash = now
		w.crashedRun = true
		w.run = runState{}
		w.save.runEnded()
		// Docker clears the out-of-memory flag when it restarts the game,
		// so an exit no look saw leaves the newest flag seen as it was.
		if read != nil {
			w.crashLog = read
		}
	}
	w.seenRestarts = cont.RestartCount
	w.crashes = keepSince(w.crashes, now.Add(-c.crashWindow))
	w.dockerStatus = cont.State.Status
	if exited(cont.State.Status) {
		exitSeenLocked(w, cont.State)
	}
	if !cont.State.StartedAt.Equal(w.startedAt) {
		if w.stopping && w.dockerStatus == "running" {
			// The same container runs a new run, so a stop asked for
			// earlier did not happen; its exit would be a crash again.
			w.stopping, w.expectRunning = false, !w.crashLoop
		}
		// A new run, including a `docker restart` that leaves the restart
		// count alone: nothing the last run knew still holds.
		w.run = runState{}
		w.save.runEnded()
		w.joins.Reset()
		w.startedAt = cont.State.StartedAt
		// The RCON connection went to the run that ended; the first command
		// on it would fail, so the next one dials the new run.
		stale = w.rc
	}
	return stale
}

// crashLoopLocked stops a game that crashed CrashLoopCount times within
// the window, and asks again while a crash-loop stop has not held (SPEC
// 8.9). cid is the world's container. w.mu is held.
func (c *Controller) crashLoopLocked(w *worldRun, cid string) {
	active := w.dockerStatus == "running" || w.dockerStatus == "restarting"
	switch {
	case len(w.crashes) >= CrashLoopCount && !w.crashLoop && !w.stopping:
		w.crashLoop, w.stopping, w.expectRunning = true, true, false
		c.log.Warn("the game crashed again and again, so Foreman stopped it", "world", w.id)
		c.stopCrashLoopLocked(w, cid)
	case w.crashLoop && !w.stopping && !w.crashStopping && active:
		// Docker refused the earlier stop, or the game ran again: ask once
		// more, at most once a poll (SPEC 8.9).
		w.stopping = true
		c.stopCrashLoopLocked(w, cid)
	}
}

// exited reports whether a container in this Docker state has a game
// that ended, so its out-of-memory flag and FinishedAt describe that exit.
// A running or paused game's flag was cleared when Docker started it, and
// its FinishedAt is an older exit's.
func exited(status string) bool {
	return status == "exited" || status == "restarting" || status == "dead"
}

// exitSeenLocked takes in Docker's out-of-memory flag from a look at the
// game's exit (SPEC 8.7) and reports whether it changed what is known:
// the flag names the exit, and a newer exit without it says that one was
// not. An exit no newer than the last one taken in is an older look's, so
// it changes nothing; nor does a look with no exit time. Foreman's own
// stop, of a crash loop say, is not the exit the flag describes, so it is
// passed over and a crash that ended before it may still be taken in.
// w.mu is held.
func exitSeenLocked(w *worldRun, st docker.State) bool {
	if !st.FinishedAt.After(w.exitAt) {
		return false
	}
	if !st.OOMKilled && w.stopping {
		return false
	}
	w.oomKilled, w.exitAt = st.OOMKilled, st.FinishedAt
	return true
}

// noteExit looks at the container once its log stream has ended, which it
// does when the game exits. Docker restarts the game within a fraction of
// a second and clears its out-of-memory flag when it does, often before
// the next poll (SPEC 8.7). A look at a game that already runs again, or
// taken across the start or end of an operation, is not taken in. One
// taken during an operation is, so a start can name an out-of-memory
// kill (see stoppedWhileLoading).
func (c *Controller) noteExit(w *worldRun, cid string) {
	w.mu.Lock()
	ops, ours := w.ops, w.containerID == cid
	w.mu.Unlock()
	if !ours {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, loadFailureReadLimit)
	cont, err := c.docker.InspectContainer(ctx, cid)
	cancel()
	if err != nil {
		return
	}
	if !exited(cont.State.Status) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ops != ops || w.containerID != cid {
		return
	}
	if exitSeenLocked(w, cont.State) {
		c.changedLocked(w, true)
	}
}

// lookStillHolds reports whether a look taken when the world had begun ops
// operations may still be taken in.
func (c *Controller) lookStillHolds(w *worldRun, ops uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.opDone == nil && w.ops == ops
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

// needsAttach reports whether the container, seen when the world's
// operation count was ops, is new to the world, or was taken on while
// stopped and now runs a game whose passwords are needed.
func (c *Controller) needsAttach(w *worldRun, cont docker.Container, ops uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opDone != nil || w.ops != ops {
		return false
	}
	return w.containerID != cont.ID || (w.noSecrets && gameLive(cont.State.Status))
}

// gameLive reports whether a container in this Docker state has a game
// process that may print secrets or answer over RCON.
func gameLive(status string) bool {
	return status == "running" || status == "restarting" || status == "paused"
}

// passwords checks that the world has an RCON password and returns it with
// every secret to keep out of the relayed log.
func (c *Controller) passwords(id string) (secret.Secret, []secret.Secret, error) {
	if err := c.hasRCONPassword(id); err != nil {
		return secret.Secret{}, nil, err
	}
	return c.secrets(id)
}

// attach takes on a container Foreman did not start in this run, such as a
// game left running across a Foreman restart (SPEC 3.8). A live game's own
// RCON password is needed both to talk to it and to keep it out of the
// relayed log, so without it nothing is taken on. A stopped container needs
// none: it is shown as stopped, and taken on again if it starts. A look
// taken before an operation began or ended is dropped (see worldRun.ops).
func (c *Controller) attach(w *worldRun, cont docker.Container, ops uint64) {
	live := gameLive(cont.State.Status)
	var ep endpoint
	var rconPW secret.Secret
	var secrets []secret.Secret
	var err error
	if live {
		// Re-attaching changes no world file (SPEC 3.8): a missing password
		// file is not made here, since the running game could not be using it.
		if rconPW, secrets, err = c.passwords(w.id); err != nil {
			err = fmt.Errorf("cannot read this world's RCON password, so it cannot watch the game: %w", err)
		}
		ep = c.attachEndpoint(w, cont)
	}
	if err != nil {
		var old *rcon.Client
		w.mu.Lock()
		if w.opDone != nil || w.ops != ops {
			w.mu.Unlock()
			return
		}
		warn := w.attachFailed != cont.ID
		w.attachFailed, w.attachErr = cont.ID, err
		if w.containerID == cont.ID {
			// Taken on while stopped: its log must not be followed now.
			old = c.dropContainerLocked(w)
		}
		w.mu.Unlock()
		closeRCON(old)
		if warn {
			c.log.Warn("cannot watch the game, so its log is not shown", "world", w.id, "error", err)
		}
		return
	}
	meta, err := c.readMeta(w.id)
	if err != nil {
		c.log.Warn("cannot read the world's settings", "world", w.id, "error", err)
	}
	var old *rcon.Client
	defer func() { closeRCON(old) }() // runs after the unlock below
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opDone != nil || w.ops != ops || (w.containerID == cont.ID && !w.noSecrets) {
		return
	}
	old = c.dropContainerLocked(w)
	w.noSecrets = !live
	w.attachFailed, w.attachErr = "", nil
	w.containerID, w.logMark = cont.ID, w.logs.Mark()
	w.seenRestarts = cont.RestartCount
	w.startedAt = cont.State.StartedAt
	w.dockerStatus = cont.State.Status
	w.expectRunning = cont.State.Status == "running" || cont.State.Status == "restarting"
	w.stopping, w.crashLoop, w.crashedRun = false, false, false
	w.crashes = nil
	w.run = runState{}
	w.save.runEnded()
	w.joins.Reset()
	// A container taken on is looked at afresh: what it did before was
	// reported by whoever saw it happen (SPEC 3.8).
	w.evState = ""
	w.secrets = secrets
	w.gameVersion = meta.GameVersion
	w.image = cont.Image
	w.lanUnpublished = ep.lanUnpublished
	if live && ep.rcon != "" {
		w.rc = c.rconClient(cont, ep.rcon, rconPW)
		if cont.State.Status == "running" {
			rc := w.rc
			c.spawn(func() { c.probeInGame(w, cont.ID, rc) })
		}
	}
}

// attachEndpoint is gameEndpoint for a game being taken on. A game whose
// RCON cannot be found safely is still watched, without RCON: its stop
// then needs Force stop, as for any game Foreman cannot reach (SPEC 4.13,
// 7.6).
func (c *Controller) attachEndpoint(w *worldRun, cont docker.Container) endpoint {
	ep, err := c.gameEndpoint(c.ctx, cont)
	if err != nil {
		c.log.Warn("cannot reach the game over RCON", "world", w.id, "error", err)
	}
	return ep
}

// probeInGame marks a re-attached game in game when it answers over RCON.
// The game opens RCON only once in game, and the log line that said so may
// be gone from Docker's rotated log (SPEC 3.8).
func (c *Controller) probeInGame(w *worldRun, cid string, rc *rcon.Client) {
	if !c.answers(c.ctx, rc) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.containerID != cid || w.rc != rc || w.run.inGame || w.run.loadFailed || w.crashedRun {
		return
	}
	w.run.inGame = true
	c.changedLocked(w, true)
}

// dropContainerLocked forgets the current container: its log follower
// ends. It returns the container's RCON connection, which the caller
// closes with closeRCON once w.mu is released. w.mu is held.
func (c *Controller) dropContainerLocked(w *worldRun) *rcon.Client {
	if w.followCancel != nil {
		w.followCancel()
	}
	w.following, w.followCancel = false, nil
	w.followedRun, w.lastLineAt = time.Time{}, time.Time{}
	rc := w.rc
	w.rc = nil
	w.containerID, w.dockerStatus = "", ""
	w.lanUnpublished = ""
	w.noSecrets = false
	w.startedAt = time.Time{}
	w.usage = usage{}
	w.expectRunning = false
	w.oomKilled, w.exitAt, w.lastDie = false, time.Time{}, time.Time{}
	w.crashLog, w.runStarts = nil, nil
	return rc
}

// stopCrashLoopLocked starts a crash-loop stop unless one is still waiting
// on Docker, which can take the whole crash-loop grace. w.mu is held.
func (c *Controller) stopCrashLoopLocked(w *worldRun, cid string) {
	if w.crashStopping {
		return
	}
	if c.spawn(func() { c.stopCrashLoop(w, cid) }) {
		w.crashStopping = true
	}
}

// stopCrashLoop stops a crash-looping game, which keeps it stopped under
// unless-stopped (SPEC 4.3, 8.9), with the short crash-loop grace (see
// CrashLoopStopGraceSeconds). When Docker refuses, the stopping mark is
// dropped so the next poll tries again; the state stays crash-looping.
func (c *Controller) stopCrashLoop(w *worldRun, cid string) {
	err := c.docker.StopContainer(c.ctx, cid, CrashLoopStopGraceSeconds)
	refused := err != nil && !errors.Is(err, docker.ErrNotModified) && !errors.Is(err, docker.ErrNotFound)
	if refused {
		c.log.Warn("could not stop the crash-looping game, will try again", "world", w.id, "container", cid, "error", err)
	}
	w.mu.Lock()
	w.crashStopping = false
	if refused && w.containerID == cid && w.crashLoop && w.opDone == nil {
		w.stopping = false
	}
	w.mu.Unlock()
	if !refused {
		c.pokeMonitor()
	}
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
		began := time.Now()
		err := c.follow(ctx, w, cid, since)
		w.mu.Lock()
		if w.followGen == gen {
			w.following, w.followCancel = false, nil
		}
		w.mu.Unlock()
		if err == nil && ctx.Err() == nil {
			c.noteExit(w, cid)
		}
		// The stream ends when the game exits: look at once (SPEC 8.7). A
		// stream that failed at once waits for the next poll instead, so a
		// broken log does not make a tight loop.
		if err == nil || time.Since(began) >= followMinRun {
			c.pokeMonitor()
		}
	})
	if !started {
		cancel()
		w.following, w.followCancel = false, nil
	}
}

// follow relays the container's log until it ends or ctx does. It returns
// nil when the stream ended as it does when the game exits, and the error
// when it could not be opened or broke off.
func (c *Controller) follow(ctx context.Context, w *worldRun, cid string, since time.Time) error {
	s, err := c.docker.Logs(ctx, cid, docker.LogOptions{Follow: true, Since: since, Timestamps: true})
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn("cannot follow the game's log", "world", w.id, "error", err)
		}
		return err
	}
	defer func() { _ = s.Close() }()
	for {
		l, err := s.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		c.handleLine(w, cid, l.Text)
	}
}

// handleLine redacts one log line, keeps it for pages, and acts on what it
// says (SPEC 8.6). The line comes with Docker's timestamp, which is kept
// to resume from and left off what pages show. The line is added and
// published under w.mu, so Watch never misses or repeats it.
func (c *Controller) handleLine(w *worldRun, cid, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.containerID != cid {
		return
	}
	// Only what is kept and shown is redacted: a short password such as
	// "0" also matches inside the timestamps the line is understood by.
	at, raw := splitDockerTime(text)
	if !at.IsZero() {
		w.lastLineAt = at
	}
	live := w.liveLineLocked(at)
	line := w.redact(raw)
	w.logs.Add(line)
	w.hub.Publish(Update{Kind: UpdateLine, Line: line})

	ev := ParseLine(text)
	publish := true
	switch ev.Kind {
	case KindOther:
		return
	case KindLoadStarted:
		// Every run of the game starts here; players from the last run are
		// gone, and a save the last run was writing ended with it.
		w.joins.Reset()
		w.save.runEnded()
		if !at.IsZero() {
			if len(w.runStarts) == maxRunStarts {
				w.runStarts = slices.Delete(w.runStarts, 0, 1)
			}
			w.runStarts = append(w.runStarts, runStart{mark: w.logs.Mark(), at: at})
		}
		// Kept past the run's end: the autosave tidy skips it (SPEC 11.6).
		w.lastLoaded = loadedSave(raw)
		if w.run.inGame {
			// A game process loads its map once, and observe resets the run
			// on a restart, so this is a re-attach replaying the log after
			// the RCON probe found the game in game (SPEC 3.8).
			w.run.save = w.saveName(raw)
			break
		}
		w.run = runState{save: w.saveName(raw)}
	case KindInGame:
		// The game is back: what named the last crash no longer holds,
		// unless a look already saw an exit after this line. A look at an
		// exit before it is an old one.
		w.run.inGame = true
		w.crashedRun = false
		if at.IsZero() || !w.exitAt.After(at) {
			w.oomKilled, w.crashLog = false, nil
			if at.After(w.exitAt) {
				w.exitAt = at
			}
		}
	case KindLoadError:
		w.run.loadError = shownText(w.redact, ev.Detail)
		publish = false
	case KindLoadFailed:
		w.run.loadFailed, w.run.inGame = true, false
		c.pokeMonitor()
	case KindSaveStarted, KindServerSaveStarted, KindSaveFinished, KindSaveFailed:
		w.save.observe(ev)
		publish = false
	case KindPlayerJoined, KindPlayerLeft:
		w.joins.Observe(ev)
		publish = false
		if live {
			e := notify.PlayerJoin
			if ev.Kind == KindPlayerLeft {
				e = notify.PlayerLeave
			}
			c.emitLocked(w, notify.Message{Event: e, Player: ev.Player})
		}
	default:
		publish = false
	}
	// A replayed line updates what is known without reporting it again.
	w.evQuiet = !live
	c.changedLocked(w, publish)
	w.evQuiet = false
}

// redact hides the world's secrets and any secret a printed command set,
// for text that a page may show (SPEC 8.6, 14.8). w.mu is held.
func (w *worldRun) redact(s string) string {
	return maskSecretCommand(Redact(s, w.secrets...))
}

// saveName reads the save's name from the unredacted line, so a short
// password inside it does not mask it, and hides it only when it is itself
// a secret. w.mu is held.
func (w *worldRun) saveName(raw string) string {
	name := loadedSave(raw)
	for _, s := range w.secrets {
		if name == s.Reveal() {
			return w.redact(name)
		}
	}
	return name
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
	u := usage{ok: true, memory: s.MemoryUsage, limit: s.MemoryLimit}
	if s.MemoryInactive < s.MemoryUsage {
		u.memory = s.MemoryUsage - s.MemoryInactive
	}
	if s.CPUTotal > s.PreCPUTotal && s.CPUSystem > s.PreCPUSystem {
		cpus := max(s.OnlineCPUs, 1)
		u.cpu = float64(s.CPUTotal-s.PreCPUTotal) / float64(s.CPUSystem-s.PreCPUSystem) * float64(cpus) * 100
	}
	return u
}
