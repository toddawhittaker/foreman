package web

// PathNewMap is a world's map settings page, for a world whose map is not
// made yet (SPEC 5.6).
const PathNewMap = "/worlds/{id}/new-map"

// newMapPath is the map settings page of the world id.
func newMapPath(id string) string { return worldPath(id) + "/new-map" }
