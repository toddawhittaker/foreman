package fakegame

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// cannedSurface is one surface of the canned factory map: charted chunks
// from (minX, minY) to (maxX, maxY) inclusive, of which the chunks at
// water are water, and the buildings on it.
type cannedSurface struct {
	index                  int
	name, kind             string
	minX, minY, maxX, maxY int
	water                  [][2]int
	buildings              []cannedBuilding
}

// cannedBuilding is a building from its engine prototype type; x1, y1, x2, y2
// are its corners in world tiles, the far corner excluded. The entities file
// writes them from the chart's top-left corner instead.
type cannedBuilding struct {
	typ            string
	x1, y1, x2, y2 int
}

// cannedFactory is two surfaces, a planet and a space platform, with
// buildings of every category on the planet and a few on the platform, so a
// test sees layers of different sizes. The prototype types are the engine's,
// as the game reports them.
var cannedFactory = []cannedSurface{
	{
		index: 1, name: "nauvis", kind: "planet",
		minX: -3, minY: -3, maxX: 2, maxY: 2,
		water: [][2]int{{-3, -3}, {-2, -3}, {-3, -2}},
		buildings: []cannedBuilding{
			{"transport-belt", 0, 0, 1, 1}, {"transport-belt", 1, 0, 2, 1},
			{"inserter", 2, 0, 3, 1},
			{"assembling-machine", 4, 0, 7, 3},
			{"furnace", 8, 0, 10, 2},
			{"steam-engine", -12, 4, -7, 7},
			{"boiler", -12, 8, -9, 10},
			{"pipe", -8, 8, -7, 9},
			{"storage-tank", 12, 4, 15, 7},
			{"straight-rail", -20, 20, -18, 22},
			{"gun-turret", 20, -10, 22, -8},
			{"wall", 20, -7, 21, -6},
			{"logistic-chest-passive-provider", 6, 6, 7, 7},
			{"roboport", 9, 6, 13, 10},
			{"simple-entity-with-owner", -30, -30, -29, -29},
		},
	},
	{
		index: 2, name: "platform-1", kind: "platform",
		minX: 0, minY: 0, maxX: 2, maxY: 1,
		buildings: []cannedBuilding{
			{"asteroid-collector", 34, 34, 37, 37},
			{"thruster", 40, 50, 43, 53},
		},
	},
}

// fakeFactoryMap is a save read for the factory map: it writes the
// gameenv.FactoryMapDir folder to the work folder as the helper mod does (SPEC
// 5.16, 14.6): manifest.json, and for each surface a chart file of
// {"x","y","data"} lines and an entities file. It is a small, valid map with
// two surfaces and buildings of several categories, the same for every
// world.
func fakeFactoryMap(c dockertest.Container) error {
	root := mountSource(c, gameenv.PreviewMount)
	if root == "" {
		return fmt.Errorf("the fake game's factory map container has no folder bound at %s", gameenv.PreviewMount)
	}
	dir := filepath.Join(root, gameenv.FactoryMapDir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.Mkdir(dir, world.DirMode); err != nil {
		return err
	}
	v := imageVersion(c.Spec.Image)
	m := factorymap.Manifest{
		Format:      factorymap.ManifestFormat,
		GameVersion: fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]),
		Tick:        8352001,
	}
	for _, s := range cannedFactory {
		ms, err := writeCannedSurface(dir, s)
		if err != nil {
			return err
		}
		m.Surfaces = append(m.Surfaces, ms)
	}
	return writeJSON(filepath.Join(dir, "manifest.json"), m)
}

func writeCannedSurface(dir string, s cannedSurface) (factorymap.ManifestSurface, error) {
	ms := factorymap.ManifestSurface{
		Index: s.index, Name: s.name, Kind: s.kind,
		MinX: s.minX, MinY: s.minY, MaxX: s.maxX, MaxY: s.maxY,
		Chart:         fmt.Sprintf("surface-%d.jsonl", s.index),
		Entities:      fmt.Sprintf("surface-%d-entities.jsonl", s.index),
		EntitiesTotal: len(s.buildings),
	}
	water := map[[2]int]bool{}
	for _, w := range s.water {
		water[w] = true
	}
	var chart bytes.Buffer
	enc := json.NewEncoder(&chart)
	for y := s.minY; y <= s.maxY; y++ {
		for x := s.minX; x <= s.maxX; x++ {
			data, err := encodeChunk(chunkPixels(x, y, water[[2]int{x, y}]))
			if err != nil {
				return ms, err
			}
			if err := enc.Encode(struct {
				X    int    `json:"x"`
				Y    int    `json:"y"`
				Data string `json:"data"`
			}{x, y, data}); err != nil {
				return ms, err
			}
			ms.Chunks++
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ms.Chart), chart.Bytes(), world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return ms, err
	}

	// The entities file: the types on the first line, then one
	// [type_index, x1, y1, x2, y2] line a building, in whole tiles from the
	// chart's top-left corner (tile = world tile - min chunk * 32), x1 and
	// y1 included, x2 and y2 excluded.
	ox, oy := s.minX*factorymap.ChunkPixels, s.minY*factorymap.ChunkPixels
	var types []string
	index := map[string]int{}
	var lines bytes.Buffer
	for _, b := range s.buildings {
		i, ok := index[b.typ]
		if !ok {
			i = len(types)
			index[b.typ] = i
			types = append(types, b.typ)
		}
		fmt.Fprintf(&lines, "[%d,%d,%d,%d,%d]\n", i, b.x1-ox, b.y1-oy, b.x2-ox, b.y2-oy)
	}
	head, err := json.Marshal(types)
	if err != nil {
		return ms, err
	}
	out := append(append(head, '\n'), lines.Bytes()...)
	return ms, os.WriteFile(filepath.Join(dir, ms.Entities), out, world.FileMode) //nolint:gosec // the fake's own test folder
}

// chunkPixels is one chunk's chart as the game gives it: 32 by 32 pixels of
// 16-bit RGB565, two bytes each, little-endian, row by row. Land is green
// with a darker line in every 8th row and column so a picture is not one
// colour; water is blue.
func chunkPixels(x, y int, water bool) []byte {
	px := make([]byte, 0, factorymap.ChunkPixels*factorymap.ChunkPixels*2)
	for row := 0; row < factorymap.ChunkPixels; row++ {
		for col := 0; col < factorymap.ChunkPixels; col++ {
			r, g, b := 60, 120, 50
			switch {
			case water:
				r, g, b = 30, 70, 140
			case row%8 == 0 || col%8 == 0:
				r, g, b = 45, 95, 40
			case (x+y)%2 != 0:
				g = 130
			}
			v := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
			px = binary.LittleEndian.AppendUint16(px, v)
		}
	}
	return px
}

// encodeChunk is what helpers.encode_string does: zlib, then base64.
func encodeChunk(px []byte) (string, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(px); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, world.FileMode) //nolint:gosec // the fake's own test folder
}
