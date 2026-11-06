package gamesettings

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Field is one top-level key and its new value, written as JSON.
type Field struct {
	Key  string
	JSON []byte
}

// SetFields returns b with each top-level key set to its value, in order. A
// key that is already there keeps its place, and a new one goes at the end;
// the rest of the file keeps its text, order and layout (SPEC 11.3). It
// refuses input that is not one JSON object and a value that is not JSON.
func SetFields(b []byte, fields []Field) ([]byte, error) {
	if len(b) > MaxBytes {
		return nil, errors.New("settings file too large")
	}
	for _, f := range fields {
		if !json.Valid(f.JSON) {
			return nil, fmt.Errorf("the value for %q is not JSON", f.Key)
		}
	}
	for _, f := range fields {
		// Each change moves the offsets after it, so the text is read again.
		root, fe := parse(b)
		if fe != nil {
			return nil, errors.New("settings file is not one JSON object")
		}
		b = setMember(b, root, f.Key, compact(f.JSON))
	}
	return b, nil
}

// UnknownKey reports whether key, a dotted path as Diff returns it, is one
// Validate has no table entry for, in file on a game line. It is false when
// there is no table for the file or line, because then nothing is known.
func UnknownKey(file, line, key string) bool {
	sp, ok := schemaFor(file, line)
	if !ok {
		return false
	}
	return unknownIn(sp, key)
}

func unknownIn(sp *spec, key string) bool {
	if sp.fields == nil {
		return false
	}
	// A key name may itself hold a dot, so every split point is tried.
	for i := 0; i <= len(key); i++ {
		if i < len(key) && key[i] != '.' {
			continue
		}
		sub, ok := sp.fields[key[:i]]
		if !ok {
			continue
		}
		if i == len(key) || !unknownIn(sub, key[i+1:]) {
			return false
		}
	}
	return true
}
