package resources

import (
	"bytes"
	"regexp"
	"strconv"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The mod folder is named after the mod and its version.
func TestModDir(t *testing.T) {
	if ModDir != "foreman-resources_1.0.0" {
		t.Errorf("ModDir %q", ModDir)
	}
}

// The script reads its parameters from ParamsFile, writes on 1.1 and 2.x
// alike, and writes the file the container copies out.
func TestControl(t *testing.T) {
	for _, want := range []string{
		`require("params")`,
		`script.on_init`,
		`local h = helpers or game`,
		`h.write_file("` + gameenv.ResourceFile + `"`,
		`h.table_to_json(`,
	} {
		if !bytes.Contains(Control, []byte(want)) {
			t.Errorf("control.lua lacks %s", want)
		}
	}
	if bytes.Contains(Control, []byte("helpers.")) {
		t.Error("control.lua calls helpers directly, which 1.1 lacks")
	}
	if ParamsFile != "params.lua" {
		t.Errorf("ParamsFile %q does not match require(\"params\")", ParamsFile)
	}
}

// The dense-count threshold sits between the most ore tiles a default map
// has and the ore tiles of Nauvis with every resource at 600%, with a factor
// of at least 2.5 on both sides (ADR 0015). Measured at 3072 tiles, seed
// 123456, on 1.1.110, 2.0.77 and 2.1.24: Vulcanus at the defaults has
// 141,912 ore tiles, the most of any planet, and Nauvis at 600% has at
// least 1,097,357 (1.1.110).
func TestDenseOreTiles(t *testing.T) {
	const largestDefault, rich = 141912, 1097357
	m := regexp.MustCompile(`(?m)^local dense_ore_tiles = params\.dense_ore_tiles or (\d+)$`).FindSubmatch(Control)
	if m == nil {
		t.Fatal("control.lua has no dense_ore_tiles default")
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	if n*2 < largestDefault*5 || n*5 > rich*2 {
		t.Errorf("dense_ore_tiles %d is not 2.5 times above %d and below %d", n, largestDefault, rich)
	}
}
