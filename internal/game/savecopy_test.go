package game

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	saveplayersmod "github.com/toddawhittaker/foreman/internal/game/saveplayers"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// testSaveCopyMod is a players read whose output the test writes itself.
// Its active mods come from the output file, a JSON list of mods.
var testSaveCopyMod = saveCopyMod{
	controlMod: controlMod{
		kind:      gameenv.KindSavePlayers,
		what:      "players",
		name:      saveplayersmod.ModName,
		dir:       saveplayersmod.ModDir,
		control:   saveplayersmod.Control,
		output:    gameenv.SavePlayersFile,
		maxOutput: saveplayers.MaxOutputBytes,
	},
	activeMods: func(base *os.Root, dir string) ([]saveplayers.Mod, error) {
		root, err := base.OpenRoot(dir)
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		b, err := world.ReadSmallFile(root, gameenv.SavePlayersFile, saveplayers.MaxOutputBytes)
		if err != nil {
			return nil, err
		}
		var mods []saveplayers.Mod
		return mods, json.Unmarshal(b, &mods)
	},
}

// saveCopyRig is a rig whose preview containers stay up until the test ends
// them, and a 2.1.24 world with a mod, a mod list and mod settings.
type saveCopyRig struct {
	*rig
	id string
}

func newSaveCopyRig(t *testing.T) *saveCopyRig {
	t.Helper()
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	id := r.newWorld("Saves")
	if err := r.worlds.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	r.putModZip(id, "fmtest", "2.1")
	r.writeFile(id, filepath.Join(world.ModsDir, world.ModSettingsFile), []byte("settings"))
	r.writeFile(id, "server-settings.json", []byte(`{"game_password":"secret"}`))
	return &saveCopyRig{rig: r, id: id}
}

type saveCopyOutcome struct {
	basis saveplayers.Basis
	err   error
	read  []byte
}

// ask runs a read in the background; the read function keeps the output.
func (r *saveCopyRig) ask(ctx context.Context, m saveCopyMod) <-chan saveCopyOutcome {
	ch := make(chan saveCopyOutcome, 1)
	go func() {
		var out []byte
		b, err := r.c.runSaveCopy(ctx, r.id, m, func(base *os.Root, dir string) error {
			root, err := base.OpenRoot(dir)
			if err != nil {
				return err
			}
			defer func() { _ = root.Close() }()
			out, err = world.ReadSmallFile(root, m.output, m.maxOutput)
			return err
		})
		ch <- saveCopyOutcome{b, err, out}
	}()
	return ch
}

func (r *saveCopyRig) outcome(ch <-chan saveCopyOutcome) saveCopyOutcome {
	r.t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(10 * time.Second):
		r.t.Fatal("runSaveCopy did not return")
		return saveCopyOutcome{}
	}
}

func (r *saveCopyRig) waitRunning() dockertest.Container {
	r.t.Helper()
	var found dockertest.Container
	eventually(r.t, func() bool {
		for _, c := range r.previewContainers() {
			if c.Status == "running" {
				found = c
				return true
			}
		}
		return false
	}, "a running save read container")
	return found
}

func (r *saveCopyRig) workDir(c dockertest.Container) string {
	r.t.Helper()
	folder, ok := strings.CutPrefix(c.Name, "foreman-"+gameenv.KindSavePlayers+"-")
	if !ok {
		r.t.Fatalf("container name %q", c.Name)
	}
	return filepath.Join(r.data, previewDir, folder)
}

// finish writes the output as the helper would and ends the container.
func (r *saveCopyRig) finish(c dockertest.Container, mods []saveplayers.Mod) {
	r.t.Helper()
	b, err := json.Marshal(mods)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.workDir(c), gameenv.SavePlayersFile), b, 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.game.Daemon().Exit(c.ID, 0, false)
}

// worldMods is what the rig's world loads.
var worldMods = []saveplayers.Mod{{Name: "base", Version: "2.1.24"}, {Name: "recycler", Version: "2.1.24"}, {Name: "fmtest", Version: "1.0.0"}}

func sameInode(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := os.Lstat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(ai, bi)
}

// The original save never changes (SPEC 8.16): the container sees it only as
// a read-only mount over a hard link in the work folder, nothing is bound from
// the worlds folder, and mod-list.json and mod-settings.dat are copies.
func TestSaveCopyMountsTheSaveReadOnlyAndCopiesTheModFiles(t *testing.T) {
	r := newSaveCopyRig(t)
	ch := r.ask(context.Background(), testSaveCopyMod)
	c := r.waitRunning()
	work := r.workDir(c)
	wd := r.worldDir(r.id)

	if !sameInode(t, filepath.Join(work, saveCopySave), filepath.Join(wd, world.SavesDir, "base.zip")) {
		t.Error("the work folder's save is not a hard link to the world's save")
	}
	saveMounted := false
	for _, m := range c.Spec.HostConfig.Mounts {
		if m.Target == gameenv.SaveCopyMount {
			saveMounted = m.ReadOnly && m.Source == filepath.Join(work, saveCopySave)
		}
		if strings.HasSuffix(m.Target, ".zip") && !m.ReadOnly {
			t.Errorf("mount %+v is writable", m)
		}
	}
	if !saveMounted {
		t.Errorf("no read-only mount of the save's link at %s: %+v", gameenv.SaveCopyMount, c.Spec.HostConfig.Mounts)
	}
	worlds := r.worlds.Dirs().Worlds + string(filepath.Separator)
	for _, b := range c.Spec.HostConfig.Binds {
		if strings.HasPrefix(b, worlds) {
			t.Errorf("bind %q is under the worlds folder", b)
		}
	}
	for _, m := range c.Spec.HostConfig.Mounts {
		if strings.HasPrefix(m.Source, worlds) {
			t.Errorf("mount %+v is from the worlds folder", m)
		}
	}
	for _, f := range []string{world.ModListFile, world.ModSettingsFile} {
		got, want := filepath.Join(work, world.ModsDir, f), filepath.Join(wd, world.ModsDir, f)
		if sameInode(t, got, want) {
			t.Errorf("%s in the work folder is a link to the world's", f)
		}
	}
	b, err := os.ReadFile(filepath.Join(work, world.ModsDir, world.ModSettingsFile)) //nolint:gosec // the test's own work folder
	if err != nil || string(b) != "settings" {
		t.Errorf("mod-settings.dat in the work folder = %q, %v", b, err)
	}
	if !sameInode(t, filepath.Join(work, world.ModsDir, "fmtest_1.0.0.zip"), filepath.Join(wd, world.ModsDir, "fmtest_1.0.0.zip")) {
		t.Error("the mod zip is not linked")
	}
	_ = filepath.WalkDir(work, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch d.Name() {
		case "config", "rconpw", "server-settings.json":
			t.Errorf("the work folder holds %s", p)
		}
		return nil
	})
	hc := c.Spec.HostConfig
	if hc.NetworkMode != "none" || !hc.ReadonlyRootfs || hc.Memory != SaveCopyMemory || hc.MemorySwap != SaveCopyMemory {
		t.Errorf("host config %+v is not the locked-down save read shape", hc)
	}
	// The game writes only to memory: a 1 GiB tmpfs, inside the memory limit.
	if got := hc.Tmpfs[previewWrite]; !strings.Contains(got, "size=1g") || len(hc.Tmpfs) != 1 {
		t.Errorf("tmpfs %q", hc.Tmpfs)
	}
	if got := c.Spec.Env; len(got) != 1 || got[0] != gameenv.PreviewKind+"="+gameenv.KindSavePlayers {
		t.Errorf("env %q", got)
	}

	r.finish(c, append(worldMods, saveplayers.Mod{Name: saveplayersmod.ModName, Version: saveplayersmod.ModVersion}))
	o := r.outcome(ch)
	if o.err != nil {
		t.Fatal(o.err)
	}
	target, _, err := r.worlds.LoadTarget(r.id)
	if err != nil {
		t.Fatal(err)
	}
	if o.basis.Save != "base.zip" || o.basis.Size != target.Size || !o.basis.Modified.Equal(target.ModTime) {
		t.Errorf("basis %+v, want the load target %+v", o.basis, target)
	}
	if len(o.read) == 0 {
		t.Error("the read function saw no output")
	}
	eventually(t, func() bool { return len(r.previewContainers()) == 0 && len(r.previewFolders()) == 0 },
		"the container and work folder to be removed")
}

// The game drops a missing mod silently and exits 0, so the mods it reports
// must be the world's (SPEC 8.16).
func TestSaveCopyRefusesOtherActiveMods(t *testing.T) {
	for name, mods := range map[string][]saveplayers.Mod{
		"one missing":   worldMods[:2],
		"one extra":     append(append([]saveplayers.Mod{}, worldMods...), saveplayers.Mod{Name: "other", Version: "1.0.0"}),
		"other version": append(append([]saveplayers.Mod{}, worldMods[:2]...), saveplayers.Mod{Name: "fmtest", Version: "1.0.1"}),
	} {
		t.Run(name, func(t *testing.T) {
			r := newSaveCopyRig(t)
			ch := r.ask(context.Background(), testSaveCopyMod)
			r.finish(r.waitRunning(), mods)
			if o := r.outcome(ch); !errors.Is(o.err, ErrSaveCopyModsDiffer) {
				t.Errorf("err = %v, want ErrSaveCopyModsDiffer", o.err)
			}
		})
	}
}

// The pre-start check runs first, and no container runs when it finds a
// problem (SPEC 8.16).
func TestSaveCopyPreCheckRefusesWithoutAContainer(t *testing.T) {
	t.Run("mod missing", func(t *testing.T) {
		r := newSaveCopyRig(t)
		r.putSave(r.id, "2.1.24-space-age-testmod.zip", "zmods.zip", time.Now())
		o := r.outcome(r.ask(context.Background(), testSaveCopyMod))
		if !errors.Is(o.err, ErrSaveCopyModsDiffer) {
			t.Errorf("err = %v, want ErrSaveCopyModsDiffer", o.err)
		}
		if n := len(r.game.Daemon().Containers()); n != 0 {
			t.Errorf("%d containers were made", n)
		}
	})
	t.Run("save newer", func(t *testing.T) {
		r := newSaveCopyRig(t)
		id, err := r.worlds.Create(context.Background(), "Old", "2.0.77", false)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.worlds.EnsureModList(id); err != nil {
			t.Fatal(err)
		}
		r.putSave(id, "2.1.24-no-space-age.zip", "new.zip", time.Now())
		r.id = id
		o := r.outcome(r.ask(context.Background(), testSaveCopyMod))
		if !errors.Is(o.err, ErrSaveCopySaveNewer) {
			t.Errorf("err = %v, want ErrSaveCopySaveNewer", o.err)
		}
		if n := len(r.game.Daemon().Containers()); n != 0 {
			t.Errorf("%d containers were made", n)
		}
	})
}

func TestSaveCopyWordsAFailedGame(t *testing.T) {
	for name, tc := range map[string]struct {
		code int
		oom  bool
		want error
	}{
		"out of memory": {137, true, ErrSaveCopyOutOfMemory},
		"exit 1":        {1, false, ErrSaveCopyBadOutput},
	} {
		t.Run(name, func(t *testing.T) {
			r := newSaveCopyRig(t)
			ch := r.ask(context.Background(), testSaveCopyMod)
			r.game.Daemon().Exit(r.waitRunning().ID, tc.code, tc.oom)
			if o := r.outcome(ch); !errors.Is(o.err, tc.want) {
				t.Errorf("err = %v, want %v", o.err, tc.want)
			}
		})
	}
	if err := saveCopyError(ErrPreviewTimeout); !errors.Is(err, ErrSaveCopyTimeout) {
		t.Errorf("a timeout is %v", err)
	}
	if err := saveCopyError(ErrPreviewImageMissing); !errors.Is(err, ErrSaveCopyImageMissing) {
		t.Errorf("a missing image is %v", err)
	}
}

func TestSaveCopyWithNoSaveIsSaveNotFound(t *testing.T) {
	r := newSaveCopyRig(t)
	if err := os.Remove(filepath.Join(r.worldDir(r.id), world.SavesDir, "base.zip")); err != nil {
		t.Fatal(err)
	}
	if o := r.outcome(r.ask(context.Background(), testSaveCopyMod)); !errors.Is(o.err, world.ErrSaveNotFound) {
		t.Errorf("err = %v, want world.ErrSaveNotFound", o.err)
	}
}

// A start cancels a read of its world, and waits until the read's container
// is gone (SPEC 8.16).
func TestCancelSaveCopiesKillsTheRunAndWaits(t *testing.T) {
	r := newSaveCopyRig(t)
	ch := r.ask(context.Background(), testSaveCopyMod)
	r.waitRunning()
	r.c.cancelSaveCopies("another-world")
	if len(r.previewContainers()) != 1 {
		t.Fatal("cancelling another world's reads ended this one")
	}
	r.c.cancelSaveCopies(r.id)
	if n := len(r.previewContainers()); n != 0 {
		t.Errorf("%d containers left after cancelSaveCopies returned", n)
	}
	if o := r.outcome(ch); !errors.Is(o.err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", o.err)
	}
	// With nothing running it returns at once.
	r.c.cancelSaveCopies(r.id)
}

func TestSaveCopyCommandRunsTheBenchmark(t *testing.T) {
	tmp := saveCopyCommand(testSaveCopyMod)
	for _, want := range []string{
		"--benchmark " + gameenv.SaveCopyMount,
		"--benchmark-ticks 8 --benchmark-runs 1",
		"--mod-directory " + gameenv.PreviewMount + "/mods",
		"mv " + previewWrite + "/script-output/" + gameenv.SavePlayersFile + " " + gameenv.PreviewMount + "/" + gameenv.SavePlayersFile,
	} {
		if !strings.Contains(tmp, want) {
			t.Errorf("command %q lacks %q", tmp, want)
		}
	}
	if strings.Contains(tmp, "--start-server") || strings.Contains(tmp, "rcon") {
		t.Errorf("command %q starts a server", tmp)
	}
	dir := testSaveCopyMod
	dir.output = gameenv.FactoryMapDir
	if got := saveCopyCommand(dir); !strings.Contains(got, "mv "+previewWrite+"/script-output/"+gameenv.FactoryMapDir+" "+gameenv.PreviewMount+"/"+gameenv.FactoryMapDir) {
		t.Errorf("folder output command %q", got)
	}
	if got := saveCopyConfig; !strings.Contains(got, "write-data="+previewWrite+"\n") || !strings.Contains(got, "read-data=/opt/factorio/data\n") {
		t.Errorf("config %q", got)
	}
}

func TestSameMods(t *testing.T) {
	want := []saveplayers.Mod{{Name: "base", Version: "2.1.24"}, {Name: "a", Version: "1.0.0"}}
	helper := saveplayers.Mod{Name: "helper", Version: "1.0.0"}
	for name, tc := range map[string]struct {
		got []saveplayers.Mod
		ok  bool
	}{
		"same, helper aside": {[]saveplayers.Mod{want[1], helper, want[0]}, true},
		"equal versions":     {[]saveplayers.Mod{{Name: "base", Version: "2.1.24"}, {Name: "a", Version: "1.0"}}, true},
		"missing":            {[]saveplayers.Mod{want[0]}, false},
		"extra":              {[]saveplayers.Mod{want[0], want[1], {Name: "b", Version: "1.0.0"}}, false},
		"twice":              {[]saveplayers.Mod{want[0], want[1], want[1]}, false},
		"version":            {[]saveplayers.Mod{want[0], {Name: "a", Version: "1.0.1"}}, false},
	} {
		err := sameMods(want, tc.got, helper.Name)
		if (err == nil) != tc.ok || (err != nil && !errors.Is(err, ErrSaveCopyModsDiffer)) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// The helper's info.json names the world's own game line, since 2.1 refuses
// a mod made for "2.0".
func TestSaveCopyHelperInfoNamesTheWorldsLine(t *testing.T) {
	r := newSaveCopyRig(t)
	ch := r.ask(context.Background(), testSaveCopyMod)
	c := r.waitRunning()
	b, err := os.ReadFile(filepath.Join(r.workDir(c), world.ModsDir, saveplayersmod.ModDir, "info.json")) //nolint:gosec // the test's own work folder
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Name, Version   string
		FactorioVersion string `json:"factorio_version"`
	}
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	if info.Name != saveplayersmod.ModName || info.Version != saveplayersmod.ModVersion || info.FactorioVersion != "2.1" {
		t.Errorf("info.json %+v", info)
	}
	r.game.Daemon().Exit(c.ID, 1, false)
	r.outcome(ch)
}

// A run that exits 0 but leaves no output is bad output, and so is one whose
// output the caller cannot read.
func TestSaveCopyWithNoOutputIsBadOutput(t *testing.T) {
	r := newSaveCopyRig(t)
	ch := r.ask(context.Background(), testSaveCopyMod)
	r.game.Daemon().Exit(r.waitRunning().ID, 0, false)
	if o := r.outcome(ch); !errors.Is(o.err, ErrSaveCopyBadOutput) {
		t.Errorf("no output: err = %v, want ErrSaveCopyBadOutput", o.err)
	}

	plain := testSaveCopyMod
	plain.activeMods = nil
	done := make(chan error, 1)
	go func() {
		_, err := r.c.runSaveCopy(context.Background(), r.id, plain, func(*os.Root, string) error {
			return errors.New("not JSON")
		})
		done <- err
	}()
	r.finish(r.waitRunning(), worldMods)
	select {
	case err := <-done:
		if !errors.Is(err, ErrSaveCopyBadOutput) {
			t.Errorf("unreadable output: err = %v, want ErrSaveCopyBadOutput", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runSaveCopy did not return")
	}
}

// A start never fails because a save read slipped in before its lock: reads
// asked for while it runs are refused, and reads already running are ended
// (SPEC 8.16).
func TestStartIsNeverBusyBecauseOfASaveRead(t *testing.T) {
	r := newRig(t, rigOptions{})
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	id := r.newWorld("Saves")
	if err := r.worlds.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	sr := &saveCopyRig{rig: r, id: id}
	first := sr.ask(context.Background(), testSaveCopyMod)
	sr.waitRunning()

	// Reads asked for in a loop while the start runs; each gives up after a
	// moment, so none outlives the test.
	stop := make(chan struct{})
	hammered := make(chan int, 1)
	go func() {
		refused := 0
		for {
			select {
			case <-stop:
				hammered <- refused
				return
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			_, err := r.c.runSaveCopy(ctx, id, testSaveCopyMod, func(*os.Root, string) error { return nil })
			cancel()
			if errors.Is(err, errSaveCopyStarting) {
				refused++
			}
		}
	}()
	r.start(id) // fails the test on any error, ErrBusy included
	close(stop)
	t.Logf("%d reads were refused while the start ran", <-hammered)
	if o := sr.outcome(first); !errors.Is(o.err, context.Canceled) {
		t.Errorf("the running read: err = %v, want context.Canceled", o.err)
	}
	r.c.saveCopies.mu.Lock()
	left := len(r.c.saveCopies.starting)
	r.c.saveCopies.mu.Unlock()
	if left != 0 {
		t.Errorf("%d worlds still marked starting after Start returned", left)
	}
}

// A read asked for while the world is starting begins nothing.
func TestSaveCopyRefusedWhileStarting(t *testing.T) {
	r := newSaveCopyRig(t)
	end := r.c.saveCopies.beginStart(r.id)
	o := r.outcome(r.ask(context.Background(), testSaveCopyMod))
	if !errors.Is(o.err, world.ErrBusy) || !errors.Is(o.err, errSaveCopyStarting) {
		t.Errorf("err = %v, want the starting refusal", o.err)
	}
	if n := len(r.game.Daemon().Containers()); n != 0 {
		t.Errorf("%d containers were made", n)
	}
	end()
	end() // a second call changes nothing
	ch := r.ask(context.Background(), testSaveCopyMod)
	r.game.Daemon().Exit(r.waitRunning().ID, 1, false)
	if o := r.outcome(ch); errors.Is(o.err, errSaveCopyStarting) {
		t.Error("a read after the start ended was refused")
	}
}

// When the save cannot be hard-linked it is copied, after the disk check
// (SPEC 6.11): a copy with the save's bytes and time, not the save itself.
func TestSaveCopyCopiesTheSaveWhenLinkingFails(t *testing.T) {
	was := linkFile
	linkFile = func(string, string) error { return errors.New("cross-device link") }
	t.Cleanup(func() { linkFile = was })

	t.Run("copied", func(t *testing.T) {
		r := newSaveCopyRig(t)
		ch := r.ask(context.Background(), testSaveCopyMod)
		c := r.waitRunning()
		got := filepath.Join(r.workDir(c), saveCopySave)
		orig := filepath.Join(r.worldDir(r.id), world.SavesDir, "base.zip")
		if sameInode(t, got, orig) {
			t.Error("the save was linked, not copied")
		}
		a, err := os.ReadFile(got) //nolint:gosec // the test's own work folder
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(orig) //nolint:gosec // the test's own world
		if err != nil {
			t.Fatal(err)
		}
		gi, _ := os.Stat(got)
		oi, _ := os.Stat(orig)
		if string(a) != string(b) || !gi.ModTime().Equal(oi.ModTime()) {
			t.Error("the copy differs from the save in bytes or time")
		}
		r.finish(c, append(worldMods, saveplayers.Mod{Name: saveplayersmod.ModName, Version: saveplayersmod.ModVersion}))
		o := r.outcome(ch)
		if o.err != nil {
			t.Fatal(o.err)
		}
		if o.basis.Size != oi.Size() || !o.basis.Modified.Equal(oi.ModTime()) {
			t.Errorf("basis %+v", o.basis)
		}
	})
	t.Run("no room", func(t *testing.T) {
		r := newSaveCopyRig(t)
		defer world.SetFreeBytesForTest(world.SpaceReserve)()
		o := r.outcome(r.ask(context.Background(), testSaveCopyMod))
		if !errors.Is(o.err, world.ErrNoSpace) {
			t.Errorf("err = %v, want world.ErrNoSpace", o.err)
		}
		if n := len(r.game.Daemon().Containers()); n != 0 {
			t.Errorf("%d containers were made", n)
		}
	})
}

// A saves folder swapped for a link to another world's is refused, and
// nothing of the other world is linked or run (SPEC 8.16, 14.8).
func TestSaveCopyRefusesASavesFolderLinkedToAnotherWorld(t *testing.T) {
	r := newSaveCopyRig(t)
	other := r.newWorld("Other")
	saves := filepath.Join(r.worldDir(r.id), world.SavesDir)
	if err := os.RemoveAll(saves); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.worldDir(other), world.SavesDir), saves); err != nil {
		t.Fatal(err)
	}
	if o := r.outcome(r.ask(context.Background(), testSaveCopyMod)); o.err == nil {
		t.Fatal("the read ran through the link")
	}
	if n := len(r.game.Daemon().Containers()); n != 0 {
		t.Errorf("%d containers were made", n)
	}
	info, err := os.Stat(filepath.Join(r.worldDir(other), world.SavesDir, "base.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
		t.Errorf("the other world's save has %d links", st.Nlink)
	}
}

// A link that lands on another file than the one the world's root saw, as a
// swap between the check and the link would make, is refused and removed.
func TestLinkSaveRefusesAnotherFile(t *testing.T) {
	r := newSaveCopyRig(t)
	decoy := filepath.Join(t.TempDir(), "decoy.zip")
	if err := os.WriteFile(decoy, []byte("decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	was := linkFile
	linkFile = func(_, dst string) error { return os.Link(decoy, dst) }
	t.Cleanup(func() { linkFile = was })

	base, err := r.c.previewBase()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	name, err := newWorkDir(base)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := base.OpenRoot(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	if _, err := r.c.linkSave(r.id, "base.zip", dir, name); !errors.Is(err, errSaveCopyLinkChanged) {
		t.Errorf("err = %v, want errSaveCopyLinkChanged", err)
	}
	if _, err := dir.Lstat(saveCopySave); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the wrong link is still there: %v", err)
	}
}

// A world with no mod-list.json yet (never started, a fresh import) is read
// with the default list, as its first start would load it, not refused as
// mods-differ.
func TestSaveCopyWorldWithNoModList(t *testing.T) {
	r := newSaveCopyRig(t)
	if err := os.Remove(filepath.Join(r.worldDir(r.id), world.ModsDir, world.ModListFile)); err != nil {
		t.Fatal(err)
	}
	ch := r.ask(context.Background(), testSaveCopyMod)
	c := r.waitRunning()
	if _, err := os.Lstat(filepath.Join(r.worldDir(r.id), world.ModsDir, world.ModListFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a mod list was written to the world: %v", err)
	}
	// Space Age off: the default list leaves its mods off; fmtest is on.
	r.finish(c, append(worldMods, saveplayers.Mod{Name: saveplayersmod.ModName, Version: saveplayersmod.ModVersion}))
	if o := r.outcome(ch); o.err != nil {
		t.Fatalf("err = %v, want a read", o.err)
	}

	ch = r.ask(context.Background(), testSaveCopyMod)
	r.finish(r.waitRunning(), append(append([]saveplayers.Mod{}, worldMods...), saveplayers.Mod{Name: "space-age", Version: "2.1.24"}))
	if o := r.outcome(ch); !errors.Is(o.err, ErrSaveCopyModsDiffer) {
		t.Errorf("with Space Age loaded: err = %v, want ErrSaveCopyModsDiffer", o.err)
	}
}

func TestWithDefaults(t *testing.T) {
	mods := []world.InstalledMod{{Name: "base", Enabled: true}, {Name: "space-age", Enabled: true}, {Name: "fmtest", Enabled: false}}
	got := withDefaults(mods, map[string]bool{"base": true, "space-age": false})
	if !got[0].Enabled || got[1].Enabled || !got[2].Enabled {
		t.Errorf("withDefaults = %+v", got)
	}
	if !mods[1].Enabled {
		t.Error("withDefaults changed its input")
	}
	if got := withDefaults(mods, nil); got[2].Enabled {
		t.Error("with a mod list the states must stay as they are")
	}
}
