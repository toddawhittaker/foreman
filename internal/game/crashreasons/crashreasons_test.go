package crashreasons

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// testdata is the real-game captures the catalogue's fixtures name.
const testdata = "../testdata"

func readLines(t *testing.T, rel string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testdata, filepath.FromSlash(rel))) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for l := range strings.Lines(string(b)) {
		lines = append(lines, strings.TrimSuffix(l, "\n"))
	}
	return lines
}

// logReason folds Line over a log with Prefer, as the game package does.
func logReason(lines []string) string {
	best := ""
	for _, l := range lines {
		best = Prefer(best, Line(l))
	}
	return best
}

// Every entry has a fixture captured from a real game, and each of its
// fixtures is named as that entry (SPEC 8.7).
func TestEveryEntryNamesItsRealFixtures(t *testing.T) {
	if len(Entries()) == 0 {
		t.Fatal("the catalogue is empty")
	}
	for _, e := range Entries() {
		if len(e.Fixtures) == 0 {
			t.Errorf("%s has no fixture captured from a real game", e.ID)
		}
		for _, fx := range e.Fixtures {
			lines := readLines(t, fx)
			var got string
			switch e.Source {
			case SourceGameLog:
				got = logReason(lines)
			case SourceDockerStart:
				for _, l := range lines {
					if id := DockerStart(l); id != "" {
						got = id
					}
				}
			}
			if got != e.ID {
				t.Errorf("%s: named %q, want %q", fx, got, e.ID)
			}
		}
	}
}

// The game's source line numbers differ by version, so no pattern may
// lean on them (testdata README, section 5).
func TestPatternsNeverMatchSourceLines(t *testing.T) {
	for _, e := range Entries() {
		if strings.Contains(e.Pattern, ".cpp") || strings.Contains(e.Pattern, `\.cpp`) {
			t.Errorf("%s matches a source file name: %q", e.ID, e.Pattern)
		}
		if regexp.MustCompile(`:\\d`).MatchString(e.Pattern) {
			t.Errorf("%s matches a line number: %q", e.ID, e.Pattern)
		}
	}
	// The same message from another source line is still named.
	if got := Line(`  0.258 Error Mods.cpp:9999: Failed to load mod "x": __x__/data.lua:1: boom`); got != "mod-error" {
		t.Errorf("a moved source line was named %q", got)
	}
}

// Real logs whose reason is not in the catalogue are unknown, never a
// guess: clean runs, signals, and the out-of-memory kill, whose log just
// stops (SPEC 8.7; out of memory comes from Docker's flag only).
func TestOtherRealLogsAreUnknown(t *testing.T) {
	named := map[string]bool{}
	for _, e := range Entries() {
		for _, fx := range e.Fixtures {
			named[fx] = true
		}
	}
	matches, err := filepath.Glob(filepath.Join(testdata, "*", "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, m := range matches {
		rel, _ := filepath.Rel(testdata, m)
		rel = filepath.ToSlash(rel)
		if named[rel] || strings.HasPrefix(rel, "docker/") {
			continue
		}
		checked++
		if got := logReason(readLines(t, rel)); got != "" {
			t.Errorf("%s: named %q, want unknown", rel, got)
		}
	}
	for _, must := range []string{"1.1.110/run-mod-missing-dep.log", "2.1.24/run-oom.log", "2.1.24/run-sigterm.log"} {
		if named[must] {
			t.Errorf("%s is a fixture, but its log names no reason", must)
		}
	}
	if checked < 30 {
		t.Fatalf("only %d other logs checked; the glob found too few", checked)
	}
}

func TestNoMatchIsUnknown(t *testing.T) {
	if got := logReason([]string{"", "   1.000 Goodbye", "Error Util.cpp:81: something new"}); got != "" {
		t.Errorf("got %q, want unknown", got)
	}
	if got := logReason(nil); got != "" {
		t.Errorf("no lines named %q", got)
	}
	if got := DockerStart(""); got != "" {
		t.Errorf("an empty error named %q", got)
	}
}

// A missing dependency's first line ends in a space, so it is not taken
// for a mod's own error; whichever comes first in the catalogue wins.
func TestCatalogueOrderDecides(t *testing.T) {
	lines := []string{
		`Error Util.cpp:81: Failed to load mod "a": __a__/data.lua:1: boom`,
		`Error Util.cpp:81: Failed to load mod "needsdep": `,
		"• needsdep",
		"    • Missing required dependency other >= 1.0.0",
	}
	if got := logReason(lines); got != "mod-missing-dependency" {
		t.Errorf("got %q", got)
	}
	if got := Line(lines[1]); got != "" {
		t.Errorf("the dependency message's first line was named %q", got)
	}
	if Prefer("", "mod-error") != "mod-error" || Prefer("mod-error", "") != "mod-error" {
		t.Error("unknown beat a reason")
	}
	if Prefer("mod-error", "mod-missing-dependency") != "mod-missing-dependency" {
		t.Error("the later entry won")
	}
}

// The port classifier reads Docker's start error, from the CLI or the
// Engine API, and nothing else.
func TestDockerStart(t *testing.T) {
	api := `failed to set up container networking: driver failed programming external connectivity on endpoint x (abc): Bind for 0.0.0.0:44198 failed: port is already allocated`
	if got := DockerStart(api); got != PortInUse {
		t.Errorf("Engine API error named %q", got)
	}
	if got := DockerStart("Bind for [::]:34197 failed: port is already allocated"); got != PortInUse {
		t.Errorf("IPv6 bind named %q", got)
	}
	if got := DockerStart("no such image"); got != "" {
		t.Errorf("another error named %q", got)
	}
	// A game log line saying the same words is not Docker's start error.
	if got := Line("Bind for 0.0.0.0:1 failed: port is already allocated"); got != "" {
		t.Errorf("a log line was named %q from a Docker-only entry", got)
	}
}

func TestIDsAreKnownAndUnique(t *testing.T) {
	var ids []string
	for _, e := range Entries() {
		if slices.Contains(ids, e.ID) {
			t.Errorf("%s twice", e.ID)
		}
		ids = append(ids, e.ID)
		if e.Source != SourceGameLog && e.Source != SourceDockerStart {
			t.Errorf("%s: source %q", e.ID, e.Source)
		}
	}
	for _, id := range []string{ModMissingDependency, ModError, SaveUnreadable, PortInUse} {
		if !slices.Contains(ids, id) {
			t.Errorf("constant %s is not in the catalogue", id)
		}
	}
	for _, id := range []string{OutOfMemory, ModsForOtherVersion} {
		if slices.Contains(ids, id) {
			t.Errorf("%s is named from Docker's flag or the mod parse, not a pattern", id)
		}
	}
}

func TestEntriesIsACopy(t *testing.T) {
	Entries()[0].ID = "changed"
	if Entries()[0].ID == "changed" {
		t.Error("a caller changed the catalogue")
	}
}
