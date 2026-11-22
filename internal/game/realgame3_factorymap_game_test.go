//go:build game

package game

// The factory map on the real game (SPEC 5.16, 8.16): a drawing is made from a
// read-only copy of the newest save by the game's own benchmark mode, and the
// save is never touched (bytes, size, modified time and inode are the same
// afterwards). A 1.1 game cannot read a chart, so the map says it is
// unsupported and nothing is drawn.
//
// A game with no player, as the fixtures and any save made by a headless
// server, charts nothing (force.chart, chart_all and a powered radar all leave
// no charted chunk), so a drawing of a fixture has no surface. The test of a
// drawing with charted surfaces therefore needs a save that people played. It
// runs when FOREMAN_TEST_CHARTED_SAVE_2_0 and FOREMAN_TEST_CHARTED_SAVE_2_1
// name a played save of a Space Age world, and is skipped otherwise.
// FOREMAN_TEST_CHARTED_MODS_2_1 names a folder with the mods the save needs,
// and its mod-list.json, when the save uses mods. Run by `make test-game`,
// never by `make verify`.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	factorymapmod "github.com/toddawhittaker/foreman/internal/game/factorymap"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The map's states (SPEC 5.16), which F5 defines as FactoryMap* constants in
// internal/game/factorymap.go.
const (
	fmDrawingState     = "drawing"
	fmFailedState      = "failed"
	fmDrawnState       = "drawn"
	fmUnsupportedState = "unsupported"
)

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
				e.runfmUnsupportedState(t, g.tag, g.fixture, g.version)
				return
			}
			t.Run("fixture has no chart", func(t *testing.T) { e.runFactoryMapNoChart(t, g.tag, g.fixture, g.version) })
			t.Run("played save", func(t *testing.T) { e.runFactoryMapPlayed(t, g.tag, g.version) })
		})
	}
}

// drawFactoryMapAndWait starts a drawing and waits for it to end.
func (e *gameEnv) drawFactoryMapAndWait(t *testing.T, c *Controller, id string) FactoryMapView {
	t.Helper()
	begin := time.Now()
	if err := c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatalf("draw: %v", err)
	}
	var view FactoryMapView
	waitFor(t, 5*time.Minute, "the drawing to end", func() bool {
		v, err := c.FactoryMap(id)
		if err != nil {
			t.Fatalf("view: %v", err)
		}
		view = v
		return v.State != fmDrawingState
	})
	t.Logf("TIMING draw: %s", time.Since(begin).Round(100*time.Millisecond))
	return view
}

// runFactoryMapNoChart draws the fixture, which no player ever charted: the
// run ends without a failure and keeps a drawing with no surface.
func (e *gameEnv) runFactoryMapNoChart(t *testing.T, tag, fixture, version string) {
	c, _ := e.controller(t)
	id := e.newWorld(t, tag, version, "fmap", fixture, map[string]time.Duration{"main.zip": time.Hour})
	e.fmEnsureModList(t, id)
	files := fmWorldFiles(t, e.worldDir(id))
	oneOffs := e.fmOneOffs(t)

	view := e.drawFactoryMapAndWait(t, c, id)
	if view.State == fmFailedState || view.Failure != "" {
		t.Fatalf("the drawing failed: state %q, failure %q", view.State, view.Failure)
	}
	store, err := c.FactoryMapStore(id)
	if err != nil {
		t.Fatal(err)
	}
	d, ok, err := store.Read()
	if err != nil || !ok {
		t.Fatalf("kept drawing: ok %v, err %v", ok, err)
	}
	if d.Failure != "" || len(d.Surfaces) != 0 || d.DrawnAt.IsZero() {
		t.Errorf("the drawing of an uncharted save is %+v, want a drawing with no surface and no failure", d)
	}
	if d.Basis.Save != "main.zip" {
		t.Errorf("the drawing's basis is %+v, want main.zip", d.Basis)
	}
	fmSameWorldFiles(t, files, fmWorldFiles(t, e.worldDir(id)))
	e.fmNoNewOneOffs(t, oneOffs)
}

// fmEnsureModList writes the world's mod list as Foreman does before a start,
// so the game enables the mods the world has and no others.
func (e *gameEnv) fmEnsureModList(t *testing.T, id string) {
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
}

// fmProbeLua is the probe mod. In a benchmark run of a copy of the save it
// writes the surfaces that have a charted chunk, as the game itself says it,
// and puts three known buildings on the first free spots of a charted chunk of
// nauvis. It loads before the helper mod (which depends on it), so the helper
// reads the buildings too.
const fmProbeLua = `
local function place(n, name, off)
  for c in n.get_chunks() do
    if game.forces["player"].is_chunk_charted(n, {c.x, c.y}) then
      for dx = 6, 26, 5 do
        for dy = 6, 26, 5 do
          local p = {c.x * 32 + dx + off, c.y * 32 + dy + off}
          if n.can_place_entity{name = name, position = p, force = "player"} then
            local e = n.create_entity{name = name, position = p, force = "player"}
            if e then return {name = name, type = e.type, x = p[1], y = p[2]} end
          end
        end
      end
    end
  end
end

local function run()
  if storage.done then return end
  storage.done = true
  local f = game.forces["player"]
  local names = {}
  for _, s in pairs(game.surfaces) do
    for c in s.get_chunks() do
      if f.is_chunk_charted(s, {c.x, c.y}) then names[#names + 1] = s.name; break end
    end
  end
  local n = game.surfaces["nauvis"]
  local placed = {}
  for _, spec in ipairs({{"assembling-machine-1", 0.5}, {"pipe", 0.5}, {"gun-turret", 0}}) do
    placed[#placed + 1] = place(n, spec[1], spec[2])
  end
  helpers.write_file("probe.json", helpers.table_to_json({names = names, placed = placed}), false)
end
script.on_init(run)
script.on_event(defines.events.on_tick, run)
`

// fmPlaced is a building the probe put on nauvis: its prototype name and type
// and its centre.
type fmPlaced struct {
	Name string  `json:"name"`
	Type string  `json:"type"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// fmHelperRun is what a benchmark run with the probe and the helper mod found.
type fmHelperRun struct {
	// Charted is the surfaces with a charted chunk.
	Charted []string
	// Placed is the buildings the probe put on nauvis.
	Placed []fmPlaced
	// Output is the helper mod's folder, script-output/factorymap.
	Output string
}

// runHelper runs a copy of the save in benchmark mode with the world's mods,
// the probe mod and the helper mod, as the factory map run does, and returns
// what the probe found and where the helper wrote.
func (e *gameEnv) runHelper(t *testing.T, id, tag, version string) fmHelperRun {
	t.Helper()
	save, err := os.ReadFile(filepath.Join(e.worldDir(id), world.SavesDir, "played.zip")) //nolint:gosec // the test's own world folder
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	for _, d := range []string{"mods/f11probe_1.0.0", "mods/" + factorymapmod.ModDir, "wd"} {
		if err := os.MkdirAll(filepath.Join(work, d), 0o755); err != nil { //nolint:gosec // the container's user reads and writes these
			t.Fatal(err)
		}
	}
	if err := os.Chmod(work, 0o777); err != nil { //nolint:gosec // the container's user writes here
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(work, "wd"), 0o777); err != nil { //nolint:gosec // the container's user writes here
		t.Fatal(err)
	}
	// The world's mods, and a mod list that also has the probe and the helper.
	entries, err := os.ReadDir(e.modsPath(id))
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		b, err := os.ReadFile(e.modsPath(id, en.Name())) //nolint:gosec // the test's own world folder
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, "mods", en.Name()), b, 0o644); err != nil { //nolint:gosec // the container's user reads this
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(work, "mods", world.ModListFile)) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Mods []map[string]any `json:"mods"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"f11probe", factorymapmod.ModName} {
		list.Mods = append(list.Mods, map[string]any{"name": name, "enabled": true})
	}
	if raw, err = json.Marshal(list); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		world.ModListFile: raw,
		"f11probe_1.0.0/info.json": []byte(`{"name":"f11probe","version":"1.0.0","title":"probe","author":"foreman tests",` +
			`"factorio_version":"` + tag + `","dependencies":["base"]}`),
		"f11probe_1.0.0/control.lua": []byte(fmProbeLua),
		// The helper depends on the probe so that the probe's buildings exist
		// when the helper reads.
		factorymapmod.ModDir + "/info.json": []byte(`{"name":"` + factorymapmod.ModName + `","version":"` + factorymapmod.ModVersion +
			`","title":"helper","author":"foreman tests","factorio_version":"` + tag + `","dependencies":["base","f11probe"]}`),
		factorymapmod.ModDir + "/control.lua": factorymapmod.Control,
		"../config.ini":                       []byte("[path]\nread-data=__PATH__executable__/../../data\nwrite-data=/work/wd\n"),
		"../save.zip":                         save,
	} {
		if err := os.WriteFile(filepath.Join(work, "mods", name), body, 0o644); err != nil { //nolint:gosec // the container's user reads this
			t.Fatal(err)
		}
	}
	cname := "foreman-e3test-f11probe-" + tag + "-" + e.run
	t.Cleanup(func() { _ = exec.CommandContext(context.Background(), "docker", "rm", "-f", cname).Run() }) //nolint:gosec // the test's own container
	e.dockerCLI(t, "run", "--name", cname,
		"--user", fmt.Sprintf("%d:%d", e.uid, e.gid), "--network", "none",
		"--entrypoint", "/opt/factorio/bin/x64/factorio",
		"-v", work+":/work", ImageRef(version),
		"--config", "/work/config.ini", "--mod-directory", "/work/mods",
		"--benchmark", "/work/save.zip", "--benchmark-ticks", "8", "--benchmark-runs", "1")
	e.dockerCLI(t, "rm", "-f", cname)
	out, err := os.ReadFile(filepath.Join(work, "wd", "script-output", "probe.json")) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Names  []string   `json:"names"`
		Placed []fmPlaced `json:"placed"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("probe.json: %v\n%s", err, out)
	}
	return fmHelperRun{Charted: got.Names, Placed: got.Placed, Output: filepath.Join(work, "wd", "script-output", gameenv.FactoryMapDir)}
}

// fmCheckBoxes checks the helper mod's box arithmetic (whole tiles counted
// from the chart's top-left corner, the far corner excluded). The expected
// boxes are worked out here from the buildings' centres and sizes, not from
// the game's bounding boxes.
func fmCheckBoxes(t *testing.T, r fmHelperRun) {
	t.Helper()
	var manifest struct {
		Surfaces []struct {
			Name   string `json:"name"`
			Index  int    `json:"index"`
			MinX   int    `json:"min_x"`
			MinY   int    `json:"min_y"`
			Entity string `json:"entities"`
		} `json:"surfaces"`
	}
	raw, err := os.ReadFile(filepath.Join(r.Output, "manifest.json")) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	var originX, originY int
	var file string
	for _, s := range manifest.Surfaces {
		if s.Name == "nauvis" {
			originX, originY, file = s.MinX*factorymap.ChunkPixels, s.MinY*factorymap.ChunkPixels, s.Entity
		}
	}
	if file == "" {
		t.Fatalf("the helper's manifest has no nauvis: %s", raw)
	}
	lines, err := os.ReadFile(filepath.Join(r.Output, file)) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(lines)), "\n")
	var types []string
	if err := json.Unmarshal([]byte(rows[0]), &types); err != nil {
		t.Fatalf("the first line of %s is not the type list: %v", file, err)
	}
	have := map[string]bool{}
	for _, row := range rows[1:] {
		have[strings.ReplaceAll(row, " ", "")] = true
	}
	// Half-size of each building's footprint, in tiles, from its centre.
	for _, p := range r.Placed {
		var x1, y1, x2, y2 int
		fx, fy := int(math.Floor(p.X)), int(math.Floor(p.Y))
		switch p.Name {
		case "assembling-machine-1": // 3 by 3, centre in a tile's middle
			x1, y1, x2, y2 = fx-1, fy-1, fx+2, fy+2
		case "pipe": // 1 by 1, centre in a tile's middle
			x1, y1, x2, y2 = fx, fy, fx+1, fy+1
		case "gun-turret": // 2 by 2, centre on a tile corner
			x1, y1, x2, y2 = fx-1, fy-1, fx+1, fy+1
		default:
			t.Fatalf("unexpected placed building %+v", p)
		}
		idx := -1
		for i, ty := range types {
			if ty == p.Type {
				idx = i
			}
		}
		if idx < 0 {
			t.Errorf("%s: its type %q is not in the helper's type list %v", p.Name, p.Type, types)
			continue
		}
		want := fmt.Sprintf("[%d,%d,%d,%d,%d]", idx, x1-originX, y1-originY, x2-originX, y2-originY)
		if !have[want] {
			t.Errorf("%s at (%v,%v): the helper has no line %s (chart corner %d,%d)", p.Name, p.X, p.Y, want, originX, originY)
		}
	}
}

// runFactoryMapPlayed draws a save that people played, which has charted
// chunks: every surface that has one is in the drawing, nothing else is, and
// each base picture and layer has the size the drawing says. A real factory
// has belts, production and power, so those layers are not empty somewhere.
func (e *gameEnv) runFactoryMapPlayed(t *testing.T, tag, version string) {
	key := strings.ReplaceAll(tag, ".", "_")
	path := os.Getenv("FOREMAN_TEST_CHARTED_SAVE_" + key)
	if path == "" {
		t.Skipf("set FOREMAN_TEST_CHARTED_SAVE_%s to a played Space Age save: a game with no player charts nothing", key)
	}
	save, err := os.ReadFile(path) //nolint:gosec // a save the person running the test named
	if err != nil {
		t.Fatal(err)
	}
	c, _ := e.controller(t)
	id, err := e.worlds.Create(context.Background(), fmt.Sprintf("e3test %s fmap-played %s", tag, e.run), version, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.docker.RemoveContainer(context.Background(), ContainerName(id), true) })
	if mods := os.Getenv("FOREMAN_TEST_CHARTED_MODS_" + key); mods != "" {
		entries, err := os.ReadDir(mods)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range entries {
			b, err := os.ReadFile(filepath.Join(mods, en.Name())) //nolint:gosec // a folder the person running the test named
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(e.modsPath(id, en.Name()), b, 0o600); err != nil { //nolint:gosec // the test's own world folder
				t.Fatal(err)
			}
		}
	} else {
		e.fmEnsureModList(t, id)
	}
	at := time.Now().Add(-time.Hour)
	e.putSave(t, id, "played.zip", save, at)
	helper := e.runHelper(t, id, tag, version)
	fmCheckBoxes(t, helper)
	want := helper.Charted
	if len(want) == 0 {
		t.Fatalf("the save %s has no charted chunk, so it cannot show a drawing", path)
	}
	files := fmWorldFiles(t, e.worldDir(id))
	oneOffs := e.fmOneOffs(t)

	view := e.drawFactoryMapAndWait(t, c, id)
	if view.State != fmDrawnState {
		t.Fatalf("the drawing ended in state %q (failure %q), want %q", view.State, view.Failure, fmDrawnState)
	}
	fmSameWorldFiles(t, files, fmWorldFiles(t, e.worldDir(id)))
	e.fmNoNewOneOffs(t, oneOffs)

	store, err := c.FactoryMapStore(id)
	if err != nil {
		t.Fatal(err)
	}
	d := view.Drawing
	if d.Basis.Save != "played.zip" || d.Basis.Size != int64(len(save)) || d.Basis.Modified.Sub(at).Abs() > time.Second {
		t.Errorf("the drawing's basis is %+v, want played.zip, %d bytes, modified %v", d.Basis, len(save), at)
	}
	if !strings.Contains(d.GameVersion, version) {
		t.Errorf("the drawing says game version %q, want it to hold %s", d.GameVersion, version)
	}
	got := map[string]bool{}
	nonEmpty := map[factorymap.Category]int{}
	for _, s := range d.Surfaces {
		got[s.Name] = true
		for cat, n := range e.fmCheckSurface(t, store, s) {
			if n > 0 {
				nonEmpty[cat]++
			}
		}
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("surface %q has a charted chunk but is not in the drawing", n)
		}
		delete(got, n)
	}
	for n := range got {
		t.Errorf("surface %q is in the drawing but has no charted chunk", n)
	}
	for _, cat := range []factorymap.Category{factorymap.CategoryBelts, factorymap.CategoryProduction, factorymap.CategoryPower} {
		if nonEmpty[cat] == 0 {
			t.Errorf("no surface has a drawn pixel in the %s layer", cat)
		}
	}
	t.Logf("%d surfaces; surfaces with a non-empty layer, by category: %v", len(d.Surfaces), nonEmpty)
}

// fmCheckSurface checks one surface's base picture and layers against the
// drawing's facts and returns each layer's drawn pixels.
func (e *gameEnv) fmCheckSurface(t *testing.T, store *factorymap.Store, s factorymap.Surface) map[factorymap.Category]int {
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
	out := map[factorymap.Category]int{}
	for _, cat := range factorymap.Categories {
		layer := fmReadPNG(t, store, s.Index, cat)
		if layer.Bounds() != base.Bounds() {
			t.Errorf("surface %q, layer %s: bounds %v, want the base picture's %v", s.Name, cat, layer.Bounds(), base.Bounds())
		}
		out[cat] = fmDrawnPixels(layer)
	}
	return out
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

// runfmUnsupportedState checks a 1.1 world: the map says it is not
// supported, a draw is refused, nothing is kept, no container is made, and the
// save is untouched.
func (e *gameEnv) runfmUnsupportedState(t *testing.T, tag, fixture, version string) {
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
	if view.State != fmUnsupportedState {
		t.Errorf("the map's state is %q, want %q", view.State, fmUnsupportedState)
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
