package game

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// failInspectAfterStart runs the rig's controller through a proxy that fails
// the first inspect of a container after its start, the way a Docker that
// stops answering does.
func failInspectAfterStart(r *rig, opts rigOptions) {
	var mu sync.Mutex
	failNext := "" // the inspect path that fails once
	r.proxyDocker(opts, func(w http.ResponseWriter, req *http.Request, next http.Handler) {
		path := req.URL.Path
		mu.Lock()
		if req.Method == http.MethodPost && strings.HasSuffix(path, "/start") {
			failNext = strings.TrimSuffix(path, "/start") + "/json"
		}
		fail := req.Method == http.MethodGet && failNext != "" && path == failNext
		if fail {
			failNext = ""
		}
		mu.Unlock()
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"inspect on fire"}`))
			return
		}
		next.ServeHTTP(w, req)
	})
}

// SPEC 7.3: a start that fails after Docker started the container stops
// it, so the failure Foreman reports matches what runs.
func TestStartThatFailsAfterTheContainerStartedStopsIt(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	failInspectAfterStart(r, rigOptions{})
	id := r.newWorld("Lost inspect")
	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	if err == nil || !strings.Contains(err.Error(), "inspect on fire") {
		t.Fatalf("start: %v, want the failed inspect", err)
	}
	if s := r.container(id).Status; s != "exited" {
		t.Errorf("container %s after the failed start, want it stopped", s)
	}
	time.Sleep(10 * r.c.pollInterval) // the monitor's looks settle the state
	if st := r.c.Status(id); st.State != StateStopped {
		t.Errorf("state %s after the failed start, want stopped", st.State)
	}
}

// When Docker refuses that stop too, the start still reports its own
// failure, and the refused stop is logged.
func TestStartThatFailsKeepsItsErrorWhenTheStopFails(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	logs := &lockedBuffer{}
	failInspectAfterStart(r, rigOptions{logs: logs})
	r.game.Daemon().SetCallFailure("POST", "stop", 500, "daemon busy")
	t.Cleanup(func() { r.game.Daemon().SetCallFailure("POST", "stop", 0, "") })
	id := r.newWorld("Lost inspect, stuck stop")
	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	if err == nil || !strings.Contains(err.Error(), "inspect on fire") || strings.Contains(err.Error(), "daemon busy") {
		t.Fatalf("start: %v, want the failed inspect alone", err)
	}
	if got := logs.String(); !strings.Contains(got, "could not stop the game after its start failed") || !strings.Contains(got, "daemon busy") {
		t.Errorf("log %q, want the refused stop", got)
	}
}
