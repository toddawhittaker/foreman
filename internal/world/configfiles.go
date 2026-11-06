package world

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
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

// maxConfigFile is the largest config file Foreman reads or writes (SPEC 14.6).
const maxConfigFile = 1 << 20

// Refusals of the config file helpers.
var (
	ErrConfigFile   = errors.New("this is not one of the world's settings files")
	ErrConfigTooBig = errors.New("the settings file is too big (over 1 MB)")
)

// ConfigRead is one config file as it is on disk. Found is false when the
// file does not exist; Hash is then empty, so a missing file differs from an
// empty one.
type ConfigRead struct {
	Data  []byte
	Hash  string
	Found bool
}

// ReadConfig reads one of the ConfigFiles. Callers that must be sure the
// file is unchanged since an earlier read compare Hash (SPEC 11.3). It
// takes no lock.
func (m *Manager) ReadConfig(id, file string) (ConfigRead, error) {
	if !slices.Contains(ConfigFiles, file) {
		return ConfigRead{}, ErrConfigFile
	}
	root, err := m.OpenWorld(id)
	if err != nil {
		return ConfigRead{}, err
	}
	defer func() { _ = root.Close() }()
	b, err := ReadSmallFile(root, path.Join(ConfigDir, file), maxConfigFile)
	if errors.Is(err, fs.ErrNotExist) {
		return ConfigRead{}, nil
	}
	if err != nil {
		return ConfigRead{}, err
	}
	sum := sha256.Sum256(b)
	return ConfigRead{Data: b, Hash: hex.EncodeToString(sum[:]), Found: true}, nil
}

// WriteConfigHeld replaces one of the ConfigFiles atomically, at the mode
// the game's group reads (SPEC 14.8). The caller holds Lock(id) and has taken
// the snapshot the change needs (SPEC 11.3). It refuses a world a newer
// Foreman wrote.
func (m *Manager) WriteConfigHeld(id, file string, data []byte) error {
	if !slices.Contains(ConfigFiles, file) {
		return ErrConfigFile
	}
	if len(data) > maxConfigFile {
		return ErrConfigTooBig
	}
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if _, err := readChangeable(root); err != nil {
		return err
	}
	if err := Mkdir(root, ConfigDir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return writeFileAtomicMode(root, path.Join(ConfigDir, file), data)
}

// ErrSettingsChanged means a file is not as it was when the page showed the
// diff. Nothing was written. The page reads the file again and shows the new
// diff (SPEC 11.3).
var ErrSettingsChanged = errors.New("the settings file changed after the preview, so nothing was saved. Check the new differences, then save again")

// ConfigChange is a confirmed change to one config file (SPEC 11.3).
type ConfigChange struct {
	File string
	// Data is the whole new file.
	Data []byte
	// SeenHash is the Hash of ReadConfig when the page showed the diff, empty
	// for a file that did not exist then.
	SeenHash string
	// NoWrite checks the file's hash but leaves writing to the game, as for
	// the admin list of a running game (SPEC 11.1).
	NoWrite bool
}

// SaveSettingsHeld applies a confirmed settings change in one order: check
// every file against its SeenHash, take the automatic snapshot with reason
// ReasonSettings (SPEC 6.2), call audit, then write each file that is not
// NoWrite. A failure at any step stops the later ones, and a hash mismatch
// or a bad file writes nothing. audit must record the change and return an
// error when it cannot, so nothing is changed without a record (SPEC 12.9).
// The caller holds Lock(id) and has saved a running game (SPEC 6.10).
func (m *Manager) SaveSettingsHeld(id string, changes []ConfigChange, by string, audit func() error) (Snapshot, error) {
	for _, ch := range changes {
		if !slices.Contains(ConfigFiles, ch.File) {
			return Snapshot{}, ErrConfigFile
		}
		if len(ch.Data) > maxConfigFile {
			return Snapshot{}, ErrConfigTooBig
		}
	}
	for _, ch := range changes {
		cur, err := m.ReadConfig(id, ch.File)
		if err != nil {
			return Snapshot{}, err
		}
		if cur.Hash != ch.SeenHash {
			return Snapshot{}, ErrSettingsChanged
		}
	}
	snap, err := m.AutoSnapshot(id, ReasonSettings, LabelBeforeSettings, by)
	if err != nil {
		return Snapshot{}, err
	}
	if err := audit(); err != nil {
		return Snapshot{}, err
	}
	for _, ch := range changes {
		if ch.NoWrite {
			continue
		}
		if err := m.WriteConfigHeld(id, ch.File, ch.Data); err != nil {
			return Snapshot{}, err
		}
	}
	return snap, nil
}

// writeFileAtomicMode is WriteFileAtomic with the mode copyMode gives name.
func writeFileAtomicMode(root *os.Root, name string, data []byte) error {
	f, temp, err := createTempMode(root, path.Dir(name), copyMode(name))
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = Commit(root, temp, name)
	}
	if err != nil {
		_ = root.Remove(temp)
	}
	return err
}
