package web

// AuditNewMap is the audit action of saving a world's map settings before its
// first start, from the page or through set_new_map (SPEC 5.6, 12.9).
const AuditNewMap = "new-map"

// newMapDetail is the audit detail of a saved map: the preset, and whether
// the settings change it, or that a map exchange string made it, then the
// world's game version. The page and set_new_map both write it.
func newMapDetail(f MapForm, version string) string {
	switch {
	case f.Mode == modeExchange:
		return "map exchange string, " + version
	case f.Modified:
		return "preset " + f.Preset + ", modified, " + version
	}
	return "preset " + f.Preset + ", " + version
}
