package web

// installedTitles maps a world's installed mods to their titles from
// info.json, for the pages that name mods in a sentence (SPEC 9.15).
func (s *server) installedTitles(id string) map[string]string {
	return s.worlds.InstalledTitles(id)
}

// portalLacks says the portal client's cache already knows the portal does
// not have the mod. It never asks the portal.
func (s *server) portalLacks(name string) bool {
	known, ok := s.portal.(interface{ KnownMissing(string) bool })
	return ok && known.KnownMissing(name)
}
