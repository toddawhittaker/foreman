// Package crashreasons names why a game stopped, from the game's log or
// Docker's start error, by the catalogue in catalogue.json (SPEC 8.7, 8.8).
// Patterns match the message's text, never the game's `File.cpp:NN`,
// which differs by version. Every entry names fixtures captured from a
// real game under internal/game/testdata. No match means unknown, "".
package crashreasons

import (
	_ "embed"
	"encoding/json"
	"regexp"
)

// Reasons the catalogue names from text.
const (
	ModMissingDependency = "mod-missing-dependency"
	ModError             = "mod-error"
	SaveUnreadable       = "save-unreadable"
	PortInUse            = "port-in-use"
)

// Reasons named without a pattern.
const (
	// OutOfMemory is named only from Docker's OOMKilled flag: the game's
	// log just stops when the kernel kills it.
	OutOfMemory = "out-of-memory"
	// ModsForOtherVersion is named when the game's log lists mods made for
	// another Factorio, which the game package already parses by version.
	ModsForOtherVersion = "mods-for-other-version"
)

// Where an entry's text comes from.
const (
	SourceGameLog     = "game-log"
	SourceDockerStart = "docker-start"
)

// Entry is one reason in the catalogue.
type Entry struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	// Pattern is a regular expression matched against one line.
	Pattern string `json:"pattern"`
	// Fixtures are paths under internal/game/testdata, each a real capture
	// this entry names.
	Fixtures []string `json:"fixtures"`
	re       *regexp.Regexp
}

//go:embed catalogue.json
var catalogueJSON []byte

// catalogue is in priority order: when several entries match one log,
// the first wins.
var catalogue = mustLoad(catalogueJSON)

func mustLoad(b []byte) []Entry {
	var c struct {
		Reasons []Entry `json:"reasons"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		panic("crashreasons: catalogue.json: " + err.Error())
	}
	for i := range c.Reasons {
		c.Reasons[i].re = regexp.MustCompile(c.Reasons[i].Pattern)
	}
	return c.Reasons
}

// Entries returns a copy of the catalogue, in priority order.
func Entries() []Entry {
	return append([]Entry(nil), catalogue...)
}

// Line names the reason one game log line gives, or "".
func Line(line string) string {
	return match(SourceGameLog, line)
}

// Log names the reason a run's game log gives: of the entries any line
// matches, the first in the catalogue; "" when none does.
func Log(lines []string) string {
	best := ""
	for _, l := range lines {
		best = Prefer(best, Line(l))
	}
	return best
}

// DockerStart names the reason in Docker's error for a container start
// that failed, or "".
func DockerStart(errText string) string {
	return match(SourceDockerStart, errText)
}

// Prefer returns whichever of two reasons comes first in the catalogue;
// any reason beats "".
func Prefer(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	for _, e := range catalogue {
		if e.ID == a || e.ID == b {
			return e.ID
		}
	}
	return a
}

func match(source, text string) string {
	for _, e := range catalogue {
		if e.Source == source && e.re.MatchString(text) {
			return e.ID
		}
	}
	return ""
}
