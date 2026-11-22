package game

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
	// saveCopyWrite is the game's write folder for a kind whose output is
	// kept on disk, inside the work folder.
	saveCopyWrite = gameenv.PreviewMount + "/write"
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

// saveCopyState is the save reads in flight, by world id, so a start can
// cancel them (SPEC 8.16). The zero value is ready to use.
type saveCopyState struct {
	mu   sync.Mutex
	runs map[string]map[*saveCopyRun]struct{}
}

// saveCopyRun is one save read in flight.
type saveCopyRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// add records a run of the world; the returned function removes it and marks
// it done.
func (s *saveCopyState) add(id string, run *saveCopyRun) (remove func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	}
}

// saveCopyMod is a control mod that runs against a copy of the world's save.
// Of controlMod it uses kind, what, name, dir, control, output, maxOutput and
// memory (zero means SaveCopyMemory); the run makes no map, so params and
// save are not used.
type saveCopyMod struct {
	controlMod
	// outputOnDisk makes the game write its data in the work folder, not the
	// 64 MB tmpfs (the factory map).
	outputOnDisk bool
	// activeMods reads the mods the game loaded from the run's output, for
	// the check after the run (SPEC 8.16): the game drops a missing mod
	// silently and exits 0. nil for a kind whose output lists none.
	activeMods func(base *os.Root, dir string) ([]saveplayers.Mod, error)
}

// saveCopyConfig is the run's config.ini: the game's write folder in the
// tmpfs, or in the work folder for a kind whose output is too big for it.
func saveCopyConfig(m saveCopyMod) string {
	write := previewWrite
	if m.outputOnDisk {
		write = saveCopyWrite
	}
	return "[path]\nread-data=" + saveCopyReadData + "\nwrite-data=" + write + "\n"
}

// saveCopyCommand is the shell script that runs the work folder's save with
// the control mod enabled, and leaves the mod's output files in the work
// folder (SPEC 8.16). It is the only place the seeding method lives.
func saveCopyCommand(m saveCopyMod) string {
	write := previewWrite
	if m.outputOnDisk {
		write = saveCopyWrite
	}
	return strings.Join([]string{
		gameBinary + ` --config ` + gameenv.PreviewMount + `/config.ini --mod-directory ` + gameenv.PreviewMount + `/` + world.ModsDir +
			` --benchmark ` + gameenv.SaveCopyMount + ` --benchmark-ticks ` + fmt.Sprint(saveCopyTicks) + ` --benchmark-runs 1 || exit 1`,
		`mv ` + write + `/script-output/` + m.output + ` ` + gameenv.PreviewMount + `/` + m.output,
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
// read, world.ErrSaveNotFound when there is no save to read.
func (c *Controller) runSaveCopy(ctx context.Context, id string, m saveCopyMod,
	read func(base *os.Root, dir string) error) (saveplayers.Basis, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer c.saveCopies.add(id, &saveCopyRun{cancel: cancel, done: make(chan struct{})})()

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
	if err := dir.WriteFile("config.ini", []byte(saveCopyConfig(m)), world.FileMode); err != nil {
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
func (c *Controller) saveCopyPreCheck(id string) error {
	pc, err := c.worlds.PreStartCheckAt(id, "")
	if err != nil {
		return err
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

// linkSave puts the world's save name in the work folder dir (named name
// under the preview folder) as saveCopySave: a hard link, or a copy when the
// link fails and the disk has room for it (SPEC 6.11). It returns the basis
// of what landed there.
func (c *Controller) linkSave(id, save string, dir *os.Root, name string) (saveplayers.Basis, error) {
	src := filepath.Join(c.worlds.Dirs().Worlds, id, world.SavesDir, save)
	dst := filepath.Join(c.previewPath(), name, saveCopySave)
	// link(2) does not follow a symlink swapped in for the save; what landed
	// must be a regular file.
	if err := os.Link(src, dst); err == nil {
		info, err := dir.Lstat(saveCopySave)
		if err != nil {
			return saveplayers.Basis{}, err
		}
		if !info.Mode().IsRegular() {
			_ = dir.Remove(saveCopySave)
			return saveplayers.Basis{}, world.ErrSaveNotFound
		}
		return saveplayers.Basis{Save: save, Size: info.Size(), Modified: info.ModTime()}, nil
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
// its newest version, the built-in ones at the game's version.
func (c *Controller) enabledMods(id string) ([]saveplayers.Mod, error) {
	mods, err := c.worlds.Mods(id)
	if err != nil {
		return nil, err
	}
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

// cancelSaveCopies kills every save read of the world that is running, so a
// start never races one (SPEC 8.16). It is called at the top of Start, and
// waits until each read has removed its container and released the world.
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
