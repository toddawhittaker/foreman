package world

import (
	"context"

	"strings"
)

// Rename changes a world's display name only; its id and folder never
// change (SPEC 5.2, 5.8). It refuses a name another world has, ignoring
// case, and a world a newer Foreman wrote.
func (m *Manager) Rename(ctx context.Context, id, name string) error {
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	// Held so a world made meanwhile cannot take the same name.
	m.createMu.Lock()
	defer m.createMu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNameEmpty
	}
	w, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	meta, err := ReadMeta(w)
	if err != nil {
		return err
	}
	if meta.Newer() {
		return ErrNewerFormat
	}
	if err := m.nameTakenByOther(ctx, id, name); err != nil {
		return err
	}
	meta.Name = name
	return WriteMeta(w, meta)
}

// nameTakenByOther returns ErrNameTaken when a world other than id has
// name, ignoring case.
func (m *Manager) nameTakenByOther(ctx context.Context, id, name string) error {
	worlds, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, w := range worlds {
		if w.ID != id && strings.EqualFold(w.Name, name) {
			return ErrNameTaken
		}
	}
	return nil
}
