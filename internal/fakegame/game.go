// Package fakegame is a fake Docker host with a fake Factorio game inside it,
// for tests. It serves the Docker API on a Unix socket (through dockertest),
// and when a container starts it replays log lines from real transcripts of
// the pinned game images and serves RCON on the container's own loopback
// address, port 27015. Test hooks make a game crash, loop on a bad save,
// gain or lose players, refuse RCON logins, hang on a save, or lose its
// image.
//
// Docker's unless-stopped policy is followed: a game that exits by itself
// is started again after a doubling delay, and one that was stopped or
// killed through the API stays stopped.
package fakegame

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

const (
	rconPort     = "27015"
	firstBackoff = 100 * time.Millisecond // Docker's first restart delay
	maxBackoff   = 3 * time.Second        // Docker's goes to a minute; tests cannot wait that long
	saveTime     = 50 * time.Millisecond  // from /server-save to "Saving finished"
	socketName   = "docker.sock"
)

// Config sets up a Game.
type Config struct {
	// Dir is an existing folder for the Docker socket. Empty means a new
	// temporary folder, removed by Close.
	Dir string
	// Images are the image references present at the start.
	Images []string
	// LoadDelay is how long a start takes between the "Loading map" line and
	// InGame. Zero loads at once, before the start call returns.
	LoadDelay time.Duration
	// SelfNetwork, when set, seeds a running container that stands for the
	// container this process runs in, on a network of this name with the
	// address SelfAddress. Its ID and name are what docker.SelfID finds here,
	// so a Foreman started beside the fake finds its own container in it.
	// State leaves it out: it is not a game.
	SelfNetwork string
	// Adopted, when set, seeds one pre-existing game container on
	// SelfNetwork and forbids the calls adopted mode never makes.
	Adopted *Adopted
}

// SelfAddress is the seeded self container's address on SelfNetwork, in a
// range apart from the tests' trusted proxies.
const SelfAddress = "10.250.0.2/24"

// behaviour is what the hooks set for a container name. It outlives the
// container, because Foreman makes a new container for every start.
type behaviour struct {
	crashLoop  bool // every start fails to load its save
	rconRefuse bool // RCON logins are refused
	saveHangs  bool // /server-save never finishes
}

// Game is the fake. Create it with Start, or with New in a test.
type Game struct {
	cfg    Config
	report func(string)
	shim   *tb
	daemon *dockertest.Daemon
	srv    *http.Server
	dir    string
	ownDir bool
	claim  net.Listener // holds this run's address prefix against other runs
	prefix string       // "127.A.B.": containers get the addresses after it
	selfID string       // the seeded self container, "" without one
	// adoptedID is the seeded adopted container, whose starts run the
	// image's entrypoint; "" without one.
	adoptedID string

	mu       sync.Mutex
	closed   bool
	behave   map[string]behaviour     // by container name
	insts    map[string]*instance     // running games, by container ID
	addrs    map[string]int           // container ID to last address octet
	manual   map[string]bool          // container IDs a person stopped
	backoff  map[string]time.Duration // container ID to its next restart delay
	commands map[string][]string      // RCON commands received, by container name
	done     chan struct{}
	wg       sync.WaitGroup
}

// Start serves the fake Docker API on a socket in cfg.Dir. Problems with
// the fake's own use (a Docker call outside SPEC 14.3) go to standard error.
func Start(cfg Config) (*Game, error) {
	return start(cfg, func(msg string) { fmt.Fprintln(os.Stderr, "fakegame:", msg) })
}

// New starts a Game that stops when the test ends. A Docker call outside
// SPEC 14.3 fails the test.
func New(t testing.TB, cfg Config) *Game {
	t.Helper()
	g, err := start(cfg, func(msg string) { t.Errorf("fakegame: %s", msg) })
	if err != nil {
		t.Fatalf("fakegame: %v", err)
	}
	t.Cleanup(g.Close)
	return g
}

func start(cfg Config, report func(string)) (*Game, error) {
	if cfg.Adopted != nil {
		if err := cfg.Adopted.validate(cfg.SelfNetwork); err != nil {
			return nil, err
		}
	}
	g := &Game{
		cfg:      cfg,
		behave:   map[string]behaviour{},
		insts:    map[string]*instance{},
		addrs:    map[string]int{},
		manual:   map[string]bool{},
		backoff:  map[string]time.Duration{},
		commands: map[string][]string{},
		done:     make(chan struct{}),
	}
	// Nothing is reported once the game is closed: a test may have ended.
	g.report = func(msg string) {
		g.mu.Lock()
		closed := g.closed
		g.mu.Unlock()
		if !closed {
			report(msg)
		}
	}
	g.shim = &tb{report: g.report}

	g.dir = cfg.Dir
	if g.dir == "" {
		dir, err := os.MkdirTemp("", "fakegame-")
		if err != nil {
			return nil, err
		}
		g.dir, g.ownDir = dir, true
	}
	var err error
	if g.prefix, g.claim, err = claimPrefix(); err != nil {
		g.cleanDir()
		return nil, err
	}
	sock := g.Socket()
	// A path past the Unix limit fails inside Listen with a vague message.
	if len(sock) > 100 {
		_ = g.claim.Close()
		g.cleanDir()
		return nil, fmt.Errorf("the socket path %q is too long for a Unix socket", sock)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = g.claim.Close()
		g.cleanDir()
		return nil, err
	}

	g.daemon = dockertest.New(g.shim)
	g.daemon.Close() // only its handler is used, through the Unix socket
	for _, ref := range cfg.Images {
		g.daemon.AddImage(ref)
	}
	if cfg.SelfNetwork != "" {
		g.seedSelf(cfg.SelfNetwork)
	}
	if cfg.Adopted != nil {
		g.seedAdopted(*cfg.Adopted, cfg.SelfNetwork)
	}
	g.srv = &http.Server{Handler: http.HandlerFunc(g.serveDocker), ReadHeaderTimeout: 10 * time.Second}
	g.spawn(func() { _ = g.srv.Serve(ln) })
	return g, nil
}

// seedSelf adds the container this process runs in, found the way Foreman
// finds its own: by the ID in mountinfo, else by the hostname.
func (g *Game) seedSelf(network string) {
	hostname, _ := os.Hostname()
	id := docker.SelfID(docker.MountinfoPath, hostname)
	if id == "" {
		return // Foreman could not look for itself either
	}
	c := dockertest.Container{Name: hostname, Status: "running", Networks: map[string]string{network: SelfAddress}}
	if id != hostname {
		c.ID = id
	}
	g.daemon.Seed(c)
	if seeded, ok := g.daemon.Container(id); ok {
		g.selfID = seeded.ID
	}
}

// claimPrefix picks a random 127.A.B. prefix and holds 127.A.B.1:27015, so a
// second run that draws the same prefix fails the bind and draws again.
func claimPrefix() (string, net.Listener, error) {
	for range 200 {
		prefix := fmt.Sprintf("127.%d.%d.", 1+rand.IntN(254), rand.IntN(255)) //nolint:gosec // not a secret
		if ln, err := net.Listen("tcp", prefix+"1:"+rconPort); err == nil {
			return prefix, ln, nil
		}
	}
	return "", nil, errors.New("no free loopback address for the fake game's RCON ports")
}

// Socket is the path of the Docker socket.
func (g *Game) Socket() string { return filepath.Join(g.dir, socketName) }

// Host is the DOCKER_HOST value for docker.New.
func (g *Game) Host() string { return "unix://" + g.Socket() }

// Daemon is the fake Docker daemon, for tests that seed containers or read
// the calls it received.
func (g *Game) Daemon() *dockertest.Daemon { return g.daemon }

// Close stops every game and the socket, and waits for them.
func (g *Game) Close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	running := make([]*instance, 0, len(g.insts))
	for _, in := range g.insts {
		running = append(running, in)
	}
	g.insts = map[string]*instance{}
	g.mu.Unlock()

	close(g.done)
	_ = g.srv.Close()
	for _, in := range running {
		in.shutdown()
	}
	g.wg.Wait()
	_ = g.claim.Close()
	g.shim.runCleanups()
	g.cleanDir()
}

func (g *Game) cleanDir() {
	if g.ownDir {
		_ = os.RemoveAll(g.dir)
	}
}

// spawn runs f in a goroutine that Close waits for. After Close it does
// nothing.
func (g *Game) spawn(f func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		f()
	}()
}

// after runs f once d has passed, unless the game closes first.
func (g *Game) after(d time.Duration, f func()) {
	g.spawn(func() {
		select {
		case <-time.After(d):
			f()
		case <-g.done:
		}
	})
}

// addrLocked returns the loopback address of a container, giving it the
// lowest free one the first time. g.mu is held.
func (g *Game) addrLocked(id string) string {
	n, ok := g.addrs[id]
	if !ok {
		taken := map[int]bool{}
		for _, used := range g.addrs {
			taken[used] = true
		}
		for n = 2; n < 255 && taken[n]; n++ {
		}
		if n == 255 {
			return ""
		}
		g.addrs[id] = n
	}
	return fmt.Sprintf("%s%d", g.prefix, n)
}
