package world

// SetUseWhitelist turns on or off the world's "only whitelisted players can
// join" switch, which the game reads at its next start (SPEC 8.3).
func (m *Manager) SetUseWhitelist(id string, on bool) error {
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	meta, err := readChangeable(root)
	if err != nil {
		return err
	}
	meta.UseWhitelist = on
	return WriteMeta(root, meta)
}
