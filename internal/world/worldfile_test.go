package world

import (
	"strings"
	"testing"
)

// The classifier accepts exactly the entries of the world file layout (SPEC
// 5.13, 14.5) and refuses everything else.
func TestWorldFileEntry(t *testing.T) {
	good := map[string]entryKind{
		"foreman-world.json":              entryFormat,
		"world.json":                      entryMeta,
		"saves/a.zip":                     entrySave,
		"saves/My Save 1.zip":             entrySave,
		"config/server-settings.json":     entryConfig,
		"mods/mod-list.json":              entryMod,
		"mods/mod-settings.dat":           entryMod,
		"mods/a_1.0.0.zip":                entryMod,
		"mods/folder-mod/info.json":       entryMod,
		"mods/folder-mod/locale/en/x.cfg": entryMod,
		"saves/":                          entryDir,
		"config/":                         entryDir,
		"mods/":                           entryDir,
		"mods/folder-mod/":                entryDir,
	}
	for name, want := range good {
		got, ok := worldFileEntry(name)
		if !ok || got != want {
			t.Errorf("%q: got %v, %v; want %v", name, got, ok, want)
		}
	}
	bad := []string{
		"",
		"/",
		"/world.json",
		"/etc/passwd",
		"../world.json",
		"saves/../world.json",
		"mods/../../x",
		"mods/a/../b",
		"mods/./a",
		"mods//a",
		"mods/a//b",
		`mods\a.zip`,
		`saves\a.zip`,
		"saves/a.zip/",
		"saves/sub/a.zip",
		"saves/.hidden.zip",
		"saves/a.tmp.zip",
		"saves/a.txt",
		"config/rconpw",
		"config/other.json",
		"config/sub/server-settings.json",
		"world.json/",
		"foreman-world.json/",
		"foreman-world.json2",
		"WORLD.json",
		"snapshots/a/world.json",
		"players.json",
		"other.txt",
		"other/",
		"mods",
		"saves",
		"mods/a\x00b",
		"mods/a\nb",
		"saves/a\x7f.zip",
		"mods/" + strings.Repeat("../", 3) + "x",
		"C:/x",
		"mods/C:\\x",
	}
	for _, name := range bad {
		if got, ok := worldFileEntry(name); ok {
			t.Errorf("%q: accepted as %v", name, got)
		}
	}
}
