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
	"os"
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

	locks locks
	// createMu is held while a new world's id is chosen and its folder made,
	// so two creations cannot pick the same id or name.
	createMu sync.Mutex
}

// ErrNameTaken is returned when another world already has the name,
// ignoring case.
var ErrNameTaken = errors.New("another world already has this name. Choose a different name")

// ErrNameEmpty is returned for a blank name.
var ErrNameEmpty = errors.New("enter a name for the world")

// NewManager makes the worlds, snapshots and imports folders if they are
// missing. now may be nil for time.Now.
func NewManager(dirs Dirs, active ActiveStore, now func() time.Time) (*Manager, error) {
	if now == nil {
		now = time.Now
	}
	for _, d := range []string{dirs.Worlds, dirs.Snapshots, dirs.Imports} {
		if err := os.Mkdir(d, DirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("make folder %s: %w", d, err)
		}
		if err := os.Chmod(d, DirMode); err != nil {
			return nil, fmt.Errorf("set mode of %s: %w", d, err)
		}
	}
	return &Manager{dirs: dirs, active: active, now: now}, nil
}

// Dirs returns the folders the Manager works in.
func (m *Manager) Dirs() Dirs { return m.dirs }

// OpenWorld opens a world's folder as a root. Callers close it.
func (m *Manager) OpenWorld(id string) (*os.Root, error) {
	if !ValidID(id) {
		return nil, fs.ErrNotExist
	}
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = worlds.Close() }()
	return worlds.OpenRoot(id)
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

// checkName trims name and refuses a blank one or one another world has,
// ignoring case. Call with createMu held.
func (m *Manager) checkName(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameEmpty
	}
	worlds, err := m.List(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range worlds {
		if strings.EqualFold(w.Name, name) {
			return "", ErrNameTaken
		}
	}
	return name, nil
}

// Create makes an empty world: its folders and world.json. The first world
// made becomes active, so there is always a world to start. It returns the
// new world's id.
func (m *Manager) Create(ctx context.Context, name, gameVersion string, spaceAge bool) (string, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	name, err := m.checkName(ctx, name)
	if err != nil {
		return "", err
	}
	id, err := m.newID(name)
	if err != nil {
		return "", err
	}
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return "", err
	}
	defer func() { _ = worlds.Close() }()

	// Built under a staging name and renamed into place, so a crash never
	// leaves a half-made world that looks real.
	staging := PrefixStaging + id + "-" + randomSuffix()
	if err := m.buildEmpty(worlds, staging, Meta{
		FormatVersion: FormatVersion,
		Name:          name,
		GameVersion:   gameVersion,
		SpaceAge:      spaceAge,
		CreatedAt:     m.now().UTC(),
	}); err != nil {
		_ = worlds.RemoveAll(staging)
		return "", err
	}
	if err := worlds.Rename(staging, id); err != nil {
		_ = worlds.RemoveAll(staging)
		return "", err
	}
	if err := syncDir(worlds, "."); err != nil {
		return "", err
	}
	if err := m.activateIfNone(ctx, id); err != nil {
		return "", err
	}
	return id, nil
}

func (m *Manager) buildEmpty(worlds *os.Root, dir string, meta Meta) error {
	if err := Mkdir(worlds, dir); err != nil {
		return err
	}
	w, err := worlds.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	for _, sub := range []string{SavesDir, ModsDir, ConfigDir} {
		if err := Mkdir(w, sub); err != nil {
			return err
		}
	}
	return WriteMeta(w, meta)
}

// activateIfNone makes id active when no world is.
func (m *Manager) activateIfNone(ctx context.Context, id string) error {
	_, found, err := m.active.ActiveWorld(ctx)
	if err != nil || found {
		return err
	}
	return m.active.SetActiveWorld(ctx, id, m.now())
}
