package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
)

// dockerTime is the timestamp `docker logs -t` puts before each captured line.
var dockerTime = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\S+Z `)

// capturedRun is a real-game capture as the controller reads it, without
// Docker's timestamps.
func capturedRun(t *testing.T, rel string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(rel))) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for l := range strings.Lines(string(b)) {
		out = append(out, dockerTime.ReplaceAllString(strings.TrimSuffix(l, "\n"), ""))
	}
	return out
}

// SPEC 8.7: the reason's Kind is the catalogue's name for the real game's
// own words, on all three lines, and unknown when the catalogue has none.
func TestLoadReasonKindFromRealCaptures(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"1.1.110/run-mod-lua-error.log", crashreasons.ModError},
		{"2.0.77/run-mod-lua-error.log", crashreasons.ModError},
		{"2.1.24/run-mod-lua-error.log", crashreasons.ModError},
		{"2.0.77/run-mod-missing-dep.log", crashreasons.ModMissingDependency},
		{"2.1.24/run-mod-missing-dep.log", crashreasons.ModMissingDependency},
		// 1.1 skips the mod and starts.
		{"1.1.110/run-mod-missing-dep.log", ""},
		{"1.1.110/run-corrupt.log", crashreasons.SaveUnreadable},
		{"2.0.77/run-truncated.log", crashreasons.SaveUnreadable},
		{"2.1.24/run-unreadable.log", crashreasons.SaveUnreadable},
		// The log of an out-of-memory kill just stops.
		{"2.1.24/run-oom.log", ""},
		{"2.1.24/run-sigterm.log", ""},
	} {
		var f loadFailure
		for _, l := range capturedRun(t, tc.file) {
			f.add(l)
		}
		if got := f.reason(masking()).Kind; got != tc.want {
			t.Errorf("%s: kind %q, want %q", tc.file, got, tc.want)
		}
	}
}

func TestLoadReasonKindForModsMadeForAnotherVersion(t *testing.T) {
	var f loadFailure
	f.add("• lonely")
	f.add(incompatible)
	if got := f.reason(masking()); got.Kind != crashreasons.ModsForOtherVersion || got.Mods != 1 {
		t.Fatalf("got %+v", got)
	}
}

// SPEC 8.7: the kind is the newest run's own. Runs that fail before the
// load-started line, as a mod's error does, are one run to the reader and
// keep their kind; a newer run that began loading and names no reason, say
// one the host's out-of-memory killer ended, is unknown.
func TestLoadReasonKindIsTheNewestRuns(t *testing.T) {
	modErr := `   0.2 Error Util.cpp:81: Failed to load mod "x": __x__/data.lua:1: boom`
	loading := "   0.5 Loading map /factorio/saves/a.zip: 10 bytes."
	looping := readAll([]string{modErr, "   0.0 Factorio 2.1.24", modErr})
	if got := looping.reason(masking()); got.Kind != crashreasons.ModError {
		t.Errorf("runs failing before loading lost their kind: %+v", got)
	}
	killed := readAll([]string{modErr, loading})
	got := killed.reason(masking())
	if got.Kind != "" {
		t.Errorf("a newer run that named no reason took the run before's kind: %+v", got)
	}
	// The run before's words are still shown while the newest says none.
	if !strings.Contains(got.Said, "boom") {
		t.Errorf("said %q, want the run before's line", got.Said)
	}
	newer := readAll([]string{modErr, loading, "   0.6 Error Foo.cpp:1: something new"})
	if got := newer.reason(masking()); got.Kind != "" || got.Said != "something new" {
		t.Errorf("the newest run's unknown error took an older kind: %+v", got)
	}
}

func TestLoadReasonOutOfMemoryWords(t *testing.T) {
	r := LoadReason{Kind: crashreasons.OutOfMemory}
	if r.IsZero() {
		t.Error("a known kind counts as no reason")
	}
	if got := (&StartError{Kind: StartStopped, Reason: r}).Error(); got != "the game did not start: it ran out of memory" {
		t.Errorf("got %q", got)
	}
}

// startUntilKilled starts a world and kills its game while it loads, as
// the kernel's out-of-memory killer would when oom is set.
func startUntilKilled(t *testing.T, oom bool) error {
	t.Helper()
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 30 * time.Second}})
	id := r.newWorld("Killed")
	done := make(chan error, 1)
	go func() { done <- r.c.Start(context.Background(), id, admin, StartOptions{}) }()
	eventually(t, func() bool {
		for _, c := range r.game.Daemon().Containers() {
			if c.Name == ContainerName(id) && c.Status == "running" &&
				slices.ContainsFunc(c.Log, func(e dockertest.LogEntry) bool { return strings.Contains(e.Text, "Loading map ") }) {
				return true
			}
		}
		return false
	}, "the game to be loading")
	if !r.game.Daemon().EmitLog(ContainerName(id), docker.Stdout, "   0.3 Loading mod base 2.1.24 (data.lua)") {
		t.Fatal("no container to log to")
	}
	code := 1
	if oom {
		code = 137
	}
	if err := r.game.Crash(ContainerName(id), code, oom); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("start did not end")
		return nil
	}
}

// SPEC 8.7: out of memory is named from Docker's flag, never from the log.
func TestStartNamesOutOfMemoryFromDockersFlag(t *testing.T) {
	var se *StartError
	if err := startUntilKilled(t, true); !errors.As(err, &se) || se.Reason.Kind != crashreasons.OutOfMemory {
		t.Fatalf("got %v (%+v), want out of memory", err, se)
	}
	se = nil
	if err := startUntilKilled(t, false); !errors.As(err, &se) || se.Reason.Kind != "" {
		t.Fatalf("got %v (%+v), want an unknown reason", err, se)
	}
}

// SPEC 8.7, 8.9: a crash loop of out-of-memory kills is named so, and a
// later start's crash that was not one is not.
func TestCrashStatusNamesOutOfMemory(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("OOM loop")
	r.start(id)
	for range CrashLoopCount {
		// The fake game runs again a little after Docker says so.
		eventually(t, func() bool { return r.game.Crash(ContainerName(id), 137, true) == nil }, "the game to run and be killed")
	}
	if st := r.waitState(id, StateCrashLooping); st.Crash.Kind != crashreasons.OutOfMemory {
		t.Errorf("crash loop kind %q, want out of memory", st.Crash.Kind)
	}
	r.start(id)
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	if st := r.waitState(id, StateCrashed); st.Crash.Kind != "" {
		t.Errorf("a later crash kept the old kind %q", st.Crash.Kind)
	}
}
