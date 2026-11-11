//go:build game

package game

// The resource count (SPEC 5.6, 14.3) against the real game images: every
// planet of each line, at 1280 and 3072, and Nauvis at 3072 with resource
// frequency and size at 600%. Each run logs its wall time, the container's
// peak memory, the patches per resource and the file's size, and stays
// within the preview limits. The spike also logs how the patches change
// with the well gap at 12 and 48 tiles, and with an ore gap of 1 and 3.
// TestRealGameD2ResourceInvariants pins what the count must always do: the
// same map gives the same patches, a resource that is off has none, the
// grouping matches a scan of every tile, and the numbers agree with the
// preview picture. Run by `make test-game`, never by `make verify`.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game/resources"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/resource"
)

// resourceSeed is the seed every count uses.
const resourceSeed = 123456

func TestRealGameD2Resources(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runResources(t, g.tag, g.version)
		})
	}
}

// countFile is resources.json as the count mod writes it.
type countFile struct {
	Planet    string                   `json:"planet"`
	Resources map[string]countResource `json:"resources"`
	Patches   []countPatch             `json:"patches"`
}

type countResource struct {
	Total    float64   `json:"total"`
	Infinite bool      `json:"infinite"`
	Normal   float64   `json:"normal"`
	Color    []float64 `json:"color"`
}

type countPatch struct {
	Name   string  `json:"name"`
	X      int     `json:"x"`
	Y      int     `json:"y"`
	Amount float64 `json:"amount"`
	N      int     `json:"n"`
	Box    [4]int  `json:"box"`
}

// parseCount reads the file and checks the contract's rules.
func parseCount(t *testing.T, b []byte, size int, planet string) countFile {
	t.Helper()
	var raw struct {
		Planet    string          `json:"planet"`
		Resources json.RawMessage `json:"resources"`
		Patches   json.RawMessage `json:"patches"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		t.Fatalf("resources.json: %v", err)
	}
	f := countFile{Planet: raw.Planet, Resources: map[string]countResource{}}
	empty := func(m json.RawMessage) bool { s := string(bytes.TrimSpace(m)); return s == "[]" || s == "{}" }
	if !empty(raw.Resources) {
		if err := json.Unmarshal(raw.Resources, &f.Resources); err != nil {
			t.Fatalf("resources: %v", err)
		}
	}
	if !empty(raw.Patches) {
		if err := json.Unmarshal(raw.Patches, &f.Patches); err != nil {
			t.Fatalf("patches: %v", err)
		}
	}
	if f.Planet != planet {
		t.Errorf("planet %q, want %q", f.Planet, planet)
	}
	whole := func(v float64) bool { return v >= 0 && v <= 1<<53 && v == math.Trunc(v) }
	sums := map[string]float64{}
	for i, p := range f.Patches {
		_, ok := f.Resources[p.Name]
		switch {
		case !ok:
			t.Errorf("patch %d: %q is not a resource", i, p.Name)
		case !whole(p.Amount) || p.N < 1:
			t.Errorf("patch %d: amount %v, n %d", i, p.Amount, p.N)
		case p.Box[0] > p.Box[2] || p.Box[1] > p.Box[3] || p.X < p.Box[0] || p.X > p.Box[2] || p.Y < p.Box[1] || p.Y > p.Box[3]:
			t.Errorf("patch %d: label (%d, %d) outside box %v", i, p.X, p.Y, p.Box)
		case p.Box[0] < -size/2 || p.Box[1] < -size/2 || p.Box[2] >= size/2 || p.Box[3] >= size/2:
			t.Errorf("patch %d: box %v outside the picture", i, p.Box)
		}
		sums[p.Name] += p.Amount
	}
	for name, r := range f.Resources {
		if !whole(r.Total) || r.Total != sums[name] {
			t.Errorf("%s: total %v, patches sum to %v", name, r.Total, sums[name])
		}
		if r.Infinite != (r.Normal > 0) || len(r.Color) != 3 {
			t.Errorf("%s: infinite %v, normal %v, color %v", name, r.Infinite, r.Normal, r.Color)
		}
		for _, c := range r.Color {
			if c < 0 || c > 255 || c != math.Trunc(c) {
				t.Errorf("%s: color %v", name, r.Color)
			}
		}
	}
	return f
}

// peakMemory follows the preview containers of this run while it lasts, and
// returns a function that stops and gives the highest memory.peak of their
// cgroups, in bytes, read every 100 ms until each container goes.
func (e *gameEnv) peakMemory() func() int64 {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var peak int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			list, _ := e.docker.ListContainers(ctx, true, []string{gameenv.LabelPreview + "=" + e.data})
			for _, s := range list {
				b, err := os.ReadFile(filepath.Join("/sys/fs/cgroup/system.slice", "docker-"+s.ID+".scope", "memory.peak"))
				if err != nil {
					continue
				}
				if v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
					mu.Lock()
					peak = max(peak, v)
					mu.Unlock()
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return func() int64 {
		cancel()
		<-done
		mu.Lock()
		defer mu.Unlock()
		return peak
	}
}

// countRun is one measured count.
type countRun struct {
	file  countFile
	bytes int
	wall  time.Duration
	peak  int64
}

// summary is a log line's patches per resource, with the fragments (patches
// of fewer than 10 entities) in brackets.
func (r countRun) summary() string {
	n, small := map[string]int{}, map[string]int{}
	for _, p := range r.file.Patches {
		n[p.Name]++
		if p.N < 10 {
			small[p.Name]++
		}
	}
	names := make([]string, 0, len(n))
	for name := range n {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d [%d]", name, n[name], small[name]))
	}
	return strings.Join(parts, ", ")
}

// countParams is the count mod's params line with extra Lua fields, so a
// gap can be changed for the spike.
func countParams(extra string) string {
	return `printf 'return { radius = %d, planet = "%s"` + extra + ` }\n' "$` + gameenv.ResourceRadius + `" "$` + gameenv.ResourcePlanet + `" > ` +
		modFile(resources.ModDir, resources.ParamsFile)
}

// measure runs m for req and planet, and returns the file, the wall time
// and the container's peak memory, whatever the result.
func (e *gameEnv) measure(t *testing.T, c *Controller, req PreviewRequest, planet string, m controlMod) ([]byte, time.Duration, int64, error) {
	t.Helper()
	line, err := checkResources(req, planet)
	if err != nil {
		t.Fatal(err)
	}
	stop := e.peakMemory()
	begin := time.Now()
	b, err := c.runControlMod(context.Background(), req, m, resourceInfo(line, req.SpaceAge), resourcesEnv(req, planet))
	wall := time.Since(begin)
	peak := stop()
	e.previewLeftovers(t, "after the count")
	return b, wall, peak, err
}

// countWith runs the count mod for req and planet with extra params and
// measures it.
func (e *gameEnv) countWith(t *testing.T, c *Controller, req PreviewRequest, planet, extraParams string) countRun {
	t.Helper()
	m := resourceMod
	m.params = countParams(extraParams)
	b, wall, peak, err := e.measure(t, c, req, planet, m)
	if err != nil {
		t.Fatalf("%s %s at %d: %v (after %s, peak %d MiB)", req.GameVersion, planet, req.size(), err, wall.Round(100*time.Millisecond), peak>>20)
	}
	return countRun{file: parseCount(t, b, req.size(), planet), bytes: len(b), wall: wall, peak: peak}
}

// stageLua is a test-only stand-in for the count mod that only generates
// the chunks and, with scan set, finds the resource entities chunk by chunk
// and counts them, so the game's own share of a count's time and memory is
// seen. It writes {"tiles": n}.
const stageLua = `local params = require("params")
script.on_init(function()
  local h = helpers or game
  local s = game.surfaces[params.planet]
  if not s then
    local p = game.planets[params.planet]
    s = p.surface or p.create_surface()
  end
  s.request_to_generate_chunks({ 0, 0 }, params.radius)
  s.force_generate_chunk_requests()
  local n = 0
  if params.scan then
    local r = params.radius
    for cx = -r, r - 1 do
      for cy = -r, r - 1 do
        n = n + #s.find_entities_filtered({ area = { { cx * 32, cy * 32 }, { cx * 32 + 32, cy * 32 + 32 } }, type = "resource" })
      end
    end
  end
  h.write_file("resources.json", '{"tiles": ' .. n .. '}', false)
end)
`

// stage runs stageLua and logs its measurements.
func (e *gameEnv) stage(t *testing.T, c *Controller, req PreviewRequest, planet string, scan bool, label string) {
	t.Helper()
	m := resourceMod
	m.control = []byte(stageLua)
	m.params = countParams(map[bool]string{true: ", scan = true", false: ""}[scan])
	b, wall, peak, err := e.measure(t, c, req, planet, m)
	t.Logf("STAGE %s %s %d%s, scan %v: wall %s, peak %d MiB, %s, error %v",
		req.GameVersion, planet, req.size(), label, scan, wall.Round(100*time.Millisecond), peak>>20, bytes.TrimSpace(b), err)
}

// count runs the count as Foreman does and logs its measurements.
func (e *gameEnv) count(t *testing.T, c *Controller, req PreviewRequest, planet, label string) countRun {
	t.Helper()
	r := e.countWith(t, c, req, planet, "")
	dumpCount(t, fmt.Sprintf("%s-%s-%d%s", req.GameVersion, planet, req.size(), strings.ReplaceAll(label, " ", "-")), r.file)
	t.Logf("MEASURE %s %s %d%s: wall %s, peak %d MiB, file %d bytes, %d patches: %s",
		req.GameVersion, planet, req.size(), label, r.wall.Round(100*time.Millisecond), r.peak>>20, r.bytes, len(r.file.Patches), r.summary())
	// Foreman's own path gives the same patches and totals, checked by
	// resource.Parse.
	res, err := c.Resources(context.Background(), req, planet)
	if err != nil {
		t.Fatalf("Resources: %v", err)
	}
	if len(res.Patches) != len(r.file.Patches) || len(res.Resources) != len(r.file.Resources) {
		t.Errorf("Resources gave %d patches of %d resources, the file %d of %d",
			len(res.Patches), len(res.Resources), len(r.file.Patches), len(r.file.Resources))
	}
	var colors []string
	for name, got := range res.Resources {
		f := r.file.Resources[name]
		// Iron ore's map colour is the same on every line; a colour scaled
		// twice is refused by Parse, and one never scaled would not match.
		if got.Total != int64(f.Total) || name == "iron-ore" && got.Color != [3]uint8{105, 133, 147} {
			t.Errorf("%s: total %d (file %v), color %v", name, got.Total, f.Total, got.Color)
		}
		colors = append(colors, fmt.Sprintf("%s %v", name, got.Color))
	}
	slices.Sort(colors)
	t.Logf("COLORS %s %s: %s", req.GameVersion, planet, strings.Join(colors, ", "))
	return r
}

// dumpCount writes f, re-encoded so field order and number form do not
// matter, to $FOREMAN_RESOURCE_DUMP/<name>.json when that is set, so two
// versions of the count mod can be compared run by run.
func dumpCount(t *testing.T, name string, f countFile) {
	t.Helper()
	dir := os.Getenv("FOREMAN_RESOURCE_DUMP")
	if dir == "" {
		return
	}
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // the test's own dump folder, named by whoever runs it
	if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(name, "%", "")+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// richMapGen is the line's default map-gen settings with every resource
// control of Nauvis at frequency and size f.
func richMapGen(t *testing.T, line string, spaceAge bool, f float64) []byte {
	t.Helper()
	b := mapgen.DefaultMapGen(line)
	if spaceAge {
		b = spaceAgeMapGen(t, line, nil)
	}
	var base map[string]any
	if err := json.Unmarshal(b, &base); err != nil {
		t.Fatal(err)
	}
	controls := base["autoplace_controls"].(map[string]any)
	for _, ctl := range mapgen.Controls(line, "nauvis") {
		if ctl.Category == "resource" {
			controls[ctl.Name] = map[string]any{"frequency": f, "size": f, "richness": 1.0}
		}
	}
	out, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *gameEnv) runResources(t *testing.T, line, version string) {
	c, _ := e.controller(t)
	// The spike measures past the limit, so a run that goes over is seen,
	// and fails, instead of being cut off.
	c.previewLimit = 3 * time.Minute
	spaceAge := line != "1.1"
	within := func(t *testing.T, r countRun) {
		t.Helper()
		if r.wall > PreviewLimit || r.peak > previewMemory {
			t.Errorf("over the preview limits: %s, %d MiB", r.wall.Round(100*time.Millisecond), r.peak>>20)
		}
	}
	for _, planet := range mapgen.Planets(line, spaceAge) {
		t.Run(planet, func(t *testing.T) {
			var big countRun
			for _, size := range []int{1280, 3072} {
				req := PreviewRequest{GameVersion: version, SpaceAge: spaceAge, Seed: resourceSeed, Size: size}
				r := e.count(t, c, req, planet, "")
				within(t, r)
				if size == 3072 {
					big = r
				}
			}
			// Every resource the planet's map shows is found at 3072.
			for _, ctl := range mapgen.Controls(line, planet) {
				if ctl.Category == "resource" && !hasResourceFor(big.file, planet, ctl.Name) {
					t.Errorf("no patch of %s at 3072", ctl.Name)
				}
			}
		})
	}
	t.Run("nauvis 3072 at 600%", func(t *testing.T) {
		req := PreviewRequest{GameVersion: version, SpaceAge: spaceAge, Seed: resourceSeed, Size: 3072}
		req.MapGenSettings = richMapGen(t, line, spaceAge, 6)
		// The worst case may go over the limits; that fails safe with the
		// out-of-memory or timeout error, so it is logged, not failed.
		m := resourceMod
		b, wall, peak, err := e.measure(t, c, req, "nauvis", m)
		if err != nil {
			t.Logf("MEASURE %s nauvis 3072 at 600%%: wall %s, peak %d MiB, error %v", version, wall.Round(100*time.Millisecond), peak>>20, err)
		} else {
			r := countRun{file: parseCount(t, b, 3072, "nauvis"), bytes: len(b), wall: wall, peak: peak}
			dumpCount(t, version+"-nauvis-3072-at-600", r.file)
			t.Logf("MEASURE %s nauvis 3072 at 600%%: wall %s, peak %d MiB, file %d bytes, %d patches: %s",
				version, wall.Round(100*time.Millisecond), peak>>20, r.bytes, len(r.file.Patches), r.summary())
		}
		// The game's own share: generating only, then generating and finding
		// the entities, at 600% and at the defaults.
		e.stage(t, c, req, "nauvis", false, " at 600%")
		e.stage(t, c, req, "nauvis", true, " at 600%")
		req.MapGenSettings = nil
		e.stage(t, c, req, "nauvis", false, "")
		e.stage(t, c, req, "nauvis", true, "")
	})
}

// clusters is the number of groups of points joined when their distance on
// both axes is at most d.
func clusters(ps [][2]int, d int) int {
	parent := make([]int, len(ps))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	n := len(ps)
	for i := range ps {
		for j := i + 1; j < len(ps); j++ {
			if abs(ps[i][0]-ps[j][0]) <= d && abs(ps[i][1]-ps[j][1]) <= d {
				if a, b := find(i), find(j); a != b {
					parent[a] = b
					n--
				}
			}
		}
	}
	return n
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// wellNames are the resources whose product is a fluid. Lithium brine is
// the only one of them that is finite (SPEC 5.6).
var wellNames = map[string]bool{"crude-oil": true, "sulfuric-acid-geyser": true, "fluorine-vent": true, "lithium-brine": true}

// tilesLua is a test-only stand-in for the count mod that writes every
// resource entity once per tile, as x, y, amount, with the flags of its
// prototype, so the test can group the tiles itself. It keeps only the
// tiles inside the picture, as the count does.
const tilesLua = `local params = require("params")
script.on_init(function()
  local h = helpers or game
  local s = game.surfaces[params.planet]
  if not s then
    local p = game.planets[params.planet]
    s = p.surface or p.create_surface()
  end
  s.request_to_generate_chunks({ 0, 0 }, params.radius)
  s.force_generate_chunk_requests()
  local out, seen = {}, {}
  local r = params.radius
  local half = r * 32
  for cx = -r, r - 1 do
    for cy = -r, r - 1 do
      for _, e in pairs(s.find_entities_filtered({ area = { { cx * 32, cy * 32 }, { cx * 32 + 32, cy * 32 + 32 } }, type = "resource" })) do
        local name = e.name
        local o = out[name]
        if not o then
          local pr = e.prototype
          local fluid = false
          for _, p in pairs(pr.mineable_properties.products or {}) do
            if p.type == "fluid" then fluid = true end
          end
          o = { fluid = fluid, infinite = pr.infinite_resource and true or false, normal = pr.normal_resource_amount or 0, t = {} }
          out[name] = o
          seen[name] = {}
        end
        local x, y = math.floor(e.position.x), math.floor(e.position.y)
        local k = (x + 8192) * 16384 + (y + 8192)
        if x >= -half and x < half and y >= -half and y < half and not seen[name][k] then
          seen[name][k] = true
          local t = o.t
          t[#t + 1] = x
          t[#t + 1] = y
          t[#t + 1] = e.amount
        end
      end
    end
  end
  h.write_file("resources.json", h.table_to_json({ resources = out }), false)
end)
`

// scanned is one resource as tilesLua wrote it.
type scanned struct {
	Fluid, Infinite bool
	Normal          float64
	T               []float64
}

// tile is one resource entity's tile and amount.
type tile struct {
	x, y   int
	amount int64
}

func (s scanned) tiles() []tile {
	out := make([]tile, 0, len(s.T)/3)
	for i := 0; i+2 < len(s.T); i += 3 {
		out = append(out, tile{int(s.T[i]), int(s.T[i+1]), int64(s.T[i+2])})
	}
	return out
}

// scan makes the test's own tile dump of planet for req.
func (e *gameEnv) scan(t *testing.T, c *Controller, req PreviewRequest, planet string) map[string]scanned {
	t.Helper()
	m := resourceMod
	m.control = []byte(tilesLua)
	m.params = countParams("")
	m.maxOutput = 64 << 20
	b, wall, peak, err := e.measure(t, c, req, planet, m)
	if err != nil {
		t.Fatalf("scan of %s: %v", planet, err)
	}
	var f struct {
		Resources map[string]scanned `json:"resources"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("scan of %s: %v", planet, err)
	}
	t.Logf("SCAN %s %s %d: wall %s, peak %d MiB, %d bytes", req.GameVersion, planet, req.size(), wall.Round(100*time.Millisecond), peak>>20, len(b))
	return f.Resources
}

// component is one group of tiles joined by components.
type component struct {
	n      int
	amount int64
	box    [4]int
	label  map[[2]int]bool
}

// components groups tiles that are within gap on both axes of each other,
// by looking up every position in reach, so it shares no code with the
// count mod's pairwise union.
func components(ts []tile, gap int) []component {
	at := make(map[[2]int]int, len(ts))
	for i, p := range ts {
		at[[2]int{p.x, p.y}] = i
	}
	parent := make([]int, len(ts))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i, p := range ts {
		for dx := -gap; dx <= gap; dx++ {
			for dy := -gap; dy <= gap; dy++ {
				if j, ok := at[[2]int{p.x + dx, p.y + dy}]; ok {
					parent[find(i)] = find(j)
				}
			}
		}
	}
	byRoot := map[int]*component{}
	var out []*component
	for i, p := range ts {
		r := find(i)
		c := byRoot[r]
		if c == nil {
			c = &component{box: [4]int{p.x, p.y, p.x, p.y}, label: map[[2]int]bool{}}
			byRoot[r] = c
			out = append(out, c)
		}
		c.n++
		c.amount += p.amount
		c.box = [4]int{min(c.box[0], p.x), min(c.box[1], p.y), max(c.box[2], p.x), max(c.box[3], p.y)}
		c.label[[2]int{p.x, p.y}] = true
	}
	list := make([]component, len(out))
	for i, c := range out {
		list[i] = *c
	}
	return list
}

// patchKey is what two groupings of a resource must agree on for a patch.
func patchKey(n int, amount int64, box [4]int) string { return fmt.Sprint(n, "/", amount, "/", box) }

// checkGrouping compares the count's patches with the scan's tiles: the
// totals, the patch count against clusters, and every patch's entities,
// amount and box against the test's own grouping.
func checkGrouping(t *testing.T, planet string, res resource.Result, scan map[string]scanned) {
	t.Helper()
	for name := range res.Resources {
		if _, ok := scan[name]; !ok {
			t.Errorf("%s: counted but the scan found none", name)
		}
	}
	for name, s := range scan {
		ts := s.tiles()
		if len(ts) == 0 {
			continue
		}
		gap := statedGap(planet, name)
		var got []string
		var entities int
		var amount int64
		for _, p := range res.Patches {
			if p.Name == name {
				got = append(got, patchKey(p.Entities, p.Amount, p.Box))
				entities += p.Entities
				amount += p.Amount
			}
		}
		var sum int64
		points := make([][2]int, len(ts))
		for i, p := range ts {
			sum += p.amount
			points[i] = [2]int{p.x, p.y}
		}
		if entities != len(ts) || amount != sum {
			t.Errorf("%s: patches hold %d entities and %d, the scan found %d tiles and %d", name, entities, amount, len(ts), sum)
		}
		if n := clusters(points, gap); n != len(got) {
			t.Errorf("%s: %d patches, but %d groups of tiles within %d of each other", name, len(got), n, gap)
		}
		comps := components(ts, gap)
		var want []string
		for _, c := range comps {
			want = append(want, patchKey(c.n, c.amount, c.box))
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: patches (entities/amount/box) differ from the scan's groups: %d patches, %d groups", name, len(got), len(want))
		}
		// The label tile is a tile of the patch whose box it lies in.
		bad := 0
		for _, p := range res.Patches {
			if p.Name != name {
				continue
			}
			ok := false
			for _, c := range comps {
				if c.box == p.Box && c.label[[2]int{p.X, p.Y}] {
					ok = true
				}
			}
			if !ok {
				bad++
				if bad == 1 {
					t.Errorf("%s: label (%d, %d) is not a tile of the patch with box %v", name, p.X, p.Y, p.Box)
				}
			}
		}
		if bad > 1 {
			t.Errorf("%s: %d labels in all are not on their patch", name, bad)
		}
		t.Logf("GROUPING %s: %d tiles, %d patches", name, len(ts), len(got))
	}
}

// hasResourceFor reports whether the file has the resource entity the
// control sets.
func hasResourceFor(f countFile, planet, control string) bool {
	_, ok := f.Resources[resource.EntityName(planet, control)]
	return ok
}

// resourceFor is the resource entity a planet's control makes, matched by
// the name the map generator control shows.
func resourceFor(names []string, planet, control string) (string, bool) {
	for _, n := range names {
		if n == resource.EntityName(planet, control) {
			return n, true
		}
	}
	return "", false
}

// resourceNames are the names of the resources a result holds, sorted.
func resourceNames(res resource.Result) []string {
	names := make([]string, 0, len(res.Resources))
	for n := range res.Resources {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// resourceControlSettings is the line's map-gen settings with each resource
// control of planet set to the entry edit returns for it.
func resourceControlSettings(t *testing.T, line string, spaceAge bool, planet string, edit func(mapgen.Control) map[string]any) []byte {
	t.Helper()
	b := mapgen.DefaultMapGen(line)
	if spaceAge {
		b = spaceAgeMapGen(t, line, nil)
	}
	var base map[string]any
	if err := json.Unmarshal(b, &base); err != nil {
		t.Fatal(err)
	}
	controls := base["autoplace_controls"].(map[string]any)
	for _, ctl := range mapgen.Controls(line, planet) {
		if ctl.Category == "resource" {
			controls[ctl.Name] = edit(ctl)
		}
	}
	out, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// countOf is Foreman's own count, failing the test on an error and checking
// that no container or folder is left behind.
func (e *gameEnv) countOf(t *testing.T, c *Controller, req PreviewRequest, planet string) resource.Result {
	t.Helper()
	res, err := c.Resources(context.Background(), req, planet)
	if err != nil {
		t.Fatalf("%s %s at %d: %v", req.GameVersion, planet, req.size(), err)
	}
	e.previewLeftovers(t, "after a count")
	return res
}

func TestRealGameD2ResourceInvariants(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			c, _ := e.controller(t)
			spaceAge := g.tag != "1.1"
			for _, planet := range mapgen.Planets(g.tag, spaceAge) {
				t.Run(planet, func(t *testing.T) { e.planetInvariants(t, c, g.tag, g.version, spaceAge, planet) })
			}
			t.Run("cancelled count", func(t *testing.T) { e.cancelledCount(t, c, g.version, spaceAge) })
		})
	}
}

// planetInvariants checks one planet's count at 1280, with the runs shared
// between the checks, then at 3072.
func (e *gameEnv) planetInvariants(t *testing.T, c *Controller, line, version string, spaceAge bool, planet string) {
	req := PreviewRequest{GameVersion: version, SpaceAge: spaceAge, Seed: resourceSeed, Size: 1280}
	begin := time.Now()
	base := e.countOf(t, c, req, planet)
	t.Logf("TIMING %s %s 1280 count: %s, %d patches of %v", version, planet, time.Since(begin).Round(100*time.Millisecond), len(base.Patches), resourceNames(base))
	if len(base.Patches) == 0 {
		t.Fatalf("%s has no patches at 1280", planet)
	}
	scan := e.scan(t, c, req, planet)

	t.Run("same request, same patches", func(t *testing.T) {
		if again := e.countOf(t, c, req, planet); !reflect.DeepEqual(again, base) {
			t.Errorf("a second count differs: %d patches, then %d", len(base.Patches), len(again.Patches))
		}
	})
	t.Run("another seed, other patches", func(t *testing.T) {
		other := req
		other.Seed = resourceSeed*7919 + 1
		res := e.countOf(t, c, other, planet)
		same := 0
		for _, p := range base.Patches {
			if slices.ContainsFunc(res.Patches, func(q resource.Patch) bool { return q.Name == p.Name && q.X == p.X && q.Y == p.Y }) {
				same++
			}
		}
		t.Logf("SEED %s %s: %d of %d label tiles are the same under the next seed", version, planet, same, len(base.Patches))
		if reflect.DeepEqual(res.Patches, base.Patches) || same*2 > len(base.Patches) {
			t.Errorf("the next seed gave %d patches, %d at the same label tiles", len(res.Patches), same)
		}
	})
	t.Run("grouping matches a scan of every tile", func(t *testing.T) { checkGrouping(t, planet, base, scan) })
	t.Run("wells and ores", func(t *testing.T) { checkKinds(t, base, scan) })
	t.Run("a resource that is off has no patches", func(t *testing.T) { e.offInvariant(t, c, line, req, planet, base) })
	t.Run("richness 200% doubles the ore", func(t *testing.T) { e.richnessInvariant(t, c, line, req, planet, base) })
	t.Run("labels sit on the picture's colours", func(t *testing.T) { e.alignmentInvariant(t, c, req, planet, base) })
	t.Run("3072 within the limits, every resource found", func(t *testing.T) { e.largestInvariant(t, c, line, req, planet) })
	if planet == "fulgora" {
		t.Run("high-frequency scrap gives few patches", func(t *testing.T) { e.scrapInvariant(t, c, line, req) })
	}
}

// checkKinds checks that wells and ores come back in their own shape: the
// resources whose product is a fluid, and only those, are wells; an
// infinite one has a normal amount to measure its yield against, and
// lithium brine is finite and shows an amount (SPEC 5.6).
func checkKinds(t *testing.T, res resource.Result, scan map[string]scanned) {
	t.Helper()
	for name, r := range res.Resources {
		s := scan[name]
		well := wellNames[name]
		if s.Fluid != well {
			t.Errorf("%s: the game says fluid %v, the test expects %v", name, s.Fluid, well)
		}
		wantInfinite := well && name != "lithium-brine"
		if r.Infinite != wantInfinite || r.Infinite != (r.Normal > 0) || float64(r.Normal) != s.Normal && r.Infinite {
			t.Errorf("%s: infinite %v, normal %d (game: infinite %v, normal %v), want infinite %v", name, r.Infinite, r.Normal, s.Infinite, s.Normal, wantInfinite)
		}
		if r.Total <= 0 {
			t.Errorf("%s: total %d", name, r.Total)
		}
	}
	for _, p := range res.Patches {
		pct, ok := resource.Yield(p)
		want := res.Resources[p.Name].Infinite
		if ok != want || ok && pct != 100*float64(p.Amount)/float64(res.Resources[p.Name].Normal) {
			t.Errorf("%s patch at (%d, %d): yield %v, shown %v, want shown %v", p.Name, p.X, p.Y, pct, ok, want)
		}
		if !ok && p.Amount <= 0 {
			t.Errorf("%s patch at (%d, %d) shows no amount", p.Name, p.X, p.Y)
		}
	}
}

// offInvariant switches off every other resource control of the planet that
// has patches and checks the count: those have none, the rest still do.
func (e *gameEnv) offInvariant(t *testing.T, c *Controller, line string, req PreviewRequest, planet string, base resource.Result) {
	off := map[string]bool{}
	var found []string
	for _, ctl := range mapgen.Controls(line, planet) {
		if name, ok := resourceFor(resourceNames(base), planet, ctl.Name); ok && ctl.Category == "resource" {
			found = append(found, name)
			if len(found)%2 == 1 {
				off[ctl.Name] = true
			}
		}
	}
	req.MapGenSettings = resourceControlSettings(t, line, req.SpaceAge, planet, func(ctl mapgen.Control) map[string]any {
		entry := map[string]any{"frequency": 1.0, "size": 1.0, "richness": 1.0}
		if off[ctl.Name] {
			entry["size"] = 0.0
		}
		return entry
	})
	res := e.countOf(t, c, req, planet)
	for i, name := range found {
		got := res.Resources[name].Total
		patches := 0
		for _, p := range res.Patches {
			if p.Name == name {
				patches++
			}
		}
		if i%2 == 0 && (patches != 0 || got != 0) {
			t.Errorf("%s is off, and has %d patches and %d in total", name, patches, got)
		}
		if i%2 == 1 && patches == 0 {
			t.Errorf("%s is on, and has no patches", name)
		}
	}
	t.Logf("OFF %s %s: off %v, found %v", req.GameVersion, planet, off, resourceNames(res))
}

// The richness band is how far a resource's total may be from twice the
// default map's. Measured on every planet of the three lines, the ratios
// were 1.9999 to 2.0004, since richness scales the amount of the same tiles;
// the band leaves room for rounding. Logged as RICHNESS.
const (
	richnessLow  = 1.99
	richnessHigh = 2.01
)

// richnessInvariant sets every resource's richness to 200% and checks that
// the totals are about twice the default map's, on the same tiles.
func (e *gameEnv) richnessInvariant(t *testing.T, c *Controller, line string, req PreviewRequest, planet string, base resource.Result) {
	req.MapGenSettings = resourceControlSettings(t, line, req.SpaceAge, planet, func(mapgen.Control) map[string]any {
		return map[string]any{"frequency": 1.0, "size": 1.0, "richness": 2.0}
	})
	res := e.countOf(t, c, req, planet)
	for name, b := range base.Resources {
		r, ok := res.Resources[name]
		if !ok {
			t.Errorf("%s has no patches at richness 200%%", name)
			continue
		}
		ratio := float64(r.Total) / float64(b.Total)
		entities := func(x resource.Result) (n int) {
			for _, p := range x.Patches {
				if p.Name == name {
					n += p.Entities
				}
			}
			return n
		}
		t.Logf("RICHNESS %s %s %s: total %d then %d, ratio %.4f, entities %d then %d", req.GameVersion, planet, name, b.Total, r.Total, ratio, entities(base), entities(res))
		if ratio < richnessLow || ratio > richnessHigh {
			t.Errorf("%s: %d at 100%%, %d at 200%%, ratio %.4f outside %.2f to %.2f", name, b.Total, r.Total, ratio, richnessLow, richnessHigh)
		}
		// On 1.1 a few tiles at a patch's edge gain enough to appear (2 of
		// 4,209 iron ore tiles); 2.x keeps them all.
		if abs(entities(base)-entities(res))*1000 > entities(base) {
			t.Errorf("%s: richness changed the number of entities, %d to %d", name, entities(base), entities(res))
		}
	}
}

// statedGap is the gap SPEC 5.6 and ADR 0015 state for a resource: 24 tiles
// for wells, 16 for Fulgora's scrap, 2 for every other ore.
func statedGap(planet, name string) int {
	switch {
	case wellNames[name]:
		return 24
	case planet == "fulgora" && name == "scrap":
		return 16
	}
	return 2
}

// scrapInvariant counts Fulgora at 3072 with scrap at the highest frequency
// step, on two seeds. At a gap of 2 they gave 2,234 and 2,436 scrap
// patches, at 12 352 and 369, and at 16 296 and 303 (ADR 0015).
func (e *gameEnv) scrapInvariant(t *testing.T, c *Controller, line string, req PreviewRequest) {
	req.Size = PreviewSizeMax
	req.MapGenSettings = resourceControlSettings(t, line, true, "fulgora", func(ctl mapgen.Control) map[string]any {
		f := 1.0
		if ctl.Name == "scrap" {
			f = 6
		}
		return map[string]any{"frequency": f, "size": 1.0, "richness": 1.0}
	})
	for _, seed := range []uint32{resourceSeed, resourceSeed*7919 + 1} {
		req.Seed = seed
		r := e.countWith(t, c, req, "fulgora", "")
		scrap := 0
		for _, p := range r.file.Patches {
			if p.Name == "scrap" {
				scrap++
			}
		}
		t.Logf("SCRAP %s seed %d: wall %s, peak %d MiB, %d scrap patches", req.GameVersion, seed, r.wall.Round(100*time.Millisecond), r.peak>>20, scrap)
		if r.wall > PreviewLimit || r.peak > previewMemory {
			t.Errorf("seed %d over the limits: %s, %d MiB", seed, r.wall.Round(100*time.Millisecond), r.peak>>20)
		}
		if scrap == 0 || scrap > 320 {
			t.Errorf("seed %d: %d scrap patches, want 1 to 320", seed, scrap)
		}
	}
}

// largestInvariant counts at PreviewSizeMax with the real limits, logs the
// peaks, and checks that every resource control of the planet has a patch.
func (e *gameEnv) largestInvariant(t *testing.T, c *Controller, line string, req PreviewRequest, planet string) {
	req.Size = PreviewSizeMax
	stop := e.peakMemory()
	begin := time.Now()
	res, err := c.Resources(context.Background(), req, planet)
	wall := time.Since(begin)
	peak := stop()
	t.Logf("PEAK %s %s %d: wall %s, peak %d MiB, %d patches of %v", req.GameVersion, planet, req.size(), wall.Round(100*time.Millisecond), peak>>20, len(res.Patches), resourceNames(res))
	if err != nil {
		t.Fatalf("%s at %d: %v", planet, req.size(), err)
	}
	e.previewLeftovers(t, "after the largest count")
	if wall > PreviewLimit || peak > previewMemory {
		t.Errorf("over the limits: %s, %d MiB", wall.Round(100*time.Millisecond), peak>>20)
	}
	for _, ctl := range mapgen.Controls(line, planet) {
		if _, ok := resourceFor(resourceNames(res), planet, ctl.Name); ctl.Category == "resource" && !ok {
			t.Errorf("no patch of %s at %d", ctl.Name, req.size())
		}
	}
}

// pixel is the picture's colour at tile (x, y) of a picture of size tiles.
func pixel(img image.Image, size, x, y int) [3]int {
	px, py := x+size/2, y+size/2
	if px < 0 || py < 0 || px >= size || py >= size {
		return [3]int{-1000, -1000, -1000}
	}
	r, g, b, _ := img.At(px, py).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}

// near reports whether a picture colour is within a few levels of a
// resource's map colour.
func near(p [3]int, c [3]uint8) bool {
	return abs(p[0]-int(c[0]))+abs(p[1]-int(c[1]))+abs(p[2]-int(c[2])) <= alignTolerance
}

const (
	// alignTolerance is how far, summed over the three channels, a pixel
	// may be from the map colour: the game paints it exactly.
	alignTolerance = 6
	// alignLarge is the fewest entities a patch needs to be checked.
	alignLarge = 100
	// alignRadius is the window around the label tile: the game paints an
	// ore as blocks of 2 by 2 pixels, so a single pixel may miss.
	alignRadius = 2
	// alignHits is how many pixels of that window must have the colour.
	alignHits = 2
	// alignDrawn is the fewest pixels of its colour a drawn resource has in
	// its patches' boxes, as a share of its entities: 1 in this many. The
	// ores measured 0.38 to 0.51, scrap 0.25.
	alignDrawn = 3
)

// hits is how many pixels of the window around (x, y) have the colour.
func hits(img image.Image, size, x, y int, c [3]uint8) int {
	n := 0
	for dx := -alignRadius; dx <= alignRadius; dx++ {
		for dy := -alignRadius; dy <= alignRadius; dy++ {
			if near(pixel(img, size, x+dx, y+dy), c) {
				n++
			}
		}
	}
	return n
}

// drawnDistinctly reports whether the picture shows the resource as its map
// colour, and shows that colour nowhere else, so that finding it at a label
// means the overlay is in the right place and not that the terrain looks
// alike. The preview draws scrap on Fulgora only for a quarter of its
// tiles, so a label may fall on a tile it left blank; a resource drawn for
// less than a third of its tiles cannot be checked.
func drawnDistinctly(t *testing.T, planet string, img image.Image, size int, name string, c [3]uint8, res resource.Result) bool {
	inside := func(x, y int) bool {
		for _, p := range res.Patches {
			if p.Name == name && x >= p.Box[0] && x <= p.Box[2] && y >= p.Box[1] && y <= p.Box[3] {
				return true
			}
		}
		return false
	}
	var in, out, entities int
	for _, p := range res.Patches {
		if p.Name == name {
			entities += p.Entities
		}
	}
	for y := -size / 2; y < size/2; y++ {
		for x := -size / 2; x < size/2; x++ {
			if near(pixel(img, size, x, y), c) {
				if inside(x, y) {
					in++
				} else {
					out++
				}
			}
		}
	}
	t.Logf("DRAWN %s %s: %d pixels of its colour in its patches' boxes for %d entities, %d elsewhere", planet, name, in, entities, out)
	return in*alignDrawn >= entities && out*100 < size*size
}

// alignmentInvariant draws the preview of the same map and checks that the
// large ore patches' label tiles lie on pixels of their resource's map
// colour, and that the picture turned over or on its side would not. Wells
// are left out: the preview draws only some of Aquilo's crude oil wells.
func (e *gameEnv) alignmentInvariant(t *testing.T, c *Controller, req PreviewRequest, planet string, base resource.Result) {
	req.Planets = []string{planet}
	pngs, err := c.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("preview of %s: %v", planet, err)
	}
	img, err := png.Decode(bytes.NewReader(pngs[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	size := req.size()
	names := map[string]string{"identity": "", "flip x": "", "flip y": "", "both": "", "transpose": ""}
	pass := map[string]int{}
	tested := 0
	low, high := 25, 0
	skipped, distinct := map[string]bool{}, map[string]bool{}
	for _, p := range base.Patches {
		r := base.Resources[p.Name]
		if p.Entities < alignLarge || wellNames[p.Name] {
			continue
		}
		if _, ok := distinct[p.Name]; !ok {
			distinct[p.Name] = drawnDistinctly(t, planet, img, size, p.Name, r.Color, base)
		}
		if !distinct[p.Name] {
			skipped[p.Name] = true
			continue
		}
		tested++
		got := hits(img, size, p.X, p.Y, r.Color)
		low = min(low, got)
		if got < alignHits {
			t.Logf("MISS %s (%d, %d) n=%d: %d hits, label pixel %v, colour %v", p.Name, p.X, p.Y, p.Entities, got, pixel(img, size, p.X, p.Y), r.Color)
		}
		for variant := range names {
			x, y := p.X, p.Y
			switch variant {
			case "flip x":
				x = -x - 1
			case "flip y":
				y = -y - 1
			case "both":
				x, y = -x-1, -y-1
			case "transpose":
				x, y = y, x
			}
			n := hits(img, size, x, y, r.Color)
			if n >= alignHits {
				pass[variant]++
			}
			if variant != "identity" && n > high {
				high = n
			}
		}
	}
	t.Logf("ALIGN %s %s: %d large patches checked (resources not drawn or not distinctive: %v); label windows with the colour: identity %d, flip x %d, flip y %d, both %d, transpose %d; fewest hits at a label %d, most in a turned picture %d",
		req.GameVersion, planet, tested, slices.Sorted(maps.Keys(skipped)), pass["identity"], pass["flip x"], pass["flip y"], pass["both"], pass["transpose"], low, high)
	if tested < 3 {
		t.Skipf("the preview draws too little of %s to check: %d large patches of a drawn, distinctive colour", planet, tested)
	}
	if pass["identity"]*10 < tested*9 {
		t.Errorf("%d of %d large patches have their colour at the label tile", pass["identity"], tested)
	}
	for _, v := range []string{"flip x", "flip y", "both", "transpose"} {
		if pass[v]*3 > tested {
			t.Errorf("the picture %s would also pass for %d of %d patches: the check cannot tell it", v, pass[v], tested)
		}
	}
}

// cancelledCount cancels a count while its container runs and checks that
// the call ends with the caller's error, nothing is left behind, and the
// slot is free again (SPEC 14.3).
func (e *gameEnv) cancelledCount(t *testing.T, c *Controller, version string, spaceAge bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Resources(ctx, PreviewRequest{GameVersion: version, SpaceAge: spaceAge, Seed: 99, Size: PreviewSizeMax}, "nauvis")
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		list, err := e.docker.ListContainers(context.Background(), true, []string{gameenv.LabelPreview + "=" + e.data})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no count container appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancelled := time.Now()
	cancel()
	select {
	case err := <-done:
		t.Logf("TIMING the call returned %s after the cancel: %v", time.Since(cancelled).Round(10*time.Millisecond), err)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error %v; want context.Canceled", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the call did not return within 15 s of the cancel")
	}
	e.previewLeftovers(t, "after a cancelled count")
	if len(c.previewSlot) != 0 {
		t.Errorf("the preview slot is still held")
	}
}
