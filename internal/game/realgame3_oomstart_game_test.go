//go:build game

package game

// An out-of-memory kill while a start loads is named, with Docker events
// allowed and with a socket proxy that refuses them (SPEC 8.7, 14.3).
// Docker restarts the killed game about 100 ms later and clears its flag,
// so this is the race the events close: with them every start must be
// named, while polling alone only logs how many it named. Foreman sets no memory limit, so
// the test lowers it as soon as the start's container appears, before the
// game has loaded far. FOREMAN_TEST_OOM_RUNS sets how many starts each
// case makes (3 by default). Run by `make test-game`, never by `make verify`.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
)

func TestRealGameOOMOnStart(t *testing.T) {
	runs := 3
	if n, err := strconv.Atoi(os.Getenv("FOREMAN_TEST_OOM_RUNS")); err == nil && n > 0 {
		runs = n
	}
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.version, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			t.Run("events on", func(t *testing.T) { e.oomStarts(t, e, g.tag, g.fixture, g.version, runs, true) })
			t.Run("events off", func(t *testing.T) {
				off := *e
				off.docker = noEventsDocker(t)
				off.oomStarts(t, e, g.tag, g.fixture, g.version, runs, false)
			})
		})
	}
}

// oomStarts makes runs starts that the kernel kills while they load and
// counts how many are named out of memory. With strict, as with events on,
// every one must be; without, as with polling alone, the rate is only
// logged, since polling can miss a kill. cli is the env whose Docker the
// test itself uses.
func (e *gameEnv) oomStarts(t *testing.T, cli *gameEnv, tag, fixture, version string, runs int, strict bool) {
	c, _ := e.controller(t)
	named := 0
	for i := range runs {
		id := e.newWorld(t, tag, version, "oom-start-"+strconv.Itoa(i), fixture, map[string]time.Duration{"cap.zip": time.Hour})
		done := make(chan error, 1)
		go func() { done <- c.Start(context.Background(), id, admin, StartOptions{}) }()
		limitWhenCreated(t, cli, id)
		var err error
		select {
		case err = <-done:
		case <-time.After(3 * time.Minute):
			t.Fatalf("run %d: the start never ended", i)
		}
		var se *StartError
		switch {
		case errors.As(err, &se) && se.Reason.Kind == crashreasons.OutOfMemory:
			named++
		case strict:
			t.Errorf("run %d: start gave %v, want an out-of-memory start error", i, err)
		default:
			t.Logf("run %d: start gave %v; polling missed the kill", i, err)
		}
		_ = c.Stop(context.Background(), id, admin, StopOptions{})
	}
	t.Logf("%d of %d out-of-memory starts named", named, runs)
}

// limitWhenCreated lowers the container's memory, swap included, as soon
// as the start has made it.
func limitWhenCreated(t *testing.T, e *gameEnv, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := exec.CommandContext(ctx, "docker", "update", "--memory", "96m", "--memory-swap", "96m", ContainerName(id)).Run() //nolint:gosec // fixed arguments on the test's own container
		cancel()
		if err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the start's container never appeared")
}

// noEventsDocker is a client through a proxy that refuses GET /events, as
// the Tecnativa socket proxy does with EVENTS=0, and passes on the rest.
func noEventsDocker(t *testing.T) *docker.Client {
	t.Helper()
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	sock, ok := strings.CutPrefix(host, "unix://")
	if !ok {
		t.Skip("the events-off case needs a unix DOCKER_HOST")
	}
	rp := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: "docker"})
	rp.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}
	rp.FlushInterval = -1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
			return
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	dc, err := docker.New("tcp://" + strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return dc
}
