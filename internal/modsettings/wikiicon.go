package modsettings

import (
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

// objectMarker is what an object tag renders to: the readable name in a span
// that carries the wiki file. The name alone is the safe default; ShowWikiIcons
// turns the marker into an image only for a page the setting is on for.
func objectMarker(file, text string) string {
	return `<span class="rt-obj" data-wiki="` + file + `">` + text + `</span>`
}

// markerRE matches objectMarker's output. The text is escaped HTML, so it
// holds no "<", and the file is built from protoNameRE's characters.
var markerRE = regexp.MustCompile(`<span class="rt-obj" data-wiki="([A-Za-z0-9_]+)">([^<]*)</span>`)

// ShowWikiIcons replaces each object marker in rendered HTML with an image
// from the wiki. The alt text is the object's name, which the image's
// onerror swaps in when the image does not load, so a failed load shows the
// name and never a broken image (SPEC 14.10).
func ShowWikiIcons(page []byte) []byte {
	return markerRE.ReplaceAll(page, []byte(`<img class="rt-obj-icon" src="`+WikiImageBase+`$1.png" alt="$2" referrerpolicy="no-referrer" loading="lazy" onerror="this.replaceWith(this.alt)">`))
}
