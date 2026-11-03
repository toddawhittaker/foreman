package world

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"
)

// leftoverPrefixes name work that a crash interrupted and that is safe to
// throw away: none of it was ever the real copy of anything.
var leftoverPrefixes = []string{PrefixPartial, PrefixStaging, PrefixDeleting}

// Recover runs once at startup, before anything else touches the worlds or
// snapshots folders, and clears what an interrupted operation left behind.
// Each step is its own function so a step that must finish work instead of
// discarding it can be added in order.
func (m *Manager) Recover(log *slog.Logger) error {
	if err := m.finishSwaps(log); err != nil {
		return err
	}
	for _, dir := range []string{m.dirs.Worlds, m.dirs.Snapshots} {
		if err := removeLeftovers(dir, log); err != nil {
			return err
		}
	}
	return nil
}

// removeLeftovers deletes every file or folder under dir whose name starts
// with a leftover prefix.
func removeLeftovers(dir string, log *slog.Logger) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var found []string
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && isLeftover(d.Name()) {
			found = append(found, p)
			if d.IsDir() {
				return fs.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("look for leftovers in %s: %w", dir, err)
	}
	for _, p := range found {
		if err := root.RemoveAll(p); err != nil {
			return fmt.Errorf("remove leftover %s: %w", path.Join(dir, p), err)
		}
		log.Info("removed leftover from an interrupted operation", "path", path.Join(dir, p))
	}
	return nil
}

func isLeftover(name string) bool {
	for _, p := range leftoverPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// finishSwaps settles a restore that stopped between its renames (R9). If
// the world's folder is there, the restored world was swapped in and the
// old one is removed. If not, the old one is put back; the staged
// replacement is a leftover the next step removes.
func (m *Manager) finishSwaps(log *slog.Logger) error {
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return err
	}
	defer func() { _ = worlds.Close() }()
	entries, err := fs.ReadDir(worlds.FS(), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		id, ok := restoreOldID(e.Name())
		if !ok {
			continue
		}
		_, err := worlds.Lstat(id)
		switch {
		case err == nil:
			if err := worlds.RemoveAll(e.Name()); err != nil {
				return fmt.Errorf("remove the old copy of restored world %s: %w", id, err)
			}
			log.Info("finished an interrupted restore", "world", id)
		case errors.Is(err, fs.ErrNotExist):
			if err := worlds.Rename(e.Name(), id); err != nil {
				return fmt.Errorf("put back world %s after an interrupted restore: %w", id, err)
			}
			log.Warn("reversed an interrupted restore; the world is as it was before", "world", id)
		default:
			return err
		}
	}
	return nil
}

// restoreOldID reads the world id from a name swapIn made: the prefix, the
// id, a hyphen and a 16-character suffix.
func restoreOldID(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, PrefixRestoreOld)
	if !ok || len(rest) < 18 || rest[len(rest)-17] != '-' {
		return "", false
	}
	id := rest[:len(rest)-17]
	return id, ValidID(id)
}
