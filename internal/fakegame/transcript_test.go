package fakegame

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded transcripts must stay exact copies of the real captures in
// internal/game/testdata (the failed-load file is the first start of
// run-corrupt.log).
func TestTranscriptsMatchTheCaptures(t *testing.T) {
	for _, v := range versions {
		dir := filepath.Join("..", "game", "testdata", v.name)
		sigterm, err := os.ReadFile(filepath.Join(dir, "run-sigterm.log")) //nolint:gosec // a fixed test file
		if err != nil {
			t.Fatal(err)
		}
		mine, err := transcriptFS.ReadFile("transcripts/" + v.name + "/run-sigterm.log")
		if err != nil {
			t.Fatal(err)
		}
		if string(mine) != string(sigterm) {
			t.Errorf("%s run-sigterm.log differs from the capture", v.name)
		}
		flag, err := os.ReadFile(filepath.Join(dir, "run-whitelist-flag-missing.log")) //nolint:gosec // a fixed test file
		if err != nil {
			t.Fatal(err)
		}
		mineFlag, err := transcriptFS.ReadFile("transcripts/" + v.name + "/run-whitelist-flag-missing.log")
		if err != nil {
			t.Fatal(err)
		}
		if string(mineFlag) != string(flag) {
			t.Errorf("%s run-whitelist-flag-missing.log differs from the capture", v.name)
		}
		corrupt, err := os.ReadFile(filepath.Join(dir, "run-corrupt.log")) //nolint:gosec // a fixed test file
		if err != nil {
			t.Fatal(err)
		}
		first, err := transcriptFS.ReadFile("transcripts/" + v.name + "/run-failed-load.log")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(corrupt), string(first)) || !strings.HasSuffix(string(first), "Goodbye\n") ||
			strings.Count(string(first), "Goodbye") != 1 {
			t.Errorf("%s run-failed-load.log is not the first start in run-corrupt.log", v.name)
		}
	}
}

func TestTranscriptsHaveNoSecret(t *testing.T) {
	for _, v := range versions {
		for _, set := range [][]line{v.head, v.tail, v.shutdown, v.failed, v.oom, v.whitelistFail} {
			for _, l := range set {
				if strings.Contains(l.text, "rcon-password") && !strings.Contains(l.text, "<private>") {
					t.Errorf("%s: a password may show in %q", v.name, l.text)
				}
			}
		}
	}
}

func TestVersionFor(t *testing.T) {
	for image, want := range map[string]string{
		"factoriotools/factorio:1.1.110@sha256:abc": "1.1.110",
		"factoriotools/factorio:1.1":                "1.1.110",
		"factoriotools/factorio:2.0.77":             "2.0.77",
		"factoriotools/factorio:2.1.30":             "2.1.24",
		"factoriotools/factorio:3.0.1":              "2.1.24",
		"factoriotools/factorio:latest":             "2.1.24",
		"factoriotools/factorio":                    "2.1.24",
		"localhost:5000/factorio":                   "2.1.24",
		"localhost:5000/factorio:2.0.1":             "2.0.77",
	} {
		if got := versionFor(image).name; got != want {
			t.Errorf("versionFor(%q) = %s, want %s", image, got, want)
		}
	}
}

func TestSplitsAreWhereTheGameWritesThem(t *testing.T) {
	for _, v := range versions {
		last := func(ls []line) string { return ls[len(ls)-1].text }
		if !strings.Contains(last(v.head), "Loading map") ||
			!strings.Contains(last(v.tail), "Starting RCON interface") ||
			!strings.Contains(v.save[0].text, "Saving game as") ||
			!strings.Contains(v.save[1].text, "Saving finished") ||
			!strings.Contains(v.shutdown[0].text, "Received SIGTERM") ||
			!strings.Contains(last(v.shutdown), "Goodbye") ||
			!strings.Contains(v.conn.text, "New RCON connection") {
			t.Errorf("%s: a piece of the transcript is cut in the wrong place", v.name)
		}
	}
}

func TestOOMAndWhitelistPiecesAreCutWhereTheGameWritesThem(t *testing.T) {
	for _, v := range versions {
		last := v.oom[len(v.oom)-1].text
		if !strings.HasPrefix(last, "Loading mod ") || strings.Contains(last, "Checksum") {
			t.Errorf("%s: the out-of-memory log ends at %q, not while mods load", v.name, last)
		}
		if !strings.HasPrefix(v.whitelistFail[0].text, "Error CommandLineMultiplayer.cpp:") ||
			!strings.Contains(v.whitelistFail[0].text, "use-server-whitelist must be set when server-whitelist is set.") ||
			!strings.Contains(v.whitelistFail[len(v.whitelistFail)-1].text, "Goodbye") {
			t.Errorf("%s: the whitelist failure is cut in the wrong place", v.name)
		}
	}
}

func TestFoldName(t *testing.T) {
	for in, want := range map[string]string{"ü-name": "u-name", "plain": "plain", "Ésope": "Esope", "": ""} {
		if got := foldName(in); got != want {
			t.Errorf("foldName(%q) = %q, want %q", in, got, want)
		}
	}
}
