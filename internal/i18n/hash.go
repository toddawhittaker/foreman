package i18n

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// HashFile is translated.json's path from the repository root. It records,
// for each key, a short hash of the English the translations were made
// from, so a later change to the English is caught (SPEC 22.5).
const HashFile = "internal/i18n/translated.json"

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

// HashJSON is Hashes as the indented JSON translated.json holds.
func HashJSON() ([]byte, error) {
	b, err := json.MarshalIndent(Hashes(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
