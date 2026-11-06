package game

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// AuditSettingsSave records a confirmed settings save. The entry names the
// file and never a value (SPEC 12.9, 14.8).
const AuditSettingsSave = "settings-save"

// ErrSettingsInvalid means the new file does not pass the checks of
// SPEC 11.2. Nothing was written.
var ErrSettingsInvalid = errors.New("the new settings are not valid, so nothing was saved")

// SettingsChange is a confirmed change to one of the world's settings files
// (SPEC 11.3).
type SettingsChange struct {
	// File is world.ServerSettingsFile or world.ServerAdminListFile.
	File string
	// Data is the whole new file.
	Data []byte
	// SeenHash is the Hash of world.ConfigRead when the page showed the diff,
	// empty for a file that did not exist then.
	SeenHash string
}

// SettingsResult says what a saved change needs next.
type SettingsResult struct {
	// RestartNeeded is true when the game runs and reads the file only at
	// start, so the change takes effect after a restart (SPEC 11.3).
	RestartNeeded bool
	// Live is true when the game was running at the save and the admin list
	// changed, so the change reached the game through its console.
	Live bool
	// Snapshot is the automatic snapshot this save took.
	Snapshot world.Snapshot
}

// PartialSaveError means the files in Saved are written but the admin
// changes could not be sent to the running game. Err is why.
type PartialSaveError struct {
	Saved []string
	Err   error
}

func (e *PartialSaveError) Error() string { return e.Err.Error() }
func (e *PartialSaveError) Unwrap() error { return e.Err }

// SaveSettings saves one confirmed change, which may name several files, in
// this order: it takes the world's lock once; saves a running game, as a
// snapshot does (SPEC 6.10); then world.SaveSettingsHeld checks every file's
// hash, takes one automatic snapshot, records one audit entry naming the
// files and writes them (SPEC 6.2, 11.3, 12.9). A running game's admin list
// is not written: the changed names are then sent over RCON as /promote and
// /demote and the game writes the file itself (SPEC 11.1). If that fails
// after other files are written, the error is a *PartialSaveError.
func (c *Controller) SaveSettings(ctx context.Context, id string, by Actor, changes []SettingsChange) (SettingsResult, error) {
	for _, ch := range changes {
		if ch.File != world.ServerSettingsFile && ch.File != world.ServerAdminListFile {
			return SettingsResult{}, world.ErrConfigFile
		}
		if err := c.checkSettings(id, ch); err != nil {
			return SettingsResult{}, err
		}
	}
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return SettingsResult{}, err
	}
	defer unlock()
	running, err := c.saveRunning(ctx, id)
	if err != nil {
		return SettingsResult{}, err
	}
	var files, saved, commands []string
	adminChanged := false
	var cfg []world.ConfigChange
	for _, ch := range changes {
		wc := world.ConfigChange{File: ch.File, Data: ch.Data, SeenHash: ch.SeenHash}
		files = append(files, ch.File)
		adminChanged = adminChanged || ch.File == world.ServerAdminListFile
		if running && ch.File == world.ServerAdminListFile {
			current, err := c.worlds.ReadConfig(id, ch.File)
			if err != nil {
				return SettingsResult{}, err
			}
			if commands, err = adminCommands(current.Data, ch.Data); err != nil {
				return SettingsResult{}, err
			}
			wc.NoWrite = true
		} else {
			saved = append(saved, ch.File)
		}
		cfg = append(cfg, wc)
	}
	snap, err := c.worlds.SaveSettingsHeld(id, cfg, by.Username, func() error {
		return c.audit(ctx, by, AuditSettingsSave, id, strings.Join(files, ", "))
	})
	if err != nil {
		return SettingsResult{}, err
	}
	if err := c.sendAdminCommands(ctx, id, commands); err != nil {
		if len(saved) > 0 {
			return SettingsResult{}, &PartialSaveError{Saved: saved, Err: err}
		}
		return SettingsResult{}, err
	}
	return SettingsResult{
		RestartNeeded: running && slices.Contains(saved, world.ServerSettingsFile),
		Live:          running && adminChanged,
		Snapshot:      snap,
	}, nil
}

// checkSettings refuses a new file that fails the checks of SPEC 11.2, before
// anything is done. The message names a line and a key, never a value.
func (c *Controller) checkSettings(id string, ch SettingsChange) error {
	if ch.File == world.ServerAdminListFile {
		if _, err := gamesettings.ParseAdminList(ch.Data); err != nil {
			return fmt.Errorf("%w: %w", ErrSettingsInvalid, err)
		}
		return nil
	}
	meta, err := c.readMeta(id)
	if err != nil {
		return err
	}
	line, ok := mapgen.Line(meta.GameVersion)
	if !ok {
		return fmt.Errorf("there is no settings rule set for Factorio %s", meta.GameVersion)
	}
	if errs := gamesettings.Validate(ch.File, line, ch.Data); len(errs) > 0 {
		return fmt.Errorf("%w (line %d, %s: %s)", ErrSettingsInvalid, errs[0].Line, errs[0].Key, errs[0].Code)
	}
	return nil
}

// adminCommands returns the console commands that turn the admin list in
// current into the one in next, or an empty non-nil list when they hold the
// same names. Names go in as they are: the console has no quoting, and
// gamesettings.ValidateAdminName has refused every space and control
// character that could end the command (SPEC 14.9).
func adminCommands(current, next []byte) ([]string, error) {
	var have []string
	if len(current) > 0 {
		var err error
		if have, err = gamesettings.ParseAdminList(current); err != nil {
			// A file Foreman cannot read is not diffed: fail safe.
			return nil, fmt.Errorf("the current admin list cannot be read, so it cannot be changed while the game runs: %w", err)
		}
	}
	want, err := gamesettings.ParseAdminList(next)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSettingsInvalid, err)
	}
	promote, demote := gamesettings.AdminDiff(have, want)
	commands := []string{}
	for _, n := range demote {
		commands = append(commands, "/demote "+n)
	}
	for _, n := range promote {
		commands = append(commands, "/promote "+n)
	}
	return commands, nil
}

// sendAdminCommands runs the commands over the running game's RCON. With none
// to send it does nothing.
func (c *Controller) sendAdminCommands(ctx context.Context, id string, commands []string) error {
	if len(commands) == 0 {
		return nil
	}
	rc, _, err := c.liveRCON(c.world(id))
	if err != nil {
		return err
	}
	for _, cmd := range commands {
		if err := c.exec(ctx, rc, cmd); err != nil {
			return err
		}
	}
	return nil
}
