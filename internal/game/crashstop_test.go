package game

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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

// newSlowStops puts a slowStops between the fake Docker and a new
// controller, which becomes the rig's.
func newSlowStops(t *testing.T, r *rig) *slowStops {
	t.Helper()
	s := &slowStops{release: make(chan struct{})}
	r.proxyDocker(rigOptions{}, func(w http.ResponseWriter, req *http.Request, next http.Handler) {
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
		next.ServeHTTP(w, req)
	})
	t.Cleanup(s.free) // runs first, so a failed test never leaves a stop hanging
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

// SPEC 8.9: the crash-loop stop gives the game only a short grace before
// Docker kills it. Docker sends SIGTERM to a run it has just started, and
// Factorio 2.0.77 hangs for good when SIGTERM reaches it while it loads its
// mods, so the full stop grace (SPEC 7.6) would leave the crash-looping
// game running for two minutes. A run so new has nothing to save.
func TestCrashLoopStopHasAShortGrace(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Loop grace")
	r.start(id)
	r.game.SetCrashLoop(ContainerName(id), true)
	r.waitState(id, StateCrashLooping)
	eventually(t, func() bool { return r.container(id).Status == "exited" }, "the container to stop")
	stop := "/containers/" + r.container(id).ID + "/stop"
	var grace []string
	for _, c := range r.game.Daemon().Calls() {
		if c.Method == "POST" && c.Path == stop {
			grace = append(grace, c.Query.Get("t"))
		}
	}
	if len(grace) == 0 {
		t.Fatal("the crash loop was not stopped with Docker's stop")
	}
	for _, g := range grace {
		if s, err := strconv.Atoi(g); err != nil || s > 10 {
			t.Errorf("crash-loop stop grace t=%q, want at most 10 seconds", g)
		}
	}
}
