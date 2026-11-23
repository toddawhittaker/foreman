package game

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/territory"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The map preview's fixed parts (SPEC 5.6, 14.3).
const (
	// PreviewLimit is how long a preview may take before its container is
	// killed.
	PreviewLimit = 60 * time.Second
	// PreviewWorldLimitMax is the most a run with a world's mods may take in
	// all, however many planets it draws, so it cannot hold the one slot for
	// long.
	PreviewWorldLimitMax = 10 * time.Minute
	// PreviewWorldLimit is how long a run that loads a world's mods may take
	// for each planet it draws: a heavy mod set takes longer to load and to
	// count than the base game (SPEC 5.6).
	PreviewWorldLimit = 120 * time.Second
	// PreviewWait is how long a preview waits for the one running before it
	// to end, before it is ErrPreviewBusy.
	PreviewWait = 20 * time.Second
	// maxPreviewWaiters is how many previews may wait for the slot; one
	// more is ErrPreviewBusy at once.
	maxPreviewWaiters = 8
	// A picture is a square of this many pixels a side, from PreviewSizeMin to
	// PreviewSizeMax in steps of PreviewSizeStep (two chunks on each side). On
	// 2.x a pixel is a tile; on 1.1 the default scale is one tile too. The
	// most is the territory cap, so every size offered can show them (SPEC 5.6).
	PreviewSizeMin     = 1024
	PreviewSizeMax     = territory.MaxSize
	PreviewSizeStep    = 128
	PreviewSizeDefault = 1280
	// previewMemory is the container's memory limit in bytes, with no swap.
	// The peak measured at 4096 pixels, above today's most, was about 553
	// MiB; this leaves room for a patch that needs more.
	previewMemory = 1 << 30
	// previewWorldMemory is the limit of a run that loads a world's mods,
	// whose prototypes and graphics count against it (SPEC 5.6).
	previewWorldMemory = 2 << 30
	// MaxPreviewPNG is the largest picture Foreman reads back (SPEC 5.6). The
	// largest measured at 4096 pixels, above today's most, was Gleba, 8.91
	// MB; this leaves room for a busier seed. At 1280 pixels they are under 1 MB.
	MaxPreviewPNG = 16 << 20
	previewPoll   = 250 * time.Millisecond
	// previewDir is the folder of the data directory that holds a preview's
	// work folder, which exists only while the preview runs.
	previewDir = "previews"
	// previewWrite is the game's write folder in the container: a tmpfs, since
	// the root is read-only and nothing may be left behind on the disk.
	previewWrite = "/tmp/factorio"
	// previewConfig points the game's write folder at previewWrite. Its read
	// folder is where the image keeps the game's data.
	previewConfig = "[path]\nread-data=__PATH__executable__/../../data\nwrite-data=" + previewWrite + "\n"
)

// Refusals of Preview. Each is a sentinel the page words itself.
var (
	// ErrPreviewBusy means another preview was still being made after
	// PreviewWait. One runs at a time across Foreman.
	ErrPreviewBusy = errors.New("another map preview is being made. Try again in a moment")
	// ErrPreviewImageMissing means the game image for the version is not
	// downloaded. A preview never downloads it (SPEC 3.7).
	ErrPreviewImageMissing = errors.New("the game image for this Factorio version is not downloaded. Download the game image first")
	// ErrPreviewTimeout means the container ran past PreviewLimit and was killed.
	ErrPreviewTimeout = errors.New("the map preview took too long and was stopped")
	// ErrPreviewInvalid means the request names something the game line does
	// not have, or settings that are not JSON.
	ErrPreviewInvalid = errors.New("the map preview request is not valid")
)

// PreviewError is a preview whose game exited with an error. Log is the end
// of the game's own output, for the log; a page words the failure itself.
// Kind is the run's kind (gameenv.Kind* or "preview"), so the error names
// the run that failed.
type PreviewError struct {
	Kind     string
	ExitCode int
	OOM      bool
	Log      string
}

func (e *PreviewError) Error() string {
	run := previewRunName(e.Kind)
	switch {
	case e.OOM:
		return run + " ran out of memory"
	case e.Log != "":
		return fmt.Sprintf("the game failed in %s (exit %d): %s", run, e.ExitCode, e.Log)
	}
	return fmt.Sprintf("the game failed in %s (exit %d)", run, e.ExitCode)
}

// previewRunName is how an error names a run of kind.
func previewRunName(kind string) string {
	switch kind {
	case gameenv.KindResources:
		return "the resource count"
	case gameenv.KindTerritories:
		return "the territory run"
	case "", "preview":
		return "the map preview"
	}
	return "the " + kind + " run"
}

// PreviewRequest says what Preview draws, from the settings Generate would
// write (SPEC 5.6).
type PreviewRequest struct {
	// GameVersion is an exact patch release of a supported line. Its image
	// must be downloaded already.
	GameVersion string
	SpaceAge    bool
	Preset      string
	Seed        uint32
	// Size is the picture's side in pixels; zero means PreviewSizeDefault.
	// Anything else must be on the steps of PreviewSizeMin to PreviewSizeMax.
	Size int
	// MapGenSettings and MapSettings are the JSON of the two settings files,
	// or nil for none.
	MapGenSettings, MapSettings []byte
	// Planets are drawn in this order; each must be in the known list:
	// KnownPlanets with World set, else mapgen.Planets(line, SpaceAge).
	Planets []string
	// World is the id of the world whose mods the run loads (SPEC 5.6, 14.3,
	// ADR 0033), or empty for the base game and Space Age alone. GameVersion
	// and SpaceAge must then be the world's.
	World string
	// KnownPlanets is the world's own planet list, from its mod-settings
	// run, when World is set. It is not used without World.
	KnownPlanets []string
}

// knownPlanets is the list a request's planets must come from.
func (r PreviewRequest) knownPlanets(line string) []string {
	if r.World != "" {
		return r.KnownPlanets
	}
	return mapgen.Planets(line, r.SpaceAge)
}

// ValidPreviewSize reports whether n is a size a preview can be drawn at.
func ValidPreviewSize(n int) bool {
	return n >= PreviewSizeMin && n <= PreviewSizeMax && (n-PreviewSizeMin)%PreviewSizeStep == 0
}

// size is the picture's side, with the default for a request that gave none.
func (r PreviewRequest) size() int {
	if r.Size == 0 {
		return PreviewSizeDefault
	}
	return r.Size
}

// PNG is one planet's picture.
type PNG struct {
	Planet string
	Data   []byte
}

// previewScript draws each planet in turn. The values come from the
// environment (gameenv), and the files are in the work folder.
var previewScript = strings.Join([]string{
	`for p in $` + gameenv.PreviewPlanets + `; do`,
	`set -- --config ` + gameenv.PreviewMount + `/config.ini --mod-directory ` + gameenv.PreviewMount + `/` + world.ModsDir +
		` --generate-map-preview ` + gameenv.PreviewMount + `/"$p".png --map-preview-size "$` + gameenv.PreviewSize + `" --map-gen-seed "$` + gameenv.Seed + `"`,
	`if [ -n "$` + gameenv.Preset + `" ]; then set -- "$@" --preset "$` + gameenv.Preset + `"; fi`,
	`if [ -f ` + gameenv.PreviewMount + `/` + world.MapGenSettingsFile + ` ]; then set -- "$@" --map-gen-settings ` + gameenv.PreviewMount + `/` + world.MapGenSettingsFile + `; fi`,
	`if [ -f ` + gameenv.PreviewMount + `/` + world.MapSettingsFile + ` ]; then set -- "$@" --map-settings ` + gameenv.PreviewMount + `/` + world.MapSettingsFile + `; fi`,
	`if [ -n "$` + gameenv.PreviewPlanetFlag + `" ]; then set -- "$@" --map-preview-planet "$p"; fi`,
	gameBinary + ` "$@" || exit 1`,
	`done`,
}, "\n")

// Preview draws the map a new world would have, one picture per planet, with
// the game's own --generate-map-preview in one one-off container from the
// game version's image (SPEC 5.6, 14.3). The game starts no server. The
// container has no network, a read-only root, no capabilities, a memory
// limit and PreviewLimit to finish, and mounts only a fresh folder of the
// data directory, deleted afterwards. One preview runs at a time: a second
// call waits up to previewWait for its turn, then is ErrPreviewBusy; a
// cancelled ctx ends the wait or the run. An image that is not downloaded is
// ErrPreviewImageMissing; a game that fails is a *PreviewError.
func (c *Controller) Preview(ctx context.Context, req PreviewRequest) ([]PNG, error) {
	line, err := checkPreview(req)
	if err != nil {
		return nil, err
	}
	var pngs []PNG
	err = c.previewRun(ctx, req, "preview", len(req.Planets), func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error {
		spec := c.previewSpec(ref, name, line, req)
		mounts, err := c.worldModMounts(base, name, req)
		if err != nil {
			return err
		}
		spec.HostConfig.Mounts = mounts
		if err := container(spec); err != nil {
			return err
		}
		pngs, err = readPreviews(base, name, req.Planets)
		return err
	})
	if err != nil {
		return nil, err
	}
	return pngs, nil
}

// previewRun is what Preview and the control-mod runs share (SPEC 14.3):
// the one slot, the image check, the base folder with its stale sweep, and
// a work folder removed afterwards. run is called with the work folder's
// name; the container func it is given runs one container to its end within
// previewRunLimit, as "foreman-<kind>-<folder>". The slot and folder are
// held until run returns, so run may read the folder.
func (c *Controller) previewRun(ctx context.Context, req PreviewRequest, kind string, drawn int,
	run func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error) error {
	return c.previewRunIn(ctx, previewPlan{
		gameVersion: req.GameVersion, kind: kind, limit: previewRunLimit(c.previewLimit, c.previewWorldLimit, req, drawn),
		fill: func(base *os.Root) (string, error) { return c.makePreviewDirFor(base, req) },
	}, run)
}

// previewRunLimit is how long a run of req that draws drawn planets may
// take: limit, or with a world's mods worldLimit for each planet, at most
// PreviewWorldLimitMax (SPEC 5.6, 14.3).
func previewRunLimit(limit, worldLimit time.Duration, req PreviewRequest, drawn int) time.Duration {
	if req.World == "" {
		return limit
	}
	return min(worldLimit*time.Duration(drawn), PreviewWorldLimitMax)
}

// previewMemoryFor is the memory limit of a run of req: larger when it
// loads a world's mods (SPEC 5.6).
func (c *Controller) previewMemoryFor(req PreviewRequest) int64 {
	if req.World == "" {
		return previewMemory
	}
	return c.previewWorldMemory
}

// previewPlan is what differs between the kinds of preview run.
type previewPlan struct {
	// gameVersion picks the image.
	gameVersion string
	// kind names the container.
	kind string
	// limit is how long the container may run.
	limit time.Duration
	// fill makes the run's work folder in base and returns its name.
	fill func(base *os.Root) (string, error)
}

// previewRunIn is previewRun for a plan of its own: the same slot, image
// check, stale sweep and clean-up.
func (c *Controller) previewRunIn(ctx context.Context, plan previewPlan,
	run func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error) error {
	kind := plan.kind
	if err := c.takePreviewSlot(ctx); err != nil {
		return err
	}
	defer func() { <-c.previewSlot }()

	ref, err := c.imageFor(plan.gameVersion)
	if err != nil {
		return err
	}
	if _, err := c.docker.InspectImage(ctx, ref); errors.Is(err, docker.ErrNotFound) {
		return ErrPreviewImageMissing
	} else if err != nil {
		return err
	}
	base, err := c.previewBase()
	if err != nil {
		return err
	}
	defer func() { _ = base.Close() }()
	c.removeStalePreviews(ctx, base)

	name, err := plan.fill(base)
	if err != nil {
		return err
	}
	defer func() {
		if err := base.RemoveAll(name); err != nil {
			c.log.Warn("could not remove a work folder", "kind", kind, "folder", name, "error", err)
		}
	}()
	return run(base, ref, name, func(spec docker.ContainerSpec) error {
		runCtx, cancel := context.WithTimeout(ctx, plan.limit)
		defer cancel()
		err := c.runOneOff(runCtx, "foreman-"+kind+"-"+name, spec)
		if pe, ok := errors.AsType[*PreviewError](err); ok {
			pe.Kind = kind
		}
		return err
	})
}

// takePreviewSlot waits for the one preview slot, up to previewWait, with
// at most maxPreviewWaiters callers waiting.
func (c *Controller) takePreviewSlot(ctx context.Context) error {
	select {
	case c.previewSlot <- struct{}{}:
		return nil
	default:
	}
	select {
	case c.previewWaiting <- struct{}{}:
		defer func() { <-c.previewWaiting }()
	default:
		return ErrPreviewBusy
	}
	wait := time.NewTimer(c.previewWait)
	defer wait.Stop()
	select {
	case c.previewSlot <- struct{}{}:
		return nil
	case <-wait.C:
		return ErrPreviewBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// checkPreview refuses a request the game line cannot draw, and returns the
// line. Planet names go to a script, so they must be the game's own.
func checkPreview(req PreviewRequest) (line string, err error) {
	line, ok := mapgen.Line(req.GameVersion)
	if !ok {
		return "", fmt.Errorf("%w: there is no preview for Factorio %q", ErrPreviewInvalid, req.GameVersion)
	}
	if req.World != "" && !world.ValidID(req.World) {
		return "", fmt.Errorf("%w: world %q", ErrPreviewInvalid, req.World)
	}
	known := req.knownPlanets(line)
	if len(req.Planets) == 0 || len(req.Planets) > len(known) {
		return "", fmt.Errorf("%w: planets", ErrPreviewInvalid)
	}
	for i, p := range req.Planets {
		if !mapgen.ValidName(p) || !slices.Contains(known, p) || slices.Contains(req.Planets[:i], p) {
			return "", fmt.Errorf("%w: planet %q", ErrPreviewInvalid, p)
		}
	}
	if !ValidPreviewSize(req.size()) {
		return "", fmt.Errorf("%w: size %d", ErrPreviewInvalid, req.Size)
	}
	if req.Preset != "" && !slices.Contains(mapgen.Presets(line), req.Preset) {
		return "", fmt.Errorf("%w: preset %q", ErrPreviewInvalid, req.Preset)
	}
	for _, b := range [][]byte{req.MapGenSettings, req.MapSettings} {
		if len(b) > gamesettings.MaxBytes || len(b) > 0 && !json.Valid(b) {
			return "", fmt.Errorf("%w: settings are not JSON, or too big", ErrPreviewInvalid)
		}
	}
	return line, nil
}

// previewPath is the folder that holds work folders, on this machine.
func (c *Controller) previewPath() string {
	return filepath.Join(filepath.Dir(c.worlds.Dirs().Worlds), previewDir)
}

// previewBase opens the folder that holds work folders, making it first.
func (c *Controller) previewBase() (*os.Root, error) {
	dir := c.previewPath()
	if err := os.MkdirAll(dir, world.DirMode); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

// newWorkDir makes an empty work folder with a random name in base.
func newWorkDir(base *os.Root) (string, error) {
	var b [8]byte
	_, _ = rand.Read(b[:])
	name := "p-" + hex.EncodeToString(b[:])
	if err := world.Mkdir(base, name); err != nil {
		return "", err
	}
	return name, nil
}

// makePreviewDir makes a fresh work folder in base and fills it with what the
// game reads: its config, a mod list that matches Space Age, and the settings
// files. The game's group can write the folder, as it can a world's.
func makePreviewDir(base *os.Root, req PreviewRequest) (string, error) {
	name, err := newWorkDir(base)
	if err != nil {
		return "", err
	}
	dir, err := base.OpenRoot(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = dir.Close() }()
	if err := world.Mkdir(dir, world.ModsDir); err != nil {
		return "", err
	}
	if err := writePreviewFiles(dir, req); err != nil {
		return "", err
	}
	list := path.Join(world.ModsDir, world.ModListFile)
	if err := dir.WriteFile(list, world.DefaultModList(req.GameVersion, req.SpaceAge), world.FileMode); err != nil {
		return "", err
	}
	return name, nil
}

// makePreviewDirFor makes the work folder of req: with the world's mods
// when req names a world, else with the base game and Space Age alone.
func (c *Controller) makePreviewDirFor(base *os.Root, req PreviewRequest) (string, error) {
	if req.World != "" {
		return c.makeWorldPreviewDir(base, req)
	}
	return makePreviewDir(base, req)
}

// writePreviewFiles writes the game's config and the request's settings
// files into a work folder.
func writePreviewFiles(dir *os.Root, req PreviewRequest) error {
	files := map[string][]byte{"config.ini": []byte(previewConfig)}
	if len(req.MapGenSettings) > 0 {
		files[world.MapGenSettingsFile] = req.MapGenSettings
	}
	if len(req.MapSettings) > 0 {
		files[world.MapSettingsFile] = req.MapSettings
	}
	for file, data := range files {
		if err := dir.WriteFile(file, data, world.FileMode); err != nil {
			return err
		}
	}
	return nil
}

// makeWorldPreviewDir makes the work folder of a run that loads the world
// req.World names (SPEC 14.3, ADR 0033): the config and settings files as
// makePreviewDir writes them, the world's mods linked in as for the mod
// settings run, and copies of its mod-list.json and mod-settings.dat. It
// never holds the world's own folder, and it holds the world's lock only
// while it reads the world. A world that server control holds is
// world.ErrBusy; one whose game version or Space Age (worldSpaceAge) is not
// the request's is ErrPreviewInvalid; mods past the link caps are world.ErrModsTooBig. The
// run mounts the linked zips read-only with worldModMounts.
func (c *Controller) makeWorldPreviewDir(base *os.Root, req PreviewRequest) (string, error) {
	check := func(meta world.Meta) error {
		if spaceAge := c.worldSpaceAge(req.World, meta); meta.GameVersion != req.GameVersion || spaceAge != req.SpaceAge {
			return fmt.Errorf("%w: the world is Factorio %s with Space Age %t", ErrPreviewInvalid, meta.GameVersion, spaceAge)
		}
		return nil
	}
	return c.fillWorldWorkDir(base, req.World, req, check, func(dir *os.Root) error {
		return copyModSettings(c.worlds, req.World, dir)
	})
}

// fillWorldWorkDir makes a work folder for a run that loads world id's mods:
// the config and the settings files of req (only the config for a zero req),
// then, with the world's lock held, check of the world's meta, the mods
// linked in, and extra. All three run in the one hold, so they describe the
// same world. Any failure removes the folder. A world that server control
// holds is world.ErrBusy; mods past the link caps are world.ErrModsTooBig.
func (c *Controller) fillWorldWorkDir(base *os.Root, id string, req PreviewRequest, check func(world.Meta) error, extra func(dir *os.Root) error) (string, error) {
	name, err := newWorkDir(base)
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = base.RemoveAll(name)
		return "", err
	}
	dir, err := base.OpenRoot(name)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = dir.Close() }()
	if err := writePreviewFiles(dir, req); err != nil {
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
	if err := check(meta); err != nil {
		return fail(err)
	}
	if err := c.worlds.LinkModsInto(id, filepath.Join(c.previewPath(), name)); err != nil {
		return fail(fmt.Errorf("could not put the world's mods in the work folder: %w", err))
	}
	if err := extra(dir); err != nil {
		return fail(err)
	}
	return name, nil
}

// worldModMounts is the read-only mounts of the mod zips in the work folder
// dir for a request that names a world, and none for one that does not.
func (c *Controller) worldModMounts(base *os.Root, dir string, req PreviewRequest) ([]docker.BindMount, error) {
	if req.World == "" {
		return nil, nil
	}
	return c.readOnlyZipMounts(base, dir)
}

// runOneOff makes and starts a one-off container, waits for it to exit, and
// removes it. Whatever happens, the container does not outlive the call: it
// is killed and deleted with a context that a cancelled request cannot cut
// short. A run past ctx's deadline is ErrPreviewTimeout; a non-zero exit is
// a *PreviewError.
func (c *Controller) runOneOff(ctx context.Context, name string, spec docker.ContainerSpec) error {
	id, err := c.docker.CreateContainer(ctx, name, spec)
	if err != nil {
		// Docker may have made the container before the call failed; the
		// name is all there is to find it by.
		c.removePreview(name)
		return err
	}
	defer c.removePreview(id)
	if err := c.docker.StartContainer(ctx, id); err != nil {
		return err
	}
	tick := time.NewTicker(previewPoll)
	defer tick.Stop()
	for {
		cont, err := c.docker.InspectContainer(ctx, id)
		switch {
		case ctx.Err() != nil:
			return c.previewStopped(ctx)
		case err != nil:
			return err
		case cont.State.Status == "exited" || cont.State.Status == "dead":
			if cont.State.ExitCode == 0 && !cont.State.OOMKilled {
				return nil
			}
			return &PreviewError{ExitCode: cont.State.ExitCode, OOM: cont.State.OOMKilled, Log: c.previewLog(id)}
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return c.previewStopped(ctx)
		}
	}
}

// previewStopped words why the wait ended: the deadline, or the caller.
func (c *Controller) previewStopped(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrPreviewTimeout
	}
	return ctx.Err()
}

// previewSpec is the preview container (SPEC 14.3, 14.4): the game's user,
// no network, a read-only root, no capabilities, a memory limit, no restart,
// and one bind mount, the work folder. dir is its name under previewDir.
func (c *Controller) previewSpec(image, dir, line string, req PreviewRequest) docker.ContainerSpec {
	env := []string{
		gameenv.PreviewPlanets + "=" + strings.Join(req.Planets, " "),
		gameenv.Seed + "=" + strconv.FormatUint(uint64(req.Seed), 10),
		gameenv.PreviewSize + "=" + strconv.Itoa(req.size()),
	}
	if req.Preset != "" {
		env = append(env, gameenv.Preset+"="+req.Preset)
	}
	// --map-preview-planet came with the planets of 2.x, the lines that have Space Age.
	if l, _ := mapgen.LineOf(line); len(l.SpaceAgeMods) > 0 {
		env = append(env, gameenv.PreviewPlanetFlag+"=1")
	}
	return docker.ContainerSpec{
		Image:      image,
		Labels:     map[string]string{gameenv.LabelPreview: c.hostData},
		Env:        env,
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"-c", previewScript},
		User:       c.user,
		HostConfig: c.oneOffHostConfigMem(dir, c.previewMemoryFor(req)),
	}
}

// oneOffHostConfig is the locked-down shape every one-off game container
// shares: no network, a read-only root, no capabilities, a memory limit, no
// restart, a tmpfs write folder and one bind mount, dir under previewDir.
func (c *Controller) oneOffHostConfig(dir string) docker.HostConfig {
	return c.oneOffHostConfigMem(dir, previewMemory)
}

// oneOffHostConfigMem is oneOffHostConfig with a memory limit of memory
// bytes, which also bounds swap to none.
func (c *Controller) oneOffHostConfigMem(dir string, memory int64) docker.HostConfig {
	return docker.HostConfig{
		Binds:          []string{filepath.Join(c.hostData, previewDir, dir) + ":" + gameenv.PreviewMount},
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
		RestartPolicy:  docker.RestartPolicy{Name: "no"},
		NetworkMode:    "none",
		ReadonlyRootfs: true,
		Memory:         memory,
		MemorySwap:     memory,
		Tmpfs:          map[string]string{previewWrite: "rw,noexec,nosuid,size=64m,mode=1777"},
	}
}

// previewLog returns the end of a failed preview's output, as one line.
func (c *Controller) previewLog(id string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := c.docker.Logs(ctx, id, docker.LogOptions{})
	if err != nil {
		return ""
	}
	defer func() { _ = s.Close() }()
	var lines []string
	for {
		l, err := s.Next()
		if err != nil {
			break
		}
		lines = append(lines, strings.TrimSpace(l.Text))
		lines = lines[max(0, len(lines)-5):]
	}
	return strings.Join(lines, " | ")
}

// removePreview kills a preview container if it still runs, then deletes it.
// It uses its own context: the request's may be over.
func (c *Controller) removePreview(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.docker.RemoveContainer(ctx, id, true); err != nil && !errors.Is(err, docker.ErrNotFound) {
		c.log.Warn("could not remove a map preview container", "container", id, "error", err)
	}
}

// removeStalePreviews deletes what an earlier preview left behind after a
// crash: containers carrying this install's preview label, and work folders.
// Only one preview runs at a time, so none of it is in use.
func (c *Controller) removeStalePreviews(ctx context.Context, base *os.Root) {
	list, err := c.docker.ListContainers(ctx, true, []string{gameenv.LabelPreview + "=" + c.hostData})
	if err != nil {
		c.log.Warn("could not look for old map preview containers", "error", err)
	}
	for _, s := range list {
		c.removePreview(s.ID)
	}
	entries, err := fs.ReadDir(base.FS(), ".")
	if err != nil {
		return
	}
	for _, e := range entries {
		if err := base.RemoveAll(e.Name()); err != nil {
			c.log.Warn("could not remove an old map preview folder", "folder", e.Name(), "error", err)
		}
	}
}

// pngSignature starts every PNG file.
var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// readPreviews reads the picture of each planet, at most MaxPreviewPNG bytes
// each, and checks it is a PNG. Each must be a regular file, so a named pipe
// the game left cannot hold the preview slot (SPEC 14.1).
func readPreviews(base *os.Root, dir string, planets []string) ([]PNG, error) {
	root, err := base.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	out := make([]PNG, 0, len(planets))
	for _, p := range planets {
		data, err := world.ReadSmallFile(root, p+".png", MaxPreviewPNG)
		if err != nil {
			return nil, fmt.Errorf("the game made no usable picture of %s: %w", p, err)
		}
		switch {
		case !bytes.HasPrefix(data, pngSignature):
			return nil, fmt.Errorf("the picture of %s is not a PNG", p)
		}
		out = append(out, PNG{Planet: p, Data: data})
	}
	return out, nil
}
