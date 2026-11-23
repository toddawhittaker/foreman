package world

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"path"
)

// ModPlanetsFile is what a planets run needs beyond the definitions run: the
// world's mod-settings.dat and the cache key of a run that reads it, the
// definitions key plus the file's hash. Startup setting values can add or
// remove planets, so the planets follow the world's file while the
// definitions keep the defaults (SPEC 5.6, 9.6). A world with no file
// returns an empty key and nil data: its planets are the definitions run's.
func (m *Manager) ModPlanetsFile(id string) (key string, data []byte, err error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return "", nil, err
	}
	data, err = ReadSmallFile(root, path.Join(ModsDir, ModSettingsFile), MaxModSettings)
	_ = root.Close()
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	defs, err := m.ModSettingsKey(id)
	if err != nil {
		return "", nil, err
	}
	h := sha256.Sum256(append([]byte(defs+"\n"), data...))
	return hex.EncodeToString(h[:]), data, nil
}
