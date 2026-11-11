package game

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/territory"
	"github.com/toddawhittaker/foreman/internal/world"
)

// chunkRadius is the picture's half width in chunks: a control mod reads
// chunks -radius to radius-1 on each axis.
func chunkRadius(req PreviewRequest) int {
	return req.size() / (2 * territory.ChunkTiles)
}

// controlMod is a control-only mod that a one-off preview container runs to
// read something from the map a preview would draw (SPEC 5.6, 14.3, ADR
// 0011): territories, or resource patches. The container creates the map
// with the mod enabled; the mod's on_init writes one JSON file, which is read
// back under a size limit.
type controlMod struct {
	// kind names the container ("foreman-<kind>-<folder>") and is the value
	// of gameenv.PreviewKind.
	kind string
	// what names the output in an error: "the game made no usable <what>
	// file".
	what string
	// name and dir are the mod's name and its folder under the mod
	// directory.
	name, dir string
	// control is the mod's control.lua.
	control []byte
	// params is the shell line that writes the mod's per-run Lua file from
	// the environment, since a mod can read neither files nor the
	// environment.
	params string
	// save is the save the run makes. It goes to the work folder, not the
	// tmpfs, which counts against the memory limit.
	save string
	// output is the file the mod writes to script-output, copied to the work
	// folder under the same name.
	output string
	// maxOutput is the most bytes of output that are read.
	maxOutput int64
	// memory is the container's memory limit in bytes; zero means
	// previewMemory.
	memory int64
}

// script makes the params file, creates a map from the same seed, preset and
// settings files a preview uses, and copies the mod's JSON to the work
// folder. The values come from the environment (gameenv), so no value of a
// request is built into it.
func (m controlMod) script() string {
	return strings.Join([]string{
		m.params + ` || exit 1`,
		`set -- --config ` + gameenv.PreviewMount + `/config.ini --mod-directory ` + gameenv.PreviewMount + `/` + world.ModsDir +
			` --create ` + gameenv.PreviewMount + `/` + m.save + ` --map-gen-seed "$` + gameenv.Seed + `"`,
		`if [ -n "$` + gameenv.Preset + `" ]; then set -- "$@" --preset "$` + gameenv.Preset + `"; fi`,
		`if [ -f ` + gameenv.PreviewMount + `/` + world.MapGenSettingsFile + ` ]; then set -- "$@" --map-gen-settings ` + gameenv.PreviewMount + `/` + world.MapGenSettingsFile + `; fi`,
		`if [ -f ` + gameenv.PreviewMount + `/` + world.MapSettingsFile + ` ]; then set -- "$@" --map-settings ` + gameenv.PreviewMount + `/` + world.MapSettingsFile + `; fi`,
		gameBinary + ` "$@" || exit 1`,
		`cp ` + previewWrite + `/script-output/` + m.output + ` ` + gameenv.PreviewMount + `/` + m.output,
	}, "\n")
}

// modInfo is a control mod's info.json. The game loads a mod only when its
// factorio_version is the game's own line, here a line such as "2.1".
func modInfo(name, version, title, description, line string, deps ...string) []byte {
	// Marshalling a plain struct of strings cannot fail.
	b, _ := json.MarshalIndent(struct {
		Name            string   `json:"name"`
		Version         string   `json:"version"`
		Title           string   `json:"title"`
		Author          string   `json:"author"`
		FactorioVersion string   `json:"factorio_version"`
		Description     string   `json:"description"`
		Dependencies    []string `json:"dependencies"`
	}{name, version, title, "Foreman", line, description, deps}, "", "  ")
	return append(b, '\n')
}

// modFile is where, in the container, the file named file goes in the
// folder dir of a mod.
func modFile(dir, file string) string {
	return gameenv.PreviewMount + "/" + world.ModsDir + "/" + dir + "/" + file
}

// runControlMod runs m for req in the preview slot (previewRun) and returns
// the bytes of its output file. info is the mod's info.json, env the
// container's own environment beside the kind, seed and preset.
func (c *Controller) runControlMod(ctx context.Context, req PreviewRequest, m controlMod, info []byte, env []string) ([]byte, error) {
	var out []byte
	err := c.previewRun(ctx, req, m.kind, func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error {
		if err := addControlMod(base, name, req, m, info); err != nil {
			return err
		}
		if err := container(c.controlModSpec(ref, name, req, m, env)); err != nil {
			return err
		}
		b, err := readControlModOutput(base, name, m)
		out = b
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// modEntry is one mod in mod-list.json, in the game's own shape.
type modEntry struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// addControlMod puts the control-only mod in the work folder and enables it
// in the mod list: the one already in the work folder's mods folder (a
// definitions run has the world's own), else the default list that
// makePreviewDir leaves out, for req's game.
func addControlMod(base *os.Root, name string, req PreviewRequest, m controlMod, info []byte) error {
	dir, err := base.OpenRoot(name)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	listPath := path.Join(world.ModsDir, world.ModListFile)
	current := world.DefaultModList(req.GameVersion, req.SpaceAge)
	if _, err := dir.Lstat(listPath); err == nil {
		if current, err = world.ReadSmallFile(dir, listPath, maxWorkModList); err != nil {
			return fmt.Errorf("could not read the mod list: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	modList, err := withControlMod(current, m.name)
	if err != nil {
		return err
	}
	modDir := path.Join(world.ModsDir, m.dir)
	if err := world.Mkdir(dir, modDir); err != nil {
		return err
	}
	for file, data := range map[string][]byte{
		listPath:                         append(modList, '\n'),
		path.Join(modDir, "info.json"):   info,
		path.Join(modDir, "control.lua"): m.control,
	} {
		if err := dir.WriteFile(file, data, world.FileMode); err != nil {
			return err
		}
	}
	return nil
}

// maxWorkModList is the most bytes read of a mod list a run starts from.
const maxWorkModList = 4 << 20

// withControlMod adds an enabled entry for the mod called name to the mod
// list JSON, keeping every other entry and field of the list as it is.
func withControlMod(list []byte, name string) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(list, &top); err != nil {
		return nil, fmt.Errorf("the mod list is not JSON: %w", err)
	}
	var mods []json.RawMessage
	if raw, ok := top["mods"]; ok {
		if err := json.Unmarshal(raw, &mods); err != nil {
			return nil, fmt.Errorf("the mod list has no list of mods: %w", err)
		}
	}
	entry, err := json.Marshal(modEntry{name, true})
	if err != nil {
		return nil, err
	}
	mods = append(mods, entry)
	if top == nil {
		top = map[string]json.RawMessage{}
	}
	if top["mods"], err = json.Marshal(mods); err != nil {
		return nil, err
	}
	return json.MarshalIndent(top, "", "  ")
}

// controlModSpec is a control mod's container: a preview container with its
// own command and environment, and the same limits (SPEC 14.3).
func (c *Controller) controlModSpec(image, dir string, req PreviewRequest, m controlMod, extra []string) docker.ContainerSpec {
	env := append([]string{gameenv.PreviewKind + "=" + m.kind}, extra...)
	env = append(env, gameenv.Seed+"="+strconv.FormatUint(uint64(req.Seed), 10))
	if req.Preset != "" {
		env = append(env, gameenv.Preset+"="+req.Preset)
	}
	return docker.ContainerSpec{
		Image:      image,
		Labels:     map[string]string{gameenv.LabelPreview: c.hostData},
		Env:        env,
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"-c", m.script()},
		User:       c.user,
		HostConfig: c.oneOffHostConfigMem(dir, cmp.Or(m.memory, previewMemory)),
	}
}

// readControlModOutput reads the run's JSON from the work folder, at most
// m.maxOutput bytes. The game's script is untrusted (SPEC 14.1): the file
// must be a regular file, so a named pipe cannot hold the read, and the
// caller's parser does the checking.
func readControlModOutput(base *os.Root, dir string, m controlMod) ([]byte, error) {
	root, err := base.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	b, err := world.ReadSmallFile(root, m.output, m.maxOutput)
	if err != nil {
		return nil, fmt.Errorf("the game made no usable %s file: %w", m.what, err)
	}
	return b, nil
}
