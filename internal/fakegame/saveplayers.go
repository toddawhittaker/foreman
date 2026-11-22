package fakegame

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/game/saveplayers"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SavePlayer is one player the fake's save read reports: the game's own
// figure, the ticks the player was online (SPEC 8.16).
type SavePlayer struct {
	Name        string `json:"name"`
	OnlineTicks int64  `json:"online_ticks"`
}

// SavePlayersOutput is what the fake's save read reports. The zero value is
// not used: ResetSavePlayers restores DefaultSavePlayers.
type SavePlayersOutput struct {
	// Tick is the save's game.tick, which /time reports as play time.
	Tick    int64        `json:"tick"`
	Players []SavePlayer `json:"players"`
	// DropMod leaves a mod out of active_mods, as the real game does for a
	// mod it silently could not load, so Foreman's after-run check
	// (SPEC 8.16, mods-differ) can be tested.
	DropMod string `json:"drop_mod"`
	// Raw, when not empty, is written as the file's bytes instead of JSON, for
	// the bad-output result. The other fields are then ignored.
	Raw string `json:"raw"`
	// NoOutput makes the run exit 0 and leave no file, as a game that ran
	// without the helper writing anything does (bad-output).
	NoOutput bool `json:"no_output"`
}

// DefaultSavePlayers is the canned read: 276 hours 40 minutes of game time
// (SPEC 8.14 gives 59,760,005 ticks as that figure) and two players.
func DefaultSavePlayers() SavePlayersOutput {
	return SavePlayersOutput{
		Tick: 59_760_005,
		Players: []SavePlayer{
			{Name: "alice", OnlineTicks: 36_000_000}, // 166 hours 40 minutes
			{Name: "bob", OnlineTicks: 216_000},      // 1 hour
		},
	}
}

// savePlayersState is the fake's configured read. A save read is made by a
// plain function of the container (makePreview), which has no Game, so the
// setting is the process's, like the kinds' other canned answers; a test
// that changes it puts it back with ResetSavePlayers.
var savePlayersState = struct {
	mu  sync.Mutex
	out SavePlayersOutput
	set bool
}{}

// SetSavePlayers sets what later save reads report.
func SetSavePlayers(o SavePlayersOutput) {
	savePlayersState.mu.Lock()
	defer savePlayersState.mu.Unlock()
	o.Players = slices.Clone(o.Players)
	savePlayersState.out, savePlayersState.set = o, true
}

// ResetSavePlayers makes later save reads report DefaultSavePlayers again.
func ResetSavePlayers() {
	savePlayersState.mu.Lock()
	defer savePlayersState.mu.Unlock()
	savePlayersState.out, savePlayersState.set = SavePlayersOutput{}, false
}

func currentSavePlayers() SavePlayersOutput {
	savePlayersState.mu.Lock()
	defer savePlayersState.mu.Unlock()
	if !savePlayersState.set {
		return DefaultSavePlayers()
	}
	out := savePlayersState.out
	out.Players = slices.Clone(out.Players)
	return out
}

// savePlayersHook serves POST /save-players: a JSON SavePlayersOutput sets
// what later save reads report, and {"reset": true} (alone) restores the
// canned one. It answers 204, or 400 with {"error": "..."}.
func savePlayersHook() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SavePlayersOutput
			Reset bool `json:"reset"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if req.Reset && !reflect.DeepEqual(req.SavePlayersOutput, SavePlayersOutput{}) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "reset stands alone: it takes no other field"})
			return
		}
		if req.Reset {
			ResetSavePlayers()
		} else {
			SetSavePlayers(req.SavePlayersOutput)
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// fakeSavePlayers is a save read for the players of a save: it writes
// gameenv.SavePlayersFile to the work folder as the helper mod does (SPEC
// 8.16). The save must be bound at gameenv.SaveCopyMount. active_mods is what
// the real game would load from the work folder's mod list: base, the
// built-in mods the list enables at the game's version, the mods whose
// zip or folder is in the mods folder, and the helper. A mod the list enables
// but the folder lacks is left out, as the real game drops it silently and
// exits 0.
func fakeSavePlayers(c dockertest.Container) error {
	dir := mountSource(c, gameenv.PreviewMount)
	if dir == "" {
		return fmt.Errorf("the fake game's save players container has no folder bound at %s", gameenv.PreviewMount)
	}
	save := mountSource(c, gameenv.SaveCopyMount)
	if save == "" {
		return fmt.Errorf("the fake game's save players container has no save bound at %s", gameenv.SaveCopyMount)
	}
	if info, err := os.Stat(save); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("the fake game's save players container has no save file at %s", save)
	}
	cfg := currentSavePlayers()
	if cfg.NoOutput {
		return nil
	}
	var b []byte
	if cfg.Raw != "" {
		b = []byte(cfg.Raw)
	} else {
		v := imageVersion(c.Spec.Image)
		version := fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
		out := struct {
			Format      int          `json:"format"`
			GameVersion string       `json:"game_version"`
			Tick        int64        `json:"tick"`
			ActiveMods  []activeMod  `json:"active_mods"`
			Players     []SavePlayer `json:"players"`
		}{1, version, cfg.Tick, activeMods(dir, version, cfg.DropMod), cfg.Players}
		if out.Players == nil {
			out.Players = []SavePlayer{}
		}
		var err error
		if b, err = json.Marshal(out); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, gameenv.SavePlayersFile), b, world.FileMode) //nolint:gosec // the fake's own test folder
}

type activeMod struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// modFile is the name of a mod's zip or folder: "<name>_<version>", the
// version being three whole numbers, and ".zip" for a zip. The name may hold
// underscores, so "alpha_beta_1.0.0.zip" is the mod alpha_beta.
var modFile = regexp.MustCompile(`^(.+)_(\d+\.\d+\.\d+)(?:\.zip)?$`)

// activeMods is what the game would load with the work folder's mod list,
// sorted by name as the helper reports them. Like the game, it loads a mod
// whose zip or folder is in the mods folder though the list does not name it
// (the game adds such a mod to the list, enabled), and leaves out one the
// list disables or one it enables that has no files.
func activeMods(dir, gameVersion, drop string) []activeMod {
	installed := installedMods(filepath.Join(dir, world.ModsDir))
	enabled := map[string]bool{world.ModBase: true}
	disabled := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(dir, world.ModsDir, world.ModListFile)); err == nil { //nolint:gosec // the fake's own test folder
		var list struct {
			Mods []struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			} `json:"mods"`
		}
		if json.Unmarshal(b, &list) == nil {
			for _, m := range list.Mods {
				if m.Enabled {
					enabled[m.Name] = true
				} else {
					disabled[m.Name] = true
				}
			}
		}
	}
	for name := range installed {
		if !disabled[name] {
			enabled[name] = true
		}
	}
	builtin := world.BuiltinMods(gameVersion)
	var mods []activeMod
	for name := range enabled {
		switch v, ok := installed[name]; {
		case name == drop:
		case name == world.ModBase || slices.Contains(builtin, name):
			mods = append(mods, activeMod{name, gameVersion})
		case name == saveplayers.ModName && !ok:
			mods = append(mods, activeMod{name, saveplayers.ModVersion})
		case ok:
			mods = append(mods, activeMod{name, v})
		}
	}
	slices.SortFunc(mods, func(a, b activeMod) int { return strings.Compare(a.Name, b.Name) })
	return mods
}

// installedMods maps each mod with a zip or folder in the mods folder to its
// newest version there, which is the one the game loads.
func installedMods(modsDir string) map[string]string {
	entries, _ := os.ReadDir(modsDir)
	out := map[string]string{}
	for _, e := range entries {
		m := modFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if v, ok := out[m[1]]; !ok || world.CompareVersions(m[2], v) > 0 {
			out[m[1]] = m[2]
		}
	}
	return out
}
