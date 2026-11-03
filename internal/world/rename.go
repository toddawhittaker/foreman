package world

import "context"

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

	w, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	meta, err := readChangeable(w)
	if err != nil {
		return err
	}
	name, err = m.checkName(ctx, id, name)
	if err != nil {
		return err
	}
	meta.Name = name
	return WriteMeta(w, meta)
}
