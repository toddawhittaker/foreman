package game

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// AuditSettingsSave records a confirmed settings save. The entry names the
// file and never a value (SPEC 12.9, 14.8).
const AuditSettingsSave = "settings-save"

// Refusals of SaveSettings.
var (
	// ErrSettingsChanged means the file is not as it was when the page showed
	// the diff. Nothing was written. The page reads the file again and shows
	// the new diff (SPEC 11.3).
	ErrSettingsChanged = errors.New("the settings file changed after the preview, so nothing was saved. Check the new differences, then save again")
	// ErrSettingsInvalid means the new file does not pass the checks of
	// SPEC 11.2. Nothing was written.
	ErrSettingsInvalid = errors.New("the new settings are not valid, so nothing was saved")
)

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
}

// SaveSettings saves a settings change in this order: it takes the world's
// lock; saves a running game, as a snapshot does (SPEC 6.10); takes the
// automatic snapshot with reason world.ReasonSettings (SPEC 6.2, 11.3);
// checks that the file still has SeenHash; records the audit entry
// settings-save; then writes the file, atomically (SPEC 12.9). A running
// game's admin list is not written: the changed names are sent over RCON as
// /promote and /demote and the game writes the file itself (SPEC 11.1).
// The audit entry comes before the write, as for every action: when it cannot
// be recorded, nothing is changed.
func (c *Controller) SaveSettings(ctx context.Context, id string, by Actor, ch SettingsChange) (SettingsResult, error) {
	if ch.File != world.ServerSettingsFile && ch.File != world.ServerAdminListFile {
		return SettingsResult{}, world.ErrConfigFile
	}
	if err := c.checkSettings(id, ch); err != nil {
		return SettingsResult{}, err
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
	if _, err := c.worlds.AutoSnapshot(id, world.ReasonSettings, world.LabelBeforeSettings, by.Username); err != nil {
		return SettingsResult{}, err
	}
	current, err := c.worlds.ReadConfig(id, ch.File)
	if err != nil {
		return SettingsResult{}, err
	}
	if current.Hash != ch.SeenHash {
		return SettingsResult{}, ErrSettingsChanged
	}
	var commands []string
	if running && ch.File == world.ServerAdminListFile {
		if commands, err = adminCommands(current.Data, ch.Data); err != nil {
			return SettingsResult{}, err
		}
	}
	if err := c.audit(ctx, by, AuditSettingsSave, id, ch.File); err != nil {
		return SettingsResult{}, err
	}
	switch {
	case commands != nil:
		err = c.sendAdminCommands(ctx, id, commands)
	default:
		err = c.worlds.WriteConfigHeld(id, ch.File, ch.Data)
	}
	if err != nil {
		return SettingsResult{}, err
	}
	return SettingsResult{RestartNeeded: running && ch.File == world.ServerSettingsFile}, nil
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
// gamesettings.ValidateAdminName has refused every character that could end
// the command (SPEC 14.9).
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
	for _, n := range slices.Concat(promote, demote) {
		// The game's console reads the first word after the command as the
		// player's name and has no quoting, so "/promote bob smith" would
		// promote bob. A name it cannot read whole is never sent (SPEC 14.9).
		if strings.ContainsFunc(n, unicode.IsSpace) {
			return nil, fmt.Errorf("%w: an admin name with a space cannot be sent to a running game. Stop the game to change it", ErrSettingsInvalid)
		}
	}
	return commands, nil
}

// sendAdminCommands runs the commands over the running game's RCON.
func (c *Controller) sendAdminCommands(ctx context.Context, id string, commands []string) error {
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
