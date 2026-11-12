//go:build game

package game

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

// webhookReceiver records the generic webhook bodies it is sent.
type webhookReceiver struct {
	mu     sync.Mutex
	events []string
}

func (h *webhookReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Event string `json:"event"`
		World struct {
			ID string `json:"id"`
		} `json:"world"`
	}
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &body)
	h.mu.Lock()
	h.events = append(h.events, body.Event)
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (h *webhookReceiver) got() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.events)
}

// SPEC 8.10, 8.12: on the real 2.1 game a start, a stop and a corrupt-save
// crash each send one webhook, and a Foreman restart sends none again.
// Joins and leaves need a game client, which no test has.
func TestRealGameEvents(t *testing.T) {
	e := newGameEnv(t)
	e.requireImage(t, ImageRef("2.1.24"))
	ctx := context.Background()

	recv := &webhookReceiver{}
	srv := httptest.NewServer(recv)
	defer srv.Close()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	key := make([]byte, 24)
	_, _ = rand.Read(key)
	n, err := notify.New(st, secret.New(string(key)+"-test-key-test-key"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	all := map[notify.Event]bool{}
	for _, ev := range notify.Events {
		all[ev] = true
	}
	if _, err := n.Add(ctx, notify.Setup{Preset: notify.Generic, URL: secret.New(srv.URL + "/hook"), Events: all, Lang: "en"}, "test"); err != nil {
		t.Fatal(err)
	}
	nctx, stopN := context.WithCancel(ctx)
	defer stopN()
	go n.Run(nctx)

	expect := func(want ...string) {
		t.Helper()
		waitFor(t, 60*time.Second, "webhooks", func() bool { return len(recv.got()) >= len(want) })
		time.Sleep(5 * time.Second)
		if got := recv.got(); !slices.Equal(got, want) {
			t.Fatalf("webhooks %v, want %v", got, want)
		}
	}

	c, stop := e.controller(t)
	c.SetNotifier(n)
	good := e.newWorld(t, "2.1", "2.1.24", "events", "2.1.24-no-space-age.zip", map[string]time.Duration{"base.zip": time.Hour})
	if err := c.Start(ctx, good, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	expect("start")

	// A Foreman restart while the game runs sends nothing again.
	stop()
	c2, stop2 := e.controller(t)
	c2.SetNotifier(n)
	waitState(t, c2, good, StateRunning, 60*time.Second)
	time.Sleep(5 * time.Second)
	expect("start")
	if err := c2.Stop(ctx, good, admin, StopOptions{}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	expect("start", "stop")

	bad := e.newWorld(t, "2.1", "2.1.24", "eventscorrupt", "", nil)
	junk := make([]byte, 4096)
	_, _ = rand.Read(junk)
	e.putSave(t, bad, "corrupt.zip", junk, time.Now().Add(-time.Hour))
	var pe *PrecheckError
	if err := c2.Start(ctx, bad, admin, StartOptions{}); !errors.As(err, &pe) {
		t.Fatalf("start without anyway = %v, want a pre-start refusal", err)
	}
	if err := c2.Start(ctx, bad, admin, StartOptions{Anyway: true, Seen: AnywayFingerprint(pe.Check, false)}); !errors.Is(err, ErrStartFailed) {
		t.Fatalf("start = %v, want ErrStartFailed", err)
	}
	waitState(t, c2, bad, StateCrashLooping, 90*time.Second)
	waitFor(t, 60*time.Second, "the crash-looping container to stop", func() bool {
		s := e.inspect(t, bad).State
		return !s.Running && s.Status != "restarting"
	})
	expect("start", "stop", "crash", "crash-loop")

	// Another restart of Foreman, with the crash-looped game stopped.
	stop2()
	c3, _ := e.controller(t)
	c3.SetNotifier(n)
	waitState(t, c3, bad, StateStopped, 60*time.Second)
	time.Sleep(5 * time.Second)
	expect("start", "stop", "crash", "crash-loop")
}
