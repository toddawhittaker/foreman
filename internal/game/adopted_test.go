package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gamedir"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The adopted container's name and the network the fake puts it on.
const (
	adoptedName = "factorio"
	testNetwork = "games"
)

type adoptedOptions struct {
	env          []string // added to the image's environment
	running      bool     // the container is running when the fake starts
	rconHostPort string
	noRun        bool
	// mount is the folder the container has at /factorio; empty means the
	// rig's game folder.
	mount string
	// prepare fills the game folder before the fake starts the container.
	prepare func(dir string)
	// gameContainer is what Foreman is told to use; empty means adoptedName.
	gameContainer string
	rconPassword  string // FOREMAN_RCON_PASSWORD
}

// newAdoptedRig runs the controller in adopted mode, outside a container,
// on a factoriotools-like container with DLC_SPACE_AGE=false, so the
// rig's worlds (Space Age off) can start. The fake fails the test on any
// call adopted mode never makes (SPEC 4.7).
func newAdoptedRig(t *testing.T, o adoptedOptions) (*rig, string) {
	t.Helper()
	dir := t.TempDir()
	if o.prepare != nil {
		o.prepare(dir)
	}
	mount := dir
	if o.mount != "" {
		mount = o.mount
	}
	r := newRig(t, rigOptions{
		fake: fakegame.Config{SelfNetwork: testNetwork, Adopted: &fakegame.Adopted{
			Name: adoptedName, GameDir: mount, Env: append([]string{"DLC_SPACE_AGE=false"}, o.env...),
			RCONHostPort: o.rconHostPort, Running: o.running,
		}},
		gameDir: dir, noRun: o.noRun, gameContainer: o.gameContainer, rconPassword: o.rconPassword,
	})
	return r, dir
}

// presents fails unless the game folder presents world id (SPEC 4.8): the
// exact link texts, as gamedir reads them.
func presents(t *testing.T, dir, id string) {
	t.Helper()
	st, err := gamedir.Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != gamedir.Ready || st.ActiveID != id {
		t.Fatalf("game folder is %s presenting %q (%s %s), want ready presenting %q", st.Kind, st.ActiveID, st.Path, st.Reason, id)
	}
	if got, _ := os.Readlink(filepath.Join(dir, "current")); got != "worlds/"+id {
		t.Errorf("current links to %q", got)
	}
	for _, name := range gamedir.FixedLinks {
		if got, _ := os.Readlink(filepath.Join(dir, name)); got != "current/"+name {
			t.Errorf("%s links to %q", name, got)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "current.new")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("current.new left behind: %v", err)
	}
}

func (r *rig) adoptedState() fakegame.ContainerState {
	r.t.Helper()
	for _, cs := range r.game.State().Containers {
		if cs.Name == adoptedName {
			return cs
		}
	}
	r.t.Fatal("no adopted container")
	return fakegame.ContainerState{}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestAdoptedStartLinksTheFreshFolderAndRunsTheContainer(t *testing.T) {
	r, dir := newAdoptedRig(t, adoptedOptions{})
	a := r.newWorld("Alpha")
	r.start(a)
	st := r.waitState(a, StateRunning)
	presents(t, dir, a)
	if !st.Adopted || st.RunningVersion != "2.1.24" || st.RCONPublished || st.Layout.Kind != gamedir.Ready {
		t.Errorf("status = adopted %v, version %q, RCON published %v, layout %s", st.Adopted, st.RunningVersion, st.RCONPublished, st.Layout.Kind)
	}
	// The password file is Foreman's, at 0640, before the image reads it (SPEC 4.5, 4.10).
	info, err := os.Stat(filepath.Join(r.worldDir(a), world.RCONPasswordFile))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("rconpw = %v, %v", info, err)
	}
	if err := r.c.Stop(context.Background(), a, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	r.waitState(a, StateStopped)
	// Stopped, never removed (SPEC 4.7).
	if cs := r.adoptedState(); cs.Status != "exited" {
		t.Errorf("container status %q", cs.Status)
	}
	for _, step := range st.Operation.Steps {
		if slices.Contains([]string{StepImage, StepCreate}, step.Name) {
			t.Errorf("adopted start shows step %q", step.Name)
		}
	}
}

func TestAdoptedContainerRefusalsNameTheVariableNotItsValue(t *testing.T) {
	const value = "s3cr3t-value"
	cases := []struct{ env, name string }{
		{"SAVES=/" + value, "SAVES"},
		{"MODS=/" + value, "MODS"},
		{"CONFIG=/" + value, "CONFIG"},
		{"SERVER_SETTINGS_FILE=/" + value, "SERVER_SETTINGS_FILE"},
		{"SERVER_ADMINLIST_FILE=/" + value, "SERVER_ADMINLIST_FILE"},
		{"LOAD_LATEST_SAVE=" + value, "LOAD_LATEST_SAVE"},
		{"LOAD_LATEST_SAVE=false", "LOAD_LATEST_SAVE"},
		{"GENERATE_NEW_SAVE=true", "GENERATE_NEW_SAVE"},
		{"UPDATE_MODS_ON_START=true", "UPDATE_MODS_ON_START"},
		{"PUID=1000", "PUID"},
		{"PGID=1000", "PGID"},
		{"RCON_PORT=" + value, "RCON_PORT"},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			r, _ := newAdoptedRig(t, adoptedOptions{env: []string{tc.env}})
			a := r.newWorld("Alpha")
			err := r.c.Start(context.Background(), a, admin, StartOptions{})
			if !errors.Is(err, ErrContainerRefused) {
				t.Fatalf("start = %v, want a container refusal", err)
			}
			if !strings.Contains(err.Error(), tc.name) || strings.Contains(err.Error(), value) {
				t.Errorf("refusal %q must name %s and never its value", err, tc.name)
			}
			if cs := r.adoptedState(); cs.Status != "exited" {
				t.Errorf("container status %q after a refusal", cs.Status)
			}
		})
	}
}

func TestAdoptedContainerAcceptsTheImageDefaults(t *testing.T) {
	r, _ := newAdoptedRig(t, adoptedOptions{env: []string{"LOAD_LATEST_SAVE=true", "GENERATE_NEW_SAVE=false", "UPDATE_MODS_ON_START=false", "SERVER_PORT=1"}})
	a := r.newWorld("Alpha")
	r.start(a)
}

// SPEC 4.8: only a .new link is evidence of a folder change left part way,
// so a normal start with the game running does not warn.
func TestChangeLeftOver(t *testing.T) {
	r, dir := newAdoptedRig(t, adoptedOptions{noRun: true})
	if r.c.changeLeftOver() {
		t.Error("a folder with no .new link counts as a change left part way")
	}
	if err := os.Symlink("worlds/x", filepath.Join(dir, gamedir.CurrentDir+".new")); err != nil {
		t.Fatal(err)
	}
	if !r.c.changeLeftOver() {
		t.Error("a leftover current.new does not count")
	}
}

// seedContainer adds a container beside the adopted one, valid apart from
// what mounts says.
func (r *rig) seedContainer(name string, mounts []docker.Mount) {
	r.game.Daemon().Seed(dockertest.Container{
		Name: name, Status: "exited", CapDrop: []string{"NET_RAW"},
		Spec:     docker.ContainerSpec{Image: "factoriotools/factorio:2.1.24", Env: []string{"PUID=845", "PGID=845", "DLC_SPACE_AGE=false", "VERSION=2.1.24"}},
		Mounts:   mounts,
		Networks: map[string]string{testNetwork: ""},
	})
}

func TestAdoptedContainerMountRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mounts func(dir string) []docker.Mount
		want   string
	}{
		{"no /factorio", func(string) []docker.Mount { return []docker.Mount{{Source: "/srv", Destination: "/data"}} }, "no mount at /factorio"},
		{"inside /factorio", func(dir string) []docker.Mount {
			return []docker.Mount{{Source: dir, Destination: "/factorio"}, {Source: "/srv", Destination: "/factorio/saves"}}
		}, "/factorio/saves"},
		{"another folder", func(string) []docker.Mount { return []docker.Mount{{Source: "/srv/other", Destination: "/factorio"}} }, "FOREMAN_GAME_DIR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newAdoptedRig(t, adoptedOptions{gameContainer: "other", noRun: true})
			r.seedContainer("other", tc.mounts(dir))
			a := r.newWorld("Alpha")
			err := r.c.Start(context.Background(), a, admin, StartOptions{})
			if !errors.Is(err, ErrContainerRefused) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("start = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}

// TestAdoptedInsideAContainer checks the checks Foreman makes against its
// own container (SPEC 4.4, 4.8): the same folder mounted in both, and a
// network they share, which RCON then uses.
func TestAdoptedInsideAContainer(t *testing.T) {
	cases := []struct {
		name    string
		self    func(dir string) dockertest.Container
		refusal string // "" means the start works
	}{
		{"same folder, shared network", func(dir string) dockertest.Container {
			return dockertest.Container{Mounts: []docker.Mount{{Source: dir, Destination: dir}}, Networks: map[string]string{testNetwork: "10.250.0.9"}}
		}, ""},
		{"no game folder mount", func(string) dockertest.Container {
			return dockertest.Container{Networks: map[string]string{testNetwork: "10.250.0.9"}}
		}, "FOREMAN_GAME_DIR is not a mount"},
		{"another folder", func(dir string) dockertest.Container {
			return dockertest.Container{Mounts: []docker.Mount{{Source: "/srv/other", Destination: dir}}, Networks: map[string]string{testNetwork: "10.250.0.9"}}
		}, "Mount the same folder in both"},
		{"no shared network", func(dir string) dockertest.Container {
			return dockertest.Container{Mounts: []docker.Mount{{Source: dir, Destination: dir}}, Networks: map[string]string{"elsewhere": "10.251.0.9"}}
		}, "shares no Docker network"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newAdoptedRig(t, adoptedOptions{noRun: true})
			// The fake seeds a container for this process when it can; it
			// has the folder and the network, and a second Foreman beside it
			// would be a stranger on the game network (SPEC 4.4).
			hostname, _ := os.Hostname()
			selfID := ""
			if own, ok := r.game.Daemon().Container(hostname); ok && tc.refusal == "" {
				selfID = own.ID
			} else {
				self := tc.self(dir)
				self.Name, self.Status = "foreman-self", "running"
				r.game.Daemon().Seed(self)
				seeded, _ := r.game.Daemon().Container("foreman-self")
				selfID = seeded.ID
			}
			r.c = r.newController(rigOptions{gameDir: dir, selfID: selfID})
			r.run(r.c)
			a := r.newWorld("Alpha")
			err := r.c.Start(context.Background(), a, admin, StartOptions{})
			if tc.refusal == "" {
				if err != nil {
					t.Fatalf("start: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrContainerRefused) || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("start = %v, want a refusal naming %q", err, tc.refusal)
			}
		})
	}
}

func TestAdoptedMissingContainerIsUnknown(t *testing.T) {
	r, _ := newAdoptedRig(t, adoptedOptions{gameContainer: "nowhere"})
	a := r.newWorld("Alpha")
	st := r.waitState(a, StateUnknown)
	if !strings.Contains(st.Reason, "FOREMAN_GAME_CONTAINER") || !strings.Contains(st.Reason, "nowhere") {
		t.Errorf("reason = %q", st.Reason)
	}
	if err := r.c.Start(context.Background(), a, admin, StartOptions{}); !errors.Is(err, ErrNoGameContainer) {
		t.Errorf("start = %v", err)
	}
}

func TestAdoptedFolderStatesRefuseServerControl(t *testing.T) {
	t.Run("unadopted", func(t *testing.T) {
		r, _ := newAdoptedRig(t, adoptedOptions{prepare: unadoptedFolder(t)})
		a := r.newWorld("Alpha")
		if err := r.c.Start(context.Background(), a, admin, StartOptions{}); !errors.Is(err, ErrNotAdopted) {
			t.Fatalf("start = %v, want ErrNotAdopted", err)
		}
		eventually(t, func() bool { return r.c.Status(a).Layout.Kind == gamedir.Unadopted }, "layout unadopted")
	})
	t.Run("broken", func(t *testing.T) {
		r, dir := newAdoptedRig(t, adoptedOptions{prepare: func(dir string) {
			if err := os.Symlink("elsewhere", filepath.Join(dir, "saves")); err != nil {
				t.Fatal(err)
			}
		}})
		a := r.newWorld("Alpha")
		err := r.c.Start(context.Background(), a, admin, StartOptions{})
		var le *LayoutError
		if !errors.As(err, &le) || le.Path != "saves" || !errors.Is(err, ErrGameFolderBroken) {
			t.Fatalf("start = %v, want a layout refusal at saves", err)
		}
		eventually(t, func() bool { return r.c.Status(a).Layout.Kind == gamedir.Broken }, "layout broken")
		if st := r.c.Status(a); st.Layout.Path != "saves" || st.Layout.Reason == "" {
			t.Errorf("layout = %+v", st.Layout)
		}
		if got, _ := os.Readlink(filepath.Join(dir, "saves")); got != "elsewhere" {
			t.Errorf("Foreman changed the broken link to %q", got)
		}
		if err := r.c.ForceStop(context.Background(), a, admin); !errors.Is(err, ErrGameFolderBroken) {
			t.Errorf("force stop = %v", err)
		}
	})
}

func TestAdoptedRestartChecksBeforeStopping(t *testing.T) {
	r, dir := newAdoptedRig(t, adoptedOptions{})
	a := r.newWorld("Alpha")
	r.start(a)
	r.waitState(a, StateRunning)
	// Someone points mods elsewhere while the game runs.
	if err := os.Remove(filepath.Join(dir, "mods")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", filepath.Join(dir, "mods")); err != nil {
		t.Fatal(err)
	}
	err := r.c.Restart(context.Background(), a, admin, StopOptions{}, StartOptions{})
	if !errors.Is(err, ErrGameFolderBroken) {
		t.Fatalf("restart = %v", err)
	}
	if cs := r.adoptedState(); cs.Status != "running" || slices.Contains(cs.Commands, "/server-save") {
		t.Errorf("the refused restart touched the game: %+v", cs)
	}
}

func TestAdoptedSpaceAgeMustMatchTheContainer(t *testing.T) {
	r, _ := newAdoptedRig(t, adoptedOptions{env: []string{"DLC_SPACE_AGE=true"}})
	a := r.newWorld("Alpha")
	err := r.c.Start(context.Background(), a, admin, StartOptions{})
	if !errors.Is(err, ErrSpaceAgeMismatch) || !strings.Contains(err.Error(), "DLC_SPACE_AGE") {
		t.Fatalf("start = %v", err)
	}
}

func TestAdoptedSwitchRefusesASpaceAgeMismatchBeforeStopping(t *testing.T) {
	r, _ := newAdoptedRig(t, adoptedOptions{})
	a := r.newWorld("Alpha")
	b, err := r.worlds.Create(context.Background(), "Beta", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	r.putSave(b, "2.1.24-space-age.zip", "base.zip", time.Now().Add(-time.Hour))
	r.start(a)
	r.waitState(a, StateRunning)
	if err := r.switchWorlds(a, b); !errors.Is(err, ErrSpaceAgeMismatch) {
		t.Fatalf("switch = %v", err)
	}
	r.waitState(a, StateRunning)
	if snaps := r.switchSnapshots(a); len(snaps) != 0 {
		t.Errorf("snapshots taken before a refusal: %+v", snaps)
	}
	r.noRecord()
}

// TestAdoptedSwitchThereAndBack switches A to B and back with the game
// running, checking the folder after each step, and that no world's
// password line shows on the other's log (SPEC 4.8, 7.9, 8.6).
func TestAdoptedSwitchThereAndBack(t *testing.T) {
	r, dir := newAdoptedRig(t, adoptedOptions{})
	a, b := r.twoWorlds()
	r.start(a)
	r.waitState(a, StateRunning)
	presents(t, dir, a)
	pwA := readText(t, filepath.Join(r.worldDir(a), world.RCONPasswordFile))

	if err := r.switchWorlds(a, b); err != nil {
		t.Fatalf("switch to B: %v", err)
	}
	r.waitState(b, StateRunning)
	presents(t, dir, b)
	if r.active() != b {
		t.Errorf("active = %q", r.active())
	}
	pwB := readText(t, filepath.Join(r.worldDir(b), world.RCONPasswordFile))
	if pwA == pwB {
		t.Fatal("the worlds share a password, so the log check proves nothing")
	}
	checkLog(t, r.c.Lines(b), pwA, pwB)
	r.waitState(a, StateStopped)

	if err := r.switchWorlds(b, a); err != nil {
		t.Fatalf("switch back: %v", err)
	}
	r.waitState(a, StateRunning)
	presents(t, dir, a)
	if r.active() != a {
		t.Errorf("active = %q", r.active())
	}
	checkLog(t, r.c.Lines(a), pwB, pwA)
	r.noRecord()
	if n := len(r.switchSnapshots(a)); n != 2 {
		t.Errorf("A has %d switch snapshots, want 2", n)
	}
}

// checkLog fails when other's password, or the world's own unredacted,
// shows in lines, or when the world's own run printed no password line.
func checkLog(t *testing.T, lines []string, other, own string) {
	t.Helper()
	printed := false
	for _, l := range lines {
		if strings.Contains(l, other) || strings.Contains(l, own) {
			t.Errorf("log line shows a password: %q", l)
		}
		printed = printed || strings.Contains(l, "--rcon-password")
	}
	if !printed {
		t.Errorf("no password line in this run's log: %q", lines)
	}
}

func TestAdoptedSwitchWhileStoppedPresentsTheNewWorld(t *testing.T) {
	r, dir := newAdoptedRig(t, adoptedOptions{})
	a, b := r.twoWorlds()
	if err := r.switchWorlds(a, b); err != nil {
		t.Fatal(err)
	}
	presents(t, dir, b)
	if r.active() != b {
		t.Errorf("active = %q", r.active())
	}
	if cs := r.adoptedState(); cs.Status != "exited" {
		t.Errorf("container status %q", cs.Status)
	}
	// B never started, so it has no RCON password yet; a stopped container
	// needs none to be shown as stopped, with Start offered (SPEC 7.1).
	if _, err := os.Lstat(filepath.Join(r.worldDir(b), world.RCONPasswordFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("B has a password file already: %v", err)
	}
	r.c.pokeMonitor()
	looked := func() bool {
		w := r.c.world(b)
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.containerID != "" || w.adoptErr != nil
	}
	eventually(t, looked, "the monitor to look at the container for B")
	if st := r.c.Status(b); st.State != StateStopped || st.Reason != "" {
		t.Errorf("B is %s (%s), want stopped", st.State, st.Reason)
	}
	r.start(b)
	r.waitState(b, StateRunning)
}

func TestAdoptedSwitchPutsTheOldWorldBackWhenTheDatabaseFails(t *testing.T) {
	r, dir := newAdoptedRig(t, adoptedOptions{})
	a, b := r.twoWorlds()
	if err := gamedir.Present(dir, a); err != nil {
		t.Fatal(err)
	}
	r.store.setFail("", true)
	if err := r.switchWorlds(a, b); !errors.Is(err, errDatabase) {
		t.Fatalf("switch = %v", err)
	}
	presents(t, dir, a)
	if r.active() != a {
		t.Errorf("active = %q", r.active())
	}
}

func TestAdoptedRecovery(t *testing.T) {
	ctx := context.Background()
	t.Run("checked, link already on the new world", func(t *testing.T) {
		r, dir := newAdoptedRig(t, adoptedOptions{noRun: true})
		a, b := r.twoWorlds()
		mustPresent(t, dir, a)
		r.recordSwitch(a, b, store.StepChecked)
		mustPresent(t, dir, b) // Foreman stopped before the database write
		if err := r.c.Recover(ctx); err != nil {
			t.Fatal(err)
		}
		n := r.c.Status(b).Notice
		if n == nil || !n.Presented || !strings.Contains(n.Text, "is active") {
			t.Fatalf("notice on B = %+v", n)
		}
		if r.active() != b {
			t.Errorf("active = %q", r.active())
		}
		r.noRecord()
	})
	t.Run("snapshotted, link still on the old world", func(t *testing.T) {
		r, dir := newAdoptedRig(t, adoptedOptions{noRun: true})
		a, b := r.twoWorlds()
		mustPresent(t, dir, a)
		r.recordSwitch(a, b, store.StepSnapshotted)
		if err := r.c.Recover(ctx); err != nil {
			t.Fatal(err)
		}
		if n := r.c.Status(a).Notice; n == nil || n.Presented {
			t.Fatalf("notice on A = %+v", n)
		}
		presents(t, dir, a)
		if r.active() != a {
			t.Errorf("active = %q", r.active())
		}
		r.noRecord()
	})
	t.Run("the link decides the active world", func(t *testing.T) {
		r, dir := newAdoptedRig(t, adoptedOptions{noRun: true})
		a, b := r.twoWorlds()
		mustPresent(t, dir, b)
		if err := r.c.Recover(ctx); err != nil {
			t.Fatal(err)
		}
		if r.active() != b {
			t.Errorf("active = %q, want %q", r.active(), b)
		}
		_ = a
	})
	t.Run("leftover current.new", func(t *testing.T) {
		r, dir := newAdoptedRig(t, adoptedOptions{noRun: true})
		a, b := r.twoWorlds()
		mustPresent(t, dir, a)
		if err := os.Symlink("worlds/"+b, filepath.Join(dir, "current.new")); err != nil {
			t.Fatal(err)
		}
		if err := r.c.Recover(ctx); err != nil {
			t.Fatal(err)
		}
		presents(t, dir, a)
	})
	t.Run("never while the game runs", func(t *testing.T) {
		r, dir := newAdoptedRig(t, adoptedOptions{noRun: true})
		a, b := r.twoWorlds()
		mustPresent(t, dir, a)
		if err := r.docker.StartContainer(ctx, adoptedName); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("worlds/"+b, filepath.Join(dir, "current.new")); err != nil {
			t.Fatal(err)
		}
		if err := r.c.Recover(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "current.new")); err != nil {
			t.Errorf("recovery changed the folder under a running game: %v", err)
		}
		if cs := r.adoptedState(); cs.Status != "running" {
			t.Errorf("recovery stopped the game: %q", cs.Status)
		}
	})
}

func mustPresent(t *testing.T, dir, id string) {
	t.Helper()
	if err := gamedir.Present(dir, id); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) recordSwitch(from, to, step string) {
	r.t.Helper()
	ctx := context.Background()
	if err := r.store.BeginSwitch(ctx, from, to, admin.Username, time.Now()); err != nil {
		r.t.Fatal(err)
	}
	if err := r.store.SetSwitchStep(ctx, from, step); err != nil {
		r.t.Fatal(err)
	}
}

func TestAdoptedRCONPortWarning(t *testing.T) {
	r, _ := newAdoptedRig(t, adoptedOptions{rconHostPort: "27016"})
	a := r.newWorld("Alpha")
	eventually(t, func() bool { return r.c.Status(a).RCONPublished }, "the RCON port warning")
}

func TestAdoptedVersions(t *testing.T) {
	t.Run("a save newer than the container is refused", func(t *testing.T) {
		r, _ := newAdoptedRig(t, adoptedOptions{env: []string{"VERSION=2.0.77"}})
		a := r.newWorld("Alpha") // a 2.1.24 save
		if err := r.c.Start(context.Background(), a, admin, StartOptions{}); !errors.Is(err, ErrSaveNewerThanImage) {
			t.Fatalf("start = %v", err)
		}
	})
	t.Run("an unknown version skips the check", func(t *testing.T) {
		r, _ := newAdoptedRig(t, adoptedOptions{env: []string{"VERSION=latest"}})
		a := r.newWorld("Alpha")
		r.start(a)
		if v := r.c.Status(a).RunningVersion; v != "unknown" {
			t.Errorf("running version %q", v)
		}
		if w, err := r.c.VersionWarning(a); err != nil || w != "" {
			t.Errorf("warning = %q, %v", w, err)
		}
	})
	t.Run("a save from another version is a warning", func(t *testing.T) {
		r, _ := newAdoptedRig(t, adoptedOptions{})
		old, err := r.worlds.Create(context.Background(), "Old", "2.0.77", false)
		if err != nil {
			t.Fatal(err)
		}
		r.putSave(old, "2.0.77-no-space-age.zip", "base.zip", time.Now().Add(-time.Hour))
		eventually(t, func() bool { return r.c.Status(old).RunningVersion == "2.1.24" }, "the running version")
		w, err := r.c.VersionWarning(old)
		if err != nil || !strings.Contains(w, "2.0.77") || !strings.Contains(w, "2.1.24") {
			t.Errorf("warning = %q, %v", w, err)
		}
		same := r.newWorld("Same")
		if w, err := r.c.VersionWarning(same); err != nil || w != "" {
			t.Errorf("warning for a matching save = %q, %v", w, err)
		}
	})
}

func TestAdoptedModeOffersNoImageDownload(t *testing.T) {
	r, _ := newAdoptedRig(t, adoptedOptions{})
	a := r.newWorld("Alpha")
	if err := r.c.PullImage(context.Background(), a, admin); !errors.Is(err, ErrAdoptedImages) {
		t.Errorf("pull = %v", err)
	}
	if _, _, err := r.c.ImageFor(context.Background(), a); !errors.Is(err, ErrAdoptedImages) {
		t.Errorf("image for = %v", err)
	}
}

// SPEC 4.10: with FOREMAN_RCON_PASSWORD set, every start writes it into the
// presented world's config/rconpw at 0640, because the image gives the game
// the password in that file. An older value in the file stays hidden.
func TestAdoptedEnvPasswordIsWrittenBeforeEachStart(t *testing.T) {
	const env, old = "env-pass-4711", "old-file-pw-0815"
	r, dir := newAdoptedRig(t, adoptedOptions{rconPassword: env})
	a, b := r.twoWorlds()
	if err := os.MkdirAll(filepath.Join(r.worldDir(b), world.ConfigDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.worldDir(b), world.RCONPasswordFile), []byte(old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.start(a)
	r.waitState(a, StateRunning)
	if err := r.switchWorlds(a, b); err != nil {
		t.Fatalf("switch: %v", err)
	}
	r.waitState(b, StateRunning)
	presents(t, dir, b)
	for _, id := range []string{a, b} {
		p := filepath.Join(r.worldDir(id), world.RCONPasswordFile)
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0o640 || readText(t, p) != env {
			t.Errorf("%s's rconpw: %v, %v", id, info, err)
		}
	}
	for _, l := range r.c.Lines(b) {
		if strings.Contains(l, env) || strings.Contains(l, old) {
			t.Errorf("log line shows a password: %q", l)
		}
	}
}

// SPEC 3.8, 4.10: a re-attach with FOREMAN_RCON_PASSWORD set uses it, and
// creates no password file in the world.
func TestAdoptedReattachUsesTheEnvPassword(t *testing.T) {
	const env = "env-pass-4711"
	r, dir := newAdoptedRig(t, adoptedOptions{rconPassword: env})
	a := r.newWorld("Alpha")
	r.start(a)
	r.waitState(a, StateRunning)
	r.stop()
	if err := os.Remove(filepath.Join(r.worldDir(a), world.RCONPasswordFile)); err != nil {
		t.Fatal(err)
	}
	r.c = r.newController(rigOptions{gameDir: dir, rconPassword: env})
	r.run(r.c)
	st := r.waitState(a, StateRunning)
	if st.Reason != "" {
		t.Errorf("reason %q", st.Reason)
	}
	if _, err := os.Lstat(filepath.Join(r.worldDir(a), world.RCONPasswordFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("re-attach made a password file: %v", err)
	}
}
