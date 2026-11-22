package fakegame_test

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// runFactoryMap runs a factory map container on the rig and returns the
// folder it wrote.
func runFactoryMap(t *testing.T, r *rig) string {
	t.Helper()
	ctx := context.Background()
	spec := docker.ContainerSpec{
		Image:  image21,
		Labels: map[string]string{gameenv.LabelPreview: "/data"},
		Env:    []string{gameenv.PreviewKind + "=" + gameenv.KindFactoryMap},
		HostConfig: docker.HostConfig{
			Binds: []string{r.world + ":" + gameenv.PreviewMount}, NetworkMode: "none",
		},
	}
	id, err := r.docker.CreateContainer(ctx, "foreman-factorymap-t", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if c := r.inspect(id); c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", c.State)
	}
	return filepath.Join(r.world, gameenv.FactoryMapDir)
}

// A factory map run leaves what the helper mod's output format says (SPEC
// 5.16, 14.6): a manifest of two surfaces, each with a chart file whose chunks
// decode to exactly 32 by 32 16-bit pixels and an entities file whose
// indexes and boxes agree with the manifest.
func TestFactoryMapRunWritesValidOutput(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	dir := runFactoryMap(t, r)

	b, err := os.ReadFile(filepath.Join(dir, "manifest.json")) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Format      int    `json:"format"`
		GameVersion string `json:"game_version"`
		Tick        int64  `json:"tick"`
		Surfaces    []struct {
			Index             int    `json:"index"`
			Name              string `json:"name"`
			Kind              string `json:"kind"`
			Chunks            int    `json:"chunks"`
			MinX              int    `json:"min_x"`
			MinY              int    `json:"min_y"`
			MaxX              int    `json:"max_x"`
			MaxY              int    `json:"max_y"`
			Chart             string `json:"chart"`
			Entities          string `json:"entities"`
			EntitiesTotal     int    `json:"entities_total"`
			EntitiesTruncated bool   `json:"entities_truncated"`
		} `json:"surfaces"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Format != 1 || m.GameVersion != "2.1.24" || m.Tick <= 0 {
		t.Fatalf("header %+v", m)
	}
	if len(m.Surfaces) != 2 {
		t.Fatalf("%d surfaces, want 2", len(m.Surfaces))
	}
	kinds := map[string]bool{}
	types := map[string]bool{}
	for _, s := range m.Surfaces {
		kinds[s.Kind] = true
		if s.Name == "" || s.EntitiesTruncated {
			t.Errorf("surface %+v", s)
		}
		checkChart(t, filepath.Join(dir, s.Chart), s.Chunks, s.MinX, s.MinY, s.MaxX, s.MaxY)
		for _, typ := range checkEntities(t, filepath.Join(dir, s.Entities), s.EntitiesTotal, s.MinX, s.MinY, s.MaxX, s.MaxY) {
			types[typ] = true
		}
	}
	if !kinds["planet"] || !kinds["platform"] {
		t.Errorf("kinds %v, want a planet and a platform", kinds)
	}
	// Several categories: belts, production, power, fluids, rail, defence and
	// logistics each have a prototype on the planet.
	for _, typ := range []string{"transport-belt", "assembling-machine", "steam-engine", "pipe", "straight-rail", "gun-turret", "roboport"} {
		if !types[typ] {
			t.Errorf("no %s entity; types %v", typ, types)
		}
	}
}

func checkChart(t *testing.T, path string, chunks, minX, minY, maxX, maxY int) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	n := 0
	seen := map[[2]int]bool{}
	for sc.Scan() {
		var c struct {
			X    int    `json:"x"`
			Y    int    `json:"y"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if c.X < minX || c.X > maxX || c.Y < minY || c.Y > maxY || seen[[2]int{c.X, c.Y}] {
			t.Errorf("chunk %d,%d outside %d,%d to %d,%d or repeated", c.X, c.Y, minX, minY, maxX, maxY)
		}
		seen[[2]int{c.X, c.Y}] = true
		raw, err := base64.StdEncoding.DecodeString(c.Data)
		if err != nil {
			t.Fatal(err)
		}
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		px, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		if len(px) != 32*32*2 {
			t.Fatalf("chunk %d,%d decodes to %d bytes, want %d", c.X, c.Y, len(px), 32*32*2)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n != chunks {
		t.Errorf("%s has %d chunks, manifest says %d", path, n, chunks)
	}
}

// checkEntities checks the first line is a list of types and each later
// line is a box over a known type, in tiles from the chart's top-left corner,
// the far corner excluded and inside the charted area; it returns the types.
func checkEntities(t *testing.T, path string, total, minX, minY, maxX, maxY int) []string {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatalf("%s is empty", path)
	}
	var types []string
	if err := json.Unmarshal(sc.Bytes(), &types); err != nil {
		t.Fatal(err)
	}
	n := 0
	for sc.Scan() {
		var e [5]int
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e[0] < 0 || e[0] >= len(types) || e[1] < 0 || e[2] < 0 || e[1] >= e[3] || e[2] >= e[4] ||
			e[3] > (maxX-minX+1)*32 || e[4] > (maxY-minY+1)*32 {
			t.Errorf("entity %v outside the chart or of an unknown type (%d types)", e, len(types))
		}
		n++
	}
	if n != total {
		t.Errorf("%s has %d entities, manifest says %d", path, n, total)
	}
	return types
}

// A second run replaces the first's folder rather than adding to it.
func TestFactoryMapRunReplacesOldOutput(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	dir := filepath.Join(r.world, gameenv.FactoryMapDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "surface-9.jsonl")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	runFactoryMap(t, r)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file stayed: %v", err)
	}
}

// Entity boxes count tiles from the chart's top-left corner: the planet's
// first belt stands at world tile 0,0, and the chart starts at chunk -3,-3,
// so the box is 96,96 to 97,97 (far corner excluded).
func TestFactoryMapEntitiesAreFromTheChartCorner(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	dir := runFactoryMap(t, r)
	b, err := os.ReadFile(filepath.Join(dir, "surface-1-entities.jsonl")) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(b, []byte("\n"))
	if got, want := string(lines[1]), "[0,96,96,97,97]"; got != want {
		t.Errorf("first entity %s, want %s", got, want)
	}
}
