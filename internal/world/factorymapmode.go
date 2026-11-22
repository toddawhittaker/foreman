package world

import "errors"

// The factory map modes (SPEC 5.16).
const (
	FactoryMapManual    = "manual"
	FactoryMapAutomatic = "automatic"
	FactoryMapOff       = "off"
)

// FactoryMapMode is a world's factory map setting, world.json's
// "factory_map": when the picture is drawn, and whether a click to draw a
// running world's newest save is refused while players are online (SPEC 5.16).
type FactoryMapMode struct {
	Mode       string `json:"mode"`
	SkipOnline bool   `json:"skip_online"`
}

// SetFactoryMapMode stores the world's factory map setting. It is Foreman's
// own setting: it takes no snapshot and is not audited (SPEC 5.16).
func (m *Manager) SetFactoryMapMode(id string, mode FactoryMapMode) error {
	return errors.New("not built")
}
