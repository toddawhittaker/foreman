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

// masking is the masking the pages use for a game log line.
func masking(secrets ...secret.Secret) func(string) string {
	return func(s string) string { return maskSecretCommand(Redact(s, secrets...)) }
}

func reasonOf(redact func(string) string, lines ...string) string {
	var f loadFailure
	for _, l := range lines {
		f.add(l)
	}
	return f.reason(redact)
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
	want := ": 7 mods were made for Factorio 2.0, and this game runs 2.1: a, b, c, d, e and 2 more"
	if got := reasonOf(masking(), lines...); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadFailureOneIncompatibleMod(t *testing.T) {
	got := reasonOf(masking(), "• lonely", incompatible)
	if want := ": 1 mod was made for Factorio 2.0, and this game runs 2.1: lonely"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A bullet belongs to the reason right under it, so a later reason with no
// bullet is counted without borrowing the earlier name.
func TestLoadFailureCountsAModWithNoBulletWithoutAName(t *testing.T) {
	got := reasonOf(masking(), "• named", incompatible, incompatible)
	if want := ": 2 mods were made for Factorio 2.0, and this game runs 2.1: named and 1 more"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The log holds every run of a crash-looping start, so the same nameless
// reason repeated by each run counts once.
func TestLoadFailureCountsANamelessModOnceAcrossRuns(t *testing.T) {
	var lines []string
	for range 3 {
		lines = append(lines, "• named", incompatible, incompatible)
	}
	got := reasonOf(masking(), lines...)
	if want := ": 2 mods were made for Factorio 2.0, and this game runs 2.1: named and 1 more"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadFailureTakesTheLargestGroupOfIncompatibleMods(t *testing.T) {
	got := reasonOf(masking(),
		"• x", "    • Incompatible Factorio version (current: 2.1, required: 1.1)",
		"• y", incompatible, "• z", incompatible)
	if want := ": 2 mods were made for Factorio 2.0, and this game runs 2.1: y, z"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadFailureShowsTheLastErrorLineAsFactorioSaying(t *testing.T) {
	got := reasonOf(masking(),
		"   0.500 Error Util.cpp:81: first",
		"   1.500 Info Something.cpp:1: not an error",
		"   2.250 Error Mod.cpp:9: the second problem",
	)
	if want := ". Factorio said: the second problem"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoadFailureTrimsALongErrorLine(t *testing.T) {
	got := reasonOf(masking(), "   1.0 Error Mod.cpp:9: "+strings.Repeat("é", 500))
	if n := len([]rune(strings.TrimPrefix(got, ". Factorio said: "))); n != maxErrorLine {
		t.Fatalf("%d characters", n)
	}
}

func TestLoadFailureShowsNothingElse(t *testing.T) {
	got := reasonOf(masking(),
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
	got := reasonOf(masking(pw), "   1.0 Error Mod.cpp:9: login with hunter2-rcon failed")
	if strings.Contains(got, "hunter2-rcon") || !strings.Contains(got, Mask) {
		t.Fatalf("got %q", got)
	}
}

// A secret that the cut to 300 characters would split must already be gone,
// or a piece of it would show.
func TestLoadFailureRedactsBeforeCutting(t *testing.T) {
	pw := secret.New("S3CRETPASSWORD")
	line := strings.Repeat("x", maxErrorLine-5) + pw.Reveal() + " tail"
	got := reasonOf(masking(pw), "   1.0 Error Mod.cpp:9: "+line)
	if strings.Contains(got, "S3CR") {
		t.Fatalf("part of the secret shows: %q", got)
	}
}

// The masking is the pages' own, so a printed command that sets a password
// and the RCON flag are masked as well as the known secrets.
func TestLoadFailureMasksPrintedSecretCommands(t *testing.T) {
	got := reasonOf(masking(), "   1.0 Error Mod.cpp:9: ran config set password letmein now")
	if strings.Contains(got, "letmein") {
		t.Fatalf("got %q", got)
	}
}

func TestLoadFailureRedactsModNames(t *testing.T) {
	pw := secret.New("sekret-mod")
	got := reasonOf(masking(pw), "• sekret-mod", incompatible)
	if strings.Contains(got, "sekret-mod") || !strings.Contains(got, Mask) {
		t.Fatalf("got %q", got)
	}
}

func TestLoadFailureCutsLongModNames(t *testing.T) {
	got := reasonOf(masking(), "• "+strings.Repeat("n", 300), incompatible)
	if !strings.Contains(got, strings.Repeat("n", maxShownModName)+"…") || strings.Contains(got, strings.Repeat("n", maxShownModName+1)) {
		t.Fatalf("got %q", got)
	}
}

func TestLoadFailureBoundsWhatItKeeps(t *testing.T) {
	var f loadFailure
	for i := range 40 {
		f.add(fmt.Sprintf("• m%d", i))
		f.add(fmt.Sprintf("    • Incompatible Factorio version (current: 2.1, required: 9.%d)", i))
	}
	if len(f.byRequired) != maxRequiredVersions {
		t.Errorf("%d versions kept", len(f.byRequired))
	}
	f = loadFailure{}
	f.add("• m")
	f.add("    • Incompatible Factorio version (current: 2.1, required: " + strings.Repeat("9", 100) + ")")
	for r := range f.byRequired {
		if len(r) != maxVersionText {
			t.Errorf("version of %d characters kept", len(r))
		}
	}
}

// startWhileGameDies starts a world whose game is still loading, then puts
// the lines in its log and makes the game exit. It returns Start's error.
func startWhileGameDies(t *testing.T, lines func(rconPassword string) []string) (string, error) {
	t.Helper()
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 30 * time.Second}})
	id := r.newWorld("Dies")
	done := make(chan error, 1)
	go func() { done <- r.c.Start(context.Background(), id, admin, StartOptions{}) }()
	eventually(t, func() bool {
		for _, c := range r.game.State().Containers {
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
	pw := rconPW.Reveal()
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
		return pw, err
	case <-time.After(10 * time.Second):
		t.Fatal("start did not end")
		return "", nil
	}
}

func TestStartExplainsIncompatibleMods(t *testing.T) {
	pw, err := startWhileGameDies(t, func(pw string) []string {
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
	pw, err := startWhileGameDies(t, func(pw string) []string {
		return []string{fmt.Sprintf("   2.000 Error Util.cpp:81: bad rcon password %s given", pw)}
	})
	if !errors.Is(err, ErrStartFailed) {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), pw) || !strings.Contains(err.Error(), "Factorio said: bad rcon password "+Mask+" given") {
		t.Fatalf("got %q", err)
	}
}

func TestStartLoadFailureWithNothingToShowKeepsThePlainText(t *testing.T) {
	_, err := startWhileGameDies(t, func(pw string) []string {
		return []string{"+ exec factorio --rcon-password " + pw}
	})
	if want := "the game did not start: the game stopped while it was loading"; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}
