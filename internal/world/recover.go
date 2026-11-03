package world

import (
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
