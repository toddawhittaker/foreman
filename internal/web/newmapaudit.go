package web

// AuditNewMap is the audit action of saving a world's map settings before its
// first start, from the page or through set_new_map (SPEC 5.6, 12.9).
const AuditNewMap = "new-map"

// newMapDetail is the audit detail of a saved map: the preset, and whether
// the settings change it, or that a map exchange string made it.
func newMapDetail(f MapForm) string {
	if f.Mode == modeExchange {
		return "map exchange string"
	}
	if f.Modified {
		return "preset " + f.Preset + ", modified"
	}
	return "preset " + f.Preset
}
