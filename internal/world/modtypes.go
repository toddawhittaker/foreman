package world

// InstalledMod is one mod in a world's mods folder or mod list (SPEC 9).
type InstalledMod struct {
	Name    string
	Version string
	File    string // zip file name in mods/; empty for a folder mod
	Enabled bool
	Local   bool // installed as a folder; never replaced
	BuiltIn bool // base or a DLC mod (SPEC 9.7)
	// Dependencies are the raw strings from the mod's own info.json, so a
	// plan can read them without the portal; empty when it has none, nil
	// when info.json cannot be read.
	Dependencies []string
}

// ModChange is one mod change, applied to a world under its lock after a
// snapshot (SPEC 9.2 to 9.6).
type ModChange struct {
	Add      []string        // staged temp file names to place in mods/
	Remove   []string        // zip file names to delete from mods/
	Enabled  map[string]bool // new enabled state, by mod name
	Settings []byte          // new mod-settings.dat, or nil to leave it
}
