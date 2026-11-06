package i18n

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// HashDir is the folder, from the repository root, holding one
// translated/<area>.json per catalogue area. Each records, for every key of
// the area, a short hash of the English the translations were made from,
// so a later change to the English is caught (SPEC 22.5). One file per area
// lets each area change without touching another's.
const HashDir = "internal/i18n/translated"

// Hashes returns the short hash of every English entry, by key.
func Hashes() map[string]string {
	out := map[string]string{}
	for k, e := range catalogue[English] {
		out[k] = hashEntry(e)
	}
	return out
}

func hashEntry(e entry) string {
	var b []byte
	if e.plural == nil {
		b = []byte(e.text)
	} else {
		cats := make([]string, 0, len(e.plural))
		for c := range e.plural {
			cats = append(cats, c)
		}
		sort.Strings(cats)
		for _, c := range cats {
			b = append(b, c+"="+e.plural[c]+"\n"...)
		}
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}

// Areas lists the catalogue areas, from the English files.
func Areas() []string {
	files, _ := fs.Glob(localeFS, "locales/en/*.json")
	var out []string
	for _, f := range files {
		out = append(out, strings.TrimSuffix(path.Base(f), ".json"))
	}
	return out
}

// HashFiles returns the content of each area's hash file, by area.
func HashFiles() (map[string][]byte, error) {
	byArea := map[string]map[string]string{}
	for _, a := range Areas() {
		byArea[a] = map[string]string{}
	}
	for k, h := range Hashes() {
		a, _, _ := strings.Cut(k, ".")
		if byArea[a] == nil {
			byArea[a] = map[string]string{}
		}
		byArea[a][k] = h
	}
	out := map[string][]byte{}
	for a, m := range byArea {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return nil, err
		}
		out[a] = append(b, '\n')
	}
	return out, nil
}
