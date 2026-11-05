package world

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

// configFileMode keeps an imported config file from other users: the
// game password sits in server-settings.json (SPEC 14.8).
const configFileMode = rconPasswordMode
