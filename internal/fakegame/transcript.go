package fakegame

import (
	"embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The transcripts are exact copies of real output from the pinned images; a
// test checks they still match internal/game/testdata.
//
//go:embed transcripts/*/*.log
var transcriptFS embed.FS

// line is one game log line: the seconds since the game started, and the
// text after that field.
type line struct {
	elapsed float64
	text    string
}

func (l line) format(elapsed float64) string { return fmt.Sprintf("%8.3f %s", elapsed, l.text) }

// version holds the pieces of one game version's log, cut from its
// transcripts.
type version struct {
	name     string // 1.1.110, 2.0.77 or 2.1.24
	failExit int    // exit code after a failed load: 1.1 exits 0, the others 1

	head     []line // start of the run, up to the "Loading map" line
	tail     []line // after loading, up to and including the RCON ready line
	conn     line   // "New RCON connection from ..."
	save     []line // the two lines a /server-save writes
	shutdown []line // SIGTERM, save, "Goodbye"
	failed   []line // one start that fails to load the save
}

// The save names in the transcripts, replaced by the name the container loads.
const (
	runSave    = "spike.zip"
	failedSave = "corrupt.zip"
)

var versions = loadVersions()

var tagMajorMinor = regexp.MustCompile(`^\d+\.\d+`)

// versionFor picks the transcripts for an image reference by the major and
// minor number of its tag. An unknown tag gets the newest version.
func versionFor(image string) *version {
	ref, _, _ := strings.Cut(image, "@")
	tag := ""
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		tag = ref[i+1:] // a colon before the last slash belongs to a registry host
	}
	if m := tagMajorMinor.FindString(tag); m != "" {
		for _, v := range versions {
			if strings.HasPrefix(v.name, m+".") {
				return v
			}
		}
	}
	return versions[len(versions)-1]
}

func loadVersions() []*version {
	var out []*version
	// Oldest first, so the last is the newest.
	for _, name := range []string{"1.1.110", "2.0.77", "2.1.24"} {
		out = append(out, loadVersion(name))
	}
	return out
}

func loadVersion(name string) *version {
	run := readLines(name, "run-sigterm.log")
	v := &version{name: name, failExit: 1, failed: readLines(name, "run-failed-load.log")}
	if strings.HasPrefix(name, "1.1.") {
		v.failExit = 0
	}
	loadAt := find(run, 0, "Loading map ")
	rconAt := find(run, loadAt, "Starting RCON interface")
	saveAt := find(run, rconAt, "Saving game as ")
	stopAt := find(run, saveAt, "Received SIGTERM")
	v.head = run[:loadAt+1]
	v.tail = run[loadAt+1 : rconAt+1]
	v.conn = run[find(run, rconAt, "New RCON connection")]
	v.save = run[saveAt : saveAt+2]
	v.shutdown = run[stopAt : find(run, stopAt, "Goodbye")+1]
	return v
}

// find returns the index of the first line at or after from that contains
// want. The transcripts are embedded, so a miss is a programming error.
func find(lines []line, from int, want string) int {
	for i := from; i < len(lines); i++ {
		if strings.Contains(lines[i].text, want) {
			return i
		}
	}
	panic("fakegame: transcript has no line containing " + strconv.Quote(want))
}

// readLines parses a transcript made with `docker logs -t`: each line is a
// Docker timestamp, then the game's own elapsed-seconds field, then text.
func readLines(version, file string) []line {
	raw, err := transcriptFS.ReadFile("transcripts/" + version + "/" + file)
	if err != nil {
		panic("fakegame: " + err.Error())
	}
	var out []line
	for _, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		_, rest, ok := strings.Cut(l, " ") // drop the Docker timestamp
		if !ok || len(rest) < 9 {
			panic("fakegame: unexpected transcript line " + strconv.Quote(l))
		}
		secs, err := strconv.ParseFloat(strings.TrimSpace(rest[:8]), 64)
		if err != nil {
			panic("fakegame: unexpected transcript line " + strconv.Quote(l))
		}
		out = append(out, line{elapsed: secs, text: rest[9:]})
	}
	return out
}
