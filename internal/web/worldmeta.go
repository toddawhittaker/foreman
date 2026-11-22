package web

import "github.com/toddawhittaker/foreman/internal/world"

// readWorldMeta reads a world's world.json.
func (s *server) readWorldMeta(id string) (world.Meta, error) {
	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		return world.Meta{}, err
	}
	defer func() { _ = root.Close() }()
	return world.ReadMeta(root)
}
