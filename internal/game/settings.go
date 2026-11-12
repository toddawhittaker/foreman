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
	// File is world.ServerSettingsFile, or the admin list, ban list or
	// whitelist.
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
	// Live is true when the game was running at the save and a player list
	// changed, so the change reached the game through its console.
	Live bool
	// Snapshot is the automatic snapshot this save took.
	Snapshot world.Snapshot
}

// PartialSaveError means the files in Saved are written but the changes to
// the lists in Failed could not be sent to the running game. Err is why.
type PartialSaveError struct {
	Saved, Failed []string
	Err           error
}

func (e *PartialSaveError) Error() string { return e.Err.Error() }
func (e *PartialSaveError) Unwrap() error { return e.Err }

// listFiles are the player lists a settings change may name besides
// server-settings.json (SPEC 8.3).
var listFiles = []string{world.ServerAdminListFile, world.ServerBanListFile, world.ServerWhiteListFile}

// SaveSettings saves one confirmed change, which may name several files, in
// this order: it takes the world's lock once; saves a running game, as a
// snapshot does (SPEC 6.10); then world.SaveSettingsHeld checks every file's
// hash, takes one automatic snapshot, records one audit entry naming the
// files and writes them (SPEC 6.2, 11.3, 12.9).
//
// A list the running game keeps is not written: the changed names are sent
// over RCON and the game writes the file itself (SPEC 8.3, 11.1). The game
// keeps the admin and ban lists, and the whitelist only when it was started
// with the world's whitelist switch on. If sending fails after other files
// are written, the error is a *PartialSaveError.
func (c *Controller) SaveSettings(ctx context.Context, id string, by Actor, changes []SettingsChange) (SettingsResult, error) {
	for _, ch := range changes {
		if ch.File != world.ServerSettingsFile && !slices.Contains(listFiles, ch.File) {
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
	keptWhitelist := false
	if running {
		if keptWhitelist, err = c.WhitelistKept(ctx, id); err != nil {
			return SettingsResult{}, err
		}
	}
	var files, saved, live, commands []string
	var cfg []world.ConfigChange
	for _, ch := range changes {
		wc := world.ConfigChange{File: ch.File, Data: ch.Data, SeenHash: ch.SeenHash}
		files = append(files, ch.File)
		sendLive := running && slices.Contains(listFiles, ch.File) &&
			(ch.File != world.ServerWhiteListFile || keptWhitelist)
		if sendLive {
			current, err := c.worlds.ReadConfig(id, ch.File)
			if err != nil {
				return SettingsResult{}, err
			}
			more, err := listCommands(ch.File, current.Data, ch.Data)
			if err != nil {
				return SettingsResult{}, err
			}
			commands = append(commands, more...)
			live = append(live, ch.File)
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
	if err := c.sendListCommands(ctx, id, commands); err != nil {
		if len(saved) > 0 {
			return SettingsResult{}, &PartialSaveError{Saved: saved, Failed: live, Err: err}
		}
		return SettingsResult{}, err
	}
	return SettingsResult{
		RestartNeeded: running && slices.Contains(saved, world.ServerSettingsFile),
		Live:          len(live) > 0,
		Snapshot:      snap,
	}, nil
}

// checkSettings refuses a new file that fails the checks of SPEC 11.2, before
// anything is done. The message names a line and a key, never a value.
func (c *Controller) checkSettings(id string, ch SettingsChange) error {
	if slices.Contains(listFiles, ch.File) {
		if _, _, err := parseList(ch.File, ch.Data); err != nil {
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

// listCommands returns the console commands that turn the player list file
// in current into the one in next, or an empty non-nil list when they hold
// the same names. Names go in as they are: the console has no quoting, and
// gamesettings.ValidateAdminName has refused every space and control
// character that could end the command (SPEC 14.9). A new ban takes the
// reason the file gives it, which must pass gamesettings.ValidateReason.
func listCommands(file string, current, next []byte) ([]string, error) {
	parse := func(b []byte) ([]string, []gamesettings.Ban, error) { return parseList(file, b) }
	var have []string
	if len(current) > 0 {
		var err error
		if have, _, err = parse(current); err != nil {
			// A file Foreman cannot read is not diffed: fail safe.
			return nil, fmt.Errorf("the current %s cannot be read, so it cannot be changed while the game runs: %w", file, err)
		}
	}
	want, bans, err := parse(next)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSettingsInvalid, err)
	}
	add, remove := gamesettings.AdminDiff(have, want)
	commands := []string{}
	for _, n := range remove {
		switch file {
		case world.ServerBanListFile:
			commands = append(commands, playerCommand(PlayerUnban, n, ""))
		case world.ServerWhiteListFile:
			commands = append(commands, playerCommand(PlayerWhitelistRemove, n, ""))
		default:
			commands = append(commands, playerCommand(PlayerDemote, n, ""))
		}
	}
	for _, n := range add {
		switch file {
		case world.ServerBanListFile:
			reason := bans[slices.IndexFunc(bans, func(b gamesettings.Ban) bool { return b.Name == n })].Reason
			if err := gamesettings.ValidateReason(reason); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrSettingsInvalid, err)
			}
			commands = append(commands, playerCommand(PlayerBan, n, reason))
		case world.ServerWhiteListFile:
			commands = append(commands, playerCommand(PlayerWhitelistAdd, n, ""))
		default:
			commands = append(commands, playerCommand(PlayerPromote, n, ""))
		}
	}
	return commands, nil
}

// parseList reads a player list file: its names, and for the ban list its
// bans.
func parseList(file string, b []byte) ([]string, []gamesettings.Ban, error) {
	switch file {
	case world.ServerBanListFile:
		bans, err := gamesettings.ParseBanList(b)
		return gamesettings.BanNames(bans), bans, err
	case world.ServerWhiteListFile:
		names, err := gamesettings.ParseWhitelist(b)
		return names, nil, err
	}
	names, err := gamesettings.ParseAdminList(b)
	return names, nil, err
}

// sendListCommands runs the commands over the running game's RCON. With none
// to send it does nothing.
func (c *Controller) sendListCommands(ctx context.Context, id string, commands []string) error {
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
