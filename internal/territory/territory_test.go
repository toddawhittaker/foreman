package territory

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// The real game's output for Vulcanus, seed 123456, at 1280 on 2.1.24:
// 25 territories reach the generated area, and 18 of them, 12 small and 6
// medium, reach into the picture.
func TestParseRealGame(t *testing.T) {
	b, err := os.ReadFile("testdata/vulcanus-123456-1280.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(b, 1280)
	if err != nil {
		t.Fatal(err)
	}
	ov := Geometry(r, 1280)
	want := []Count{{Small, 12}, {Medium, 6}}
	if !slices.Equal(ov.Counts, want) {
		t.Errorf("counts %v, want %v", ov.Counts, want)
	}
}

func TestParseKeepsTerritories(t *testing.T) {
	b := `{"territories":[
		{"unit":"small-demolisher","chunks":[[0,0],[1,0]]},
		{"unit":"medium-demolisher","chunks":[[-2,-2]]},
		{"unit":"big-demolisher","chunks":[[1,1]]},
		{"unit":"","chunks":[[-1,1]]}]}`
	r, err := Parse([]byte(b), 128)
	if err != nil {
		t.Fatal(err)
	}
	want := []Territory{
		{Small, []Chunk{{0, 0}, {1, 0}}},
		{Medium, []Chunk{{-2, -2}}},
		{Big, []Chunk{{1, 1}}},
		{Unknown, []Chunk{{-1, 1}}},
	}
	if len(r.Territories) != len(want) {
		t.Fatalf("got %v, want %v", r.Territories, want)
	}
	for i, w := range want {
		g := r.Territories[i]
		if g.Size != w.Size || !slices.Equal(g.Chunks, w.Chunks) {
			t.Errorf("territory %d is %v, want %v", i, g, w)
		}
	}
}

// A 128 picture shows chunks -2 to 1: the rest are dropped, and so is a
// territory with nothing left.
func TestParseClipsToPicture(t *testing.T) {
	b := `{"territories":[
		{"unit":"small-demolisher","chunks":[[-3,0],[-2,0],[1,0],[2,0],[0,-3],[0,2]]},
		{"unit":"medium-demolisher","chunks":[[5,5],[-3,-3]]}]}`
	r, err := Parse([]byte(b), 128)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Territories) != 1 || !slices.Equal(r.Territories[0].Chunks, []Chunk{{-2, 0}, {1, 0}}) {
		t.Errorf("got %v, want one territory with chunks -2,0 and 1,0", r.Territories)
	}
}

func TestParseEmpty(t *testing.T) {
	r, err := Parse([]byte(`{"territories":[]}`), 1280)
	if err != nil || len(r.Territories) != 0 {
		t.Errorf("got %v, %v; want no territories", r, err)
	}
}

// SPEC 14.1: the JSON comes from a game container, so Parse refuses what
// the script never writes.
func TestParseRefuses(t *testing.T) {
	many := `{"territories":[` + strings.Repeat(`{"unit":"","chunks":[]},`, MaxTerritories) + `{"unit":"","chunks":[]}]}`
	big := `{"territories":[{"unit":"","chunks":[` + strings.Repeat(`[0,0],`, MaxJSON/6) + `[0,0]]}]}`
	for name, b := range map[string]string{
		"too big":              big,
		"too many territories": many,
		"unknown demolisher":   `{"territories":[{"unit":"behemoth-biter","chunks":[[0,0]]}]}`,
		"chunk not a pair":     `{"territories":[{"unit":"","chunks":[[0,0,0]]}]}`,
		"chunk too short":      `{"territories":[{"unit":"","chunks":[[0]]}]}`,
		"fractional chunk":     `{"territories":[{"unit":"","chunks":[[0.5,0]]}]}`,
		"huge chunk":           `{"territories":[{"unit":"","chunks":[[1e30,0]]}]}`,
		"chunk named twice":    `{"territories":[{"unit":"","chunks":[[0,0]]},{"unit":"","chunks":[[0,0]]}]}`,
		"unknown field":        `{"territories":[],"extra":1}`,
		"object not a list":    `{"territories":{}}`,
		"data after":           `{"territories":[]} {}`,
		"not JSON":             `territories`,
	} {
		if _, err := Parse([]byte(b), 1280); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err %v, want ErrInvalid", name, err)
		}
	}
	if len(big) <= MaxJSON {
		t.Fatalf("the too-big case is only %d bytes", len(big))
	}
}

func TestParseRefusesSize(t *testing.T) {
	for _, size := range []int{0, -128, 100, 1300, MaxPicture + 64} {
		if _, err := Parse([]byte(`{"territories":[]}`), size); !errors.Is(err, ErrInvalid) {
			t.Errorf("size %d: err %v, want ErrInvalid", size, err)
		}
	}
	for _, size := range []int{64, 1024, 1280, MaxSize, 3584, MaxPicture} {
		if _, err := Parse([]byte(`{"territories":[]}`), size); err != nil {
			t.Errorf("size %d: %v", size, err)
		}
	}
}

// The territory sizes are offered only up to MaxSize, which must be a
// preview size: 1024 to 4096 in steps of 128. It is 3072, the largest size
// measured with room to spare on 2.0 and 2.1: 750 MiB of 1 GiB and 27 s of
// 60 at worst.
func TestMaxSizeIsAPreviewSize(t *testing.T) {
	if MaxSize < 1024 || MaxSize > MaxPicture || MaxSize%128 != 0 {
		t.Errorf("MaxSize %d is not a preview size", MaxSize)
	}
	if MaxSize != 3072 {
		t.Errorf("MaxSize is %d; raising it needs new measurements in a real game", MaxSize)
	}
}

func TestLabel(t *testing.T) {
	for size, want := range map[string]string{Small: "Small", Medium: "Medium", Big: "Big", Unknown: "Unknown", "other": "Unknown"} {
		if got := Label(i18n.English, size); got != want {
			t.Errorf("Label(%q) = %q, want %q", size, got, want)
		}
	}
}

// Every glyph of every language's label words is in the fonts the PNG is
// drawn with, so a translation that needs a new glyph fails here (SPEC
// 22.5) rather than drawing boxes. fonts/NOTICE says how to add one.
func TestLabelGlyphsInFonts(t *testing.T) {
	fs, err := fonts()
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range i18n.Languages {
		for _, size := range Sizes {
			text := Label(lang.Tag, size)
			if !hasGlyphs(fontFor(fs, text), text) {
				t.Errorf("%s: no font has every glyph of %q", lang.Tag, text)
			}
		}
	}
}

// The Chinese subset holds the glyphs the zh-Hans labels are expected to
// use, the game's own words for the sizes; Go Bold has none of them.
func TestFontsCoverChineseSizes(t *testing.T) {
	fs, err := fonts()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"小型", "中型", "大型", "未知"} {
		if hasGlyphs(fs[0], text) {
			t.Errorf("Go Bold has %q; the subset is not needed for it", text)
		}
		if !hasGlyphs(fs[1], text) {
			t.Errorf("the Chinese subset lacks a glyph of %q", text)
		}
	}
	if fontFor(fs, "Мелкий") != fs[0] {
		t.Error("Cyrillic is not drawn with Go Bold")
	}
}

func ExampleParse() {
	r, _ := Parse([]byte(`{"territories":[{"unit":"small-demolisher","chunks":[[0,0],[1,0],[9,9]]}]}`), 128)
	fmt.Println(r.Territories)
	// Output: [{small [{0 0} {1 0}]}]
}
