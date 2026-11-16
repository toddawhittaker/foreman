package web

// installedTitles maps a world's installed mods to their titles from
// info.json, for the pages that name mods in a sentence (SPEC 9.15). A mod
// with no title is left out, and so shows by its name. A mods folder that
// cannot be read gives none.
func (s *server) installedTitles(id string) map[string]string {
	installed, err := s.worlds.Mods(id)
	if err != nil {
		s.log.Warn("read the mod titles", "world", id, "error", err)
		return nil
	}
	titles := map[string]string{}
	for _, m := range installed {
		if m.Title != "" {
			titles[m.Name] = m.Title
		}
	}
	return titles
}
