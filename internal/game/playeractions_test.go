package game

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func (r *rig) player(id string, action PlayerAction, name, reason string) error {
	r.t.Helper()
	return r.c.Player(context.Background(), id, admin, action, name, reason)
}

// playerCommandsSent are the player commands the game was sent, ignoring
// Foreman's own polling.
func (r *rig) playerCommandsSent(id string) []string {
	var out []string
	for _, c := range r.commands(id) {
		for _, a := range playerCommands {
			if strings.HasPrefix(c, a.command+" ") {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (r *rig) playerAudits() []string {
	var out []string
	for _, e := range r.audit.list() {
		if e.Action == AuditPlayer {
			out = append(out, e.Detail)
		}
	}
	return out
}

// SPEC 14.9: the console reads the first word as the name and has no
// quoting, and RCON strips leading spaces, so a name with a space, a control
// or invisible character, or over 64 bytes is refused, never escaped. Nothing
// is audited or sent.
func TestPlayerActionRefusesUnsafeNames(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Names")
	r.start(id)
	bad := []string{"", "bob smith", " bob", "bob x", "bob　x", "a\x00b", "a\rb", "a\nb", "x​", "a‮b", "a b",
		strings.Repeat("a", 65), "\xff"}
	for _, action := range []PlayerAction{PlayerKick, PlayerBan, PlayerUnban, PlayerPromote, PlayerDemote, PlayerWhitelistAdd, PlayerWhitelistRemove} {
		for _, name := range bad {
			if err := r.player(id, action, name, ""); !errors.Is(err, ErrPlayerName) {
				t.Errorf("%s %q: %v, want ErrPlayerName", action, name, err)
			}
		}
	}
	if err := r.player(id, PlayerPromote, strings.Repeat("a", 64), ""); err != nil {
		t.Errorf("a 64-byte name was refused: %v", err)
	}
	if sent := r.playerCommandsSent(id); len(sent) != 1 {
		t.Errorf("sent %q, want only the 64-byte promotion", sent)
	}
	if got := r.playerAudits(); len(got) != 1 {
		t.Errorf("audited %q", got)
	}
}

// SPEC 8.2: a reason is one line of at most 200 characters, with no NUL, CR
// or LF, and only kick and ban take one.
func TestPlayerActionRefusesBadReasons(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Reasons")
	r.start(id)
	for _, reason := range []string{"a\nb", "a\rb", "a\x00b", "ok\n/promote mallory", strings.Repeat("r", 201)} {
		for _, action := range []PlayerAction{PlayerKick, PlayerBan} {
			if err := r.player(id, action, "bob", reason); !errors.Is(err, ErrPlayerReason) {
				t.Errorf("%s with %q: %v", action, reason, err)
			}
		}
	}
	if err := r.player(id, PlayerUnban, "bob", "why"); !errors.Is(err, ErrPlayerReason) {
		t.Errorf("unban with a reason: %v", err)
	}
	if err := r.player(id, PlayerBan, "bob", strings.Repeat("é", 200)); err != nil {
		t.Errorf("a 200-character reason was refused: %v", err)
	}
	if sent := r.playerCommandsSent(id); len(sent) != 1 {
		t.Errorf("sent %q, want only the good ban", sent)
	}
}

// SPEC 12.9: every player action is audited before it is sent; if the audit
// fails, nothing is sent.
func TestPlayerActionIsAuditedFirst(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Audit")
	r.start(id)
	r.audit.setFail(true)
	if err := r.player(id, PlayerBan, "bob", "griefing"); err == nil {
		t.Fatal("the ban went ahead without an audit entry")
	}
	if sent := r.playerCommandsSent(id); len(sent) != 0 {
		t.Errorf("sent %q with the audit log down", sent)
	}
	r.audit.setFail(false)
	if err := r.player(id, PlayerBan, "bob", "griefing"); err != nil {
		t.Fatal(err)
	}
	if got := r.playerAudits(); !slices.Equal(got, []string{"ban bob: griefing"}) {
		t.Errorf("audit %q", got)
	}
}

// SPEC 8.2, ruling on live player actions: each action reaches the running
// game over RCON, the game writes its own list file, and no snapshot is
// taken.
func TestPlayerActionsChangeTheGamesLists(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Lists")
	r.start(id)
	steps := []struct {
		action       PlayerAction
		name, reason string
	}{
		{PlayerBan, "bob", "griefing"},
		{PlayerBan, "carl", ""},
		{PlayerUnban, "carl", ""},
		{PlayerPromote, "dana", ""},
		{PlayerPromote, "erin", ""},
		{PlayerDemote, "erin", ""},
	}
	for _, s := range steps {
		if err := r.player(id, s.action, s.name, s.reason); err != nil {
			t.Fatalf("%s %s: %v", s.action, s.name, err)
		}
	}
	want := []string{"/ban bob griefing", "/ban carl", "/unban carl", "/promote dana", "/promote erin", "/demote erin"}
	if sent := r.playerCommandsSent(id); !slices.Equal(sent, want) {
		t.Errorf("sent %q, want %q", sent, want)
	}
	bans := r.config(id, world.ServerBanListFile)
	if !strings.Contains(bans, `"username": "bob"`) || !strings.Contains(bans, `"reason": "griefing"`) || strings.Contains(bans, "carl") {
		t.Errorf("ban list %q", bans)
	}
	if admins := r.config(id, world.ServerAdminListFile); !strings.Contains(admins, "dana") || strings.Contains(admins, "erin") {
		t.Errorf("admin list %q", admins)
	}
	if n := len(r.snapshots(id)); n != 0 {
		t.Errorf("%d snapshots taken for live player actions", n)
	}
}

// SPEC 8.2, 17.9: a command the game answers without doing it is a
// refusal carrying the game's reply, so the page can show it.
func TestPlayerActionRefusedByTheGame(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Refused")
	r.start(id)
	var refused *PlayerRefusedError
	if err := r.player(id, PlayerKick, "ghost", "bye"); !errors.As(err, &refused) || refused.Reply != "Player ghost doesn't exist." {
		t.Errorf("kick of a player who is not on: %v", err)
	}
	if err := r.player(id, PlayerUnban, "ghost", ""); !errors.As(err, &refused) || refused.Reply != "ghost wasn't banned." {
		t.Errorf("unban of a player who is not banned: %v", err)
	}
	if err := r.game.Join(ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	if err := r.player(id, PlayerKick, "bob", "afk"); err != nil {
		t.Errorf("kick of a player who is on: %v", err)
	}
}

func TestPlayerActionNeedsARunningGame(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stopped")
	if err := r.player(id, PlayerBan, "bob", ""); !errors.Is(err, ErrNotRunning) {
		t.Errorf("error %v, want ErrNotRunning", err)
	}
	if len(r.playerAudits()) != 0 {
		t.Error("an action that was not sent was audited")
	}
}

// SPEC 8.3: the game keeps the whitelist only when it was started with the
// world's switch on. With it off, a live whitelist change would be lost at
// the next stop, so it is refused before anything is sent.
func TestWhitelistActionsFollowTheSwitch(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Whitelist")
	r.start(id)
	if err := r.player(id, PlayerWhitelistAdd, "hank", ""); !errors.Is(err, ErrWhitelistOff) {
		t.Errorf("switch off: %v, want ErrWhitelistOff", err)
	}
	if sent := r.playerCommandsSent(id); len(sent) != 0 || len(r.playerAudits()) != 0 {
		t.Errorf("sent %q", sent)
	}

	r2 := newRig(t, rigOptions{})
	id2 := r2.newWorld("Whitelist on")
	if err := r2.worlds.SetUseWhitelist(id2, true); err != nil {
		t.Fatal(err)
	}
	r2.start(id2)
	if err := r2.player(id2, PlayerWhitelistAdd, "hank", ""); err != nil {
		t.Fatal(err)
	}
	if err := r2.player(id2, PlayerWhitelistAdd, "ivy", ""); err != nil {
		t.Fatal(err)
	}
	if err := r2.player(id2, PlayerWhitelistRemove, "hank", ""); err != nil {
		t.Fatal(err)
	}
	if got := r2.config(id2, world.ServerWhiteListFile); !strings.Contains(got, "ivy") || strings.Contains(got, "hank") {
		t.Errorf("whitelist file %q, want the game's own write holding ivy", got)
	}
}

// SPEC 8.3: editing the ban list or whitelist of a running world goes over
// RCON for the lists the game keeps; a whitelist the game does not keep
// (switch off) is written by Foreman, after the settings snapshot.
func TestSaveListsOnARunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Running lists")
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerBanListFile), []byte(`["old"]`))
	r.start(id)
	res, err := r.c.SaveSettings(context.Background(), id, admin, []SettingsChange{
		{File: world.ServerBanListFile, Data: []byte(`["new", {"username": "why", "reason": "spam"}]`), SeenHash: r.seen(id, world.ServerBanListFile)},
		{File: world.ServerWhiteListFile, Data: []byte(`["hank"]`), SeenHash: r.seen(id, world.ServerWhiteListFile)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Live {
		t.Error("the ban change was sent to the running game, so it is live")
	}
	want := []string{"/unban old", "/ban new", "/ban why spam"}
	if sent := r.playerCommandsSent(id); !slices.Equal(sent, want) {
		t.Errorf("sent %q, want %q", sent, want)
	}
	if got := r.config(id, world.ServerWhiteListFile); got != `["hank"]` {
		t.Errorf("whitelist %q: with the switch off Foreman writes it", got)
	}
	if len(r.settingsSnapshots(id)) != 1 {
		t.Error("the save took no snapshot")
	}
}

func TestSaveListsOnAStoppedWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stopped lists")
	bans := `["new", {"username": "why", "reason": "spam"}]`
	for file, data := range map[string]string{world.ServerBanListFile: bans, world.ServerWhiteListFile: `["hank"]`} {
		if _, err := r.saveSettings(id, file, data); err != nil {
			t.Fatal(err)
		}
		if got := r.config(id, file); got != data {
			t.Errorf("%s is %q", file, got)
		}
	}
	if len(r.settingsSnapshots(id)) != 2 {
		t.Error("each save takes a snapshot")
	}
	if _, err := r.saveSettings(id, world.ServerBanListFile, `["a b"]`); !errors.Is(err, ErrSettingsInvalid) {
		t.Errorf("a name with a space: %v", err)
	}
}
