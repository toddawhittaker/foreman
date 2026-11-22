package web

import (
	"net/url"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The form builders with a line's built-in catalogue, as the tests use them.

func buildMapGenForm(line string, spaceAge bool, posted func(name string) (string, bool)) MapGenForm {
	return buildMapGenFormIn(line, mapgen.Builtin(line, spaceAge), posted)
}

func NewMapGenFormForPreset(line string, spaceAge bool, preset string) MapGenForm {
	return mapGenFormForPreset(line, mapgen.Builtin(line, spaceAge), preset)
}

func NewMapGenFormFromFile(line string, spaceAge bool, file []byte) MapGenForm {
	return mapGenFormFromFile(line, mapgen.Builtin(line, spaceAge), file)
}

func presetValues(line string, spaceAge bool, preset, seed string) url.Values {
	return presetValuesIn(line, mapgen.Builtin(line, spaceAge), preset, seed)
}

func fileValues(line string, spaceAge bool, file []byte, seed, raw string, wholeFile bool) url.Values {
	return fileValuesIn(line, mapgen.Builtin(line, spaceAge), file, seed, raw, wholeFile)
}
