package game

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
)

// AuditPlayer records a player action sent to a running game (SPEC 8.2,
// 12.9). The entry names the action and the player, and the reason when
// there is one.
const AuditPlayer = "player"

// PlayerAction is one change to a player sent over RCON (SPEC 8.2).
type PlayerAction string

// The player actions. The values are the last part of their web path.
const (
	PlayerKick            PlayerAction = "kick"
	PlayerBan             PlayerAction = "ban"
	PlayerUnban           PlayerAction = "unban"
	PlayerPromote         PlayerAction = "promote"
	PlayerDemote          PlayerAction = "demote"
	PlayerWhitelistAdd    PlayerAction = "whitelist-add"
	PlayerWhitelistRemove PlayerAction = "whitelist-remove"
)

// playerCommands are the console commands of each action, and whether a reply
// says the game did it. The game has no quoting: the name is the first word.
var playerCommands = map[PlayerAction]struct {
	command string
	done    func(reply string) bool
}{
	PlayerKick: {"/kick", func(r string) bool { return r == "" }},
	PlayerBan: {"/ban", func(r string) bool {
		return strings.HasPrefix(r, "Added ") && strings.HasSuffix(r, " to the banlist.") || strings.HasSuffix(r, " is already banned.")
	}},
	PlayerUnban: {"/unban", func(r string) bool { return strings.HasPrefix(r, "Unbanned ") }},
	PlayerPromote: {"/promote", func(r string) bool {
		return r == "" || strings.Contains(r, " is already in the admin list")
	}},
	PlayerDemote: {"/demote", func(r string) bool { return r == "" }},
	PlayerWhitelistAdd: {"/whitelist add", func(r string) bool {
		return strings.HasPrefix(r, "Added ") && strings.HasSuffix(r, " to the whitelist.") || strings.HasSuffix(r, " is already whitelisted.")
	}},
	PlayerWhitelistRemove: {"/whitelist remove", func(r string) bool {
		return strings.HasPrefix(r, "Removed ") && strings.HasSuffix(r, " from the whitelist.")
	}},
}

// Known reports whether a is one of the player actions.
func (a PlayerAction) Known() bool { _, ok := playerCommands[a]; return ok }

// TakesReason is true for kick and ban, the actions that carry a reason.
func (a PlayerAction) TakesReason() bool { return a == PlayerKick || a == PlayerBan }

var (
	// ErrPlayerAction means the action is not one Foreman sends.
	ErrPlayerAction = errors.New("there is no such player action")
	// ErrPlayerName means the name cannot be sent safely to the game, so it
	// is refused rather than escaped (SPEC 14.9).
	ErrPlayerName = errors.New("the player name cannot be sent to the game")
	// ErrPlayerReason means the reason is not one line of at most
	// gamesettings.MaxReasonChars characters, or was given for an action
	// that takes none.
	ErrPlayerReason = errors.New("the reason cannot be sent to the game")
	// ErrWhitelistOff means the running game was not started with the
	// world's whitelist switch on, so it would not keep a whitelist change
	// (SPEC 8.3).
	ErrWhitelistOff = errors.New("the game was started with the whitelist off, so it would not keep this change")
)

// PlayerRefusedError means the game answered the command but did not do it,
// for example a kick of a player who is not on. Reply is the game's answer,
// with the world's secrets removed.
type PlayerRefusedError struct{ Reply string }

func (e *PlayerRefusedError) Error() string { return "the game refused: " + e.Reply }

// CheckPlayerAction refuses, before anything is sent, an action, name or
// reason Foreman would not send (SPEC 8.2, 14.9).
func CheckPlayerAction(action PlayerAction, name, reason string) error {
	if !action.Known() {
		return ErrPlayerAction
	}
	if err := gamesettings.ValidateAdminName(name); err != nil {
		return fmt.Errorf("%w: %w", ErrPlayerName, err)
	}
	if reason != "" && !action.TakesReason() {
		return ErrPlayerReason
	}
	if err := gamesettings.ValidateReason(reason); err != nil {
		return fmt.Errorf("%w: %w", ErrPlayerReason, err)
	}
	return nil
}

// playerCommand is the console command for a checked action.
func playerCommand(action PlayerAction, name, reason string) string {
	cmd := playerCommands[action].command + " " + name
	if reason != "" {
		cmd += " " + reason
	}
	return cmd
}

// Player sends one player action to the world's running game (SPEC 8.2).
// The game writes its own list file, so no snapshot is taken. The action is
// recorded in the audit log before it is sent, and is not sent if it cannot
// be recorded (SPEC 12.9). A reply that does not say the game did it is a
// *PlayerRefusedError.
func (c *Controller) Player(ctx context.Context, id string, by Actor, action PlayerAction, name, reason string) error {
	if err := CheckPlayerAction(action, name, reason); err != nil {
		return err
	}
	rc, secrets, err := c.liveRCON(c.world(id))
	if err != nil {
		return err
	}
	if action == PlayerWhitelistAdd || action == PlayerWhitelistRemove {
		kept, err := c.WhitelistKept(ctx, id)
		if err != nil {
			return err
		}
		if !kept {
			return ErrWhitelistOff
		}
	}
	detail := string(action) + " " + name
	if reason != "" {
		detail += ": " + reason
	}
	if err := c.audit(ctx, by, AuditPlayer, id, Redact(detail, secrets...)); err != nil {
		return err
	}
	reply, err := c.execReply(ctx, rc, playerCommand(action, name, reason))
	if err != nil {
		return err
	}
	if reply = strings.TrimSpace(reply); !playerCommands[action].done(reply) {
		return &PlayerRefusedError{Reply: Redact(reply, secrets...)}
	}
	return nil
}

// WhitelistKept reports whether the world's game runs with its whitelist
// switch on, so the game enforces the whitelist and writes its file
// (SPEC 8.3). The switch is read from the running container, since a change
// to it takes effect only at the next start.
func (c *Controller) WhitelistKept(ctx context.Context, id string) (bool, error) {
	cont, found, err := c.ourContainer(ctx, id)
	if err != nil || !found {
		return false, err
	}
	return cont.State.Status == "running" && cont.Labels[LabelWhitelist] == "1", nil
}
