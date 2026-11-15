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
	// maxParamBytes caps the resolved parameters of one key together.
	maxParamBytes = 4 * MaxLocalisedChars
	// maxWork caps the parameters looked at in one resolution, so a huge
	// list costs little even when most of it is never used.
	maxWork = 1 << 14
)

// LocaleSections is one language's locale text: section, then key, then the
// text as written in the .cfg files.
type LocaleSections map[string]map[string]string

// ResolveLocalised resolves a localised string as the game reports it in
// JSON, the way the game does: a plain string is itself; an array is a key
// ("section.name") and parameters, each a localised string, put in place of
// __1__, __2__ and so on in the key's text; the key "" joins its parameters.
// It returns false when the string is absent, a key is missing, or a bound is
// passed. Each key is looked up in sections in order, the first that holds
// it winning, as the game falls back to English key by key. The result is
// untrusted text, to be escaped when shown.
func ResolveLocalised(raw json.RawMessage, sections ...LocaleSections) (string, bool) {
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
	sections []LocaleSections
	work     int
}

func (r *resolver) lookup(section, name string) (string, bool) {
	for _, s := range r.sections {
		if t, ok := s[section][name]; ok {
			return t, true
		}
	}
	return "", false
}

func (r *resolver) resolve(v any, depth int) (string, bool) {
	r.work++
	if depth > MaxLocalisedDepth || r.work > maxWork {
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
			if r.work > maxWork {
				return "", false
			}
		}
		return "", false
	}
	section, name, ok := strings.Cut(key, ".")
	if !ok {
		return "", false
	}
	text, ok := r.lookup(section, name)
	if !ok {
		return "", false
	}
	// Only the parameters the text uses are resolved, each once.
	resolved := map[int]string{}
	total := 0
	for _, n := range paramRefs(text, len(params)) {
		s, ok := r.resolve(params[n-1], depth+1)
		if !ok {
			return "", false
		}
		if total += len(s); total > maxParamBytes {
			return "", false
		}
		resolved[n] = s
	}
	out := substitute(text, resolved)
	return out, fits(out)
}

// paramRefs lists, once each and in order of first use, the numbers n of
// the __n__ in text with 1 <= n <= count.
func paramRefs(text string, count int) []int {
	var out []int
	seen := map[int]bool{}
	for {
		n, _, rest, ok := nextRef(text, count)
		if !ok {
			return out
		}
		if n > 0 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
		text = rest
	}
}

// nextRef finds the next "__" in text. It returns the parameter number (0
// when the "__" does not start a usable __n__), the text before it, and the
// text after it.
func nextRef(text string, count int) (n int, before, rest string, ok bool) {
	i := strings.Index(text, "__")
	if i < 0 {
		return 0, text, "", false
	}
	j := i + 2
	for j < len(text) && j-i < 12 && text[j] >= '0' && text[j] <= '9' {
		j++
	}
	num, err := strconv.Atoi(text[i+2 : j])
	if j == i+2 || !strings.HasPrefix(text[j:], "__") || err != nil || num < 1 || num > count {
		return 0, text[:i+2], text[i+2:], true
	}
	return num, text[:i], text[j+2:], true
}

// substitute puts params in place of __1__, __2__ and so on, stopping as soon
// as the text is too long. A number with no parameter stays as written.
func substitute(text string, params map[int]string) string {
	var b strings.Builder
	for b.Len() <= 4*MaxLocalisedChars {
		n, before, rest, ok := nextRef(text, len(params)+1<<30)
		if !ok {
			b.WriteString(before)
			break
		}
		b.WriteString(before)
		if p, has := params[n]; has {
			b.WriteString(p)
		} else if n > 0 {
			b.WriteString(text[len(before) : len(text)-len(rest)])
		}
		text = rest
	}
	return b.String()
}

// fits reports whether s is within MaxLocalisedChars. A UTF-8 character is
// at most four bytes, so short strings are not counted.
func fits(s string) bool {
	return len(s) <= MaxLocalisedChars || utf8.RuneCountInString(s) <= MaxLocalisedChars
}
