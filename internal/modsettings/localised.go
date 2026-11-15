package modsettings

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Bounds on resolving one localised string (SPEC 14.6). Past either, the
// string does not resolve and the caller falls back.
const (
	// MaxLocalisedDepth caps how deeply a localised string's parameters nest.
	MaxLocalisedDepth = 20
	// MaxLocalisedChars caps a resolved string's length in characters.
	MaxLocalisedChars = 1000
)

// LocaleSections is one language's locale text: section, then key, then the
// text as written in the .cfg files.
type LocaleSections map[string]map[string]string

// ResolveLocalised resolves a localised string as the game reports it in
// JSON, the way the game does: a plain string is itself; an array is a key
// ("section.name") and parameters, each a localised string, put in place of
// __1__, __2__ and so on in the key's text; the key "" joins its parameters.
// It returns false when the string is absent, a key is missing, or a bound is
// passed. The result is untrusted text, to be escaped when shown.
func ResolveLocalised(raw json.RawMessage, sections LocaleSections) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	r := resolver{sections: sections}
	s, ok := r.resolve(v, 0)
	if !ok || utf8.RuneCountInString(s) > MaxLocalisedChars {
		return "", false
	}
	return s, true
}

type resolver struct {
	sections LocaleSections
}

func (r *resolver) resolve(v any, depth int) (string, bool) {
	if depth > MaxLocalisedDepth {
		return "", false
	}
	switch v := v.(type) {
	case string:
		return v, fits(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	case []any:
		return r.resolveList(v, depth)
	case map[string]any:
		// Lua writes an empty table as {}: an empty localised string.
		return "", len(v) == 0
	}
	return "", false
}

func (r *resolver) resolveList(list []any, depth int) (string, bool) {
	if len(list) == 0 {
		return "", true
	}
	key, ok := list[0].(string)
	if !ok {
		return "", false
	}
	params := list[1:]
	if key == "" {
		var b strings.Builder
		for _, p := range params {
			s, ok := r.resolve(p, depth+1)
			if !ok {
				return "", false
			}
			b.WriteString(s)
			if !fits(b.String()) {
				return "", false
			}
		}
		return b.String(), true
	}
	if key == "?" {
		// The game's fallback key: the first parameter that resolves.
		for _, p := range params {
			if s, ok := r.resolve(p, depth+1); ok {
				return s, true
			}
		}
		return "", false
	}
	section, name, ok := strings.Cut(key, ".")
	if !ok {
		return "", false
	}
	text, ok := r.sections[section][name]
	if !ok {
		return "", false
	}
	resolved := make([]string, len(params))
	for i, p := range params {
		if resolved[i], ok = r.resolve(p, depth+1); !ok {
			return "", false
		}
	}
	out := substitute(text, resolved)
	return out, fits(out)
}

// substitute puts params in place of __1__, __2__ and so on, stopping as soon
// as the text is too long. A number with no parameter stays as written.
func substitute(text string, params []string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, "__")
		if i < 0 {
			b.WriteString(text)
			break
		}
		j := i + 2
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		n, err := strconv.Atoi(text[i+2 : j])
		if j == i+2 || !strings.HasPrefix(text[j:], "__") || err != nil || n < 1 || n > len(params) {
			b.WriteString(text[:i+2])
			text = text[i+2:]
			continue
		}
		b.WriteString(text[:i])
		b.WriteString(params[n-1])
		text = text[j+2:]
		if b.Len() > 4*MaxLocalisedChars {
			break
		}
	}
	return b.String()
}

// fits reports whether s is within MaxLocalisedChars. A UTF-8 character is
// at most four bytes, so short strings are not counted.
func fits(s string) bool {
	return len(s) <= MaxLocalisedChars || utf8.RuneCountInString(s) <= MaxLocalisedChars
}
