package gameenv

// Names a save read shares with the fake game. A save read is a preview
// container with PreviewKind set to one of these kinds: it runs a copy of the
// world's save in benchmark mode with a control-only mod and leaves the mod's
// output in the work folder (SPEC 8.16, 5.16, 14.3).
const (
	// KindSavePlayers is PreviewKind's value for a run that reads the players
	// of a save (SPEC 8.14).
	KindSavePlayers = "saveplayers"
	// KindFactoryMap is PreviewKind's value for a run that reads a save's
	// charted chunks and buildings (SPEC 5.16).
	KindFactoryMap = "factorymap"
	// SavePlayersFile is the JSON the players run leaves in the work folder.
	SavePlayersFile = "saveplayers.json"
	// FactoryMapDir is the folder the factory map run leaves in the work
	// folder: manifest.json and one file per surface.
	FactoryMapDir = "factorymap"
	// SaveCopyMount is where the run sees the save, read-only.
	SaveCopyMount = PreviewMount + "/save.zip"
)
