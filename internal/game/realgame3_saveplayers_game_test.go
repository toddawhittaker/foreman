//go:build game

package game

// Reading the players of a save against the real game on 2.0 and 2.1 (SPEC
// 8.14, 8.16, ADR 0030, plan rules 1, 2, 11): a read returns the game's tick
// and every player the game itself lists; the original save, and every other
// file of the world, is untouched by the read (bytes, size, modified time to
// the nanosecond, inode); and a hostile command in a container made like the
// read's, which tries to write the save, is refused by the kernel.
//
// What the read must return is not taken from Foreman's own code: a second,
// plain game run on a copy of the save, with a small Lua mod written here,
// says what the game holds. Run by `make test-game`, never by `make verify`.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// spOracleControl is the oracle mod's control.lua: it writes the tick
// and every player the save holds, connected or not, to oracle.json.
const spOracleControl = `script.on_init(function()
  local players = {}
  for _, p in pairs(game.players) do
    players[#players + 1] = {name = p.name, online_time = p.online_time}
  end
  helpers.write_file("oracle.json", helpers.table_to_json({tick = game.tick, players = players}), false)
end)
`

// spPlayersFixtures are optional saves with real players in them, one per game
// version, made by a person with a real client (the game cannot create a
// player without one). When a file is there the read is checked against it
// too; see the test's skip message.
const spPlayersFixtureDir = "testdata/saveplayers"

func TestRealGameSavePlayersRead(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if g.tag == "1.1" || (only != "" && only != g.tag) {
			continue // the plan names 2.0.77 and 2.1.24
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			t.Run("fresh save", func(t *testing.T) {
				e.runSavePlayersRead(t, g.tag, g.fixture, g.version, "fresh")
			})
			t.Run("save with players", func(t *testing.T) {
				fx := filepath.Join(spPlayersFixtureDir, g.version+"-players.zip")
				if _, err := os.Stat(fx); err != nil {
					t.Skipf("no %s: the game cannot make a player without a client, so a save "+
						"with players must be made by hand and committed there. Until then "+
						"the player list is compared only on a save with none", fx)
				}
				e.runSavePlayersRead(t, g.tag, "", g.version, fx)
			})
		})
	}
}

func TestRealGameSavePlayersHostile(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if g.tag == "1.1" || (only != "" && only != g.tag) {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runSavePlayersHostile(t, g.tag, g.fixture, g.version)
		})
	}
}

// spFileState is what a read must not change in a world's file.
type spFileState struct {
	sha     string
	size    int64
	modTime time.Time // compared to the nanosecond
	inode   uint64
	mode    fs.FileMode
}

// spWorldState lists every file under dir, by path relative to it, with its
// state. A folder is listed by its path alone, so a new or removed file or
// folder shows as a difference. The files are read, so a link is not followed.
func spWorldState(t *testing.T, dir string) map[string]spFileState {
	t.Helper()
	out := map[string]spFileState{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st := spFileState{size: info.Size(), mode: info.Mode()}
		if sys, ok := info.Sys().(*syscall.Stat_t); ok {
			st.inode = sys.Ino
		}
		if info.Mode().IsRegular() {
			st.modTime = info.ModTime()
			f, err := os.Open(p) //nolint:gosec // inside the test's own data folder
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				return err
			}
			st.sha = hex.EncodeToString(h.Sum(nil))
		}
		out[rel] = st
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// spSameWorld fails for every file that differs between two states, except the
// ones named in except (by relative path).
func spSameWorld(t *testing.T, before, after map[string]spFileState, except ...string) {
	t.Helper()
	for p, b := range before {
		if slices.Contains(except, p) {
			continue
		}
		a, ok := after[p]
		switch {
		case !ok:
			t.Errorf("%s is gone after the read", p)
		case a.sha != b.sha || a.size != b.size:
			t.Errorf("%s changed: size %d to %d, sha256 %.12s to %.12s", p, b.size, a.size, b.sha, a.sha)
		case !a.modTime.Equal(b.modTime):
			t.Errorf("%s: modified time %s became %s", p, b.modTime.Format(time.RFC3339Nano), a.modTime.Format(time.RFC3339Nano))
		case a.inode != b.inode:
			t.Errorf("%s: inode %d became %d", p, b.inode, a.inode)
		case a.mode != b.mode:
			t.Errorf("%s: mode %v became %v", p, b.mode, a.mode)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok && !slices.Contains(except, p) {
			t.Errorf("%s is new after the read", p)
		}
	}
}

// oracleResult is what the game itself says a save holds.
type oracleResult struct {
	Tick    int64
	Players map[string]int64 // name to online_time
}

// oracleRead runs the game plainly (not through Foreman) on a copy of the
// save, with the oracle mod, in a network-less container, and returns what the
// mod wrote. The container is named for this run and removed at the end.
func (e *gameEnv) oracleRead(t *testing.T, version, saveFile string) oracleResult {
	t.Helper()
	scratch := t.TempDir()
	line := version[:3]
	modDir := filepath.Join(scratch, "mods", "a12oracle_1.0.0")
	if err := os.MkdirAll(modDir, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(p string, b []byte) {
		t.Helper()
		if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // the test's own scratch folder
			t.Fatal(err)
		}
	}
	write(filepath.Join(modDir, "info.json"), modInfo("a12oracle", "1.0.0", "A12 oracle", "test", line, "base"))
	write(filepath.Join(modDir, "control.lua"), []byte(spOracleControl))
	write(filepath.Join(scratch, "mods", world.ModListFile),
		[]byte(`{"mods":[{"name":"base","enabled":true},{"name":"a12oracle","enabled":true}]}`))
	write(filepath.Join(scratch, "config.ini"), []byte("[path]\nread-data=__PATH__executable__/../../data\nwrite-data=/work/wd\n"))
	b, err := os.ReadFile(saveFile) //nolint:gosec // a save the test made
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(scratch, "save.zip"), b)

	name := "foreman-a12oracle-" + e.run
	cmd := exec.CommandContext(context.Background(), "docker", "run", "--rm", "--name", name, //nolint:gosec // fixed arguments on the test's own container
		"--user", fmt.Sprintf("%d:%d", e.uid, e.gid), "--network", "none", "--entrypoint", "sh",
		"-v", scratch+":/work", ImageRef(version), "-c",
		gameBinary+" -c /work/config.ini --mod-directory /work/mods --benchmark /work/save.zip --benchmark-ticks 8 --benchmark-runs 1")
	t.Cleanup(func() { _ = exec.CommandContext(context.Background(), "docker", "rm", "-f", name).Run() }) //nolint:gosec // the test's own container
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the oracle run failed: %v\n%s", err, spTail(out, 1500))
	}
	raw, err := os.ReadFile(filepath.Join(scratch, "wd", "script-output", "oracle.json")) //nolint:gosec // the test's own scratch folder
	if err != nil {
		t.Fatalf("the oracle mod wrote nothing: %v", err)
	}
	var parsed struct {
		Tick    int64
		Players json.RawMessage
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("the oracle's output is not JSON: %v: %s", err, raw)
	}
	res := oracleResult{Tick: parsed.Tick, Players: map[string]int64{}}
	// The game writes an empty Lua table as {} or [].
	if bytes.HasPrefix(bytes.TrimSpace(parsed.Players), []byte("[")) {
		var list []struct {
			Name       string
			OnlineTime int64 `json:"online_time"`
		}
		if err := json.Unmarshal(parsed.Players, &list); err != nil {
			t.Fatalf("the oracle's player list: %v: %s", err, raw)
		}
		for _, p := range list {
			res.Players[p.Name] = p.OnlineTime
		}
	}
	return res
}

func spTail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

// aSaveWithTicks makes the world's save one the game has played for a few
// seconds: it starts the world with auto-pause off and stops it, which saves.
// A save made by --create has tick 0, which would prove nothing about
// game.tick. It returns the save's name.
func (e *gameEnv) aSaveWithTicks(t *testing.T, c *Controller, id string) string {
	t.Helper()
	ctx := context.Background()
	settings, err := gamesettings.SetFields(gamesettings.NewServerSettings("a12"), []gamesettings.Field{
		{Key: "auto_pause", JSON: []byte("false")},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(e.worldDir(id), world.ConfigDir, world.ServerSettingsFile)
	if err := os.WriteFile(p, settings, 0o600); err != nil { //nolint:gosec // the test's own world folder
		t.Fatal(err)
	}
	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	save := c.Status(id).Save
	time.Sleep(8 * time.Second) // about 480 ticks
	if err := c.Stop(ctx, id, admin, StopOptions{Countdown: time.Second}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitState(t, c, id, StateStopped, time.Minute)
	if save == "" {
		t.Fatal("the controller does not say which save the game loaded")
	}
	return save
}

// runSavePlayersRead checks one read. source is "fresh" for a fixture-made
// world that is first played for a few seconds, or the path of a ready save
// with players (fixture is then empty).
func (e *gameEnv) runSavePlayersRead(t *testing.T, tag, fixture, version, source string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	var id string
	if fixture != "" {
		id = e.newWorld(t, tag, version, "sp", fixture, map[string]time.Duration{"main.zip": time.Hour})
	} else {
		b, err := os.ReadFile(source) //nolint:gosec // a committed fixture
		if err != nil {
			t.Fatal(err)
		}
		id = e.newWorld(t, tag, version, "sp", "", nil)
		e.putSave(t, id, "main.zip", b, time.Now().Add(-time.Hour))
	}
	saveName := "main.zip"
	if fixture != "" {
		saveName = e.aSaveWithTicks(t, c, id)
	}
	savePath := filepath.Join(e.worldDir(id), world.SavesDir, saveName)

	want := e.oracleRead(t, version, savePath)
	if fixture != "" && want.Tick <= 0 {
		t.Fatalf("the game made a save at tick %d; the test needs a nonzero tick to prove anything", want.Tick)
	}
	before := spWorldState(t, e.worldDir(id))
	e.previewLeftovers(t, "before the read")

	// The read starts from a page view and never waits (SPEC 8.16): ask
	// until it is over.
	var view SavePlayersView
	waitFor(t, 4*time.Minute, "the read of the save", func() bool {
		v, err := c.SavePlayers(ctx, id)
		if err != nil {
			t.Fatalf("SavePlayers: %v", err)
		}
		view = v
		return v.State != SavePlayersReading
	})
	if view.State != SavePlayersReady {
		t.Fatalf("state %q (failure %q), want %q; log tail:\n%s", view.State, view.Failure, SavePlayersReady,
			strings.Join(logLines(c, id), "\n"))
	}

	// game.tick, to the minute the page shows.
	if !view.GameTimeKnown {
		t.Error("the view does not know the game time")
	}
	wantTime := time.Duration(want.Tick) * time.Second / 60
	if d := view.GameTime - wantTime; d < -time.Second || d > time.Second {
		t.Errorf("game time %s, the game says tick %d = %s", view.GameTime, want.Tick, wantTime)
	}
	// Every player the game holds, and no other.
	got := map[string]time.Duration{}
	for _, p := range view.Players {
		if _, dup := got[p.Name]; dup {
			t.Errorf("player %q is listed twice", p.Name)
		}
		got[p.Name] = p.PlayTime
	}
	if len(got) != len(want.Players) {
		t.Errorf("%d players listed, the game holds %d (%v)", len(got), len(want.Players), want.Players)
	}
	for name, ticks := range want.Players {
		pt, ok := got[name]
		if !ok {
			t.Errorf("player %q is missing from the list", name)
			continue
		}
		wantPT := time.Duration(ticks) * time.Second / 60
		if d := pt - wantPT; d < -time.Second || d > time.Second {
			t.Errorf("player %q: play time %s, the game says %d ticks = %s", name, pt, ticks, wantPT)
		}
	}

	// The record on disk is the exact tick.
	raw, err := e.worlds.ReadPlayersRecord(id)
	if err != nil {
		t.Fatalf("no players.json after a good read: %v", err)
	}
	rec, err := saveplayers.Parse(raw)
	if err != nil {
		t.Fatalf("players.json: %v", err)
	}
	if rec.GameTicks != want.Tick {
		t.Errorf("players.json has game_ticks %d, the game says %d", rec.GameTicks, want.Tick)
	}
	if rec.Basis.Save != saveName {
		t.Errorf("players.json is for save %q, the read was of %q", rec.Basis.Save, saveName)
	}
	for _, p := range rec.Players {
		if ticks, ok := want.Players[p.Name]; !ok || p.SeededTicks != ticks {
			t.Errorf("players.json: %q has %d seeded ticks, the game says %d (known %v)", p.Name, p.SeededTicks, ticks, ok)
		}
	}

	// Nothing in the world changed but players.json; the run left nothing.
	after := spWorldState(t, e.worldDir(id))
	spSameWorld(t, before, after, world.PlayersFile)
	if _, ok := after[filepath.Join(world.SavesDir, saveName)]; !ok {
		t.Errorf("the save %s is not in the world after the read", saveName)
	}
	e.previewLeftovers(t, "after the read")
	// The save, again on its own and by the plain game: still readable, same
	// tick (the read did not move the game on).
	if again := e.oracleRead(t, version, savePath); again.Tick != want.Tick {
		t.Errorf("the save reads as tick %d after the read, %d before", again.Tick, want.Tick)
	}
}

// hostileScript is run in a container made like a save read's. It first checks
// that the work folder is writable and the save readable (so the refusals
// below are the mount's, not a container that cannot write at all), then
// tries every way to change the save; each must fail. It exits 0 only when
// all do.
func hostileScript(save string) string {
	q := "'" + save + "'"
	return strings.Join([]string{
		`fail=0`,
		`echo ok > ` + gameenv.PreviewMount + `/control.txt || { echo "CONTROL: the work folder is not writable"; exit 8; }`,
		`head -c 4 ` + q + ` > /dev/null || { echo "CONTROL: the save cannot be read"; exit 8; }`,
		`try() { if "$@" 2>/dev/null; then echo "SUCCEEDED: $*"; fail=1; else echo "refused: $*"; fi; }`,
		`try sh -c 'echo x >> ` + save + `'`,
		`try sh -c 'echo x > ` + save + `'`,
		`try sh -c ': > ` + save + `'`,
		`try truncate -s 0 ` + q,
		`try chmod 777 ` + q,
		`try touch ` + q,
		`try rm -f ` + q,
		`try mv ` + q + ` ` + gameenv.PreviewMount + `/moved.zip`,
		`try dd if=/dev/zero of=` + q + ` bs=1 count=1 conv=notrunc`,
		`exit $fail`,
	}, "\n")
}

// runSavePlayersHostile makes a work folder the way a save read does (the
// world's save hard-linked into it, then mounted read-only over the link) and
// runs the hostile script in the same one-off container spec. The container
// must exit 0 (every attempt refused) and the world's save must not change.
//
// TODO(A12, after A1 merges): build the container with A1's spec function
// instead of this copy of the mount shape.
func (e *gameEnv) runSavePlayersHostile(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	id := e.newWorld(t, tag, version, "hostile", fixture, map[string]time.Duration{"main.zip": time.Hour})
	savePath := filepath.Join(e.worldDir(id), world.SavesDir, "main.zip")
	before := spWorldState(t, e.worldDir(id))

	base, err := c.previewBase()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	dir, err := newWorkDir(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(c.previewPath(), dir)) })
	link := filepath.Join(c.previewPath(), dir, "save.zip")
	if err := os.Link(savePath, link); err != nil {
		t.Fatalf("hard link of the save: %v", err)
	}
	ref, err := c.imageFor(version)
	if err != nil {
		t.Fatal(err)
	}
	spec := docker.ContainerSpec{
		Image:      ref,
		Labels:     map[string]string{gameenv.LabelPreview: c.hostData},
		Env:        []string{gameenv.PreviewKind + "=" + gameenv.KindSavePlayers},
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"-c", hostileScript(gameenv.SaveCopyMount)},
		User:       c.user,
		HostConfig: c.oneOffHostConfigMem(dir, previewMemory),
	}
	spec.HostConfig.Mounts = []docker.BindMount{{
		Type: "bind", Source: filepath.Join(c.hostData, previewDir, dir, "save.zip"),
		Target: gameenv.SaveCopyMount, ReadOnly: true,
	}}

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	name := "foreman-" + gameenv.KindSavePlayers + "-" + dir
	err = c.runOneOff(runCtx, name, spec)
	var pe *PreviewError
	switch {
	case err == nil:
	case errors.As(err, &pe):
		t.Errorf("the hostile run exited %d: %s", pe.ExitCode, pe.Log)
	default:
		t.Fatalf("the hostile run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.previewPath(), dir, "control.txt")); err != nil {
		t.Errorf("the control write left no file: %v", err)
	}

	// The world is exactly as it was: the link shares the save's inode, so a
	// write through it would show here.
	spSameWorld(t, before, spWorldState(t, e.worldDir(id)))
	// The attempts to remove or move the link failed too, so it still is the
	// save's own inode: the test proves what it says.
	linkInfo, lerr := os.Stat(link)
	saveInfo, serr := os.Stat(savePath)
	if lerr != nil || serr != nil || !os.SameFile(linkInfo, saveInfo) {
		t.Errorf("the work folder's link is not the save's inode after the run (%v, %v)", lerr, serr)
	}
}
