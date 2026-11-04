package game

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// State is the game's state as Foreman shows it (SPEC 7.1, R16). The values
// match the badge classes in DESIGN 7.5.
type State string

// The states of a world's game.
const (
	StateStopped      State = "stopped"
	StateStarting     State = "starting"
	StateRunning      State = "running" // the server reached InGame
	StateStopping     State = "stopping"
	StateCrashed      State = "crashed"
	StateCrashLooping State = "crash-looping"
	StateUnknown      State = "unknown" // Docker cannot be asked; Status.Reason says why
)

// Limits from the spec.
const (
	// StartTimeout is how long a start may take to reach InGame and answer
	// over RCON (SPEC 7.3).
	StartTimeout = 5 * time.Minute
	// StopCountdown is how long players are warned before a graceful stop
	// (SPEC 7.6, R21: a constant until the settings page).
	StopCountdown = 60 * time.Second
	// StopGraceSeconds is how long Docker waits for the game to save and
	// exit after SIGTERM before it kills it (R28).
	StopGraceSeconds = 120
	// CrashLoopCount crashes within CrashLoopWindow make a crash loop (SPEC 8.9).
	CrashLoopCount  = 3
	CrashLoopWindow = 10 * time.Minute
	// CrashLines is how many log lines are shown with a crash (SPEC 8.7).
	CrashLines = 20
)

// Defaults that the spec leaves to Foreman.
const (
	pollInterval = 5 * time.Second        // how often the monitor inspects each container (R13)
	saveTimeout  = 2 * time.Minute        // how long /server-save may take to finish
	rconRetry    = 500 * time.Millisecond // how often a start retries RCON after InGame
	hubBuffer    = 256                    // events a slow page may fall behind by before it is dropped
	followMinRun = time.Second            // a log stream that broke off sooner is retried at the next poll
)

// Audit actions this package records (SPEC 12.9, R11).
const (
	AuditStart         = "start"
	AuditStop          = "stop"
	AuditRestart       = "restart"
	AuditForceStop     = "force-stop"
	AuditImageDownload = "image-download"
	AuditConsole       = "console"
)

// Errors a caller can test for with errors.Is. The text is shown to the admin.
var (
	ErrAlreadyRunning  = errors.New("the game is already running. Stop it first")
	ErrNotRunning      = errors.New("the game is not running")
	ErrNotReady        = errors.New("the game is starting or restarting after a crash. Wait until it is running, or stop it, then try again")
	ErrImageMissing    = errors.New("the game image for this world's Factorio version is not downloaded. Download the game image, then start again")
	ErrRCONUnreachable = errors.New("cannot reach the game over RCON, so it cannot warn players or save first. Use Force stop to stop it without saving")
	ErrSaveTimeout     = errors.New("the game did not finish saving in time, so nothing was stopped or snapshotted. Try again, or use Force stop")
	ErrStartFailed     = errors.New("the game did not start")
	ErrCancelled       = errors.New("a forced stop interrupted this")
	ErrDownloading     = errors.New("the game image is already being downloaded")
	ErrForeign         = errors.New("a Docker container with this world's container name exists, but Foreman did not create it. Rename or remove that container, then try again")
)

// PrecheckError is a start refused by the pre-start check (SPEC 7.4). The
// caller may offer "Start anyway" when Check.CanStartAnyway() (R5, R17).
type PrecheckError struct {
	Check world.PreCheck
}

func (e *PrecheckError) Error() string {
	texts := make([]string, 0, len(e.Check.Problems))
	for _, p := range e.Check.Problems {
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, " ")
}

// Actor is the user an action is done for (SPEC 12.9).
type Actor struct {
	UserID   int64
	Username string
}

// AuditLog records who did what. *store.Store satisfies it.
type AuditLog interface {
	AddAudit(ctx context.Context, e store.AuditEntry) error
}

// Config sets up a Controller. main.go fills it from the environment.
type Config struct {
	Docker *docker.Client
	Worlds *world.Manager
	Audit  AuditLog
	// HostDataDir is the data directory's path on the Docker host, for the
	// bind mount and the install label (SPEC 3.5, R2).
	HostDataDir string
	// GameUID and GameGID are what the game runs as (SPEC 3.6, R1).
	GameUID, GameGID int
	// Images maps a game's major.minor version ("2.0") to the pinned image
	// reference that runs it (R8, R32).
	Images map[string]string
	// Network is FOREMAN_GAME_NETWORK. Outside a container empty means
	// Docker's default bridge; inside one it is required (R36).
	Network string
	// DockerHost is DOCKER_HOST. Only its scheme is used: over tcp:// a
	// socket proxy shares a network with Foreman (R36).
	DockerHost string
	// TrustedProxies is FOREMAN_TRUSTED_PROXIES; the game network must not
	// overlap it (R36, SPEC 12.6).
	TrustedProxies []netip.Prefix
	// SelfID is Foreman's own container (docker.SelfID); empty outside one.
	SelfID string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Log takes Foreman's own messages; nil means slog.Default.
	Log *slog.Logger
}

// Controller runs each world's game container (SPEC 4.3 to 4.6, 7, 8.1,
// 8.4, 8.6, 8.7, 8.9). Every method takes a world id, so nothing assumes
// one world (SPEC 5.3). It is safe for concurrent use.
type Controller struct {
	docker   *docker.Client
	worlds   *world.Manager
	auditLog AuditLog
	hostData string
	user     string
	images   map[string]string
	network  string
	selfID   string
	// dockerTCP and proxies are what the game network is checked against (R36).
	dockerTCP bool
	proxies   []netip.Prefix
	now       func() time.Time
	log       *slog.Logger

	// Limits, shortened by tests.
	pollInterval  time.Duration
	startTimeout  time.Duration
	saveTimeout   time.Duration
	rconRetry     time.Duration
	crashWindow   time.Duration
	rconCallLimit time.Duration

	ctx     context.Context // ends when Run returns; log followers live in it
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	spawnMu sync.Mutex
	closed  bool          // Run has returned; no goroutine may start
	poke    chan struct{} // asks the monitor to look now

	mu        sync.Mutex // guards the fields below; never held while taking a worldRun's mu
	runs      map[string]*worldRun
	listed    bool  // the monitor has listed Foreman's containers at least once
	dockerErr error // the last listing failed
}

// New checks cfg and returns a Controller. It contacts nothing; Run starts
// the monitor.
func New(cfg Config) (*Controller, error) {
	if cfg.Docker == nil || cfg.Worlds == nil || cfg.Audit == nil {
		return nil, errors.New("game controller: Docker, Worlds and Audit are required")
	}
	// The path goes into a "host:container" bind, so a colon or comma would
	// change the mount (SPEC 4.6).
	if !filepath.IsAbs(cfg.HostDataDir) || filepath.Clean(cfg.HostDataDir) != cfg.HostDataDir ||
		strings.ContainsAny(cfg.HostDataDir, ":,") {
		return nil, fmt.Errorf("the host data directory %q must be a clean absolute path without a colon or comma", cfg.HostDataDir)
	}
	if cfg.GameUID < 0 || cfg.GameGID < 0 {
		return nil, errors.New("the game's user and group IDs must not be negative")
	}
	if len(cfg.Images) == 0 {
		return nil, errors.New("game controller: no game images are configured")
	}
	if err := checkNetworkName(cfg.Network); err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{
		docker: cfg.Docker, worlds: cfg.Worlds, auditLog: cfg.Audit,
		hostData: cfg.HostDataDir,
		user:     fmt.Sprintf("%d:%d", cfg.GameUID, cfg.GameGID),
		images:   cfg.Images, network: cfg.Network, selfID: cfg.SelfID,
		dockerTCP: strings.HasPrefix(cfg.DockerHost, "tcp://"), proxies: cfg.TrustedProxies,
		now: now, log: log,
		pollInterval: pollInterval, startTimeout: StartTimeout, saveTimeout: saveTimeout,
		rconRetry: rconRetry, crashWindow: CrashLoopWindow,
		rconCallLimit: rcon.DefaultTimeout,
		ctx:           ctx, cancel: cancel,
		poke: make(chan struct{}, 1),
		runs: map[string]*worldRun{},
	}, nil
}

// worldRun is what the controller knows about one world's game.
type worldRun struct {
	id    string
	hub   *Hub[Update]
	logs  *LogBuffer
	joins *JoinTimes

	mu            sync.Mutex
	containerID   string // "" when there is no container
	dockerStatus  string // Docker's State.Status at the last look; "" without a container
	inspectErr    error  // the last inspect failed
	startedAt     time.Time
	seenRestarts  int  // Docker's restart count at the last look
	expectRunning bool // the game should be running: an exit nobody asked for is a crash
	stopping      bool // Foreman asked the container to stop: its exit is never a crash (SPEC 7.6)
	crashLoop     bool
	crashes       []time.Time // crashes seen within the crash-loop window
	lastCrash     time.Time
	crashedRun    bool // Docker restarted the game after a crash and it is not back in game
	run           runState
	saves         int           // "Saving finished" lines seen
	changed       chan struct{} // closed and replaced whenever the game's state moves
	rc            *rcon.Client
	secrets       []secret.Secret // removed from every relayed line (SPEC 8.6)
	gameVersion   string
	image         string
	usage         usage

	following    bool
	followGen    int
	followCancel context.CancelFunc
	followedRun  time.Time // the run the follower reads
	lastLineAt   time.Time // Docker's time for the last line read, to resume from

	op       *Operation // the operation running now, or the last one
	opState  State      // what the operation shows while it runs; "" to let the container decide
	opCancel context.CancelFunc
	opDone   chan struct{} // nil when no operation is running
	download *Operation

	adoptFailed string // a container whose world's secrets could not be read, warned about once
	adoptErr    error  // why that container could not be taken on
}

// runState is what the log said about the current run of the game.
type runState struct {
	inGame     bool
	loadFailed bool
	loadError  string // the game's reason for the failed load
	save       string // the save it loaded
}

type usage struct {
	ok     bool
	cpu    float64
	memory uint64
}

// world returns the record for id, making it on first use.
func (c *Controller) world(id string) *worldRun {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.runs[id]
	if w == nil {
		w = &worldRun{
			id: id, hub: NewHub[Update](hubBuffer), logs: NewLogBuffer(), joins: NewJoinTimes(),
			changed: make(chan struct{}),
		}
		c.runs[id] = w
	}
	return w
}

// allRuns returns every world record known so far.
func (c *Controller) allRuns() []*worldRun {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*worldRun, 0, len(c.runs))
	for _, w := range c.runs {
		out = append(out, w)
	}
	return out
}

// dockerState reports whether Docker has been listed yet and the last
// listing error.
func (c *Controller) dockerState() (listed bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listed, c.dockerErr
}

// UpdateKind says what an Update carries.
type UpdateKind int

// The kinds of Update.
const (
	UpdateStatus UpdateKind = iota // Status holds the new status
	UpdateLine                     // Line holds a new log line, already redacted
)

// Update is one change a watching page is told about.
type Update struct {
	Kind   UpdateKind
	Status Status
	Line   string
}

// StepState is the word shown beside a step (DESIGN 7.6).
type StepState string

// The step words.
const (
	StepWaiting  StepState = "waiting"
	StepProgress StepState = "progress"
	StepDone     StepState = "done"
	StepFailed   StepState = "failed"
	StepSkipped  StepState = "skipped"
)

// Step is one named step of an operation.
type Step struct {
	Name  string
	State StepState
}

// Operation is a start, stop, restart, restore or image download in
// progress, or the last one to finish.
type Operation struct {
	Action string // one of the Audit* actions, or "restore"
	Steps  []Step
	Done   bool
	Error  string // why it failed; empty when it worked or is running
}

func (o *Operation) clone() *Operation {
	if o == nil {
		return nil
	}
	out := *o
	out.Steps = append([]Step(nil), o.Steps...)
	return &out
}

// Status is everything the server page shows about a world's game
// (SPEC 7.1, 7.2, 8.7).
type Status struct {
	World string
	State State
	// Reason says why the state is Unknown. A crash's reason is not named
	// in this version (SPEC 8.7 P1): CrashLines are shown instead.
	Reason string
	// CrashLines are the last log lines while crashed or crash-looping.
	CrashLines []string
	LastCrash  time.Time
	// StartedAt is when the current run began; zero when not running.
	StartedAt   time.Time
	Uptime      time.Duration
	GameVersion string
	Image       string
	// Save is the save the current run loaded, from its log.
	Save        string
	HaveUsage   bool
	CPUPercent  float64
	MemoryBytes uint64
	// Operation is the operation running now, or the last one.
	Operation *Operation
	// Download is the image download running now, or the last one.
	Download *Operation
}

// View is what a page shows when it opens: the status and the log so far.
type View struct {
	Status Status
	Lines  []string
}

// Status returns the world's status as last seen. It makes no Docker call.
func (c *Controller) Status(id string) Status {
	w := c.world(id)
	w.mu.Lock()
	defer w.mu.Unlock()
	return c.statusLocked(w)
}

// Watch returns the world's status and log so far, and a channel of the
// changes after them. The two are taken together, so a page sees every
// line once, with no gap. Call the function to stop watching; the channel
// is also closed when the page falls too far behind.
func (c *Controller) Watch(id string) (View, <-chan Update, func()) {
	w := c.world(id)
	w.mu.Lock()
	defer w.mu.Unlock()
	v := View{Status: c.statusLocked(w), Lines: w.logs.Lines()}
	ch, stop := w.hub.Subscribe()
	return v, ch, stop
}

// Lines returns the world's log so far, for a page that does not follow
// it live.
func (c *Controller) Lines(id string) []string {
	return c.world(id).logs.Lines()
}

// statusLocked builds the status. w.mu is held.
func (c *Controller) statusLocked(w *worldRun) Status {
	st := Status{
		World: w.id, LastCrash: w.lastCrash, GameVersion: w.gameVersion, Image: w.image,
		Save: w.run.save, Operation: w.op.clone(), Download: w.download.clone(),
	}
	st.State, st.Reason = c.stateLocked(w)
	if st.State == StateRunning || st.State == StateStarting || st.State == StateStopping || st.State == StateCrashed {
		if w.dockerStatus == "running" && !w.startedAt.IsZero() {
			st.StartedAt = w.startedAt
			st.Uptime = max(c.now().Sub(w.startedAt), 0)
		}
	}
	if w.dockerStatus == "running" && w.usage.ok {
		st.HaveUsage, st.CPUPercent, st.MemoryBytes = true, w.usage.cpu, w.usage.memory
	}
	if st.State == StateCrashed || st.State == StateCrashLooping {
		lines := w.logs.Lines()
		st.CrashLines = lines[max(len(lines)-CrashLines, 0):]
	}
	return st
}

// stateLocked decides the state from what Docker and the log said
// (SPEC 7.1, 8.7, 8.9, R16, R27). The exit code is never used: 1.1 exits 0
// after a failed load. w.mu is held.
func (c *Controller) stateLocked(w *worldRun) (State, string) {
	if w.opDone != nil && w.opState != "" {
		return w.opState, ""
	}
	listed, dockerErr := c.dockerState()
	if w.opDone == nil {
		if dockerErr != nil {
			return StateUnknown, "Foreman cannot reach Docker: " + dockerErr.Error()
		}
		if w.inspectErr != nil {
			return StateUnknown, "Foreman cannot read the game container from Docker: " + w.inspectErr.Error()
		}
		if w.adoptErr != nil && w.containerID == "" {
			return StateUnknown, "Foreman cannot read this world's RCON password, so it cannot watch the game: " + w.adoptErr.Error()
		}
		if !listed && w.containerID == "" {
			return StateUnknown, "Foreman is still asking Docker about the game."
		}
	}
	if w.crashLoop {
		return StateCrashLooping, ""
	}
	switch w.dockerStatus {
	case "":
		return StateStopped, ""
	case "running":
		switch {
		case w.stopping:
			return StateStopping, ""
		case w.run.loadFailed || w.crashedRun:
			return StateCrashed, ""
		case w.run.inGame:
			return StateRunning, ""
		}
		return StateStarting, ""
	case "restarting":
		if w.stopping {
			return StateStopping, ""
		}
		return StateCrashed, ""
	case "paused":
		return StateUnknown, "The game container is paused in Docker. Foreman never pauses it; unpause it with Docker."
	case "created":
		// Docker never ran it, so it cannot have crashed.
		return StateStopped, ""
	}
	if w.expectRunning && !w.stopping {
		return StateCrashed, ""
	}
	return StateStopped, ""
}

// changedLocked wakes anything waiting on the game's state and, when
// publish is set, sends the status to watching pages. w.mu is held.
func (c *Controller) changedLocked(w *worldRun, publish bool) {
	close(w.changed)
	w.changed = make(chan struct{})
	if publish {
		w.hub.Publish(Update{Kind: UpdateStatus, Status: c.statusLocked(w)})
	}
}

// publish sends the world's status to watching pages.
func (c *Controller) publish(w *worldRun) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c.changedLocked(w, true)
}

// pokeMonitor asks the monitor to look at the containers now.
func (c *Controller) pokeMonitor() {
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

// audit records an action. When it cannot, the action does not go ahead,
// so nothing is done that the audit log does not show (SPEC 12.9).
func (c *Controller) audit(ctx context.Context, by Actor, action, id, detail string) error {
	err := c.auditLog.AddAudit(ctx, store.AuditEntry{
		Time: c.now(), UserID: by.UserID, Username: by.Username,
		Action: action, WorldID: id, Detail: detail,
	})
	if err != nil {
		return fmt.Errorf("could not record this in the audit log, so it was not done: %w", err)
	}
	return nil
}
