package fakegame

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The fake models the two things the start script and the preview script run
// the game for besides the server: --create (SPEC 4.3) and
// --generate-map-preview (SPEC 5.6). Neither runs a shell; the fake reads the
// same environment the scripts do.

// envValue returns the value of a NAME=value entry of a container's
// environment, or "".
func envValue(env []string, name string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}
	return ""
}

// mountSource returns the host folder bound at dest, or "".
func mountSource(c dockertest.Container, dest string) string {
	for _, m := range c.Mounts {
		if m.Destination == dest {
			return m.Source
		}
	}
	return ""
}

var imageTag = regexp.MustCompile(`:(\d+)\.(\d+)\.(\d+)$`)

// imageVersion reads the game version from an image tag such as
// factoriotools/factorio:2.0.77, as the real game would report it. An image
// with another tag gets the newest transcript's version.
func imageVersion(image string) [3]uint16 {
	m := imageTag.FindStringSubmatch(image)
	if m == nil {
		m = imageTag.FindStringSubmatch(":" + versionFor(image).name)
	}
	var v [3]uint16
	for i := range v {
		n, _ := strconv.Atoi(m[i+1])
		v[i] = uint16(n) //nolint:gosec // three digits of a tag
	}
	return v
}

// createSave is the start script's create step: when the container is told
// to make the map and the world's saves folder holds no save, it writes one
// there, named for the world. Like the script, it never replaces a save.
func createSave(c dockertest.Container) error {
	if envValue(c.Spec.Env, gameenv.NewMap) == "" {
		return nil
	}
	root := mountSource(c, "/factorio")
	saves := filepath.Join(root, world.SavesDir)
	if found, _ := filepath.Glob(filepath.Join(saves, "*.zip")); len(found) > 0 {
		return nil
	}
	id := envValue(c.Spec.Env, gameenv.WorldID)
	if !world.ValidID(id) {
		return fmt.Errorf("the fake game cannot make a save for world id %q", id)
	}
	v := imageVersion(c.Spec.Image)
	b, err := saveZip(id, v, enabledMods(root))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(saves, id+".zip"), b, world.FileMode) //nolint:gosec // the fake's own test folder
}

// enabledMods lists the mods the world's mod list enables, base first as
// the game writes it. A world with no list has base only.
func enabledMods(root string) []string {
	b, err := os.ReadFile(filepath.Join(root, world.ModsDir, world.ModListFile)) //nolint:gosec // the fake's own test folder
	if err != nil {
		return []string{"base"}
	}
	var list struct {
		Mods []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"mods"`
	}
	if json.Unmarshal(b, &list) != nil {
		return []string{"base"}
	}
	var out []string
	for _, m := range list.Mods {
		if m.Enabled {
			out = append(out, m.Name)
		}
	}
	return out
}

// saveZip builds a save the save reader (SPEC 9.9) accepts: a zip holding
// level.dat0 with a header for the given game version and mods, each mod at
// the game's version, as built-in mods are. The rest of a real save is not
// modelled.
func saveZip(id string, v [3]uint16, mods []string) ([]byte, error) {
	var h bytes.Buffer
	u16 := func(n uint16) { _ = binary.Write(&h, binary.LittleEndian, n) }
	str := func(s string) { h.WriteByte(byteOf(len(s))); h.WriteString(s) }
	modVer := func() {
		for _, n := range v {
			h.WriteByte(byteOf(int(n)))
		}
	}
	u16(v[0])
	u16(v[1])
	u16(v[2])
	u16(0)         // build
	h.WriteByte(0) // unknown flag
	str("")        // campaign
	str("")        // scenario name
	str("")        // mod that holds the scenario
	h.WriteByte(0) // difficulty
	h.Write([]byte{0, 0})
	str("") // next level
	h.Write([]byte{0, 0, 0, 0})
	modVer() // version the save was loaded from
	h.Write([]byte{0, 0})
	h.WriteByte(0) // allowed commands
	if v[0] >= 2 {
		h.Write(make([]byte, 6))
	}
	h.WriteByte(byteOf(len(mods)))
	for _, m := range mods {
		str(m)
		modVer()
		h.Write(make([]byte, 4)) // CRC
	}

	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	w, err := zw.Create(id + "/level.dat0")
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(h.Bytes()); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return z.Bytes(), nil
}

// byteOf converts a name length, a mod count or a colour part to a byte.
func byteOf(n int) byte { return byte(n) } //nolint:gosec // the callers' values are under 256

// drawPreview is --generate-map-preview: it writes <planet>.png for each
// planet in the work folder bound at /out. The picture is a solid square
// whose colour comes from the planet and the seed, so a test can tell two
// previews apart.
func drawPreview(c dockertest.Container) error {
	dir := mountSource(c, gameenv.PreviewMount)
	if dir == "" {
		return fmt.Errorf("the fake game's preview container has no folder bound at %s", gameenv.PreviewMount)
	}
	seed := envValue(c.Spec.Env, gameenv.Seed)
	for _, p := range strings.Fields(envValue(c.Spec.Env, gameenv.PreviewPlanets)) {
		b, err := previewPNG(p, seed)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, p+".png"), b, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
			return err
		}
	}
	return nil
}

func previewPNG(planet, seed string) ([]byte, error) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(planet + "/" + seed))
	sum := h.Sum32()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	fill := color.RGBA{R: byteOf(int(sum & 0xff)), G: byteOf(int(sum >> 8 & 0xff)), B: byteOf(int(sum >> 16 & 0xff)), A: 255}
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, fill)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
