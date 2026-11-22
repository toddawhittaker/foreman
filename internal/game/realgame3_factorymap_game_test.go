//go:build game

package game

// The factory map on the real game (SPEC 5.16, 8.16). A save with buildings of
// every category on two surfaces is drawn from a read-only copy by the game's
// own benchmark mode. The test checks that the drawing has every surface of
// the game, that each surface's base picture and the layers of the categories
// built there are not empty, and that the save was not touched: its bytes,
// size, modified time and inode are the same afterwards. A 1.1 game cannot
// read a chart, so the map says it is not supported and nothing is drawn.
//
// The save with buildings is made by a throwaway server in a one-off container
// with no network and no published port, so the test never holds the host's
// game port. Run by `make test-game`, never by `make verify`.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The map's state when the world's game cannot be read (SPEC 5.16), as the
// players of a save say it (SavePlayersNotSupported).
const factoryMapNotSupported = "not-supported"

// factoryBuilderLua builds a factory on the first tick of a throwaway server:
// every category on nauvis, belts and an assembler on a second surface, then
// saves the game and writes what it built to script-output/built.json. The
// entity names exist in both 2.0 and 2.1. A position that does not fit is
// skipped, and the test fails when a category has nothing built.
const factoryBuilderLua = `
local function try(surface, name, positions, direction)
  for _, p in ipairs(positions) do
    local ok, e = pcall(function()
      return surface.create_entity{name = name, position = p, direction = direction, force = "player"}
    end)
    if ok and e and e.valid then return true end
  end
  return false
end

local function build()
  local f = game.forces["player"]
  local n = game.surfaces["nauvis"]
  for _, e in pairs(n.find_entities({{-45, -45}, {45, 45}})) do
    if e.valid and e.type ~= "character" then e.destroy() end
  end
  local tiles = {}
  for x = -45, 44 do for y = -45, 44 do tiles[#tiles + 1] = {name = "concrete", position = {x, y}} end end
  n.set_tiles(tiles)

  local extra = game.create_surface("f11-extra", {width = 96, height = 96})
  extra.request_to_generate_chunks({0, 0}, 2)
  extra.force_generate_chunk_requests()
  f.chart(n, {{-96, -96}, {96, 96}})
  f.chart(extra, {{-48, -48}, {48, 48}})

  local built = {}
  local function put(surface, name, positions, direction)
    local key = surface.name .. "/" .. name
    built[key] = try(surface, name, positions, direction)
  end
  local E = defines.direction.east
  put(n, "transport-belt", {{0.5, 0.5}}, E)
  put(n, "inserter", {{3.5, 0.5}}, E)
  put(n, "assembling-machine-1", {{8.5, 0.5}})
  put(n, "small-electric-pole", {{12.5, 0.5}})
  put(n, "steam-engine", {{16.5, 6.5}, {16.5, 8.5}}, defines.direction.north)
  put(n, "pipe", {{22.5, 0.5}})
  put(n, "storage-tank", {{26.5, 0.5}})
  put(n, "straight-rail", {{-10, -10}, {-9, -9}, {-8, -8}})
  put(n, "stone-wall", {{-4.5, 5.5}})
  put(n, "gun-turret", {{-8, 5}, {-8.5, 5.5}})
  put(n, "iron-chest", {{-4.5, -4.5}})
  put(n, "radar", {{-20.5, -20.5}})
  put(extra, "transport-belt", {{0.5, 0.5}}, E)
  put(extra, "assembling-machine-1", {{6.5, 0.5}})

  local names = {}
  for _, s in pairs(game.surfaces) do names[#names + 1] = s.name end
  helpers.write_file("built.json", helpers.table_to_json({built = built, surfaces = names}), false)
  game.server_save("f11built")
end

script.on_event(defines.events.on_tick, function(event)
  if not storage.f11_done then
    storage.f11_done = true
    build()
  end
end)
`

// builtFactory is what the builder reports.
type builtFactory struct {
	Built    map[string]bool `json:"built"`
	Surfaces []string        `json:"surfaces"`
}

// fmLayerOf is the category each built entity belongs to, by the categories of
// SPEC 5.16, which is the test's own table and not the code's.
var fmLayerOf = map[string]factorymap.Category{
	"transport-belt":       factorymap.CategoryBelts,
	"inserter":             factorymap.CategoryBelts,
	"assembling-machine-1": factorymap.CategoryProduction,
	"small-electric-pole":  factorymap.CategoryPower,
	"steam-engine":         factorymap.CategoryPower,
	"pipe":                 factorymap.CategoryFluids,
	"storage-tank":         factorymap.CategoryFluids,
	"straight-rail":        factorymap.CategoryRail,
	"stone-wall":           factorymap.CategoryDefence,
	"gun-turret":           factorymap.CategoryDefence,
	"iron-chest":           factorymap.CategoryLogistics,
}

func TestRealGameFactoryMap(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			if g.tag == "1.1" {
				e.runFactoryMapNotSupported(t, g.tag, g.fixture, g.version)
				return
			}
			e.runFactoryMap(t, g.tag, g.fixture, g.version)
		})
	}
}

// buildFactorySave makes a save with buildings from the fixture, with a
// throwaway server in a container of its own that has no network, and returns
// its bytes and what was built. The server runs with a copy of the world's
// mods folder, which holds the builder mod. The container is removed at the
// end.
func (e *gameEnv) buildFactorySave(t *testing.T, id, tag, fixture, version string) ([]byte, builtFactory) {
	t.Helper()
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "mods"), 0o755); err != nil { //nolint:gosec // the container's user reads this folder
		t.Fatal(err)
	}
	if err := os.Chmod(work, 0o755); err != nil { //nolint:gosec // the container's user reads this folder
		t.Fatal(err)
	}
	fix, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"save.zip":   string(fix),
		"config.ini": "[path]\nread-data=__PATH__executable__/../../data\nwrite-data=/work/wd\n",
		"server-settings.json": `{"name":"f11","description":"f11","tags":[],"visibility":{"public":false,"lan":false},` +
			`"require_user_verification":false,"auto_pause":false,"autosave_interval":0}`,
	}
	for name, body := range files {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { //nolint:gosec // the container's user reads these folders
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil { //nolint:gosec // the container's user reads these files
			t.Fatal(err)
		}
	}

	if err := os.MkdirAll(filepath.Join(work, "wd", "saves"), 0o755); err != nil { //nolint:gosec // the container's user writes here
		t.Fatal(err)
	}
	// The server's mods are the world's: its mod list and the builder's zip.
	for _, f := range []string{world.ModListFile, "f11build_1.0.0.zip"} {
		b, err := os.ReadFile(e.modsPath(id, f)) //nolint:gosec // the test's own world folder
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, "mods", f), b, 0o644); err != nil { //nolint:gosec // the container's user reads this file
			t.Fatal(err)
		}
	}
	name := "foreman-e3test-f11build-" + tag + "-" + e.run
	t.Cleanup(func() { _ = exec.CommandContext(context.Background(), "docker", "rm", "-f", name).Run() }) //nolint:gosec // the test's own container
	e.dockerCLI(t, "run", "-d", "--name", name,
		"--user", fmt.Sprintf("%d:%d", e.uid, e.gid), "--network", "none",
		"--entrypoint", "/opt/factorio/bin/x64/factorio",
		"-v", work+":/work", ImageRef(version),
		"--config", "/work/config.ini", "--mod-directory", "/work/mods",
		"--start-server", "/work/save.zip", "--server-settings", "/work/server-settings.json",
		"--port", "34991")

	saved := filepath.Join(work, "wd", "saves", "f11built.zip")
	builtJSON := filepath.Join(work, "wd", "script-output", "built.json")
	logs := func() string {
		out, _ := exec.CommandContext(context.Background(), "docker", "logs", name).CombinedOutput() //nolint:gosec // the test's own container
		return string(out)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for !fmFileExists(builtJSON) || !fmFileExists(saved) || !strings.Contains(logs(), "Saving finished") {
		if out, _ := exec.CommandContext(context.Background(), "docker", "inspect", "-f", "{{.State.Running}}", name).Output(); strings.TrimSpace(string(out)) != "true" { //nolint:gosec // the test's own container
			t.Fatalf("the throwaway server stopped before it saved the factory:\n%s", logs())
		}
		if time.Now().After(deadline) {
			t.Fatalf("the throwaway server did not save the factory in 2 minutes:\n%s", logs())
		}
		time.Sleep(500 * time.Millisecond)
	}
	e.dockerCLI(t, "rm", "-f", name)

	raw, err := os.ReadFile(builtJSON) //nolint:gosec // the test's own work folder
	if err != nil {
		t.Fatal(err)
	}
	var built builtFactory
	if err := json.Unmarshal(raw, &built); err != nil {
		t.Fatalf("built.json: %v\n%s", err, raw)
	}
	for _, key := range []string{"nauvis/transport-belt", "nauvis/inserter", "nauvis/assembling-machine-1",
		"nauvis/small-electric-pole", "nauvis/steam-engine", "nauvis/pipe", "nauvis/storage-tank",
		"nauvis/straight-rail", "nauvis/stone-wall", "nauvis/gun-turret", "nauvis/iron-chest",
		"f11-extra/transport-belt", "f11-extra/assembling-machine-1"} {
		if !built.Built[key] {
			t.Fatalf("the builder could not place %s: %s", key, raw)
		}
	}
	save, err := os.ReadFile(saved) //nolint:gosec // the test's own work folder
	if err != nil {
		t.Fatal(err)
	}
	return save, built
}

// addBuilderMod puts the builder mod into the world: its zip and an entry in
// the mod list Foreman writes for the world. The built save then asks for no
// mod the world lacks, and the read runs with the mods the save was made with.
func (e *gameEnv) addBuilderMod(t *testing.T, id, tag string) {
	t.Helper()
	unlock, err := e.worlds.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	err = e.worlds.EnsureModList(id)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(e.modsPath(id, world.ModListFile)) //nolint:gosec // the test's own world folder
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Mods []map[string]any `json:"mods"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	list.Mods = append(list.Mods, map[string]any{"name": "f11build", "enabled": true})
	if raw, err = json.Marshal(list); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.modsPath(id, world.ModListFile), raw, 0o600); err != nil { //nolint:gosec // the test's own world folder
		t.Fatal(err)
	}
	zip := zipOfFiles(t, "f11build", "1.0.0", map[string]string{
		"info.json": `{"name":"f11build","version":"1.0.0","title":"Factory builder","author":"foreman tests",` +
			`"factorio_version":"` + tag + `","dependencies":["base"]}`,
		"control.lua": factoryBuilderLua,
	})
	if err := os.WriteFile(e.modsPath(id, "f11build_1.0.0.zip"), zip, 0o600); err != nil { //nolint:gosec // the test's own world folder
		t.Fatal(err)
	}
}

func fmFileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// mapFileState is what the test pins of a world file.
type mapFileState struct {
	sum      string
	size     int64
	modified time.Time
	inode    uint64
}

// fmWorldFiles records every regular file of a world except players.json, which
// the players of a save (8.14) may write.
func fmWorldFiles(t *testing.T, dir string) map[string]mapFileState {
	t.Helper()
	out := map[string]mapFileState{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == world.PlayersFile {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // the test's own world folder
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		st := mapFileState{sum: hex.EncodeToString(sum[:]), size: info.Size(), modified: info.ModTime()}
		if sys, ok := info.Sys().(*syscall.Stat_t); ok {
			st.inode = sys.Ino
		}
		out[rel] = st
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func fmSameWorldFiles(t *testing.T, before, after map[string]mapFileState) {
	t.Helper()
	for rel, b := range before {
		a, ok := after[rel]
		switch {
		case !ok:
			t.Errorf("%s was removed", rel)
		case a.sum != b.sum:
			t.Errorf("%s changed: SHA-256 %s, was %s", rel, a.sum, b.sum)
		case a.size != b.size:
			t.Errorf("%s changed size: %d, was %d", rel, a.size, b.size)
		case !a.modified.Equal(b.modified):
			t.Errorf("%s changed modified time: %v, was %v", rel, a.modified, b.modified)
		case a.inode != b.inode:
			t.Errorf("%s changed inode: %d, was %d", rel, a.inode, b.inode)
		}
	}
	for rel := range after {
		if _, ok := before[rel]; !ok {
			t.Errorf("%s was added", rel)
		}
	}
}

// fmOneOffs lists the factory map's one-off containers on the host.
func (e *gameEnv) fmOneOffs(t *testing.T) map[string]bool {
	t.Helper()
	all, err := e.docker.ListContainers(context.Background(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, c := range all {
		for _, n := range c.Names {
			if strings.Contains(n, "factorymap") {
				out[n] = true
			}
		}
	}
	return out
}

func (e *gameEnv) fmNoNewOneOffs(t *testing.T, before map[string]bool) {
	t.Helper()
	for n := range e.fmOneOffs(t) {
		if !before[n] {
			t.Errorf("the read left the container %s behind", n)
			_ = exec.CommandContext(context.Background(), "docker", "rm", "-f", strings.TrimPrefix(n, "/")).Run() //nolint:gosec // a container this test made
		}
	}
}

func (e *gameEnv) runFactoryMap(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	id := e.newWorld(t, tag, version, "fmap", "", nil)
	e.addBuilderMod(t, id, tag)
	save, built := e.buildFactorySave(t, id, tag, fixture, version)
	at := time.Now().Add(-time.Hour)
	e.putSave(t, id, "factory.zip", save, at)
	dir := e.worldDir(id)
	files := fmWorldFiles(t, dir)
	oneOffs := e.fmOneOffs(t)

	begin := time.Now()
	if err := c.DrawFactoryMap(ctx, id); err != nil {
		t.Fatalf("draw: %v", err)
	}
	var view FactoryMapView
	waitFor(t, 5*time.Minute, "the drawing", func() bool {
		v, err := c.FactoryMap(id)
		if err != nil {
			t.Fatalf("view: %v", err)
		}
		view = v
		return !v.Drawing.DrawnAt.IsZero() || v.Failure != "" || v.Drawing.Failure != ""
	})
	t.Logf("TIMING draw: %s", time.Since(begin).Round(100*time.Millisecond))
	if view.Failure != "" || view.Drawing.Failure != "" {
		t.Fatalf("the drawing failed: %q %q", view.Failure, view.Drawing.Failure)
	}

	// The save is unchanged: bytes, size, modified time and inode.
	fmSameWorldFiles(t, files, fmWorldFiles(t, dir))
	e.fmNoNewOneOffs(t, oneOffs)

	store, err := c.FactoryMapStore(id)
	if err != nil {
		t.Fatal(err)
	}
	d, ok, err := store.Read()
	if err != nil || !ok {
		t.Fatalf("kept drawing: ok %v, err %v", ok, err)
	}
	if d.Basis.Save != "factory.zip" || d.Basis.Size != int64(len(save)) || d.Basis.Modified.Sub(at).Abs() > time.Second {
		t.Errorf("the drawing's basis is %+v, want factory.zip, %d bytes, modified %v", d.Basis, len(save), at)
	}
	if !strings.Contains(d.GameVersion, version) {
		t.Errorf("the drawing says game version %q, want it to hold %s", d.GameVersion, version)
	}

	// Every surface of the game is drawn, and nothing else.
	names := map[string]factorymap.Surface{}
	for _, s := range d.Surfaces {
		names[s.Name] = s
	}
	if len(d.Surfaces) != len(built.Surfaces) {
		t.Errorf("the drawing has %d surfaces (%v), the game has %d (%v)", len(d.Surfaces), names, len(built.Surfaces), built.Surfaces)
	}
	for _, n := range built.Surfaces {
		s, ok := names[n]
		if !ok {
			t.Errorf("surface %q is not in the drawing", n)
			continue
		}
		e.fmCheckSurface(t, store, s, built)
	}
}

// fmCheckSurface checks one surface's base picture and its layers. A layer of a
// category with a building on the surface has a drawn pixel; every layer has
// the base picture's size.
func (e *gameEnv) fmCheckSurface(t *testing.T, store *factorymap.Store, s factorymap.Surface, built builtFactory) {
	t.Helper()
	if s.Width <= 0 || s.Height <= 0 || s.Width > factorymap.MaxSidePixels || s.Height > factorymap.MaxSidePixels || s.TilesPerPixel < 1 {
		t.Errorf("surface %q: size %dx%d, %d tiles a pixel", s.Name, s.Width, s.Height, s.TilesPerPixel)
	}
	base := fmReadPNG(t, store, s.Index, "")
	if b := base.Bounds(); b.Dx() != s.Width || b.Dy() != s.Height {
		t.Errorf("surface %q: the base picture is %dx%d, the drawing says %dx%d", s.Name, b.Dx(), b.Dy(), s.Width, s.Height)
	}
	if fmDrawnPixels(base) == 0 {
		t.Errorf("surface %q: the base picture is empty", s.Name)
	}
	want := map[factorymap.Category]bool{}
	for key, ok := range built.Built {
		surface, entity, _ := strings.Cut(key, "/")
		if cat, known := fmLayerOf[entity]; ok && surface == s.Name && known {
			want[cat] = true
		}
	}
	for _, cat := range factorymap.Categories {
		layer := fmReadPNG(t, store, s.Index, cat)
		if layer.Bounds() != base.Bounds() {
			t.Errorf("surface %q, layer %s: bounds %v, want the base picture's %v", s.Name, cat, layer.Bounds(), base.Bounds())
		}
		if want[cat] && fmDrawnPixels(layer) == 0 {
			t.Errorf("surface %q, layer %s: empty, but a building of the category stands there", s.Name, cat)
		}
	}
}

func fmReadPNG(t *testing.T, store *factorymap.Store, surface int, layer factorymap.Category) image.Image {
	t.Helper()
	f, err := store.OpenPNG(surface, layer)
	if err != nil {
		t.Fatalf("picture of surface %d, layer %q: %v", surface, layer, err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("picture of surface %d, layer %q is not a PNG: %v", surface, layer, err)
	}
	return img
}

// fmDrawnPixels counts the pixels that are not fully transparent.
func fmDrawnPixels(img image.Image) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				n++
			}
		}
	}
	return n
}

// runFactoryMapNotSupported checks a 1.1 world: the map says it is not
// supported, a draw is refused, nothing is kept, no container is made, and the
// save is untouched.
func (e *gameEnv) runFactoryMapNotSupported(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	id := e.newWorld(t, tag, version, "fmap", fixture, map[string]time.Duration{"old.zip": time.Hour})
	dir := e.worldDir(id)
	files := fmWorldFiles(t, dir)
	oneOffs := e.fmOneOffs(t)

	view, err := c.FactoryMap(id)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != factoryMapNotSupported {
		t.Errorf("the map's state is %q, want %q", view.State, factoryMapNotSupported)
	}
	if err := c.DrawFactoryMap(ctx, id); err == nil {
		t.Error("a draw on a 1.1 world was not refused")
	}
	if view, err = c.FactoryMap(id); err != nil || !view.Drawing.DrawnAt.IsZero() {
		t.Errorf("the 1.1 world has a drawing: %+v, %v", view.Drawing, err)
	}
	if store, err := c.FactoryMapStore(id); err == nil {
		if d, ok, _ := store.Read(); ok {
			t.Errorf("the 1.1 world has kept pictures: %+v", d)
		}
	}
	fmSameWorldFiles(t, files, fmWorldFiles(t, dir))
	e.fmNoNewOneOffs(t, oneOffs)
}
