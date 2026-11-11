// Contract stub: task T3 of the mod settings epic fills these bodies.

package game

import (
	"context"
	"errors"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// Errors the mod settings page shows when the definitions run fails. Each
// leaves the world untouched; download and upload still work (SPEC 9.6).
var (
	// ErrModSettingsImageMissing means the world's game image is not
	// downloaded.
	ErrModSettingsImageMissing = errors.New("the game image for this Factorio version is not downloaded. Download the game image first")
	// ErrModSettingsOutOfMemory means the run hit its memory limit.
	ErrModSettingsOutOfMemory = errors.New("reading the mod settings ran out of memory")
	// ErrModSettingsTimeout means the run hit its time limit.
	ErrModSettingsTimeout = errors.New("reading the mod settings took too long and was stopped")
	// ErrModSettingsBadOutput means the run's files were missing or invalid.
	ErrModSettingsBadOutput = errors.New("the game did not report the mod settings in a form Foreman can read")
)

// ModSettingDefs is what one definitions run yields for a world: the
// setting definitions, the mod-settings.dat the game wrote with every
// default, and the settings' text in Foreman's eight languages.
type ModSettingDefs struct {
	Defs     modsettings.Definitions
	Defaults []byte
	Locale   modsettings.Locale
}

// ModSettingDefs returns the world's mod setting definitions, running the
// world's own image once and caching the result until the world's game
// version or mods change.
func (c *Controller) ModSettingDefs(ctx context.Context, id string) (*ModSettingDefs, error) {
	return nil, errors.New("game: mod setting definitions not built yet")
}
