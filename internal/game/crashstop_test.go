package game

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// slowStops sits between the controller and the fake Docker. Every stop
// call waits until release is closed, and the most stops in flight at once
// is recorded.
type slowStops struct {
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	now     int
	most    int
}

// free lets every stop through, now and later.
func (s *slowStops) free() { s.once.Do(func() { close(s.release) }) }

func (s *slowStops) peak() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.most
}

// newSlowStops swaps the rig's Docker client for one that goes through a
// slowStops, and makes the rig's controller on it.
func newSlowStops(t *testing.T, r *rig) *slowStops {
	t.Helper()
	s := &slowStops{release: make(chan struct{})}
	sock := strings.TrimPrefix(r.game.Host(), "unix://")
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: "docker"})
	proxy.FlushInterval = -1
	proxy.ErrorLog = log.New(io.Discard, "", 0) // a stop cut off when the test ends is expected
	proxy.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/stop") {
			s.mu.Lock()
			s.now++
			s.most = max(s.most, s.now)
			s.mu.Unlock()
			<-s.release
			defer func() {
				s.mu.Lock()
				s.now--
				s.mu.Unlock()
			}()
		}
		proxy.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(s.free) // runs first, so a failed test never leaves a stop hanging
	dc, err := docker.New("tcp://" + strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	r.docker = dc
	r.c = r.newController(rigOptions{})
	r.stop = r.run(r.c)
	return s
}

// SPEC 8.9: while a crash-loop stop waits on Docker (up to the stop grace),
// the runs Docker keeps starting do not start more stops alongside it.
func TestCrashLoopStopsDoNotOverlap(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	s := newSlowStops(t, r)
	id := r.newWorld("Loop slow stop")
	r.start(id)
	r.game.SetCrashLoop(ContainerName(id), true)
	r.waitState(id, StateCrashLooping)
	// While the stop hangs, Docker restarts the game and this run stays up.
	r.game.SetCrashLoop(ContainerName(id), false)
	eventually(t, func() bool { return r.container(id).Status == "running" }, "Docker to run the game again")
	time.Sleep(300 * time.Millisecond) // many polls
	if p := s.peak(); p != 1 {
		t.Errorf("%d crash-loop stops in flight at once, want 1", p)
	}
	s.free()
	eventually(t, func() bool { return r.container(id).Status == "exited" }, "the container to stop")
	if st := r.c.Status(id); st.State != StateCrashLooping {
		t.Errorf("state %s after the stop", st.State)
	}
}
