package game

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// A save read is the fifth and sixth kind of one-off container (SPEC 8.16,
// 14.3, ADR 0030): the world's save is linked into a work folder and mounted
// read-only, the world's mods and a control-only helper mod are added, and the
// game runs it with --benchmark for a few ticks, no network. The helper
// writes JSON that the caller reads.

// A save read's limits (SPEC 8.16, 14.3). A read needs about as much memory
// as running the save: 6.9 GB was measured for a 187 MB save with 44 mods.
const (
	// SaveCopyLimit is how long a save read may take before its container
	// is killed.
	SaveCopyLimit = 5 * time.Minute
	// SaveCopyMemory is the container's memory limit in bytes, with no swap.
	SaveCopyMemory = 12 << 30
	// saveCopyTicks is how many ticks the benchmark runs: the helper writes
	// its output in on_init, before the first.
	saveCopyTicks = 8
	// saveCopySave is the save's link in the work folder, seen in the
	// container at gameenv.SaveCopyMount.
	saveCopySave = "save.zip"
	// saveCopyTmpfs is the size of the game's in-memory write folder. It
	// holds the helper's output (the factory map's at most
	// factorymap.MaxOutputBytes) and counts against the memory limit; the
	// game never writes to the host's disk (SPEC 8.16, 14.3).
	saveCopyTmpfs = "1g"
	// saveCopyReadData is the game's data folder in the image.
	saveCopyReadData = "/opt/factorio/data"
)

// The refusals of a save read, one for each failure id of a record's seed
// (SPEC 8.16). A caller maps them with the saveplayers.Result* constants.
var (
	ErrSaveCopyImageMissing = errors.New("the game image for this world's version is not downloaded")
	ErrSaveCopyOutOfMemory  = errors.New("the game ran out of memory reading the save")
	ErrSaveCopyTimeout      = errors.New("reading the save took too long")
	ErrSaveCopyModsDiffer   = errors.New("the mods the game loaded are not the world's mods")
	ErrSaveCopySaveNewer    = errors.New("the save was made by a newer game than the world's")
	ErrSaveCopyBadOutput    = errors.New("the game made no usable output")
)

// saveCopyState is the save reads in flight, by world id, and the worlds a
// start is under way for, so a start can cancel the reads and no new one
// begins until it ends (SPEC 8.16). The zero value is ready to use.
type saveCopyState struct {
	mu       sync.Mutex
	runs     map[string]map[*saveCopyRun]struct{}
	starting map[string]int
}

// saveCopyRun is one save read in flight.
type saveCopyRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// errSaveCopyStarting refuses a read of a world that is starting. Nothing is
// kept; the next view reads again.
var errSaveCopyStarting = fmt.Errorf("%w: the world is starting", world.ErrBusy)

// add records a run of the world, or refuses it while the world is
// starting. The returned function removes it and marks it done.
func (s *saveCopyState) add(id string, run *saveCopyRun) (remove func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.starting[id] > 0 {
		return nil, errSaveCopyStarting
	}
	if s.runs == nil {
		s.runs = map[string]map[*saveCopyRun]struct{}{}
	}
	if s.runs[id] == nil {
		s.runs[id] = map[*saveCopyRun]struct{}{}
	}
	s.runs[id][run] = struct{}{}
	return func() {
		s.mu.Lock()
		delete(s.runs[id], run)
		if len(s.runs[id]) == 0 {
			delete(s.runs, id)
		}
		s.mu.Unlock()
		close(run.done)
	}, nil
}

// beginStart marks the world as starting until the returned function is
// called: a read asked for meanwhile is refused.
func (s *saveCopyState) beginStart(id string) (end func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.starting == nil {
		s.starting = map[string]int{}
	}
	s.starting[id]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.starting[id]--; s.starting[id] <= 0 {
				delete(s.starting, id)
			}
		})
	}
}

// saveCopyMod is a control mod that runs against a copy of the world's save.
// Of controlMod it uses kind, what, name, dir, control, output (a file or a
// folder in script-output), maxOutput and memory (zero means
// SaveCopyMemory); the run makes no map, so params and save are not used.
type saveCopyMod struct {
	controlMod
	// activeMods reads the mods the game loaded from the run's output, for
	// the check after the run (SPEC 8.16): the game drops a missing mod
	// silently and exits 0. nil for a kind whose output lists none.
	activeMods func(base *os.Root, dir string) ([]saveplayers.Mod, error)
}

// saveCopyConfig is the run's config.ini: the game's write folder is the
// in-memory folder, for every kind.
const saveCopyConfig = "[path]\nread-data=" + saveCopyReadData + "\nwrite-data=" + previewWrite + "\n"

// saveCopyCommand is the shell script that runs the work folder's save with
// the control mod enabled, and leaves the mod's output files in the work
// folder (SPEC 8.16). It is the only place the seeding method lives. Only
// the named output leaves the in-memory write folder.
func saveCopyCommand(m saveCopyMod) string {
	return strings.Join([]string{
		gameBinary + ` --config ` + gameenv.PreviewMount + `/config.ini --mod-directory ` + gameenv.PreviewMount + `/` + world.ModsDir +
			` --benchmark ` + gameenv.SaveCopyMount + ` --benchmark-ticks ` + fmt.Sprint(saveCopyTicks) + ` --benchmark-runs 1 || exit 1`,
		`mv ` + previewWrite + `/script-output/` + m.output + ` ` + gameenv.PreviewMount + `/` + m.output,
	}, "\n")
}

// saveCopyInfo is the helper's info.json for the game line. It needs the
// base game only.
func saveCopyInfo(m saveCopyMod, line string) []byte {
	version := strings.TrimPrefix(m.dir, m.name+"_")
	return modInfo(m.name, version, "Foreman "+m.what,
		"Reads the "+m.what+" of a save for Foreman. Adds no prototypes.", line, "base")
}

// runSaveCopy reads the save the world would load, in the preview slot. It
// calls read with the work folder's root and the run's folder while the files
// are there, and returns the basis of the save that was read, or an
// ErrSaveCopy* error.
//
// The save is a hard link (a copy when linking fails, after the SPEC 6.11
// space check) mounted read-only, so neither the game nor anything else in
// the container can change it. The mods are linked or copied as for the mod
// settings run, and mod-settings.dat is a copy, since the game rewrites it;
// config/, the RCON password and server-settings.json never enter the work
// folder. The pre-start check runs first, under the world's lock: a mod
// problem is ErrSaveCopyModsDiffer and a newer save ErrSaveCopySaveNewer, and
// no container runs. A start of the world cancels the read (cancelSaveCopies).
//
// Other errors: world.ErrBusy when the world's lock is held, ErrPreviewBusy
// when the slot stays taken, ctx's error when ctx ends or a start cancels the
// read, world.ErrSaveNotFound when there is no save to read. A read asked
// for while the world is starting is refused at once with world.ErrBusy.
func (c *Controller) runSaveCopy(ctx context.Context, id string, m saveCopyMod,
	read func(base *os.Root, dir string) error) (saveplayers.Basis, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	remove, err := c.saveCopies.add(id, &saveCopyRun{cancel: cancel, done: make(chan struct{})})
	if err != nil {
		return saveplayers.Basis{}, err
	}
	defer remove()

	meta, err := c.readMeta(id)
	if err != nil {
		return saveplayers.Basis{}, err
	}
	line, ok := mapgen.Line(meta.GameVersion)
	if !ok {
		return saveplayers.Basis{}, fmt.Errorf("%w: there is no game for Factorio %q", ErrSaveCopyImageMissing, meta.GameVersion)
	}
	var (
		basis saveplayers.Basis
		want  []saveplayers.Mod
	)
	plan := previewPlan{
		gameVersion: meta.GameVersion, kind: m.kind, limit: SaveCopyLimit,
		fill: func(base *os.Root) (string, error) {
			var name string
			var err error
			name, basis, want, err = c.makeSaveCopyDir(base, id, meta.GameVersion, m)
			return name, err
		},
	}
	err = c.previewRunIn(ctx, plan, func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error {
		if err := addControlMod(base, name, m.controlMod, saveCopyInfo(m, line)); err != nil {
			return err
		}
		spec, err := c.saveCopySpec(base, ref, name, m)
		if err != nil {
			return err
		}
		// A start that began while the folder was made wins: no container.
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := container(spec); err != nil {
			return err
		}
		if err := checkSaveCopyOutput(base, name, m.output); err != nil {
			return err
		}
		if m.activeMods != nil {
			got, err := m.activeMods(base, name)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
			}
			if err := sameMods(want, got, m.name); err != nil {
				return err
			}
		}
		if err := read(base, name); err != nil {
			if isSaveCopyError(err) {
				return err
			}
			return fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
		}
		// A start that came while the output was read wins.
		return ctx.Err()
	})
	if err != nil {
		return saveplayers.Basis{}, saveCopyError(err)
	}
	return basis, nil
}

// checkSaveCopyOutput refuses a run that exited without leaving its output,
// a regular file or a folder, in the work folder dir: the helper writes
// nothing when its own read fails.
func checkSaveCopyOutput(base *os.Root, dir, output string) error {
	info, err := base.Lstat(path.Join(dir, output))
	if err != nil {
		return fmt.Errorf("%w: the game left no %s", ErrSaveCopyBadOutput, output)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("%w: %s is not a regular file or folder", ErrSaveCopyBadOutput, output)
	}
	return nil
}

// isSaveCopyError reports whether err is already one of the ErrSaveCopy*
// refusals.
func isSaveCopyError(err error) bool {
	for _, e := range []error{ErrSaveCopyImageMissing, ErrSaveCopyOutOfMemory, ErrSaveCopyTimeout,
		ErrSaveCopyModsDiffer, ErrSaveCopySaveNewer, ErrSaveCopyBadOutput} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// saveCopySpec is the run's container: a control mod container whose save
// and mod zips are mounted read-only over their links (SPEC 8.16, 14.3).
func (c *Controller) saveCopySpec(base *os.Root, image, dir string, m saveCopyMod) (docker.ContainerSpec, error) {
	mounts, err := c.readOnlyZipMounts(base, dir)
	if err != nil {
		return docker.ContainerSpec{}, err
	}
	mounts = append(mounts, c.readOnlyMount(path.Join(dir, saveCopySave), gameenv.SaveCopyMount))
	spec := c.controlModSpec(image, dir, PreviewRequest{}, m.controlMod, nil)
	spec.Env = []string{gameenv.PreviewKind + "=" + m.kind}
	spec.Cmd = []string{"-c", saveCopyCommand(m)}
	spec.HostConfig = c.oneOffHostConfigMem(dir, cmp.Or(m.memory, SaveCopyMemory))
	spec.HostConfig.Tmpfs = map[string]string{previewWrite: "rw,noexec,nosuid,size=" + saveCopyTmpfs + ",mode=1777"}
	spec.HostConfig.Mounts = mounts
	return spec, nil
}

// makeSaveCopyDir makes the run's work folder: config.ini, the save the world
// would load as a link, the world's mods with a copy of mod-settings.dat.
// It holds the world's lock while it reads the world, and returns the save's
// basis and the enabled mods the game is given. The helper is added after.
func (c *Controller) makeSaveCopyDir(base *os.Root, id, gameVersion string, m saveCopyMod) (name string, basis saveplayers.Basis, want []saveplayers.Mod, err error) {
	name, err = newWorkDir(base)
	if err != nil {
		return "", basis, nil, err
	}
	fail := func(err error) (string, saveplayers.Basis, []saveplayers.Mod, error) {
		_ = base.RemoveAll(name)
		return "", saveplayers.Basis{}, nil, err
	}
	dir, err := base.OpenRoot(name)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.WriteFile("config.ini", []byte(saveCopyConfig), world.FileMode); err != nil {
		return fail(err)
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return fail(err)
	}
	defer unlock()
	meta, err := c.readMeta(id)
	if err != nil {
		return fail(err)
	}
	if meta.GameVersion != gameVersion {
		// The image was chosen for another version.
		return fail(world.ErrBusy)
	}
	if err := c.saveCopyPreCheck(id); err != nil {
		return fail(err)
	}
	target, found, err := c.worlds.LoadTarget(id)
	if err != nil {
		return fail(err)
	}
	if !found {
		return fail(world.ErrSaveNotFound)
	}
	if basis, err = c.linkSave(id, target.Name, dir, name); err != nil {
		return fail(err)
	}
	if want, err = c.enabledMods(id); err != nil {
		return fail(err)
	}
	if err := c.worlds.LinkModsInto(id, filepath.Join(c.previewPath(), name)); err != nil {
		return fail(fmt.Errorf("could not put the world's mods in the work folder: %w", err))
	}
	if err := copyModSettings(c.worlds, id, dir); err != nil {
		return fail(err)
	}
	return name, basis, want, nil
}

// saveCopyPreCheck runs the pre-start check, because the game drops a mod it
// cannot find and still exits 0 (SPEC 7.4, 8.16). Any problem fails the read:
// a newer save is ErrSaveCopySaveNewer, a game Foreman does not run is
// ErrSaveCopyImageMissing, no save world.ErrSaveNotFound, and every other
// problem ErrSaveCopyModsDiffer, since the mods cannot be shown to match.
//
// A world with no mod-list.json yet (never started, a fresh import) is not
// held to the Space Age check of its list: the run gives the game the default
// list, as the first start would.
func (c *Controller) saveCopyPreCheck(id string) error {
	pc, err := c.worlds.PreStartCheckAt(id, "")
	if err != nil {
		return err
	}
	defaults, err := c.defaultModList(id)
	if err != nil {
		return err
	}
	if defaults != nil {
		pc.Problems = slices.DeleteFunc(pc.Problems, func(p world.Problem) bool { return p.Kind == world.ProblemModListSpaceAge })
	}
	for _, p := range pc.Problems {
		switch p.Kind {
		case world.ProblemSaveNewer:
			return fmt.Errorf("%w: %s", ErrSaveCopySaveNewer, p.Text)
		case world.ProblemGameVersion:
			return fmt.Errorf("%w: %s", ErrSaveCopyImageMissing, p.Text)
		case world.ProblemNoSave:
			return world.ErrSaveNotFound
		}
	}
	if len(pc.Problems) > 0 {
		return fmt.Errorf("%w: %s", ErrSaveCopyModsDiffer, pc.Problems[0].Text)
	}
	return nil
}

// errSaveCopyLinkChanged refuses a read whose link is not the save the
// world's folder holds.
var errSaveCopyLinkChanged = errors.New("the save changed while it was being linked, so it is not read")

// linkFile makes a hard link; tests make it fail to reach the copy.
var linkFile = os.Link

// linkSave puts the world's save name in the work folder dir (named name
// under the preview folder) as saveCopySave: a hard link, or a copy when the
// link fails and the disk has room for it (SPEC 6.11). It returns the basis
// of what landed there.
func (c *Controller) linkSave(id, save string, dir *os.Root, name string) (saveplayers.Basis, error) {
	// The save as the world's root sees it: a link anywhere in the path that
	// leads out of the world is refused here.
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return saveplayers.Basis{}, err
	}
	want, err := root.Lstat(path.Join(world.SavesDir, save))
	_ = root.Close()
	if err != nil || !want.Mode().IsRegular() {
		return saveplayers.Basis{}, world.ErrSaveNotFound
	}
	src := filepath.Join(c.worlds.Dirs().Worlds, id, world.SavesDir, save)
	dst := filepath.Join(c.previewPath(), name, saveCopySave)
	if err := linkFile(src, dst); err == nil {
		// The path could have been swapped between the two calls: what landed
		// must be the very file the root saw.
		got, err := dir.Lstat(saveCopySave)
		if err != nil || !os.SameFile(got, want) {
			_ = dir.Remove(saveCopySave)
			return saveplayers.Basis{}, errSaveCopyLinkChanged
		}
		return saveplayers.Basis{Save: save, Size: got.Size(), Modified: got.ModTime()}, nil
	}
	f, info, err := c.worlds.OpenSave(id, save)
	if err != nil {
		return saveplayers.Basis{}, err
	}
	defer func() { _ = f.Close() }()
	release, err := world.Reserve(c.previewPath(), uint64(info.Size())) //nolint:gosec // a file size is not negative
	if err != nil {
		return saveplayers.Basis{}, err
	}
	defer release()
	out, err := dir.OpenFile(saveCopySave, os.O_WRONLY|os.O_CREATE|os.O_EXCL, world.FileMode)
	if err != nil {
		return saveplayers.Basis{}, err
	}
	_, err = io.Copy(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = dir.Chtimes(saveCopySave, info.ModTime(), info.ModTime())
	}
	if err != nil {
		return saveplayers.Basis{}, fmt.Errorf("could not copy the save to the work folder: %w", err)
	}
	return saveplayers.Basis{Save: save, Size: info.Size(), Modified: info.ModTime()}, nil
}

// copyModSettings copies the world's mod-settings.dat into the work folder's
// mods folder, when there is one. It is a new file, never a link: the game
// rewrites it.
func copyModSettings(worlds *world.Manager, id string, dir *os.Root) error {
	src, err := worlds.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	p := path.Join(world.ModsDir, world.ModSettingsFile)
	b, err := world.ReadSmallFile(src, p, world.MaxModSettings)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not read the world's mod settings: %w", err)
	}
	return dir.WriteFile(p, b, world.FileMode)
}

// enabledMods is the mods the game loads for the world: each enabled mod at
// its newest version, the built-in ones at the game's version. A world with
// no mod-list.json has its mods on or off as the default list the run gives
// the game says.
func (c *Controller) enabledMods(id string) ([]saveplayers.Mod, error) {
	mods, err := c.worlds.Mods(id)
	if err != nil {
		return nil, err
	}
	defaults, err := c.defaultModList(id)
	if err != nil {
		return nil, err
	}
	mods = withDefaults(mods, defaults)
	newest := map[string]string{}
	for _, m := range mods {
		if !m.Enabled {
			continue
		}
		if v, ok := newest[m.Name]; !ok || world.CompareVersions(m.Version, v) > 0 {
			newest[m.Name] = m.Version
		}
	}
	out := make([]saveplayers.Mod, 0, len(newest))
	for name, v := range newest {
		out = append(out, saveplayers.Mod{Name: name, Version: v})
	}
	return out, nil
}

// defaultModList returns, by name, whether each mod is on in the
// mod-list.json EnsureModList would write, when the world has none yet; nil
// when it has one. Nothing is written: the run's work folder gets the same
// default list (LinkModsInto), so the game loads the mods it names.
func (c *Controller) defaultModList(id string) (map[string]bool, error) {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if _, err := root.Lstat(path.Join(world.ModsDir, world.ModListFile)); !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	meta, err := world.ReadMeta(root)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Mods []modEntry `json:"mods"`
	}
	if err := json.Unmarshal(world.DefaultModList(meta.GameVersion, meta.SpaceAge), &doc); err != nil {
		return nil, err
	}
	on := map[string]bool{}
	for _, m := range doc.Mods {
		on[m.Name] = m.Enabled
	}
	return on, nil
}

// withDefaults turns each built-in mod on or off as the default list would,
// for a world with no mod-list.json (defaults not nil). A mod the default
// list does not name stays on, as the game turns it on.
func withDefaults(mods []world.InstalledMod, defaults map[string]bool) []world.InstalledMod {
	if defaults == nil {
		return mods
	}
	out := slices.Clone(mods)
	for i, m := range out {
		if on, ok := defaults[m.Name]; ok {
			out[i].Enabled = on
		} else {
			out[i].Enabled = true
		}
	}
	return out
}

// sameMods refuses a run whose game loaded other mods than want, the helper
// called helper aside, as ErrSaveCopyModsDiffer.
func sameMods(want, got []saveplayers.Mod, helper string) error {
	wanted := map[string]string{}
	for _, m := range want {
		wanted[m.Name] = m.Version
	}
	seen := map[string]bool{}
	for _, m := range got {
		if m.Name == helper {
			continue
		}
		v, ok := wanted[m.Name]
		switch {
		case !ok:
			return fmt.Errorf("%w: the game loaded %s, which the world does not enable", ErrSaveCopyModsDiffer, m.Name)
		case seen[m.Name]:
			return fmt.Errorf("%w: the game listed %s twice", ErrSaveCopyModsDiffer, m.Name)
		case world.CompareVersions(v, m.Version) != 0:
			return fmt.Errorf("%w: the game loaded %s %s, the world has %s", ErrSaveCopyModsDiffer, m.Name, m.Version, v)
		}
		seen[m.Name] = true
	}
	for name := range wanted {
		if !seen[name] {
			return fmt.Errorf("%w: the game did not load %s", ErrSaveCopyModsDiffer, name)
		}
	}
	return nil
}

// saveCopyError words a failed run as one of the ErrSaveCopy* errors. Errors
// a caller handles on its own (a busy slot or world, a cancelled run) and
// errors already wrapped pass through.
func saveCopyError(err error) error {
	var pe *PreviewError
	switch {
	case errors.Is(err, ErrPreviewImageMissing):
		return ErrSaveCopyImageMissing
	case errors.Is(err, ErrPreviewTimeout):
		return ErrSaveCopyTimeout
	case errors.Is(err, errControlModTaken):
		return fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
	case errors.As(err, &pe) && pe.OOM:
		return fmt.Errorf("%w: %w", ErrSaveCopyOutOfMemory, err)
	case errors.As(err, &pe):
		return fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
	}
	return err
}

// beginStartSaveCopies marks the world as starting, so no save read of it
// begins, then kills every read of it that is running and waits until each
// has removed its container and released the world (SPEC 8.16). Start calls
// it first, so a read can never hold the world's lock it needs; it calls the
// returned function when it ends.
func (c *Controller) beginStartSaveCopies(id string) (end func()) {
	end = c.saveCopies.beginStart(id)
	c.cancelSaveCopies(id)
	return end
}

// cancelSaveCopies kills every save read of the world that is running, and
// waits until each has removed its container and released the world.
func (c *Controller) cancelSaveCopies(id string) {
	s := &c.saveCopies
	s.mu.Lock()
	runs := make([]*saveCopyRun, 0, len(s.runs[id]))
	for run := range s.runs[id] {
		runs = append(runs, run)
	}
	s.mu.Unlock()
	for _, run := range runs {
		run.cancel()
	}
	for _, run := range runs {
		<-run.done
	}
}
