package world

import (
	"io/fs"
	"strings"
)

// Names of the files in a world's config/ folder that Foreman knows about
// (SPEC 5.1). The game reads each through a flag of the start script
// (SPEC 4.5); an import copies all of ConfigFiles (SPEC 5.5).
const (
	ServerSettingsFile  = "server-settings.json"
	MapGenSettingsFile  = "map-gen-settings.json"
	MapSettingsFile     = "map-settings.json"
	ServerAdminListFile = "server-adminlist.json"
	ServerBanListFile   = "server-banlist.json"
	ServerWhiteListFile = "server-whitelist.json"
)

// ConfigFiles are the files of a world's config/ folder that an import
// copies. rconpw is not one: Foreman makes its own (SPEC 4.5).
var ConfigFiles = []string{
	ServerSettingsFile, MapGenSettingsFile, MapSettingsFile,
	ServerAdminListFile, ServerBanListFile, ServerWhiteListFile,
}

// copyMode is the mode for a file copied to name inside a world: files
// under config/ are readable by the game's group only, since the game
// password sits in server-settings.json (SPEC 14.8). Everything else is
// FileMode.
func copyMode(name string) fs.FileMode {
	if strings.HasPrefix(name, ConfigDir+"/") {
		return rconPasswordMode
	}
	return FileMode
}
