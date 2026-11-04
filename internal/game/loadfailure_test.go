package game

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/secret"
)

func reasonOf(secrets []secret.Secret, lines ...string) string {
	var f loadFailure
	for _, l := range lines {
		f.add(l)
	}
	return f.reason(secrets)
}

const incompatible = "    • Incompatible Factorio version (current: 2.1, required: 2.0)"

func TestLoadFailureNamesIncompatibleMods(t *testing.T) {
	var lines []string
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		lines = append(lines,
			`   1.000 Error Util.cpp:81: Failed to load mod "Other":`,
			"• "+name,
			incompatible)
	}
	// The same mod twice counts once.
	lines = append(lines, "• a", incompatible)
	want := "7 mods were made for Factorio 2.0, and this game runs 2.1: a, b, c, d, e and 2 more"
	if got := reasonOf(nil, lines...); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadFailureOneIncompatibleMod(t *testing.T) {
	got := reasonOf(nil, "• lonely", incompatible)
	if want := "1 mod was made for Factorio 2.0, and this game runs 2.1: lonely"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadFailureTakesTheLargestGroupOfIncompatibleMods(t *testing.T) {
	got := reasonOf(nil,
		"• x", "    • Incompatible Factorio version (current: 2.1, required: 1.1)",
		"• y", incompatible, "• z", incompatible)
	if want := "2 mods were made for Factorio 2.0, and this game runs 2.1: y, z"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadFailureShowsTheLastErrorLineWithoutItsPrefix(t *testing.T) {
	got := reasonOf(nil,
		"   0.500 Error Util.cpp:81: first",
		"   1.500 Info Something.cpp:1: not an error",
		"   2.250 Error Mod.cpp:9: the second problem",
	)
	if got != "the second problem" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadFailureTrimsALongErrorLine(t *testing.T) {
	got := reasonOf(nil, "   1.0 Error Mod.cpp:9: "+strings.Repeat("é", 500))
	if n := len([]rune(got)); n != maxErrorLine {
		t.Fatalf("%d characters", n)
	}
}

func TestLoadFailureShowsNothingElse(t *testing.T) {
	got := reasonOf(nil,
		"+ exec /opt/factorio/bin/x64/factorio --rcon-password hunter2",
		"   1.0 Info Foo.cpp:1: something",
		"random output",
		"Error without a source location")
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadFailureRedactsTheErrorLine(t *testing.T) {
	pw := secret.New("hunter2-rcon")
	got := reasonOf([]secret.Secret{pw}, "   1.0 Error Mod.cpp:9: login with hunter2-rcon failed")
	if strings.Contains(got, "hunter2-rcon") || !strings.Contains(got, Mask) {
		t.Fatalf("got %q", got)
	}
}

func TestLoadFailureRedactsModNames(t *testing.T) {
	pw := secret.New("sekret-mod")
	got := reasonOf([]secret.Secret{pw}, "• sekret-mod", incompatible)
	if strings.Contains(got, "sekret-mod") || !strings.Contains(got, Mask) {
		t.Fatalf("got %q", got)
	}
}

// startWhileGameDies starts a world whose game is still loading, then puts
// the lines in its log and makes the game exit. It returns Start's error.
func startWhileGameDies(t *testing.T, lines func(rconPassword string) []string) (*rig, string, error) {
	t.Helper()
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 30 * time.Second}})
	id := r.newWorld("Dies")
	done := make(chan error, 1)
	go func() { done <- r.c.Start(context.Background(), id, admin, StartOptions{}) }()
	var pw string
	eventually(t, func() bool {
		cs := r.game.State().Containers
		for _, c := range cs {
			if c.Name == ContainerName(id) && c.Status == "running" {
				return true
			}
		}
		return false
	}, "the game to be loading")
	rconPW, _, err := r.c.secrets(id)
	if err != nil {
		t.Fatal(err)
	}
	pw = rconPW.Reveal()
	for _, l := range lines(pw) {
		if !r.game.Daemon().EmitLog(ContainerName(id), docker.Stderr, l) {
			t.Fatal("no container to log to")
		}
	}
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		return r, pw, err
	case <-time.After(10 * time.Second):
		t.Fatal("start did not end")
		return nil, "", nil
	}
}

func TestStartExplainsIncompatibleMods(t *testing.T) {
	_, pw, err := startWhileGameDies(t, func(pw string) []string {
		return []string{
			"+ exec /opt/factorio/bin/x64/factorio --rcon-password " + pw,
			`   2.000 Error Util.cpp:81: Failed to load mod "AfraidOfTheDark":`,
			"• mining-patch-planner",
			incompatible,
		}
	})
	if !errors.Is(err, ErrStartFailed) {
		t.Fatalf("got %v", err)
	}
	want := "the game did not start: 1 mod was made for Factorio 2.0, and this game runs 2.1: mining-patch-planner"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err, want)
	}
	if strings.Contains(err.Error(), pw) {
		t.Fatal("the message holds the RCON password")
	}
}

func TestStartLoadFailureNeverShowsTheRCONPassword(t *testing.T) {
	_, pw, err := startWhileGameDies(t, func(pw string) []string {
		return []string{fmt.Sprintf("   2.000 Error Util.cpp:81: bad rcon password %s given", pw)}
	})
	if !errors.Is(err, ErrStartFailed) {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), pw) || !strings.Contains(err.Error(), "bad rcon password "+Mask+" given") {
		t.Fatalf("got %q", err)
	}
}

func TestStartLoadFailureWithNothingToShowKeepsThePlainText(t *testing.T) {
	_, _, err := startWhileGameDies(t, func(pw string) []string {
		return []string{"+ exec factorio --rcon-password " + pw}
	})
	if want := "the game did not start: the game stopped while it was loading"; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}
