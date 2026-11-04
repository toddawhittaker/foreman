package world

import (
	"os"
	"slices"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

// SaveInfo is what a save's header says about the game it was made with.
type SaveInfo struct {
	GameVersion string
	// SpaceAge is true when the save was played with the Space Age mods.
	SpaceAge bool
}

// ReadSaveInfo reads the save named name inside root. ok is false when the
// save cannot be read for any reason; callers then ask the admin instead.
func ReadSaveInfo(root *os.Root, name string) (info SaveInfo, ok bool) {
	f, st, err := openRegular(root, name)
	if err != nil {
		return SaveInfo{}, false
	}
	defer func() { _ = f.Close() }()
	save, err := savefile.Read(f, st.Size())
	if err != nil {
		return SaveInfo{}, false
	}
	info.GameVersion = save.Version.String()
	dlc := spaceAgeMods[gameMinor(info.GameVersion)]
	for _, m := range save.Mods {
		if slices.Contains(dlc, m.Name) {
			info.SpaceAge = true
			break
		}
	}
	return info, true
}
