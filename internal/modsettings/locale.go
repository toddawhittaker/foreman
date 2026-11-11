package modsettings

import (
	"bytes"
	"errors"
	"strings"
)

var errCfgSize = errors.New("modsettings: a locale file is too large")

// ParseLocaleCfg reads a .cfg file's [mod-setting-name] and
// [mod-setting-description] sections, keyed by setting name. Values are kept
// as written, minus the line ending and any invalid UTF-8.
func ParseLocaleCfg(data []byte) (names, descriptions map[string]string, err error) {
	if len(data) > MaxCfgFileBytes {
		return nil, nil, errCfgSize
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	names = map[string]string{}
	descriptions = map[string]string{}
	var into map[string]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';':
			continue
		case trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']':
			switch strings.TrimSpace(trimmed[1 : len(trimmed)-1]) {
			case "mod-setting-name":
				into = names
			case "mod-setting-description":
				into = descriptions
			default:
				into = nil
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
	return names, descriptions, nil
}
