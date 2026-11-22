package chart

import (
	"bytes"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The mod folder is named after the mod and its version.
func TestModDir(t *testing.T) {
	if ModDir != "foreman-chart_1.0.0" {
		t.Errorf("ModDir %q", ModDir)
	}
}

// SPEC 14.3: the square is 1024 by 1024 tiles, so at most 1,024 chunks.
func TestLimits(t *testing.T) {
	if Radius*2*32 != 1024 || MaxChunks != 1024 || MaxFileBytes != 4<<20 {
		t.Errorf("radius %d, chunks %d, bytes %d", Radius, MaxChunks, MaxFileBytes)
	}
}

// The script reads its params from ParamsFile, writes into the folder the
// container moves out, ends the test run, and writes the manifest last.
func TestControl(t *testing.T) {
	for _, want := range []string{
		`require("params")`,
		`local OUT = "` + gameenv.ChartDir + `/"`,
		`script.finish_test(passed)`,
		`force.get_chunk_chart`,
		`create_surface`,
	} {
		if !bytes.Contains(Control, []byte(want)) {
			t.Errorf("control.lua lacks %s", want)
		}
	}
	if ParamsFile != "params.lua" {
		t.Errorf("ParamsFile %q does not match require(\"params\")", ParamsFile)
	}
	chart := bytes.Index(Control, []byte(`helpers.write_file(OUT .. file`))
	manifest := bytes.Index(Control, []byte(`helpers.write_file(OUT .. "manifest.json"`))
	if chart < 0 || manifest < chart {
		t.Error("the manifest is not written after the chart")
	}
}
