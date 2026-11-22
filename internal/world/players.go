package world

import (
	"errors"
	"io/fs"
)

// PlayersFile is the name of Foreman's record of the players of the save the
// world loads, in the world folder (SPEC 5.1, 8.14). Snapshots, restores and
// clones carry it with the rest of the folder.
const PlayersFile = "players.json"

// maxPlayersRecord is the most bytes of players.json that are read. The game
// container can write the world folder, so the file is untrusted (SPEC 14.6).
const maxPlayersRecord = 4 << 20

// ReadPlayersRecord returns the bytes of the world's players.json, under a
// size limit, refusing a link or anything but a regular file. It returns
// fs.ErrNotExist when there is none. Parsing is the saveplayers package's.
func (m *Manager) ReadPlayersRecord(id string) ([]byte, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return ReadSmallFile(root, PlayersFile, maxPlayersRecord)
}

// WritePlayersRecord replaces the world's players.json atomically. It takes
// no snapshot (SPEC 5.1).
func (m *Manager) WritePlayersRecord(id string, b []byte) error {
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return WriteFileAtomic(root, PlayersFile, b)
}

// RemovePlayersRecord removes the world's players.json. A world without one
// is not an error.
func (m *Manager) RemovePlayersRecord(id string) error {
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(PlayersFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
