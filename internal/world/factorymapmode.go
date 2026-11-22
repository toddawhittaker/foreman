package world

import "errors"

// The factory map modes (SPEC 5.16).
const (
	FactoryMapManual    = "manual"
	FactoryMapAutomatic = "automatic"
	FactoryMapOff       = "off"
)

// ErrFactoryMapMode is returned for a mode other than automatic, manual or
// off. It names no file.
var ErrFactoryMapMode = errors.New("the factory map mode must be automatic, manual or off")

// FactoryMapMode is a world's factory map setting, world.json's
// "factory_map": when the picture is drawn, and whether a click to draw a
// running world's newest save is refused while players are online (SPEC 5.16).
type FactoryMapMode struct {
	Mode       string `json:"mode"`
	SkipOnline bool   `json:"skip_online"`
}

// DefaultFactoryMapMode is the setting of a world that has none: manual,
// with skip-online on (SPEC 5.16).
func DefaultFactoryMapMode() FactoryMapMode {
	return FactoryMapMode{Mode: FactoryMapManual, SkipOnline: true}
}

// FactoryMapSetting is the world's factory map setting, the default when
// world.json has none.
func (m Meta) FactoryMapSetting() FactoryMapMode {
	if m.FactoryMap == nil {
		return DefaultFactoryMapMode()
	}
	return *m.FactoryMap
}

// validFactoryMapMode reports whether mode is one of the three modes.
func validFactoryMapMode(mode string) bool {
	switch mode {
	case FactoryMapManual, FactoryMapAutomatic, FactoryMapOff:
		return true
	}
	return false
}

// SetFactoryMapMode stores the world's factory map setting. It is Foreman's
// own setting: it takes no snapshot and is not audited, and only world.json
// changes (SPEC 5.16). A mode other than the three is refused, as is a world
// a newer Foreman wrote.
func (m *Manager) SetFactoryMapMode(id string, mode FactoryMapMode) error {
	if !validFactoryMapMode(mode.Mode) {
		return ErrFactoryMapMode
	}
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	w, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	meta, err := readChangeable(w)
	if err != nil {
		return err
	}
	meta.FactoryMap = &FactoryMapMode{Mode: mode.Mode, SkipOnline: mode.SkipOnline}
	return WriteMeta(w, meta)
}
