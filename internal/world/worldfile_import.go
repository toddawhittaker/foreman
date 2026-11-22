package world

import (
	"context"
	"errors"
	"os"
)

// CreateWorldFileTemp opens a staged file for an uploaded world file, in the
// worlds folder with the PrefixWorldFile prefix, and returns it with its name
// (SPEC 5.14). It removes staged files older than an hour and keeps at most
// two.
func (m *Manager) CreateWorldFileTemp() (*os.File, string, error) {
	return nil, "", errors.New("not built")
}

// ReadWorldFile reads a staged world file's directory and small entries,
// under the limits of 14.5 and 14.6, for the preview. It changes nothing.
func (m *Manager) ReadWorldFile(temp string, lim WorldFileLimits) (WorldFileInfo, error) {
	return WorldFileInfo{}, errors.New("not built")
}

// ImportWorldFile makes a new world from a staged world file under the name
// given, and returns its id. It removes temp on success and on a bad file,
// and keeps it on a refusal the form can fix: a name in use, or too little
// disk space (SPEC 5.14).
func (m *Manager) ImportWorldFile(ctx context.Context, temp, name string, lim WorldFileLimits) (string, error) {
	return "", errors.New("not built")
}
