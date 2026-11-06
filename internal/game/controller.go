package game

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
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

// State is the game's state as Foreman shows it (SPEC 7.1). The values
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
	// (SPEC 7.6: a constant until the settings page).
	StopCountdown = 60 * time.Second
	// StopGraceSeconds is how long Docker waits for the game to save and
	// exit after SIGTERM before it kills it (SPEC 7.6).
	StopGraceSeconds = 120
	// CrashLoopCount crashes within CrashLoopWindow make a crash loop (SPEC 8.9).
	CrashLoopCount  = 3
	CrashLoopWindow = 10 * time.Minute
)

// Defaults that the spec leaves to Foreman.
const (
	pollInterval = 5 * time.Second        // how often the monitor inspects each container (SPEC 8.7)
	saveTimeout  = 2 * time.Minute        // how long /server-save may take to finish
	rconRetry    = 500 * time.Millisecond // how often a start retries RCON after InGame
	hubBuffer    = 256                    // events a slow page may fall behind by before it is dropped
	followMinRun = time.Second            // a log stream that broke off sooner is retried at the next poll
)

// Audit actions this package records (SPEC 12.9).
const (
	AuditStart         = "start"
	AuditStop          = "stop"
	AuditRestart       = "restart"
	AuditForceStop     = "force-stop"
	AuditStopCancel    = "stop-cancel"
	AuditImageDownload = "image-download"
	AuditConsole       = "console"
	AuditSwitch        = "switch"
	AuditUpdate        = "game-update"
	AuditRollback      = "rollback"
	// A start, restart, switch or update the checks refused before anything
	// was done is recorded under its own action, with the reason (SPEC 12.9).
	AuditStartRefused   = "start-refused"
	AuditRestartRefused = "restart-refused"
	AuditSwitchRefused  = "switch-refused"
	AuditUpdateRefused  = "game-update-refused"
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
	ErrStopCancelled   = errors.New("the stop was cancelled, so nothing was stopped or saved and the game runs on")
	ErrNoCountdown     = errors.New("there is no stop countdown to cancel. It may have just ended")
	ErrDownloading     = errors.New("the game image is already being downloaded")
	ErrForeign         = errors.New("a Docker container with this world's container name exists, but Foreman did not create it. Rename or remove that container, then try again")
)

// ImageDownloadError is a failed download of a game image. Err is the
// cause, for the log; the page words the failure itself (SPEC 17.9).
type ImageDownloadError struct {
	Image string
	// NotFound is true when Docker answered that it has no such image.
	NotFound bool
	Err      error
}

func (e *ImageDownloadError) Error() string { return "download " + e.Image + ": " + e.Err.Error() }
func (e *ImageDownloadError) Unwrap() error { return e.Err }

// pullError words a failed pull of ref. A Docker that answers nothing keeps
// its own plain sentence.
func pullError(ref string, err error) error {
	if _, ok := errors.AsType[*docker.UnreachableError](err); ok {
		return err
	}
	return &ImageDownloadError{Image: ref, NotFound: errors.Is(err, docker.ErrNotFound), Err: err}
}

// PrecheckError is a start refused by the pre-start check (SPEC 7.4). The
// caller may offer "Start anyway" when Check.CanStartAnyway() (SPEC 7.4).
type PrecheckError struct {
	Check world.PreCheck
	// Changed is set when a go-ahead anyway was refused because the
	// problems, or whether the game runs, differ from what the admin
	// confirmed (SPEC 7.4, 17.5).
	Changed bool
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
	// Switches records switches in progress and the active world (SPEC 5.3,
	// 7.10). Without it Switch refuses.
	Switches SwitchStore
	// HostDataDir is the data directory's path on the Docker host, for the
	// bind mount and the install label (SPEC 3.5, 4.3).
	HostDataDir string
	// GameUID and GameGID are what the game runs as (SPEC 3.6, 4.6).
	GameUID, GameGID int
	// Network is FOREMAN_GAME_NETWORK. Outside a container empty means
	// Docker's default bridge; inside one it is required (SPEC 4.4).
	Network string
	// DockerHost is DOCKER_HOST. Over tcp:// a socket proxy shares a
	// network with Foreman, so the game network must not hold its address
	// (SPEC 4.4).
	DockerHost string
	// TrustedProxies is FOREMAN_TRUSTED_PROXIES; the game network must not
	// overlap it (SPEC 4.4, 12.6).
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
	switches SwitchStore
	hostData string
	user     string
	network  string
	selfID   string
	// What the game network is checked against (SPEC 4.4). dockerHost is the
	// host DOCKER_HOST names over tcp://.
	dockerTCP  bool
	dockerHost string
	proxies    []netip.Prefix
	// containerEnvs are the files that mark a container (Docker's and
	// Podman's), and lookupHost the DNS lookup; tests change both.
	containerEnvs []string
	lookupHost    func(ctx context.Context, host string) ([]string, error)
	now           func() time.Time
	log           *slog.Logger

	// Limits, shortened by tests.
	pollInterval  time.Duration
	startTimeout  time.Duration
	saveTimeout   time.Duration
	rconRetry     time.Duration
	crashWindow   time.Duration
	rconCallLimit time.Duration
	previewLimit  time.Duration

	previewWait time.Duration

	// previewSlot holds a token while a map preview runs: one at a time
	// (SPEC 14.3).
	previewSlot chan struct{}
	// previewWaiting holds a token for each caller waiting for the slot.
	previewWaiting chan struct{}

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
	// subnets are the game networks' address ranges at the last good read;
	// subnetsKnown says there has been one (SPEC 12.6).
	subnets      []netip.Prefix
	subnetsKnown bool
}

// New checks cfg and returns a Controller. It contacts nothing; Run starts
// the monitor.
func New(cfg Config) (*Controller, error) {
	if cfg.Docker == nil || cfg.Worlds == nil || cfg.Audit == nil {
		return nil, errors.New("game controller: Docker, Worlds and Audit are required")
	}
	if cfg.GameUID < 0 || cfg.GameGID < 0 {
		return nil, errors.New("the game's user and group IDs must not be negative")
	}
	// The path goes into a "host:container" bind, so a colon or comma
	// would change the mount (SPEC 4.6).
	if !filepath.IsAbs(cfg.HostDataDir) || filepath.Clean(cfg.HostDataDir) != cfg.HostDataDir ||
		strings.ContainsAny(cfg.HostDataDir, ":,") {
		return nil, fmt.Errorf("the host data directory %q must be a clean absolute path without a colon or comma", cfg.HostDataDir)
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
		docker: cfg.Docker, worlds: cfg.Worlds, auditLog: cfg.Audit, switches: cfg.Switches,
		hostData: cfg.HostDataDir,
		user:     fmt.Sprintf("%d:%d", cfg.GameUID, cfg.GameGID),
		network:  cfg.Network, selfID: cfg.SelfID,
		dockerTCP: strings.HasPrefix(cfg.DockerHost, "tcp://"), dockerHost: tcpHost(cfg.DockerHost),
		proxies: cfg.TrustedProxies, containerEnvs: []string{docker.DockerEnvPath, docker.PodmanEnvPath}, lookupHost: net.DefaultResolver.LookupHost,
		now: now, log: log,
		pollInterval: pollInterval, startTimeout: StartTimeout, saveTimeout: saveTimeout,
		rconRetry: rconRetry, crashWindow: CrashLoopWindow,
		rconCallLimit: rcon.DefaultTimeout, previewLimit: PreviewLimit, previewWait: PreviewWait,
		previewSlot: make(chan struct{}, 1), previewWaiting: make(chan struct{}, maxPreviewWaiters),
		ctx: ctx, cancel: cancel,
		poke: make(chan struct{}, 1),
		runs: map[string]*worldRun{},
	}, nil
}

// tcpHost returns the host a tcp:// DOCKER_HOST names, else "".
func tcpHost(dockerHost string) string {
	u, err := url.Parse(dockerHost)
	if err != nil || u.Scheme != "tcp" {
		return ""
	}
	return u.Hostname()
}

// worldRun is what the controller knows about one world's game.
type worldRun struct {
	id    string
	hub   *Hub[Update]
	logs  *LogBuffer
	joins *JoinTimes

	mu          sync.Mutex
	containerID string // "" when there is no container
	// logMark is where the current container's lines begin in logs, so a
	// crash reason is never taken from an older container (SPEC 8.7).
	logMark       uint64
	dockerStatus  string // Docker's State.Status at the last look; "" without a container
	inspectErr    error  // the last inspect failed
	startedAt     time.Time
	seenRestarts  int  // Docker's restart count at the last look
	expectRunning bool // the game should be running: an exit nobody asked for is a crash
	stopping      bool // Foreman asked the container to stop: its exit is never a crash (SPEC 7.6)
	crashLoop     bool
	crashStopping bool        // a crash-loop stop is waiting on Docker; one at a time (SPEC 8.9)
	crashes       []time.Time // crashes seen within the crash-loop window
	lastCrash     time.Time
	crashedRun    bool // Docker restarted the game after a crash and it is not back in game
	run           runState
	saves         int           // "Saving finished" lines seen
	changed       chan struct{} // closed and replaced whenever the game's state moves
	rc            *rcon.Client
	secrets       []secret.Secret // removed from every relayed line (SPEC 8.6)
	// noSecrets is set when the container was taken on while stopped,
	// without its world's passwords; it is taken on again before its log
	// is followed.
	noSecrets   bool
	gameVersion string
	image       string
	usage       usage

	following    bool
	followGen    int
	followCancel context.CancelFunc
	followedRun  time.Time // the run the follower reads
	lastLineAt   time.Time // Docker's time for the last line read, to resume from

	op       *Operation // the operation running now, or the last one
	opState  State      // what the operation shows while it runs; "" to let the container decide
	opCancel context.CancelFunc
	opDone   chan struct{} // nil when no operation is running
	opRun    *opRun        // the operation running now; nil when none
	// countdownCancel ends the stop countdown running now; nil when there
	// is none. It is set and cleared under mu, so a cancel is either in
	// time or refused.
	countdownCancel context.CancelCauseFunc
	download        *Operation
	notice          *SwitchNotice // an unfinished switch found at startup; cleared by the next operation

	attachFailed string // a container whose world's secrets could not be read, warned about once
	attachErr    error  // why that container could not be taken on
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
	// Until is when a countdown ends, set on the warn step once players are
	// being warned (SPEC 7.6); zero otherwise.
	Until time.Time
}

// Operation is a start, stop, restart, restore, switch or image download
// in progress, or the last one to finish.
type Operation struct {
	Action string // one of the Audit* actions, or "restore"
	Steps  []Step
	Done   bool
	Error  string // why it failed; empty when it worked or is running
	// Err is the error Error was made from, so a page can word it.
	Err error
	// Switch is set on a switch, which both worlds show (SPEC 7.9).
	Switch *SwitchInfo
	// Players are who was online when the stop's countdown began; empty
	// when there was no countdown (SPEC 7.6).
	Players []Player
}

// CountdownEnds returns when the stop's countdown ends while players are
// being warned, and false at any other time (SPEC 7.6).
func (o *Operation) CountdownEnds() (time.Time, bool) {
	if o == nil {
		return time.Time{}, false
	}
	for _, s := range o.Steps {
		if s.Name == StepWarn && s.State == StepProgress && !s.Until.IsZero() {
			return s.Until, true
		}
	}
	return time.Time{}, false
}

func (o *Operation) clone() *Operation {
	if o == nil {
		return nil
	}
	out := *o
	out.Steps = append([]Step(nil), o.Steps...)
	out.Players = append([]Player(nil), o.Players...)
	if o.Switch != nil {
		sw := *o.Switch
		out.Switch = &sw
	}
	return &out
}

// newOperation makes an operation whose steps all wait.
func newOperation(action string, steps ...[]string) *Operation {
	op := &Operation{Action: action}
	for _, list := range steps {
		for _, s := range list {
			op.Steps = append(op.Steps, Step{Name: s, State: StepWaiting})
		}
	}
	return op
}

// opRun is one operation in progress. A switch holds two worlds; every
// other operation holds one.
type opRun struct {
	c *Controller
	// w is the world the current step acts on. Only the operation's own
	// goroutine changes it; others read it once ended is closed.
	w      *worldRun
	ws     []*worldRun // every world the operation holds, in id order
	op     *Operation  // shared by every world in ws
	cancel context.CancelFunc
	ended  chan struct{}
}

// beginOp records an operation on one world and shows its steps. The
// returned context ends when ForceStop interrupts the operation. Call end
// when done.
func (c *Controller) beginOp(ctx context.Context, w *worldRun, action string, steps ...[]string) (context.Context, *opRun) {
	return c.beginOpOn(ctx, []*worldRun{w}, newOperation(action, steps...))
}

// beginOpOn records op on every world in ws, which must be in id order. Each
// world shows it and is busy until it ends, and a forced stop on any of them
// interrupts it. Starting an action clears a world's switch notice.
func (c *Controller) beginOpOn(ctx context.Context, ws []*worldRun, op *Operation) (context.Context, *opRun) {
	ctx, cancel := context.WithCancel(ctx)
	o := &opRun{c: c, w: ws[0], ws: ws, op: op, cancel: cancel, ended: make(chan struct{})}
	o.lockAll()
	for _, w := range ws {
		w.op, w.opState, w.opCancel, w.opDone, w.opRun = op, "", cancel, o.ended, o
		w.notice = nil
		c.changedLocked(w, true)
	}
	o.unlockAll()
	return ctx, o
}

// lockAll takes every world's mu in id order, so two operations never
// deadlock and a page never sees a step change on one world only.
func (o *opRun) lockAll() {
	for _, w := range o.ws {
		w.mu.Lock()
	}
}

func (o *opRun) unlockAll() {
	for i := len(o.ws) - 1; i >= 0; i-- {
		o.ws[i].mu.Unlock()
	}
}

// publishLocked sends the status of every world; lockAll is held.
func (o *opRun) publishLocked() {
	for _, w := range o.ws {
		o.c.changedLocked(w, true)
	}
}

func (o *opRun) set(state StepState, names ...string) {
	o.lockAll()
	defer o.unlockAll()
	for i := range o.op.Steps {
		for _, n := range names {
			if o.op.Steps[i].Name == n {
				o.op.Steps[i].State = state
			}
		}
	}
	o.publishLocked()
}

func (o *opRun) begin(name string)    { o.set(StepProgress, name) }
func (o *opRun) done(name string)     { o.set(StepDone, name) }
func (o *opRun) skip(names ...string) { o.set(StepSkipped, names...) }

// show sets the state the current world's page shows while the operation
// runs.
func (o *opRun) show(s State) {
	o.w.mu.Lock()
	defer o.w.mu.Unlock()
	o.w.opState = s
	o.c.changedLocked(o.w, true)
}

// end finishes the operation, marking the step in progress failed when err
// is set, and lets the monitor look after the containers again. It returns
// err, with an interruption by ForceStop as ErrCancelled.
func (o *opRun) end(err error) error {
	if errors.Is(err, context.Canceled) {
		err = ErrCancelled
	}
	o.lockAll()
	for i := range o.op.Steps {
		switch o.op.Steps[i].State {
		case StepProgress:
			o.op.Steps[i].State = StepDone
			if err != nil {
				o.op.Steps[i].State = StepFailed
			}
		case StepWaiting:
			// A step that will never run must not read as waiting.
			if err != nil {
				o.op.Steps[i].State = StepSkipped
			}
		}
	}
	o.op.Done = true
	if err != nil {
		o.op.Error, o.op.Err = err.Error(), err
	}
	o.cancel()
	for _, w := range o.ws {
		w.opState, w.opCancel, w.opDone, w.opRun = "", nil, nil, nil
	}
	o.publishLocked()
	o.unlockAll()
	close(o.ended)
	o.c.pokeMonitor()
	return err
}

// Status is everything the server page shows about a world's game
// (SPEC 7.1, 7.2, 8.7).
type Status struct {
	World string
	State State
	// Reason says why the state is Unknown; Unknown holds its facts, so a
	// page can word it (SPEC 22.3).
	Reason  string
	Unknown UnknownReason
	// CrashReason is the game's own reason for a crash, from its log, while
	// crashed or crash-looping; "" when the log names none (SPEC 8.7). It
	// is masked like every line a page shows, and is a sentence's end:
	// ": 2 mods were made for..." or ". Factorio said: ...".
	CrashReason string
	// Crash is what CrashReason is made from.
	Crash     LoadReason
	LastCrash time.Time
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
	// Notice reports an unfinished switch found at startup, on the world
	// that is active after it (SPEC 7.10); nil when there is none.
	Notice *SwitchNotice
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
	if w.notice != nil {
		n := *w.notice
		st.Notice = &n
	}
	st.State, st.Unknown = c.stateLocked(w)
	st.Reason = st.Unknown.Text()
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
		var f loadFailure
		for _, l := range w.logs.LinesSince(w.logMark) {
			f.add(l)
		}
		st.Crash = f.reason(w.redact)
		st.CrashReason = strings.TrimSuffix(st.Crash.text(), ".")
	}
	return st
}

// stateLocked decides the state from what Docker and the log said
// (SPEC 7.1, 8.7, 8.9). The exit code is never used: 1.1 exits 0
// after a failed load. w.mu is held.
func (c *Controller) stateLocked(w *worldRun) (State, UnknownReason) {
	if w.opDone != nil && w.opState != "" {
		return w.opState, UnknownReason{}
	}
	listed, dockerErr := c.dockerState()
	if w.opDone == nil {
		if dockerErr != nil {
			return StateUnknown, dockerFault(UnknownDocker, dockerErr)
		}
		if w.inspectErr != nil {
			return StateUnknown, dockerFault(UnknownInspect, w.inspectErr)
		}
		if w.attachErr != nil && w.containerID == "" {
			r := UnknownReason{Kind: UnknownPassword, text: "Foreman " + w.attachErr.Error()}
			if inner := errors.Unwrap(w.attachErr); inner != nil {
				r.Detail = inner.Error()
			}
			return StateUnknown, r
		}
		if !listed && w.containerID == "" {
			return StateUnknown, UnknownReason{Kind: UnknownAsking}
		}
	}
	if w.crashLoop {
		return StateCrashLooping, UnknownReason{}
	}
	switch w.dockerStatus {
	case "":
		return StateStopped, UnknownReason{}
	case "running":
		switch {
		case w.stopping:
			return StateStopping, UnknownReason{}
		case w.run.loadFailed || w.crashedRun:
			return StateCrashed, UnknownReason{}
		case w.run.inGame:
			return StateRunning, UnknownReason{}
		}
		return StateStarting, UnknownReason{}
	case "restarting":
		if w.stopping {
			return StateStopping, UnknownReason{}
		}
		return StateCrashed, UnknownReason{}
	case "paused":
		return StateUnknown, UnknownReason{Kind: UnknownPaused}
	case "created":
		// Docker never ran it, so it cannot have crashed.
		return StateStopped, UnknownReason{}
	}
	if w.expectRunning && !w.stopping {
		return StateCrashed, UnknownReason{}
	}
	return StateStopped, UnknownReason{}
}

// The kinds of UnknownReason.
const (
	// UnknownUnreachable: Docker answers nothing at Target (DOCKER_HOST).
	UnknownUnreachable = "docker-unreachable"
	// UnknownDocker: listing the containers failed.
	UnknownDocker = "docker"
	// UnknownInspect: reading the game container failed.
	UnknownInspect = "inspect"
	// UnknownPassword: the world's RCON password cannot be read; Detail
	// says why.
	UnknownPassword = "password"
	// UnknownAsking: Docker has not answered yet.
	UnknownAsking = "asking"
	// UnknownPaused: someone paused the game container in Docker.
	UnknownPaused = "paused"
)

// UnknownReason says why a game's state is Unknown; Kind is "" when it is
// known.
type UnknownReason struct {
	Kind, Target, Detail string
	text                 string
}

// Text is the reason in English, for the log and the API.
func (r UnknownReason) Text() string {
	switch r.Kind {
	case UnknownUnreachable, UnknownPassword:
		return r.text
	case UnknownDocker:
		return "Foreman cannot reach Docker. The details are in Foreman's log."
	case UnknownInspect:
		return "Foreman cannot read the game container from Docker. The details are in Foreman's log."
	case UnknownAsking:
		return "Foreman is still asking Docker about the game."
	case UnknownPaused:
		return "The game container is paused in Docker. Foreman never pauses it; unpause it with Docker."
	}
	return ""
}

// dockerFault is the reason shown for a failed Docker call. A Docker that
// answers nothing already has a plain sentence of its own; any other fault
// shows plain, never the raw error.
func dockerFault(kind string, err error) UnknownReason {
	if ue, ok := errors.AsType[*docker.UnreachableError](err); ok {
		return UnknownReason{Kind: UnknownUnreachable, Target: ue.Target, text: ue.Error()}
	}
	return UnknownReason{Kind: kind}
}

// logDockerFault logs a failed listing of containers with the raw error,
// which the page never shows. It logs once while the same fault lasts.
func (c *Controller) logDockerFault(before, err error) {
	c.logFault("cannot reach Docker", before, err)
}

// logFault logs a Docker fault's raw error once while the same fault lasts.
func (c *Controller) logFault(msg string, before, err error, args ...any) {
	if before != nil && before.Error() == err.Error() {
		return
	}
	raw := err
	if ue, ok := errors.AsType[*docker.UnreachableError](err); ok {
		raw = ue.Err
	}
	c.log.Warn(msg, append(args, "error", raw)...)
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

// refusedActions names the audit action of each refusable action.
var refusedActions = map[string]string{
	AuditStart: AuditStartRefused, AuditRestart: AuditRestartRefused, AuditSwitch: AuditSwitchRefused,
	AuditUpdate: AuditUpdateRefused,
}

// IsRefusal reports whether err is a check refusing the world as it is,
// rather than a fault such as Docker or the disk failing: the pre-start
// check (SPEC 7.4) or a switch refusal.
func IsRefusal(err error) bool {
	var pe *PrecheckError
	return errors.As(err, &pe) ||
		errors.Is(err, ErrSwitchSame) || errors.Is(err, ErrSwitchNotActive) ||
		errors.Is(err, ErrSwitchNoTarget) || errors.Is(err, ErrSwitchTargetRunning)
}

// checkFailed handles an error from the checks a start, restart or switch
// runs before it does anything. A refusal is recorded as such; any other
// error is audited as the action itself, which then failed.
func (c *Controller) checkFailed(ctx context.Context, by Actor, action, id, detail string, err error) error {
	if IsRefusal(err) {
		return c.refused(ctx, by, action, id, detail, err)
	}
	if aerr := c.audit(ctx, by, action, id, detail); aerr != nil {
		return aerr
	}
	return err
}

// refused records a start, restart or switch that the checks refused before
// anything was done: one log line and one audit entry, each with the
// reason, under the action's own refused name (SPEC 12.9). The reason is the
// refusal's text, which names problems and never a secret. It returns err.
func (c *Controller) refused(ctx context.Context, by Actor, action, id, detail string, err error) error {
	reason := err.Error()
	if detail != "" {
		reason = detail + ": " + reason
	}
	c.log.Warn(action+" refused", "world", id, "user", by.Username, "reason", reason)
	if aerr := c.audit(ctx, by, refusedActions[action], id, reason); aerr != nil {
		c.log.Error("could not record the refusal in the audit log", "world", id, "action", action, "error", aerr)
	}
	return err
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
