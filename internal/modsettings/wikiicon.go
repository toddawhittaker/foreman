package modsettings

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// WikiImageBase is where the Factorio wiki serves the game's icons. A
// browser loads them directly, and only while an admin has turned the
// setting on; Foreman never fetches, stores or proxies one (SPEC 14.10).
const WikiImageBase = WikiHost + "/images/"

// WikiHost is the one host a page may load images from besides itself.
const WikiHost = "https://wiki.factorio.com"

// protoNameRE is what a prototype name may hold for Foreman to build a wiki
// address from it: lower-case letters, digits, hyphen and underscore.
var protoNameRE = regexp.MustCompile(`^[a-z0-9_-]{1,100}$`)

// WikiIconFile is the wiki's image file name for a prototype name:
// space-science-pack becomes Space_science_pack. It reports false for any
// name outside protoNameRE, so a mod's text never reaches an address.
func WikiIconFile(name string) (string, bool) {
	if !protoNameRE.MatchString(name) {
		return "", false
	}
	r := []rune(strings.ReplaceAll(name, "-", "_"))
	r[0] = unicode.ToUpper(r[0])
	return string(r), true
}

// objectKind is the icon kind an object tag or an [img=kind/name] path names:
// the tag "planet" is a space location. It reports false for a kind the icon
// index is not keyed by, and for "starmap", which is not an object tag.
func objectKind(tag string) (string, bool) {
	if tag == "planet" {
		tag = "space-location"
	}
	if tag == "starmap" || !ValidIconKind(tag) {
		return "", false
	}
	return tag, true
}

// objectMarker is what an object tag renders to: the readable name in a span
// that carries the wiki file and, when the tag's kind is one the icon index
// knows, the object as "kind/name". The name alone is the safe default;
// ShowModIcons and ShowWikiIcons turn the marker into an image only for a page
// they are on for.
func objectMarker(file, kind, name, text string) string {
	obj := ""
	if k, ok := objectKind(kind); ok {
		obj = ` data-obj="` + k + `/` + name + `"`
	}
	return `<span class="rt-obj" data-wiki="` + file + `"` + obj + `>` + text + `</span>`
}

// markerRE matches objectMarker's output. The text is escaped HTML, so it
// holds no "<", the file is built from protoNameRE's characters, and the
// object is a kind from iconKinds and a name from protoNameRE.
var markerRE = regexp.MustCompile(`<span class="rt-obj" data-wiki="([A-Za-z0-9_]+)"(?: data-obj="([a-z-]+)/([a-z0-9_-]+)")?>([^<]*)</span>`)

// ShowModIcons replaces each object marker whose object has an icon in the
// world's mods with an image Foreman serves itself, from
// /worlds/{worldID}/icons/{kind}/{name}.png. The alt text is the object's
// name, which the image's onerror swaps in when the image does not load. A
// marker with no icon in idx stays as it is, for ShowWikiIcons and then the
// plain name (SPEC 14.6, 14.10). Run it before ShowWikiIcons.
func ShowModIcons(page []byte, worldID string, idx IconIndex) []byte {
	if idx.Len() == 0 {
		return page
	}
	base := "/worlds/" + html.EscapeString(url.PathEscape(worldID)) + "/icons/"
	return markerRE.ReplaceAllFunc(page, func(m []byte) []byte {
		sub := markerRE.FindSubmatch(m)
		if len(sub[2]) == 0 {
			return m
		}
		kind, name := string(sub[2]), string(sub[3])
		if _, ok := idx.Lookup(kind, name); !ok {
			return m
		}
		return []byte(`<img class="rt-obj-icon" src="` + base + kind + "/" + name + `.png" alt="` + string(sub[4]) + `" loading="lazy" onerror="this.replaceWith(this.alt)">`)
	})
}

// ShowWikiIcons replaces each object marker in rendered HTML with an image
// from the wiki. The alt text is the object's name, which the image's
// onerror swaps in when the image does not load, so a failed load shows the
// name and never a broken image (SPEC 14.10).
func ShowWikiIcons(page []byte) []byte {
	return markerRE.ReplaceAll(page, []byte(`<img class="rt-obj-icon" src="`+WikiImageBase+`$1.png" alt="$4" referrerpolicy="no-referrer" loading="lazy" onerror="this.replaceWith(this.alt)">`))
}
