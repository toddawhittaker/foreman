package modsettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode"
)

// Limits on a world's icon index (SPEC 14.6). A file past either is refused
// whole.
const (
	MaxIconIndexBytes   = 4 << 20
	MaxIconIndexEntries = 20000
	// MaxIconIndexMods bounds the active mod list the index carries.
	MaxIconIndexMods = 2048
	// MaxIconPathLen bounds an in-zip icon path.
	MaxIconPathLen = 512
	// MaxIconSize is the largest icon size, in pixels, an entry may claim:
	// the reader refuses a picture over 4096 by 4096 anyway.
	MaxIconSize = 4096
	// maxModNameLen is the longest mod name Foreman accepts, the same as
	// internal/world: <name>_<version>.zip must fit a 255-byte file name.
	maxModNameLen = 200
)

var (
	errIconIndexSize  = errors.New("modsettings: the icon index is too large")
	errIconIndexJSON  = errors.New("modsettings: the icon index is not valid")
	errIconIndexCount = errors.New("modsettings: the icon index lists too many entries")
)

// iconKinds are the object kinds an icon is looked up by. They are the
// prototype kinds an object tag in a setting's text can name, with the tag
// "planet" folded into "space-location" by the caller, plus "starmap" for a
// space location's star map picture.
var iconKinds = map[string]bool{
	"item": true, "fluid": true, "recipe": true, "technology": true,
	"entity": true, "tile": true, "virtual-signal": true,
	"space-location": true, "quality": true, "achievement": true,
	"item-group": true, "equipment": true, "asteroid-chunk": true,
	"starmap": true,
}

// ValidIconKind reports whether kind is one the icon index is keyed by.
func ValidIconKind(kind string) bool { return iconKinds[kind] }

// builtInMods are the game's own mods. Their art is Wube's, so an icon path
// under one is never served (SPEC 14.5, 14.10).
var builtInMods = map[string]bool{
	"base": true, "core": true, "space-age": true,
	"quality": true, "elevated-rails": true, "recycler": true, // 2.1 adds recycler
}

var iconVersionRE = regexp.MustCompile(`^\d+(\.\d+){1,3}$`)

// IconRef is where one object's icon is: the mod that owns it, that mod's
// active version, the path inside the mod's zip (without the zip's top
// folder, such as "graphics/widget.png") and the size in pixels of the
// icon's first picture, which is the top-left Size by Size square of the
// file.
type IconRef struct {
	Mod     string
	Version string
	// Path is the in-zip path with the leading "__mod__/" removed.
	Path string
	Size int
}

type iconKey struct{ kind, name string }

// IconIndex maps an object's kind and name to its icon in a world's mods. The
// zero value finds nothing.
type IconIndex struct {
	icons map[iconKey]IconRef
	// Dropped counts entries left out for a bad kind, name, path, size or
	// mod, or for repeating an earlier entry.
	Dropped int
}

// Len is the number of icons the index holds.
func (x IconIndex) Len() int { return len(x.icons) }

// Lookup returns the icon of the object of the given kind and name.
func (x IconIndex) Lookup(kind, name string) (IconRef, bool) {
	r, ok := x.icons[iconKey{kind, name}]
	return r, ok
}

// The icon file a world's mod-settings run writes (icons.json). Its shape,
// which internal/game/modsettings/data-final-fixes.lua and control.lua write:
//
//	{
//	  "mods": {                          // the active mods and their versions
//	    "fake-planet": "1.0.0"
//	  },
//	  "icons": [                         // one per object with a plain icon
//	    {
//	      "kind": "item",                // see iconKinds; "starmap" for a
//	                                     // space location's starmap_icon
//	      "name": "fake-widget",         // the prototype name
//	      "path": "__fake-planet__/graphics/widget.png",
//	      "size": 64                     // the icon_size (or starmap_icon_size)
//	    }
//	  ]
//	}
//
// The export holds only icons of one untinted layer: a plain "icon", or
// "icons" with one entry that has no tint, shift or scale. An empty list or
// map may be written as {} (Lua's empty table). Unknown fields are ignored.
type rawIconIndex struct {
	Mods  map[string]string `json:"mods"`
	Icons luaIconList       `json:"icons"`
}

type rawIcon struct {
	Kind string  `json:"kind"`
	Name string  `json:"name"`
	Path string  `json:"path"`
	Size float64 `json:"size"`
}

// luaIconList reads a JSON array, or {} or null for an empty one.
type luaIconList []rawIcon

func (l *luaIconList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" || string(b) == "{}" {
		*l = nil
		return nil
	}
	var v []rawIcon
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*l = v
	return nil
}

// DecodeIconIndex reads a world's icon file under the SPEC 14.6 limits: a
// file over MaxIconIndexBytes, with more than MaxIconIndexEntries entries or
// MaxIconIndexMods mods, or not of the shape above is refused whole, with an
// error that holds no text from the file. An entry is left out and counted in
// Dropped when its kind is not one of iconKinds, its name is not
// ^[a-z0-9_-]{1,100}$, its size is not a whole number from 1 to
// MaxIconSize, it repeats an earlier entry's kind and name, or its path is
// not safe to read from the mod's zip: it must be "__<mod>__/<file>.png"
// for a mod in "mods" with a version of dotted numbers, the mod must not be
// one of the game's own, and the file part must have no empty, "." or ".."
// segment, no backslash, no control character, and be under MaxIconPathLen
// characters. A mod listed with a bad name or version has all its entries
// left out.
func DecodeIconIndex(data []byte) (IconIndex, error) {
	if len(data) > MaxIconIndexBytes {
		return IconIndex{}, errIconIndexSize
	}
	var raw rawIconIndex
	// Errors are replaced, so no text from the file reaches a log.
	if err := json.Unmarshal(data, &raw); err != nil {
		return IconIndex{}, errIconIndexJSON
	}
	if len(raw.Icons) > MaxIconIndexEntries || len(raw.Mods) > MaxIconIndexMods {
		return IconIndex{}, errIconIndexCount
	}
	x := IconIndex{icons: make(map[iconKey]IconRef, len(raw.Icons))}
	for _, e := range raw.Icons {
		ref, ok := e.check(raw.Mods)
		k := iconKey{e.Kind, e.Name}
		if _, dup := x.icons[k]; !ok || dup {
			x.Dropped++
			continue
		}
		x.icons[k] = ref
	}
	return x, nil
}

// check validates one entry against the active mods.
func (e rawIcon) check(mods map[string]string) (IconRef, bool) {
	if !iconKinds[e.Kind] || !protoNameRE.MatchString(e.Name) {
		return IconRef{}, false
	}
	if e.Size < 1 || e.Size > MaxIconSize || e.Size != math.Trunc(e.Size) {
		return IconRef{}, false
	}
	mod, rest, ok := splitIconPath(e.Path)
	if !ok {
		return IconRef{}, false
	}
	version, active := mods[mod]
	if !active || !iconVersionRE.MatchString(version) {
		return IconRef{}, false
	}
	return IconRef{Mod: mod, Version: version, Path: rest, Size: int(e.Size)}, true
}

// splitIconPath splits "__<mod>__/<file>" into the mod and the file, and
// reports whether the path is one Foreman may read from the mod's zip.
func splitIconPath(p string) (mod, rest string, ok bool) {
	if len(p) > MaxIconPathLen || !strings.HasPrefix(p, "__") || !strings.HasSuffix(p, ".png") {
		return "", "", false
	}
	// The prefix ends at the first "__/", so a mod name may end in "_".
	mod, rest, found := strings.Cut(p[2:], "__/")
	if !found || !validIconModName(mod) || builtInMods[mod] {
		return "", "", false
	}
	if strings.Contains(rest, `\`) {
		return "", "", false
	}
	for _, r := range rest {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return "", "", false
		}
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", "", false
		}
	}
	return mod, rest, true
}

// validIconModName is the rule internal/world.ValidModName applies to a mod
// name, repeated here because that package imports this one.
func validIconModName(name string) bool {
	if name == "" || len(name) > maxModNameLen || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}
