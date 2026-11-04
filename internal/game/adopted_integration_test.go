//go:build game

package game

// These tests run the controller in adopted mode (SPEC 4.7 to 4.12, 7.9,
// 7.10) against a game container the test creates as an operator's compose
// file would: the image's own entrypoint, PUID and PGID set to the test
// runner, the game folder bind-mounted at /factorio, RCON not published.
// Foreman's side runs in the test process, outside any container, so the
// container's /factorio source is compared with FOREMAN_GAME_DIR as a path.
// They are run by `make test-game`, never by `make verify`.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gamedir"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// t14Prefix names every container and network this test makes, so none can
// be mistaken for an operator's.
const t14Prefix = "foreman-e4-t14-"

func TestRealGameAdopted(t *testing.T) {
	// selfContainer treats these files as "Foreman is in a container"; the
	// test plays Foreman outside one, so path comparison must apply.
	for _, f := range []string{docker.DockerEnvPath, docker.PodmanEnvPath} {
		if exists(f) {
			t.Skipf("%s exists: this test plays Foreman outside a container and must run on the Docker host", f)
		}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("the docker command plays the operator and is not on PATH")
	}
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	pid := strconv.Itoa(os.Getpid())
	network := t14Prefix + "net-" + pid
	operator(t, "network", "create", network)
	t.Cleanup(func() { _, _ = dockerCLI("network", "rm", network) })

	total := time.Now()
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		image := g.image
		if v := os.Getenv(g.env); v != "" {
			image = v
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			start := time.Now()
			if _, err := dockerCLI("image", "inspect", image); err != nil {
				t.Fatalf("the game image %s is not on this host. Pull it with docker pull, then run again", image)
			}
			e := newAdoptedEnv(t, g.tag, g.version, image, network, pid)
			e.runImage(t, g.fixture)
			t.Logf("TIMING adopted %s: %s", g.tag, time.Since(start).Round(time.Second))
		})
	}
	t.Logf("TIMING adopted total: %s", time.Since(total).Round(time.Second))
}

// dockerCLI runs the docker command and returns its combined output.
func dockerCLI(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput() //nolint:gosec // fixed arguments built by the test
	return string(out), err
}

// operator runs a docker command the operator would, failing the test on
// error. The output is not printed: docker logs hold the RCON password.
func operator(t *testing.T, args ...string) string {
	t.Helper()
	out, err := dockerCLI(args...)
	if err != nil {
		t.Fatalf("docker %s: %v", args[0], err)
	}
	return out
}

type adoptedEnv struct {
	tag, version, image, network string
	container                    string
	game, data                   string // FOREMAN_GAME_DIR and FOREMAN_DATA_DIR
	uid, gid                     int
	store                        *store.Store
	worlds                       *world.Manager
	audit                        *memAudit
	run                          string
}

func newAdoptedEnv(t *testing.T, tag, version, image, network, pid string) *adoptedEnv {
	t.Helper()
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	run := hex.EncodeToString(b)
	// Under the repository's .cache so the path is the Docker host's path.
	base := filepath.Join(repoRoot(t), ".cache", "test-game", "t14-"+run)
	e := &adoptedEnv{
		tag: tag, version: version, image: image, network: network,
		container: t14Prefix + strings.ReplaceAll(tag, ".", "") + "-" + pid,
		game:      filepath.Join(base, "game"), data: filepath.Join(base, "data"),
		uid: os.Getuid(), gid: os.Getgid(), audit: &memAudit{}, run: run,
	}
	for _, d := range []string{e.game, e.data} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = dockerCLI("rm", "-f", "-v", e.container)
		if err := os.RemoveAll(base); err != nil {
			// The image's chown runs late in its entrypoint; a file it made
			// before then is root's. Remove it as root inside the image.
			_, _ = dockerCLI("run", "--rm", "--entrypoint", "rm", "-v", base+":/t14", image, "-rf", "/t14/game", "/t14/data")
			if err := os.RemoveAll(base); err != nil {
				t.Errorf("could not remove %s: %v", base, err)
			}
		}
	})
	return e
}

// createContainer makes the game container as an operator's compose file
// would. With start, it is also started (docker run -d); without, it is
// only created (docker compose up --no-start).
func (e *adoptedEnv) createContainer(t *testing.T, spaceAge *bool, start bool) {
	t.Helper()
	verb := []string{"create"}
	if start {
		verb = []string{"run", "-d"}
	}
	args := append(verb, "--name", e.container, "--network", e.network,
		"--restart", "unless-stopped",
		"-e", "PUID="+strconv.Itoa(e.uid), "-e", "PGID="+strconv.Itoa(e.gid),
		"-v", e.game+":/factorio")
	if spaceAge != nil {
		args = append(args, "-e", "DLC_SPACE_AGE="+strconv.FormatBool(*spaceAge))
	}
	operator(t, append(args, e.image)...)
}

// containerLog is the container's whole log, as docker logs shows it.
func (e *adoptedEnv) containerLog(t *testing.T) string {
	t.Helper()
	return operator(t, "logs", e.container)
}

func (e *adoptedEnv) containerState(t *testing.T) (id, status string, startedAt time.Time, restarts int) {
	t.Helper()
	out := operator(t, "inspect", "--format", "{{.Id}} {{.State.Status}} {{.State.StartedAt}} {{.RestartCount}}", e.container)
	f := strings.Fields(out)
	if len(f) != 4 {
		t.Fatalf("docker inspect gave %q", out)
	}
	at, err := time.Parse(time.RFC3339Nano, f[2])
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(f[3])
	return f[0], f[1], at, n
}

// waitInGame waits until the container's log has n lines saying the game
// reached InGame.
func (e *adoptedEnv) waitInGame(t *testing.T, n int, limit time.Duration) {
	t.Helper()
	waitFor(t, limit, fmt.Sprintf("InGame line %d in the container's log", n), func() bool {
		out, _ := dockerCLI("logs", e.container)
		return strings.Count(out, "to(InGame)") >= n
	})
}

func (e *adoptedEnv) controller(t *testing.T) *Controller {
	t.Helper()
	dc, err := docker.NewAdopted(cmpOr(os.Getenv("DOCKER_HOST"), "unix:///var/run/docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{
		Docker: dc, Worlds: e.worlds, Audit: e.audit, Switches: e.store,
		GameUID: e.uid, GameGID: e.gid,
		GameContainer: e.container, GameDir: e.game,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.pollInterval = time.Second
	return c
}

func runMonitor(t *testing.T, c *Controller) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { cancel(); <-done }) })
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// fileSum is one file in a manifest. A link has only its target.
type fileSum struct {
	size   int64
	sha    string
	mod    time.Time
	target string
}

// manifest lists every file and link under dir by relative path, never
// following a link.
func manifest(t *testing.T, dir string) map[string]fileSum {
	t.Helper()
	out := map[string]fileSum{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error { //nolint:gosec // the test's own folder
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[rel] = fileSum{target: target}
		case d.Type().IsRegular():
			b, err := os.ReadFile(p) //nolint:gosec // the test's own folder
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			out[rel] = fileSum{size: int64(len(b)), sha: hex.EncodeToString(sum[:]), mod: info.ModTime()}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("manifest of %s: %v", dir, err)
	}
	return out
}

func sameContent(a, b fileSum) bool {
	return a.size == b.size && a.sha == b.sha && a.target == b.target
}

// requireSame fails unless every file in want is in got with the same
// content, and got has nothing else.
func requireSame(t *testing.T, what string, want, got map[string]fileSum) {
	t.Helper()
	for name, w := range want {
		g, ok := got[name]
		switch {
		case !ok:
			t.Errorf("%s: %s is gone", what, name)
		case !sameContent(w, g):
			t.Errorf("%s: %s changed (size %d to %d)", what, name, w.size, g.size)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: %s is new", what, name)
		}
	}
}

func isAutosave(name string) bool { return strings.HasPrefix(name, "_autosave") }

// requireSavesKept checks a world's saves across a step: none is lost, and
// only the save its stop wrote and the game's autosaves may change or
// appear (SPEC 7.6, 7.9).
func requireSavesKept(t *testing.T, what string, before, after map[string]fileSum, stopSaved string) {
	t.Helper()
	for name, b := range before {
		a, ok := after[name]
		switch {
		case !ok:
			t.Errorf("%s: save %s was lost", what, name)
		case !sameContent(a, b) && name != stopSaved && !isAutosave(name):
			t.Errorf("%s: save %s changed", what, name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok && !isAutosave(name) {
			t.Errorf("%s: save %s appeared", what, name)
		}
	}
}

// newest is the save the image's --start-server-load-latest picks.
func newest(saves map[string]fileSum) string {
	best := ""
	for name, s := range saves {
		if best == "" || s.mod.After(saves[best].mod) {
			best = name
		}
	}
	return best
}

func (e *adoptedEnv) worldSaves(t *testing.T, id string) map[string]fileSum {
	t.Helper()
	return manifest(t, filepath.Join(e.game, gamedir.WorldsDir, id, world.SavesDir))
}

// password reads a world's RCON password file, as the image does.
func (e *adoptedEnv) password(t *testing.T, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.game, gamedir.WorldsDir, id, world.RCONPasswordFile)) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	pw := strings.TrimSpace(string(b))
	if len(pw) < 8 {
		t.Fatalf("world %s's RCON password file holds %d characters, too few to scan for", id, len(pw))
	}
	return pw
}

// requireNoSecret checks everything the controller publishes for each
// world (its lines, its Status) and the audit log for the passwords
// (SPEC 8.6, 14.8). Neither value is printed.
func (e *adoptedEnv) requireNoSecret(t *testing.T, c *Controller, ids []string, passwords ...string) {
	t.Helper()
	for _, id := range ids {
		v, _, stop := c.Watch(id)
		stop()
		status, err := json.Marshal(v.Status)
		if err != nil {
			t.Fatal(err)
		}
		for i, pw := range passwords {
			for n, l := range v.Lines {
				if strings.Contains(l, pw) {
					t.Errorf("world %s: log line %d of %d holds RCON password %d", id, n+1, len(v.Lines), i+1)
				}
			}
			if bytes.Contains(status, []byte(pw)) {
				t.Errorf("world %s: its Status holds RCON password %d", id, i+1)
			}
		}
	}
	audit, err := json.Marshal(e.audit.list())
	if err != nil {
		t.Fatal(err)
	}
	for i, pw := range passwords {
		if bytes.Contains(audit, []byte(pw)) {
			t.Errorf("the audit log holds RCON password %d", i+1)
		}
	}
}

func countLines(c *Controller, id, text string) int {
	n := 0
	for _, l := range logLines(c, id) {
		if strings.Contains(l, text) {
			n++
		}
	}
	return n
}

// requirePresents checks the game folder presents world id with exactly
// the links SPEC 4.8 names, and the database agrees.
func (e *adoptedEnv) requirePresents(t *testing.T, id string) {
	t.Helper()
	st, err := gamedir.Inspect(e.game)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != gamedir.Ready || st.ActiveID != id {
		t.Fatalf("the game folder is %s presenting %q (%s %s), want ready presenting %s", st.Kind, st.ActiveID, st.Path, st.Reason, id)
	}
	links := map[string]string{gamedir.CurrentDir: gamedir.WorldsDir + "/" + id}
	for _, name := range gamedir.FixedLinks {
		links[name] = gamedir.CurrentDir + "/" + name
	}
	for name, want := range links {
		got, err := os.Readlink(filepath.Join(e.game, name))
		if err != nil || got != want {
			t.Errorf("%s links to %q (%v), want %q", name, got, err, want)
		}
	}
	active, found, err := e.store.ActiveWorld(context.Background())
	if err != nil || !found || active != id {
		t.Errorf("the database's active world is %q (found %v, %v), want %s", active, found, err, id)
	}
}

// requireInGame checks the game runs world id on the save named, from the
// controller's view and from the game's own log line.
func requireInGame(t *testing.T, c *Controller, id, save, version string) {
	t.Helper()
	st := waitState(t, c, id, StateRunning, 2*time.Minute)
	if st.Save != save {
		t.Errorf("world %s loaded %q, want %q", id, st.Save, save)
	}
	if st.RunningVersion != version || st.RCONPublished || !st.Adopted {
		t.Errorf("running version %q, RCON published %v, adopted %v; want %q, false, true", st.RunningVersion, st.RCONPublished, st.Adopted, version)
	}
	waitLog(t, c, id, loadingLine(id, save))
	waitLog(t, c, id, "to(InGame)")
}

// loadingLine is how the real game logs the load of a world's save: it
// resolves the saves link, so the line names the world's own folder.
func loadingLine(id, save string) string {
	return "Loading map /factorio/" + gamedir.WorldsDir + "/" + id + "/" + world.SavesDir + "/" + save + ":"
}

// preForeman returns the one pre-foreman folder.
func (e *adoptedEnv) preForeman(t *testing.T) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(e.game, gamedir.PreForemanPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d pre-foreman folders, want 1", len(matches))
	}
	return matches[0]
}

// originals lists the original saves, mods and config, with paths as they
// are inside the pre-foreman folder.
func originals(t *testing.T, dir string) map[string]fileSum {
	t.Helper()
	out := map[string]fileSum{}
	for _, sub := range gamedir.FixedLinks {
		for name, s := range manifest(t, filepath.Join(dir, sub)) {
			out[filepath.Join(sub, name)] = s
		}
	}
	return out
}

func (e *adoptedEnv) runImage(t *testing.T, fixture string) {
	ctx := context.Background()
	twoX := strings.HasPrefix(e.version, "2.")
	parent := t // the store and the monitor outlive each step
	step := func(name string, f func(t *testing.T)) {
		t.Helper()
		begin := time.Now()
		ok := t.Run(name, f)
		t.Logf("TIMING adopted %s %s: %s", e.tag, name, time.Since(begin).Round(100*time.Millisecond))
		if !ok {
			t.FailNow() // later steps depend on this one
		}
	}
	// One adopted container serves worlds of one Space Age setting; the
	// fixtures have Space Age off, so the 2.x container turns it off.
	var dlc *bool
	if twoX {
		off := false
		dlc = &off
	}
	fixtureBytes, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}

	var (
		c          *Controller
		a, b       string // world ids
		pwA, pwB   string
		kept       map[string]fileSum // the original folders before adoption, less the save the stop writes
		pre        string
		preAtStart map[string]fileSum
		cid        string
	)

	step("0 the operator's game makes its own map", func(t *testing.T) {
		e.createContainer(t, dlc, true)
		e.waitInGame(t, 1, 3*time.Minute)
		if _, err := os.Stat(filepath.Join(e.game, world.SavesDir, "_autosave1.zip")); err != nil { //nolint:gosec // the test's own folder
			t.Fatalf("the image made no map: %v", err)
		}
		// A save the stop will not write, to show the originals are kept
		// byte for byte. It is older, so the game keeps loading its own.
		p := filepath.Join(e.game, world.SavesDir, "keep.zip")
		if err := os.WriteFile(p, fixtureBytes, 0o600); err != nil { //nolint:gosec // the test's own folder
			t.Fatal(err)
		}
		old := time.Now().Add(-24 * time.Hour)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		kept = originals(t, e.game)
		delete(kept, filepath.Join(world.SavesDir, "_autosave1.zip"))
		cid, _, _, _ = e.containerState(t)
	})

	step("1 Foreman starts on the unadopted folder without touching the game", func(t *testing.T) {
		st, err := store.Open(ctx, filepath.Join(e.data, "foreman.db"))
		if err != nil {
			t.Fatal(err)
		}
		parent.Cleanup(func() { _ = st.Close() })
		e.store = st
		e.worlds, err = world.NewManager(world.Dirs{
			Worlds:    filepath.Join(e.game, gamedir.WorldsDir),
			Snapshots: datadir.Snapshots(e.data), Imports: datadir.Imports(e.data),
		}, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _, startedBefore, _ := e.containerState(t)
		c = e.controller(t)
		// Startup's recovery never starts or stops a game (SPEC 3.8).
		if err := c.Recover(ctx); err != nil {
			t.Fatalf("recover: %v", err)
		}
		runMonitor(parent, c)
		id, status, startedAt, restarts := e.containerState(t)
		if id != cid || status != "running" || !startedAt.Equal(startedBefore) || restarts != 0 {
			t.Fatalf("startup touched the game: id same %v, status %s, restarted %v, restarts %d", id == cid, status, !startedAt.Equal(startedBefore), restarts)
		}
		d, err := c.AdoptionDefaults(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Unadopted || !d.Running || d.GameVersion != e.version {
			t.Fatalf("adoption defaults %+v, want unadopted, running, version %s", d, e.version)
		}
		if twoX && d.SpaceAge {
			t.Fatal("the form would default Space Age on for a container with DLC_SPACE_AGE=false")
		}
		t.Logf("EVIDENCE the adoption form defaults Space Age to %v on %s", d.SpaceAge, e.tag)
	})

	step("2 adoption keeps the originals and runs the adopted world (SPEC 4.8)", func(t *testing.T) {
		// The form's default for 1.1 is irrelevant: 1.1 has no Space Age.
		adoption, err := c.ReserveAdoption(ctx, "t14 A "+e.tag+" "+e.run, e.version, false)
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}
		a = adoption.ID
		began := time.Now()
		if err := c.Adopt(ctx, adoption, admin, StopOptions{Countdown: 2 * time.Second}); err != nil {
			t.Fatalf("adopt: %v", err)
		}
		t.Logf("adoption, with its stop and start, took %s", time.Since(began).Round(100*time.Millisecond))
		requireInGame(t, c, a, "_autosave1.zip", e.version)
		e.requirePresents(t, a)

		pre = e.preForeman(t)
		preAtStart = originals(t, pre)
		got := maps.Clone(preAtStart)
		delete(got, filepath.Join(world.SavesDir, "_autosave1.zip"))
		requireSame(t, "the pre-foreman folder", kept, got)
		// The save the stop wrote is in both, as one file (SPEC 4.8).
		p1, err := os.Stat(filepath.Join(pre, world.SavesDir, "_autosave1.zip"))
		if err != nil {
			t.Fatal(err)
		}
		p2, err := os.Stat(filepath.Join(e.game, gamedir.WorldsDir, a, world.SavesDir, "_autosave1.zip")) //nolint:gosec // the test's own folder
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(p1, p2) {
			t.Error("the adopted save is not a hard link to the original")
		}
		if s := e.worldSaves(t, a); len(s) != 2 || !sameContent(s["keep.zip"], kept[filepath.Join(world.SavesDir, "keep.zip")]) {
			t.Errorf("world A's saves are %v, want keep.zip unchanged and _autosave1.zip", slices.Sorted(maps.Keys(s)))
		}
		snaps, err := e.worlds.Snapshots(a)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(snaps, func(s world.Snapshot) bool { return s.Reason == world.ReasonAdoption }) {
			t.Error("no Before adoption snapshot")
		}
		// Foreman restarted the operator's container; it never recreates it (SPEC 4.7).
		if id, status, _, restarts := e.containerState(t); id != cid || status != "running" || restarts != 0 {
			t.Errorf("container same %v, status %s, restarts %d", id == cid, status, restarts)
		}
		pwA = e.password(t, a)
		if !strings.Contains(e.containerLog(t), pwA) {
			t.Fatal("the image's set -x did not print world A's RCON password, so the redaction check would prove nothing")
		}
		e.requireNoSecret(t, c, []string{a}, pwA)
	})

	step("3 a second world from the import folder", func(t *testing.T) {
		dir := filepath.Join(datadir.Imports(e.data), "t14b")
		if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // the test's own folder
			t.Fatal(err)
		}
		p := filepath.Join(dir, "beta.zip")
		if err := os.WriteFile(p, fixtureBytes, 0o600); err != nil { //nolint:gosec // the test's own folder
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		b, err = e.worlds.ImportFolder(ctx, "t14b", "t14 B "+e.tag+" "+e.run, e.version, false)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		e.requirePresents(t, a)
	})

	// switchTo switches from one world to the other with the game running
	// and checks every save of both worlds across it.
	switchTo := func(t *testing.T, from, to string) {
		t.Helper()
		before := map[string]map[string]fileSum{from: e.worldSaves(t, from), to: e.worldSaves(t, to)}
		stopSaved := c.Status(from).Save
		want := newest(before[to])
		began := time.Now()
		if err := c.Switch(ctx, from, to, admin, StopOptions{Countdown: 2 * time.Second}, StartOptions{}); err != nil {
			t.Fatalf("switch: %v", err)
		}
		t.Logf("switch took %s", time.Since(began).Round(100*time.Millisecond))
		requireInGame(t, c, to, want, e.version)
		e.requirePresents(t, to)
		if st := c.Status(from); st.State != StateStopped {
			t.Errorf("world %s is %s after the switch, want stopped", from, st.State)
		}
		requireSavesKept(t, "world "+from, before[from], e.worldSaves(t, from), stopSaved)
		requireSavesKept(t, "world "+to, before[to], e.worldSaves(t, to), "")
		// The graceful stop saved the old world (SPEC 7.6).
		if after := e.worldSaves(t, from)[stopSaved]; !after.mod.After(before[from][stopSaved].mod) {
			t.Errorf("the stop did not write %s", stopSaved)
		}
		// The game writing a save must not reach the kept originals through
		// a hard link (SPEC 4.8).
		requireSame(t, "the pre-foreman folder", preAtStart, originals(t, pre))
		if id, _, _, _ := e.containerState(t); id != cid {
			t.Error("the game container was recreated")
		}
		if n := len(e.mustSwitches(t)); n != 0 {
			t.Errorf("%d switch records are left after the switch", n)
		}
	}

	step("4 switch A to B", func(t *testing.T) {
		worldB := filepath.Join(e.game, gamedir.WorldsDir, b)
		filesB := manifest(t, worldB)
		switchTo(t, a, b)
		// What the image's entrypoint writes into a world it starts on.
		after := manifest(t, worldB)
		for _, name := range slices.Sorted(maps.Keys(after)) {
			if before, ok := filesB[name]; !ok || !sameContent(before, after[name]) {
				t.Logf("EVIDENCE the first start on world B wrote %s (new %v)", name, !ok)
			}
		}
		pwB = e.password(t, b)
		if pwB == pwA {
			t.Fatal("worlds A and B have the same RCON password")
		}
		// The log follows this run only (SPEC 4.10, 8.6), so A's lines, and
		// with them A's password line, are not on B's page.
		if logHas(c, b, "/factorio/"+gamedir.WorldsDir+"/"+a+"/") {
			t.Error("world B's log shows world A's run")
		}
		if n, m := countLines(c, b, "--rcon-password "+Mask), countLines(c, a, "--rcon-password "+Mask); n == 0 || n > m {
			t.Errorf("world B's log has %d masked password lines, world A's run had %d", n, m)
		}
		log := e.containerLog(t)
		if !strings.Contains(log, pwA) || !strings.Contains(log, pwB) {
			t.Fatal("the container's log lacks a world's password, so the redaction check would prove nothing")
		}
		e.requireNoSecret(t, c, []string{a, b}, pwA, pwB)
	})

	step("5 switch B back to A", func(t *testing.T) {
		switchTo(t, b, a)
		if logHas(c, a, "/factorio/"+gamedir.WorldsDir+"/"+b+"/") {
			t.Error("world A's log shows world B's run")
		}
		e.requireNoSecret(t, c, []string{a, b}, pwA, pwB)
		// PUID and PGID match Foreman's, so the image's chown leaves every
		// file usable by both (SPEC 3.6).
		e.requireOwned(t)
	})

	if !twoX {
		return
	}
	step("6 a container whose DLC_SPACE_AGE disagrees is refused before anything changes (SPEC 7.4)", func(t *testing.T) {
		if err := c.Stop(ctx, a, admin, StopOptions{Countdown: 2 * time.Second}); err != nil {
			t.Fatalf("stop: %v", err)
		}
		// The operator recreates the container with Space Age on, not yet started.
		operator(t, "rm", "-v", e.container)
		on := true
		e.createContainer(t, &on, false)
		newID, _, _, _ := e.containerState(t)
		worldsBefore := manifest(t, filepath.Join(e.game, gamedir.WorldsDir))
		snapsA, _ := e.worlds.Snapshots(a)
		snapsB, _ := e.worlds.Snapshots(b)

		if err := c.Start(ctx, a, admin, StartOptions{}); !errors.Is(err, ErrSpaceAgeMismatch) {
			t.Errorf("start error = %v, want ErrSpaceAgeMismatch", err)
		}
		if err := c.Switch(ctx, a, b, admin, StopOptions{Countdown: 2 * time.Second}, StartOptions{}); !errors.Is(err, ErrSpaceAgeMismatch) {
			t.Errorf("switch error = %v, want ErrSpaceAgeMismatch", err)
		}
		id, status, _, _ := e.containerState(t)
		if id != newID || status != "created" {
			t.Errorf("container same %v, status %s, want created and never started", id == newID, status)
		}
		e.requirePresents(t, a)
		requireSame(t, "the worlds folder", worldsBefore, manifest(t, filepath.Join(e.game, gamedir.WorldsDir)))
		requireSame(t, "the pre-foreman folder", preAtStart, originals(t, pre))
		if sa, _ := e.worlds.Snapshots(a); len(sa) != len(snapsA) {
			t.Errorf("world A has %d snapshots, had %d", len(sa), len(snapsA))
		}
		if sb, _ := e.worlds.Snapshots(b); len(sb) != len(snapsB) {
			t.Errorf("world B has %d snapshots, had %d", len(sb), len(snapsB))
		}
		if n := len(e.mustSwitches(t)); n != 0 {
			t.Errorf("%d switch records are left after the refusal", n)
		}
		e.requireNoSecret(t, c, []string{a, b}, pwA, pwB)
	})
}

func (e *adoptedEnv) requireOwned(t *testing.T) {
	t.Helper()
	err := filepath.WalkDir(e.game, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && (int(st.Uid) != e.uid || int(st.Gid) != e.gid) {
			rel, _ := filepath.Rel(e.game, p)
			t.Errorf("%s is owned by %d:%d, want %d:%d", rel, st.Uid, st.Gid, e.uid, e.gid)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *adoptedEnv) mustSwitches(t *testing.T) []store.Switch {
	t.Helper()
	s, err := e.store.ListSwitches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
