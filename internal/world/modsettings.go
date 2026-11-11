// Contract stub: task T2 of the mod settings epic fills these bodies.

package world

import "errors"

var errModSettingsNotBuilt = errors.New("world: mod settings not built yet")

// ModSettingsKey returns the cache key for a world's mod setting
// definitions: a SHA-256 of the game version plus the mods fingerprint
// without mod-settings.dat, since setting values cannot change definitions
// (SPEC 9.6).
func (m *Manager) ModSettingsKey(id string) (string, error) {
	return "", errModSettingsNotBuilt
}

// LinkModsInto fills dst, a definitions run's mods folder, from the world's
// mods/: zips by hard link (copy when linking fails), folder mods and
// mod-list.json by copy, DefaultModList when the world has none. It never
// copies mod-settings.dat.
func (m *Manager) LinkModsInto(id, dst string) error {
	return errModSettingsNotBuilt
}

// ModLocaleFiles returns the raw locale .cfg files of each enabled mod,
// keyed by mod name, then Factorio locale code, for the codes given. It
// enforces the per-file and per-language limits in package modsettings.
func (m *Manager) ModLocaleFiles(id string, codes []string) (map[string]map[string][][]byte, error) {
	return nil, errModSettingsNotBuilt
}
