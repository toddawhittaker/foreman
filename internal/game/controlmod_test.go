package game

import "testing"

// The game loads a control mod only when factorio_version is its own line,
// and the territory mod needs Space Age, which has Vulcanus. The count mod
// depends on Space Age only when the map has it. The bytes are pinned so a
// change to modInfo cannot alter what the game reads.
func TestModInfoJSON(t *testing.T) {
	for name, tc := range map[string]struct{ got, want string }{
		"territories 2.1": {string(territoryInfo("2.1")), `{
  "name": "foreman-territories",
  "version": "1.0.0",
  "title": "Foreman territories",
  "author": "Foreman",
  "factorio_version": "2.1",
  "description": "Writes Vulcanus's demolisher territories for Foreman's map preview. Adds no prototypes.",
  "dependencies": [
    "base",
    "space-age"
  ]
}
`},
		"resources 1.1": {string(resourceInfo("1.1", false)), `{
  "name": "foreman-resources",
  "version": "1.0.0",
  "title": "Foreman resources",
  "author": "Foreman",
  "factorio_version": "1.1",
  "description": "Counts one planet's resource patches for Foreman's map preview. Adds no prototypes.",
  "dependencies": [
    "base"
  ]
}
`},
		"resources 2.0 Space Age": {string(resourceInfo("2.0", true)), `{
  "name": "foreman-resources",
  "version": "1.0.0",
  "title": "Foreman resources",
  "author": "Foreman",
  "factorio_version": "2.0",
  "description": "Counts one planet's resource patches for Foreman's map preview. Adds no prototypes.",
  "dependencies": [
    "base",
    "space-age"
  ]
}
`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: info.json is\n%s\nwant\n%s", name, tc.got, tc.want)
		}
	}
}
