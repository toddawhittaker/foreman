//go:build game

package game

// The resource count (SPEC 5.6, 14.3) against the real game images: every
// planet of each line, at 1280 and 3072, and Nauvis at 3072 with resource
// frequency and size at 600%. Each run logs its wall time, the container's
// peak memory, the patches per resource and the file's size, and stays
// within the preview limits. The spike also logs how the patches change
// with the well gap at 12 and 48 tiles, and with an ore gap of 1 and 3. Run by
// `make test-game`, never by `make verify`.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	b, err := c.runControlMod(context.Background(), req, m, resources.InfoJSON(line, req.SpaceAge), resourcesEnv(req, planet))
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
			if line != "2.1" && line != "1.1" || os.Getenv("FOREMAN_RESOURCE_NO_GAPS") != "" {
				return
			}
			// The spike's checks of the gaps, on 2.1 and 1.1 only.
			req := PreviewRequest{GameVersion: version, SpaceAge: spaceAge, Seed: resourceSeed, Size: 3072}
			for _, extra := range []string{", well_gap = 12", ", well_gap = 48", ", ore_gap = 1", ", ore_gap = 3"} {
				r := e.countWith(t, c, req, planet, extra)
				t.Logf("GAP %s %s 3072%s: %d patches (default %d): %s", version, planet, extra, len(r.file.Patches), len(big.file.Patches), r.summary())
			}
			logWells(t, big.file)
			logFragments(t, big.file, 3072)
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

// hasResourceFor reports whether the file has a patch whose entity is the
// control's resource: the control name with the planet's prefix dropped and
// _ as -, matched either way round.
func hasResourceFor(f countFile, planet, control string) bool {
	want := strings.ReplaceAll(strings.TrimPrefix(control, planet+"_"), "_", "-")
	for name := range f.Resources {
		got := strings.ReplaceAll(strings.TrimPrefix(name, planet+"_"), "_", "-")
		if got == want || strings.HasPrefix(got, want) || strings.HasPrefix(want, got) {
			return true
		}
	}
	return false
}

// boxGap is the distance on the farther axis between two boxes' nearest
// tiles: 1 when they touch at a side or corner, 0 when they overlap.
func boxGap(a, b [4]int) int {
	dx := max(a[0]-b[2], b[0]-a[2], 0)
	dy := max(a[1]-b[3], b[1]-a[3], 0)
	return max(dx, dy)
}

// nearestGap is the smallest boxGap from patch i to another patch of the
// same resource, or -1 when there is none.
func nearestGap(f countFile, i int) int {
	best := -1
	for j, q := range f.Patches {
		if j != i && q.Name == f.Patches[i].Name {
			if g := boxGap(f.Patches[i].Box, q.Box); best < 0 || g < best {
				best = g
			}
		}
	}
	return best
}

// logFragments logs, for each ore, its patches of fewer than 10 entities:
// how many touch the picture's edge, how many lie within 3 tiles of another
// patch of the same ore, and their share of the ore's total.
func logFragments(t *testing.T, f countFile, size int) {
	t.Helper()
	for name, r := range f.Resources {
		if r.Infinite {
			continue
		}
		var small, edge, near int
		var amount float64
		for i, p := range f.Patches {
			if p.Name != name || p.N >= 10 {
				continue
			}
			small++
			amount += p.Amount
			if p.Box[0] == -size/2 || p.Box[1] == -size/2 || p.Box[2] == size/2-1 || p.Box[3] == size/2-1 {
				edge++
			}
			if g := nearestGap(f, i); g >= 0 && g <= 3 {
				near++
			}
		}
		if small > 0 {
			t.Logf("FRAGMENTS %s: %d under 10 entities, %d at the edge, %d within 3 tiles of another, %.3f%% of the total",
				name, small, edge, near, 100*amount/max(r.Total, 1))
		}
	}
}

// logWells logs, for each infinite resource, each patch's wells, box and
// the box gap to the nearest other patch of it, to judge whether the well
// gap joins one field or splits it.
func logWells(t *testing.T, f countFile) {
	t.Helper()
	for name, r := range f.Resources {
		if !r.Infinite {
			continue
		}
		var parts []string
		for i, p := range f.Patches {
			if p.Name == name {
				parts = append(parts, fmt.Sprintf("%d wells %dx%d gap %d", p.N, p.Box[2]-p.Box[0]+1, p.Box[3]-p.Box[1]+1, nearestGap(f, i)))
			}
		}
		slices.Sort(parts)
		t.Logf("WELLS %s: %s", name, strings.Join(parts, "; "))
	}
}

// wellsLua is a test-only stand-in for the count mod that writes every
// resource prototype found, with its flags, and every entity of a resource
// whose product is a fluid, so the spike can judge the well gap.
const wellsLua = `local params = require("params")
script.on_init(function()
  local h = helpers or game
  local s = game.surfaces[params.planet]
  if not s then
    local p = game.planets[params.planet]
    s = p.surface or p.create_surface()
  end
  s.request_to_generate_chunks({ 0, 0 }, params.radius)
  s.force_generate_chunk_requests()
  local protos, wells, seen = {}, {}, {}
  local r = params.radius
  for cx = -r, r - 1 do
    for cy = -r, r - 1 do
      for _, e in pairs(s.find_entities_filtered({ area = { { cx * 32, cy * 32 }, { cx * 32 + 32, cy * 32 + 32 } }, type = "resource" })) do
        local name = e.name
        if not protos[name] then
          local pr = e.prototype
          local fluid = false
          for _, p in pairs(pr.mineable_properties.products or {}) do
            if p.type == "fluid" then fluid = true end
          end
          protos[name] = { infinite = pr.infinite_resource and true or false, normal = pr.normal_resource_amount or 0,
            category = pr.resource_category or "", fluid = fluid }
        end
        local x, y = math.floor(e.position.x), math.floor(e.position.y)
        local k = name .. ":" .. x .. ":" .. y
        if protos[name].fluid and not seen[k] then
          seen[k] = true
          wells[#wells + 1] = { name = name, x = x, y = y }
        end
      end
    end
  end
  h.write_file("resources.json", h.table_to_json({ protos = protos, wells = wells }), false)
end)
`

// TestRealGameD2ResourceWells logs, for each planet with wells, how many
// groups single-linkage clustering makes at each well gap, so a gap on a
// plateau can be chosen, and each resource prototype's flags.
func TestRealGameD2ResourceWells(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag || g.tag == "2.0" {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			c, _ := e.controller(t)
			spaceAge := g.tag != "1.1"
			for _, planet := range mapgen.Planets(g.tag, spaceAge) {
				m := resourceMod
				m.control = []byte(wellsLua)
				m.params = countParams("")
				req := PreviewRequest{GameVersion: g.version, SpaceAge: spaceAge, Seed: resourceSeed, Size: 3072}
				b, _, _, err := e.measure(t, c, req, planet, m)
				if err != nil {
					t.Fatal(err)
				}
				var f struct {
					Protos map[string]any `json:"protos"`
					Wells  json.RawMessage
				}
				if err := json.Unmarshal(b, &f); err != nil {
					t.Fatal(err)
				}
				var wells []struct {
					Name string
					X, Y int
				}
				_ = json.Unmarshal(f.Wells, &wells)
				t.Logf("PROTOS %s %s: %v", g.version, planet, f.Protos)
				by := map[string][][2]int{}
				for _, w := range wells {
					by[w.Name] = append(by[w.Name], [2]int{w.X, w.Y})
				}
				for name, ws := range by {
					var parts []string
					for _, d := range []int{4, 6, 8, 10, 12, 14, 16, 18, 20, 24, 28, 32, 40, 48} {
						parts = append(parts, fmt.Sprintf("%d:%d", d, clusters(ws, d)))
					}
					t.Logf("CLUSTERS %s %s %s (%d wells): %s", g.version, planet, name, len(ws), strings.Join(parts, " "))
				}
			}
		})
	}
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
