//go:build game

package web

// A world file against a real factoriotools/factorio container (SPEC 5.13,
// 5.14, 14.5, ADR 0031): a world with a real save, a made-up mod and a game
// password is exported from one data folder, imported into a second data
// folder, and started there until the game reaches InGame. The imported
// world must hold the same save and mod bytes, load the same save, run the
// mod, carry no password and not the old host's RCON password. The two
// folders share nothing but the Docker daemon; the first world's game is
// stopped before the second one starts, because a game binds the host port
// 34197. Run by `make test-game`, or alone with
// `go test -tags game -run TestRealGameWorldFile ./internal/web/`.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	worldFileGameVersion = "2.1.24"
	worldFileProbeMod    = "foreman-t8-probe"
	worldFileProbeLoaded = "FOREMAN-T8-LOADED"
	worldFileInGameLog   = "changing state from(CreatingGame) to(InGame)"
	// worldFilePassword is the made-up game password that must never reach
	// the file or the second world.
	worldFilePassword = "hunter2-t8-secret" //nolint:gosec // a made-up value the test looks for
)

// worldFileProbeZip builds the made-up mod: an info.json the game accepts, a
// data.lua so the game prints its "Loading mod" line, and a control.lua that
// logs a line only this test's mod writes.
func worldFileProbeZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dir := worldFileProbeMod + "_1.0.0/"
	for name, body := range map[string]string{
		"info.json": `{"name":"` + worldFileProbeMod + `","version":"1.0.0","title":"Foreman probe","author":"foreman tests",` +
			`"description":"made up for the real-game test","factorio_version":"2.1"}`,
		"data.lua":    "-- nothing to define\n",
		"control.lua": `log("` + worldFileProbeLoaded + `")` + "\n",
	} {
		w, err := zw.Create(dir + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// worldFileHashes returns the SHA-256 of every regular file under dir, keyed
// by its slash path relative to dir.
func worldFileHashes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // the test's own folder
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// worldFileEnv is one data folder with its own store, world manager and game
// controller.
type worldFileEnv struct {
	hn   *harness
	ctrl *game.Controller
	// by is a user of this data folder's own store, whom the audit log needs.
	by game.Actor
}

func newWorldFileEnv(t *testing.T, dc *docker.Client) *worldFileEnv {
	t.Helper()
	e := &worldFileEnv{}
	makeGame := func(worlds *world.Manager, st *store.Store) *game.Controller {
		c, err := game.New(game.Config{
			Docker: dc, Worlds: worlds, Audit: st, Switches: st,
			HostDataDir: filepath.Dir(worlds.Dirs().Worlds), GameUID: os.Getuid(), GameGID: os.Getgid(),
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
		e.ctrl = c
		return c
	}
	e.hn = newHarnessWith(t, nil, makeGame)
	u := addTestUser(t, e.hn)
	e.by = game.Actor{UserID: u.ID, Username: u.Username}
	return e
}

func (e *worldFileEnv) dir(id string) string { return filepath.Join(e.hn.worlds.Dirs().Worlds, id) }

// inGames counts the InGame transitions in the world's game log.
func (e *worldFileEnv) inGames(id string) int {
	n := 0
	for _, l := range e.ctrl.Lines(id) {
		if strings.Contains(l, worldFileInGameLog) {
			n++
		}
	}
	return n
}

// logHas reports whether any line of the world's game log contains s.
func (e *worldFileEnv) logHas(id, s string) bool {
	return slices.ContainsFunc(e.ctrl.Lines(id), func(l string) bool { return strings.Contains(l, s) })
}

// cleanup stops the world's game and removes its container when the test ends.
func (e *worldFileEnv) cleanup(t *testing.T, dc *docker.Client, id string) {
	t.Helper()
	t.Cleanup(func() {
		_ = e.ctrl.Stop(context.Background(), id, e.by, game.StopOptions{})
		if err := dc.RemoveContainer(context.Background(), game.ContainerName(id), true); err != nil &&
			!strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "No such container") {
			t.Errorf("could not remove test container: %v", err)
		}
	})
}

func TestRealGameWorldFile(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && !strings.HasPrefix(worldFileGameVersion, only) {
		t.Skipf("runs on %s only", worldFileGameVersion)
	}
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dc.InspectImage(context.Background(), game.ImageRef(worldFileGameVersion)); err != nil {
		t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", worldFileGameVersion, err)
	}
	ctx := context.Background()

	// --- Host A: a world with a real save, the probe mod and a password.
	a := newWorldFileEnv(t, dc)
	idA, err := a.hn.worlds.Create(ctx, "worldfile-realgame6", worldFileGameVersion, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(game.ContainerName(idA), "foreman-worldfile-realgame6") {
		t.Fatalf("container name %s is not recognisable as this test's", game.ContainerName(idA))
	}
	a.cleanup(t, dc, idA)
	dirA := a.dir(idA)
	save, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", "2.1.24-no-space-age.zip"))
	if err != nil {
		t.Fatal(err)
	}
	modZip := worldFileProbeZip(t)
	modList := `{"mods":[{"name":"base","enabled":true},{"name":"` + worldFileProbeMod + `","enabled":true},` +
		// The save has no Space Age; unlisted, the game would turn the DLC on.
		`{"name":"elevated-rails","enabled":false},{"name":"quality","enabled":false},{"name":"space-age","enabled":false}]}`
	// Foreman's own default server-settings.json, with a made-up password.
	var settingsMap map[string]any
	if err := json.Unmarshal(gamesettings.NewServerSettings("worldfile-realgame6"), &settingsMap); err != nil {
		t.Fatal(err)
	}
	settingsMap["game_password"] = worldFilePassword
	settings, err := json.Marshal(settingsMap)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string][]byte{
		filepath.Join(world.SavesDir, "main.zip"):                    save,
		filepath.Join(world.ModsDir, worldFileProbeMod+"_1.0.0.zip"): modZip,
		filepath.Join(world.ModsDir, world.ModListFile):              []byte(modList),
		filepath.Join(world.ConfigDir, world.ServerSettingsFile):     settings,
	} {
		if err := os.WriteFile(filepath.Join(dirA, path), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// The world runs on host A, so the game writes its own files (a new
	// autosave, mod-settings.dat) into the folder the export reads.
	if err := a.ctrl.Start(ctx, idA, a.by, game.StartOptions{}); err != nil {
		t.Fatalf("start on host A: %v", err)
	}
	waitUntil(t, 3*time.Minute, "InGame on host A", func() bool { return a.inGames(idA) > 0 })
	if !a.logHas(idA, worldFileProbeLoaded) {
		t.Fatalf("host A: the game never logged %q, so the probe mod did not run", worldFileProbeLoaded)
	}
	rconA, err := os.ReadFile(filepath.Join(dirA, world.RCONPasswordFile)) //nolint:gosec // the test's own folder
	if err != nil || len(bytes.TrimSpace(rconA)) == 0 {
		t.Fatalf("host A has no RCON password after a start: %v", err)
	}
	if err := a.ctrl.Stop(ctx, idA, a.by, game.StopOptions{}); err != nil {
		t.Fatalf("stop on host A: %v", err)
	}
	targetA, found, err := a.hn.worlds.LoadTarget(idA)
	if err != nil || !found {
		t.Fatalf("host A has no save to load after its run: found=%v err=%v", found, err)
	}

	// --- Export: the whole world, saves and mods, into a file on disk.
	exp, err := a.hn.worlds.PrepareExport(ctx, idA, world.ExportAllSaves)
	if err != nil {
		t.Fatalf("PrepareExport: %v", err)
	}
	filePath := filepath.Join(t.TempDir(), "worldfile-realgame6-world.zip")
	out, err := os.Create(filePath) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	if err := exp.WriteZip(out, "t8-test"); err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exp.Close(); err != nil {
		t.Fatalf("Close of the export: %v", err)
	}
	if !exp.GamePasswordSet() {
		t.Error("the export did not notice the game password")
	}
	// The staging folder is gone, and the world's own folder is unchanged.
	if left, _ := filepath.Glob(filepath.Join(a.hn.worlds.Dirs().Worlds, ".staging-export-*")); len(left) != 0 {
		t.Errorf("the export left staging folders behind: %v", left)
	}

	// The file's contents, read as someone without Foreman would.
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		t.Fatalf("the world file is not a ZIP: %v", err)
	}
	var names []string
	var zipFiles = map[string][]byte{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		if strings.HasSuffix(f.Name, "/") || strings.HasPrefix(f.Name, world.SavesDir+"/") || strings.HasPrefix(f.Name, world.ModsDir+"/") && strings.HasSuffix(f.Name, ".zip") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		zipFiles[f.Name] = b
	}
	_ = zr.Close()
	if len(names) == 0 || names[0] != world.WorldFileFormatFile {
		t.Fatalf("the first entry is %v, want %s", names, world.WorldFileFormatFile)
	}
	var doc world.WorldFileDoc
	if err := json.Unmarshal(zipFiles[world.WorldFileFormatFile], &doc); err != nil || doc.FormatVersion != 1 ||
		doc.Saves != world.ExportAllSaves || !doc.GamePasswordSet {
		t.Errorf("format file: %+v (%v)", doc, err)
	}
	for _, want := range []string{world.MetaFile, "saves/main.zip", "saves/" + targetA.Name, "mods/" + worldFileProbeMod + "_1.0.0.zip",
		"mods/" + world.ModListFile, "config/" + world.ServerSettingsFile} {
		if !slices.Contains(names, want) {
			t.Errorf("the world file has no %s; it has %v", want, names)
		}
	}
	for _, n := range names {
		if strings.Contains(n, "rconpw") || strings.Contains(n, ".tmp.zip") || strings.HasPrefix(n, "snapshots") {
			t.Errorf("the world file holds %s, which is never exported", n)
		}
	}
	for n, b := range zipFiles {
		if strings.Contains(string(b), worldFilePassword) {
			t.Errorf("%s in the world file holds the game password", n)
		}
		if bytes.Contains(b, bytes.TrimSpace(rconA)) {
			t.Errorf("%s in the world file holds the RCON password", n)
		}
	}
	if _, ok := zipFiles[world.MetaFile]; ok {
		var m map[string]any
		_ = json.Unmarshal(zipFiles[world.MetaFile], &m)
		if _, has := m["imported_from"]; has {
			t.Error("world.json in the file still has imported_from")
		}
		if _, has := m["archived"]; has {
			t.Error("world.json in the file still has archived")
		}
	}

	// --- Host B: a second data folder with nothing in it. Import the file.
	b := newWorldFileEnv(t, dc)
	lim := world.WorldFileLimits{Save: 8 << 30, Mod: 2 << 30}
	f, temp, err := b.hn.worlds.CreateWorldFileTemp()
	if err != nil {
		t.Fatalf("CreateWorldFileTemp: %v", err)
	}
	src, err := os.Open(filePath) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(f, src); err != nil {
		t.Fatal(err)
	}
	_ = src.Close()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := b.hn.worlds.ReadWorldFile(temp, lim)
	if err != nil {
		t.Fatalf("ReadWorldFile: %v", err)
	}
	if info.Name != "worldfile-realgame6" || info.GameVersion != worldFileGameVersion || info.SpaceAge ||
		info.Saves < 1 || info.ModFiles < 2 || !info.GamePasswordSet || info.ForemanVersion != "t8-test" || info.Bytes <= 0 {
		t.Errorf("preview: %+v", info)
	}
	// Only the staged upload is there, a hidden file: no world folder yet.
	if left, _ := os.ReadDir(b.hn.worlds.Dirs().Worlds); slices.ContainsFunc(left, func(d os.DirEntry) bool { return !strings.HasPrefix(d.Name(), ".") }) {
		t.Errorf("reading the file for the preview made a world folder: %v", left)
	}
	idB, err := b.hn.worlds.ImportWorldFile(ctx, temp, "worldfile-realgame6-copy", lim)
	if err != nil {
		t.Fatalf("ImportWorldFile: %v", err)
	}
	if idB == idA || !strings.HasPrefix(game.ContainerName(idB), "foreman-worldfile-realgame6-copy") {
		t.Fatalf("imported id %q (host A's is %q), container %s", idB, idA, game.ContainerName(idB))
	}
	b.cleanup(t, dc, idB)
	dirB := b.dir(idB)
	if _, err := os.Stat(temp); err == nil {
		t.Error("the staged upload is still there after a successful import")
	}

	// The copy is exact where it must be: every save, the mod and mod-list.
	for _, rel := range []string{"saves/main.zip", "saves/" + targetA.Name, "mods/" + worldFileProbeMod + "_1.0.0.zip", "mods/" + world.ModListFile} {
		want := worldFileHashes(t, dirA)[rel]
		got := worldFileHashes(t, dirB)[rel]
		if want == "" || got != want {
			t.Errorf("%s: host A %q, host B %q", rel, want, got)
		}
	}
	// Secrets did not travel, and the new host made no RCON password yet.
	if got, err := os.ReadFile(filepath.Join(dirB, world.ConfigDir, world.ServerSettingsFile)); err != nil || strings.Contains(string(got), worldFilePassword) { //nolint:gosec // the test's own folder
		t.Errorf("server-settings.json on host B: %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(dirB, world.RCONPasswordFile)); err == nil {
		t.Error("host B has an RCON password before its first start")
	}
	metaB, err := os.ReadFile(filepath.Join(dirB, world.MetaFile)) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	var mb map[string]any
	_ = json.Unmarshal(metaB, &mb)
	if mb["name"] != "worldfile-realgame6-copy" || mb["game_version"] != worldFileGameVersion || mb["archived"] != nil || mb["imported_from"] != nil || mb["new_map"] != nil {
		t.Errorf("world.json on host B: %s", metaB)
	}
	// A ZIP entry keeps its time to the second, so the sub-second part is lost.
	targetB, found, err := b.hn.worlds.LoadTarget(idB)
	if err != nil || !found || targetB.Name != targetA.Name || targetB.Size != targetA.Size || !targetB.ModTime.Equal(targetA.ModTime.Truncate(time.Second)) {
		t.Errorf("the save that loads: host A %+v, host B %+v (found %v, %v)", targetA, targetB, found, err)
	}

	// --- Start on host B until InGame, with the save and the mod.
	if err := b.ctrl.Start(ctx, idB, b.by, game.StartOptions{}); err != nil {
		t.Fatalf("start on host B: %v", err)
	}
	waitUntil(t, 3*time.Minute, "InGame on host B", func() bool { return b.inGames(idB) > 0 })
	if got := b.ctrl.Status(idB).GameVersion; got != worldFileGameVersion {
		t.Errorf("host B runs %s, want %s", got, worldFileGameVersion)
	}
	if !b.logHas(idB, worldFileProbeLoaded) {
		t.Errorf("host B: the game never logged %q, so the imported mod did not run", worldFileProbeLoaded)
	}
	if !b.logHas(idB, "saves/"+targetA.Name) {
		t.Errorf("host B: the game's log never names saves/%s, the save that loads", targetA.Name)
	}
	if b.logHas(idB, "Creating new map") {
		t.Error("host B made a new map instead of loading the imported save")
	}
	rconB, err := os.ReadFile(filepath.Join(dirB, world.RCONPasswordFile)) //nolint:gosec // the test's own folder
	if err != nil || len(bytes.TrimSpace(rconB)) == 0 || bytes.Equal(bytes.TrimSpace(rconA), bytes.TrimSpace(rconB)) {
		t.Errorf("host B's RCON password must exist and differ from host A's (%v)", err)
	}
	t.Logf("exported %d entries; imported as %s; host B reached InGame on %s", len(names), idB, worldFileGameVersion)
}
