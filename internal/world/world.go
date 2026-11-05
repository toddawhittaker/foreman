// Package world manages worlds on disk: each one a folder of saves, mods,
// config and world.json (SPEC 5.1, 5.2). Every file operation inside a
// world goes through os.Root, so a planted symlink or a ".." can never
// reach outside it.
package world

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Folders inside a world (SPEC 5.1).
const (
	SavesDir  = "saves"
	ModsDir   = "mods"
	ConfigDir = "config"
)

// MaxIDLength is the longest a world id may be.
const MaxIDLength = 40

// ActiveStore records which world is active (SPEC 5.3). *store.Store
// satisfies it.
type ActiveStore interface {
	ActiveWorld(ctx context.Context) (id string, found bool, err error)
	SetActiveWorld(ctx context.Context, id string, now time.Time) error
}

// Dirs are the folders a Manager works in.
type Dirs struct {
	Worlds    string
	Snapshots string
	Imports   string
}

// Manager owns the worlds and snapshots folders.
type Manager struct {
	dirs   Dirs
	active ActiveStore
	now    func() time.Time
	// log takes warnings about work that failed after an operation had
	// already succeeded, so the caller is not told it failed.
	log *slog.Logger

	locks locks
	// createMu is held while a new world's id is chosen and its folder made,
	// so two creations cannot pick the same id or name.
	createMu sync.Mutex

	// addSaveWait is how long AddSave waits for a busy world.
	addSaveWait time.Duration
	// afterStage is a test seam, called once a restore has staged its world
	// and before the final name check. Nil in production.
	afterStage func()
}

// ErrNameTaken is returned when another world already has the name,
// ignoring case.
var ErrNameTaken = errors.New("another world already has this name. Choose a different name")

// ErrNameEmpty is returned for a blank name.
var ErrNameEmpty = errors.New("enter a name for the world")

// ErrWorldsIsLink is returned by NewManager when the worlds folder is a
// symbolic link.
var ErrWorldsIsLink = errors.New("the worlds folder is a symbolic link, which Foreman refuses to use. Replace it with a real folder")

// NewManager makes the worlds, snapshots and imports folders if they are
// missing. now may be nil for time.Now.
func NewManager(dirs Dirs, active ActiveStore, now func() time.Time) (*Manager, error) {
	if now == nil {
		now = time.Now
	}
	// A link here would send every world's files, and the chmod below,
	// somewhere else (SPEC 14.5).
	if info, err := os.Lstat(dirs.Worlds); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrWorldsIsLink, dirs.Worlds)
	}
	for _, d := range []string{dirs.Worlds, dirs.Snapshots, dirs.Imports} {
		if err := os.Mkdir(d, DirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("make folder %s: %w", d, err)
		}
		if err := chmodFolder(d, DirMode); err != nil {
			return nil, fmt.Errorf("set mode of %s: %w", d, err)
		}
	}
	return &Manager{dirs: dirs, active: active, now: now, log: slog.Default(), addSaveWait: AddSaveWait}, nil
}

// Dirs returns the folders the Manager works in.
func (m *Manager) Dirs() Dirs { return m.dirs }

// OpenWorld opens a world's folder as a root. Callers close it.
func (m *Manager) OpenWorld(id string) (*os.Root, error) {
	if !ValidID(id) {
		return nil, fs.ErrNotExist
	}
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = worlds.Close() }()
	return openDir(worlds, id)
}

// OpenWorlds opens the worlds folder, refusing a link in its place. The
// caller closes it (SPEC 14.5).
func (m *Manager) OpenWorlds() (*os.Root, error) {
	return openFolder(m.dirs.Worlds)
}

// ValidID reports whether id has the shape IDFromName makes.
func ValidID(id string) bool {
	if id == "" || len(id) > MaxIDLength || id[0] == '-' || id[len(id)-1] == '-' {
		return false
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// IDFromName makes the base of a world id from its name: lowercase ASCII
// letters and digits, other runs turned into one hyphen, at most
// MaxIDLength characters. A name with no letters or digits becomes "world".
func IDFromName(name string) string {
	var b strings.Builder
	hyphen := false
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			hyphen = false
			b.WriteRune(c)
			continue
		}
		hyphen = true
	}
	id := b.String()
	if len(id) > MaxIDLength {
		id = strings.TrimRight(id[:MaxIDLength], "-")
	}
	if id == "" {
		id = "world"
	}
	return id
}

// newID returns an id for name that no world and no snapshot folder uses:
// the base, then base-2, base-3 and so on. An id is never reused, so a
// deleted world's snapshots stay its own. Call with createMu held.
func (m *Manager) newID(name string) (string, error) {
	base := IDFromName(name)
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			suffix := "-" + strconv.Itoa(n)
			id = strings.TrimRight(base[:min(len(base), MaxIDLength-len(suffix))], "-") + suffix
		}
		taken, err := m.idTaken(id)
		if err != nil {
			return "", err
		}
		if !taken {
			return id, nil
		}
	}
}

func (m *Manager) idTaken(id string) (bool, error) {
	for _, dir := range []string{m.dirs.Worlds, m.dirs.Snapshots} {
		_, err := os.Lstat(dir + "/" + id)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

// checkName trims name and refuses a blank one or one that a world other
// than id has, ignoring case (SPEC 5.2). id is empty for a new world. Call with
// createMu held, so no other world can take the name meanwhile.
func (m *Manager) checkName(ctx context.Context, id, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameEmpty
	}
	worlds, err := m.List(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range worlds {
		if w.ID != id && strings.EqualFold(w.Name, name) {
			return "", ErrNameTaken
		}
	}
	return name, nil
}

// Create makes an empty world: its folders and world.json. The first world
// made becomes active, so there is always a world to start. It returns the
// new world's id.
func (m *Manager) Create(ctx context.Context, name, gameVersion string, spaceAge bool) (string, error) {
	return m.create(ctx, name, gameVersion, spaceAge, nil, nil)
}

// create builds a whole new world, world.json included, in one staging
// folder and renames it into place once, so a failure or crash at any
// point leaves no world at all (SPEC 5.5). fill, when not nil, adds files
// to the staging folder; it gets the worlds folder and the staging
// folder's name in it, and runs before createMu is taken, so a long copy
// does not hold up other worlds.
func (m *Manager) create(ctx context.Context, name, gameVersion string, spaceAge bool, from *ImportedFrom, fill func(worlds *os.Root, dir string) error) (string, error) {
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return "", err
	}
	defer func() { _ = worlds.Close() }()

	staging := PrefixStaging + "new-" + randomSuffix()
	committed := false
	defer func() {
		if !committed {
			_ = worlds.RemoveAll(staging)
		}
	}()
	if err := mkdirWorld(worlds, staging); err != nil {
		return "", err
	}
	if fill != nil {
		if err := fill(worlds, staging); err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	m.createMu.Lock()
	defer m.createMu.Unlock()
	name, err = m.checkName(ctx, "", name)
	if err != nil {
		return "", err
	}
	id, err := m.newID(name)
	if err != nil {
		return "", err
	}
	if err := writeMetaIn(worlds, staging, Meta{
		FormatVersion: FormatVersion,
		Name:          name,
		GameVersion:   gameVersion,
		SpaceAge:      spaceAge,
		CreatedAt:     m.now().UTC(),
		ImportedFrom:  from,
	}); err != nil {
		return "", err
	}
	if err := worlds.Rename(staging, id); err != nil {
		return "", err
	}
	committed = true
	if err := syncDir(worlds, "."); err != nil {
		return "", err
	}
	if err := m.activateIfNone(ctx, id); err != nil {
		return "", err
	}
	return id, nil
}

// mkdirWorld makes dir in worlds with a world's empty folders.
func mkdirWorld(worlds *os.Root, dir string) error {
	if err := Mkdir(worlds, dir); err != nil {
		return err
	}
	for _, sub := range []string{SavesDir, ModsDir, ConfigDir} {
		if err := Mkdir(worlds, path.Join(dir, sub)); err != nil {
			return err
		}
	}
	return nil
}

func writeMetaIn(worlds *os.Root, dir string, meta Meta) error {
	w, err := worlds.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	if err := WriteMeta(w, meta); err != nil {
		return err
	}
	return syncDir(w, ".")
}

// activateIfNone makes id active when no world is.
func (m *Manager) activateIfNone(ctx context.Context, id string) error {
	_, found, err := m.active.ActiveWorld(ctx)
	if err != nil || found {
		return err
	}
	return m.active.SetActiveWorld(ctx, id, m.now())
}
