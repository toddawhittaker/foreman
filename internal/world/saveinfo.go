package world

import (
	"os"
	"slices"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

// spaceAgeMod is the mod that makes a save a Space Age save.
const spaceAgeMod = "space-age"

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
	info.SpaceAge = usesSpaceAge(save.Mods)
	return info, true
}

// usesSpaceAge reports whether the save holds space-age itself. A save with
// just quality or elevated-rails gets the flag off, and the pre-start check
// then reports the mismatch, since the flag is all or nothing (SPEC 9.7).
func usesSpaceAge(mods []savefile.Mod) bool {
	return slices.ContainsFunc(mods, func(m savefile.Mod) bool { return m.Name == spaceAgeMod })
}
