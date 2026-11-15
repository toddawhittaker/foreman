package modsettings

import (
	"bytes"
	"errors"
	"strings"
)

var errCfgSize = errors.New("modsettings: a locale file is too large")

// ParseLocaleCfg reads every section of a .cfg file, keyed by section, then
// by key. Lines before the first section are skipped. Values are kept as
// written, minus the line ending and any invalid UTF-8; a later line with
// the same key wins.
func ParseLocaleCfg(data []byte) (LocaleSections, error) {
	if len(data) > MaxCfgFileBytes {
		return nil, errCfgSize
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	sections := LocaleSections{}
	var into map[string]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';':
			continue
		case trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']':
			name := strings.ToValidUTF8(strings.TrimSpace(trimmed[1:len(trimmed)-1]), "�")
			if into = sections[name]; into == nil {
				into = map[string]string{}
				sections[name] = into
			}
			continue
		}
		if into == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		into[strings.ToValidUTF8(key, "�")] = strings.ToValidUTF8(value, "�")
	}
	return sections, nil
}

// Merge copies o's text into s, o winning on a key both hold.
func (s LocaleSections) Merge(o LocaleSections) {
	for section, keys := range o {
		into := s[section]
		if into == nil {
			into = make(map[string]string, len(keys))
			s[section] = into
		}
		for k, v := range keys {
			into[k] = v
		}
	}
}
