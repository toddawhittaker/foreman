// Package dockertest is a fake Docker daemon for tests. It keeps the state of
// containers and images, answers the calls of SPEC 14.3 the way the real
// daemon does for the cases Foreman meets, and fails the test on any call
// outside that list, on a versioned call without the version prefix, and on
// an allowed call it does not implement (GET /events).
package dockertest

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// Container is the fake's record of one container.
type Container struct {
	ID           string
	Name         string // without the leading slash
	Spec         docker.ContainerSpec
	Status       string // created, running or exited
	ExitCode     int
	OOMKilled    bool
	RestartCount int
	StartedAt    time.Time
	FinishedAt   time.Time
	Mounts       []docker.Mount
	Networks     map[string]string // network name to IP address, set while running
	Log          []LogEntry
}

// clone copies the parts a later call could change under the caller.
func (c *Container) clone() Container {
	out := *c
	out.Log = append([]LogEntry(nil), c.Log...)
	out.Networks = maps.Clone(c.Networks)
	return out
}

// LogEntry is one line the container wrote.
type LogEntry struct {
	Stream docker.Stream
	Text   string
}

// Call is one request the fake received.
type Call struct {
	Method string
	Path   string // without the /vX.Y prefix
	Query  url.Values
}

// Daemon is the fake. Create it with New.
type Daemon struct {
	t   testing.TB
	srv *httptest.Server

	mu         sync.Mutex
	apiVersion string
	images     map[string]bool
	pullFail   map[string]string
	pulls      []string
	containers []*Container
	serial     int
	calls      []Call
	failStatus int
	failMsg    string
	changed    chan struct{} // closed and replaced whenever state or a log changes
}

// New starts a fake daemon that speaks API version docker.MaxAPIVersion and
// stops when the test ends.
func New(t testing.TB) *Daemon {
	t.Helper()
	d := &Daemon{
		t:          t,
		apiVersion: docker.MaxAPIVersion,
		images:     map[string]bool{},
		pullFail:   map[string]string{},
		changed:    make(chan struct{}),
	}
	d.srv = httptest.NewServer(d)
	t.Cleanup(d.srv.Close)
	return d
}

// Host is the DOCKER_HOST value for docker.New.
func (d *Daemon) Host() string {
	return "tcp://" + strings.TrimPrefix(d.srv.URL, "http://")
}

// Close stops the daemon, so later calls fail as if Docker were down.
func (d *Daemon) Close() { d.srv.Close() }

// SetAPIVersion changes the version GET /version reports.
func (d *Daemon) SetAPIVersion(v string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.apiVersion = v
}

// SetFailure makes every call answer with status and message, until it is
// called again with status 0.
func (d *Daemon) SetFailure(status int, message string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failStatus, d.failMsg = status, message
}

// AddImage makes an image present under ref.
func (d *Daemon) AddImage(ref string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.images[ref] = true
}

// RemoveImage makes an image missing.
func (d *Daemon) RemoveImage(ref string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.images, ref)
}

// FailPull makes pulling ref fail with message inside a 200 reply, the way a
// real daemon reports many registry errors. A pull without FailPull succeeds
// and adds the image.
func (d *Daemon) FailPull(ref, message string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pullFail[ref] = message
}

// Pulls lists the references pulled so far, in order.
func (d *Daemon) Pulls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.pulls...)
}

// Calls lists every request received so far, in order.
func (d *Daemon) Calls() []Call {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Call(nil), d.calls...)
}

// Seed adds a container as given, for ones the code under test did not
// create (Foreman's own container, or another program's). An empty ID or
// Status gets a default.
func (d *Daemon) Seed(c Container) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c.ID == "" {
		c.ID = d.newID()
	}
	if c.Status == "" {
		c.Status = "created"
	}
	d.containers = append(d.containers, &c)
	d.broadcast()
}

// Container returns a copy of the container with this name or ID.
func (d *Daemon) Container(ref string) (Container, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.find(ref)
	if c == nil {
		return Container{}, false
	}
	return c.clone(), true
}

// Containers returns copies of every container, oldest first.
func (d *Daemon) Containers() []Container {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Container, len(d.containers))
	for i, c := range d.containers {
		out[i] = c.clone()
	}
	return out
}

// EmitLog appends a line to a container's output and passes it to anyone
// following. It reports false if there is no such container.
func (d *Daemon) EmitLog(ref string, stream docker.Stream, text string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.find(ref)
	if c == nil {
		return false
	}
	c.Log = append(c.Log, LogEntry{Stream: stream, Text: text})
	d.broadcast()
	return true
}

// Exit makes a running container stop by itself, as a crash would.
func (d *Daemon) Exit(ref string, code int, oomKilled bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c := d.find(ref); c != nil && c.Status == "running" {
		d.markExited(c, code)
		c.OOMKilled = oomKilled
	}
}

// Restart makes Docker's restart policy bring an exited container back.
func (d *Daemon) Restart(ref string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c := d.find(ref); c != nil && c.Status != "running" {
		c.RestartCount++
		d.markRunning(c)
	}
}

var versionPrefix = regexp.MustCompile(`^/v\d+\.\d+(/.*)$`)

func (d *Daemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	unversioned := path == "/version" || path == "/_ping"
	if m := versionPrefix.FindStringSubmatch(path); m != nil {
		path = m[1]
	} else if !unversioned {
		d.t.Errorf("dockertest: %s %s has no API version prefix", r.Method, r.URL.Path)
	}
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: r.Method, Path: path, Query: r.URL.Query()})
	failStatus, failMsg := d.failStatus, d.failMsg
	d.mu.Unlock()
	if !docker.Allowed(r.Method, path) {
		d.t.Errorf("dockertest: %s %s is outside SPEC 14.3", r.Method, r.URL.Path)
		reply(w, http.StatusForbidden, "outside SPEC 14.3")
		return
	}
	if failStatus != 0 {
		reply(w, failStatus, failMsg)
		return
	}
	d.route(w, r, path)
}

func (d *Daemon) route(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(path, "/") // "", first, ...
	switch {
	case path == "/_ping":
		_, _ = w.Write([]byte("OK"))
	case path == "/version":
		d.mu.Lock()
		v := d.apiVersion
		d.mu.Unlock()
		writeJSON(w, map[string]string{"Version": "29.0.0", "ApiVersion": v})
	case path == "/containers/json":
		d.list(w, r)
	case path == "/containers/create":
		d.create(w, r)
	case path == "/images/create":
		d.pull(w, r)
	case parts[1] == "images":
		d.inspectImage(w, strings.TrimSuffix(strings.TrimPrefix(path, "/images/"), "/json"))
	case parts[1] == "containers" && r.Method == http.MethodDelete:
		d.remove(w, r, parts[2])
	case parts[1] == "containers" && len(parts) == 4:
		d.containerCall(w, r, parts[2], parts[3])
	default:
		d.t.Errorf("dockertest: %s %s is allowed by SPEC 14.3 but not implemented", r.Method, path)
		reply(w, http.StatusNotImplemented, "not implemented by dockertest")
	}
}
