package factorymap

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The mod folder is named after the mod and its version.
func TestModDir(t *testing.T) {
	if ModDir != "foreman-factorymap_1.0.0" {
		t.Errorf("ModDir %q", ModDir)
	}
}

// luaConst reads `local NAME = <expression of whole numbers and *>` from the
// script.
func luaConst(t *testing.T, name string) int {
	t.Helper()
	m := regexp.MustCompile(`(?m)^local ` + name + ` = ([0-9 *]+)$`).FindSubmatch(Control)
	if m == nil {
		t.Fatalf("control.lua has no `local %s = <number>`", name)
	}
	n := 1
	for _, f := range strings.Split(string(m[1]), "*") {
		v, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		n *= v
	}
	return n
}

// The script's limits are SPEC 14.6's: what the parser refuses, the mod does
// not write. The byte limits sit under the 512 MiB total, so the chart stops
// before the buildings' room is gone.
func TestLimitsMatchTheParser(t *testing.T) {
	if got := luaConst(t, "MAX_SURFACES"); got != factorymap.MaxSurfaces {
		t.Errorf("MAX_SURFACES %d, parser %d", got, factorymap.MaxSurfaces)
	}
	if got := luaConst(t, "MAX_CHUNKS_PER_SURFACE"); got != factorymap.MaxChunksPerSurface {
		t.Errorf("MAX_CHUNKS_PER_SURFACE %d, parser %d", got, factorymap.MaxChunksPerSurface)
	}
	if got := luaConst(t, "MAX_ENTITIES"); got != factorymap.MaxEntities {
		t.Errorf("MAX_ENTITIES %d, parser %d", got, factorymap.MaxEntities)
	}
	if got := luaConst(t, "CHUNK"); got != factorymap.ChunkPixels {
		t.Errorf("CHUNK %d, parser %d", got, factorymap.ChunkPixels)
	}
	maxBytes, maxChart := luaConst(t, "MAX_BYTES"), luaConst(t, "MAX_CHART_BYTES")
	if maxBytes >= factorymap.MaxOutputBytes {
		t.Errorf("MAX_BYTES %d leaves no room under %d", maxBytes, factorymap.MaxOutputBytes)
	}
	if maxChart >= maxBytes {
		t.Errorf("MAX_CHART_BYTES %d must be under MAX_BYTES %d", maxChart, maxBytes)
	}
}

// Every manifest field the parser reads is one the script writes, and the
// script writes the format and the folder the rest of Foreman uses.
func TestManifestFieldsAndFolder(t *testing.T) {
	src := string(Control)
	typ := reflect.TypeOf(factorymap.ManifestSurface{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if !regexp.MustCompile(`(?m)^\s+` + tag + ` = `).MatchString(src) {
			t.Errorf("control.lua does not write the manifest field %q", tag)
		}
	}
	mtyp := reflect.TypeOf(factorymap.Manifest{})
	for i := 0; i < mtyp.NumField(); i++ {
		tag := strings.Split(mtyp.Field(i).Tag.Get("json"), ",")[0]
		if !strings.Contains(src, `"`+tag+`"`) && !strings.Contains(src, `'{"`+tag+`"`) && !strings.Contains(src, `,"`+tag+`"`) {
			t.Errorf("control.lua does not write the top-level manifest field %q", tag)
		}
	}
	if !strings.Contains(src, `'{"format":`+strconv.Itoa(factorymap.ManifestFormat)+`,`) {
		t.Errorf("control.lua does not write format %d", factorymap.ManifestFormat)
	}
	if want := `local OUT = "` + gameenv.FactoryMapDir + `/"`; !strings.Contains(src, want) {
		t.Errorf("control.lua lacks %s", want)
	}
	for _, want := range []string{`"manifest.json"`, `"surface-"`, `"-entities.jsonl"`, `".jsonl"`} {
		if !strings.Contains(src, want) {
			t.Errorf("control.lua lacks %s", want)
		}
	}
}

// The script only reads the map. It must not change anything in the game, and
// it writes only under its own folder.
func TestReadOnly(t *testing.T) {
	for _, bad := range []string{
		"create_entity", "destroy(", ".chart(", "chart_all", "rechart", "request_to_generate_chunks",
		"force_generate_chunk_requests", "create_surface", "delete_surface", "game.write_file", "remote.", "rcon.",
	} {
		if strings.Contains(string(Control), bad) {
			t.Errorf("control.lua must not use %s", bad)
		}
	}
	// Every write goes through OUT.
	for _, line := range strings.Split(string(Control), "\n") {
		if strings.Contains(line, "helpers.write_file(") && !strings.Contains(line, "OUT ..") {
			t.Errorf("a write outside OUT: %s", strings.TrimSpace(line))
		}
	}
}

// The entity file is written the way internal/factorymap reads it: types
// counted from 0, and a box in tiles from the chart's corner with the far
// corner excluded.
func TestEntityLineContract(t *testing.T) {
	src := string(Control)
	for _, want := range []string{
		"i = #types\n", // 0-based index
		"local origin_x, origin_y = min_x * CHUNK",     // origin is the minimum chunk
		"math.floor(corner(lt",                         // box rounded out
		"math.ceil(corner(rb",                          // box rounded out
		"if x2 > width then x2 = width end",            // kept inside the chart
		"if x1 < 0 then x1 = 0 end",                    // never negative
		`list = helpers.table_to_json(types)`,          // the type list comes first
		"script.on_init(run_once)",                     // a mod added to a save
		"script.on_event(defines.events.on_tick, run_", // a save that holds the mod
		"storage.foreman_factorymap_done",              // once only
	} {
		if !strings.Contains(src, want) {
			t.Errorf("control.lua lacks %q", want)
		}
	}
	// The manifest is written last, so a stopped run leaves none.
	if strings.LastIndex(src, `"manifest.json"`) < strings.LastIndex(src, "write_entities(force, s, entity_file") {
		t.Error("the manifest must be written after every surface")
	}
}
