package fakegame

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The replies and log lines below are what the pinned game versions printed
// to the RCON commands in internal/game/testdata/<version>/rcon-players.log
// and run-players.log, which were identical in all three except where a
// comment says otherwise. Where a case was not captured, a comment says so.

const (
	banUsage  = "Invalid parameter\n<add/remove/get/clear> <player> - Adds or removes a player from the banlist. Same as /ban or /unban.\n"
	listUsage = "Invalid parameter\n<enable/disable/add/remove/get/clear> <player> - Enables, disables, adds or removes a player from the whitelist, where only whitelisted players can join the game. Enter nothing for 'player' when using 'get' to print a list of all whitelisted players.\n"

	luaWarning = "[WARNING] Using Lua console commands will disable achievements. Please repeat the command to proceed."
)

// stamp puts the game's date and time before a log line.
func stamp(text string) string {
	return time.Now().UTC().Format("2006-01-02 15:04:05") + " " + text
}

// reply answers one RCON command. RCON strips the leading spaces of what it
// is sent, so text that starts with spaces and then "/" is a command, as it
// was in the real game; anything else is said in chat by the server.
func (in *instance) reply(command string) string {
	in.g.mu.Lock()
	in.g.commands[in.name] = append(in.g.commands[in.name], command)
	hangs := in.g.behave[in.name].saveHangs
	in.g.mu.Unlock()

	text := strings.TrimLeft(command, " ")
	switch strings.TrimSpace(text) {
	case "":
		return ""
	case "/players online":
		in.mu.Lock()
		defer in.mu.Unlock()
		out := fmt.Sprintf("Online players (%d):\n", len(in.players))
		for _, p := range in.players {
			out += "  " + p + " (online)\n"
		}
		return out
	case "/players":
		// With players on, the lines are those of /players online; that form
		// was not captured.
		in.mu.Lock()
		defer in.mu.Unlock()
		out := fmt.Sprintf("Players (%d):\n", len(in.players))
		for _, p := range in.players {
			out += "  " + p + " (online)\n"
		}
		return out
	case "/server-save":
		// The reply is immediate; the save finishes later, shown in the log.
		in.emit(in.v.save[:1], runSave)
		if !hangs {
			in.g.after(saveTime, func() { in.emit(in.v.save[1:], runSave) })
		}
		return "Saving the map\n"
	}
	if !strings.HasPrefix(text, "/") {
		in.emitText(stamp("[CHAT] <server>: " + text))
		return ""
	}
	name, args, _ := strings.Cut(text[1:], " ")
	in.mu.Lock()
	reply, logs := in.run(name, args)
	in.mu.Unlock()
	for _, l := range logs {
		in.emitText(stamp(l))
	}
	return reply
}

// run executes one slash command with in.mu held. It returns the reply and
// the log lines, which the caller writes once the lock is released.
func (in *instance) run(name, args string) (reply string, logs []string) {
	player, rest := firstWord(args)
	switch name {
	case "kick":
		return in.kick(player, rest)
	case "ban":
		return in.ban(player, rest)
	case "unban":
		return in.unban(player)
	case "bans":
		return in.banned(), nil
	case "banlist":
		if player == "get" {
			return in.banned(), nil
		}
	case "promote":
		return in.promote(player)
	case "demote":
		return in.demote(player)
	case "whitelist":
		return in.whitelistCommand(player, rest)
	case "c", "command", "silent-command", "sc":
		return in.lua(name == "c" || name == "command", args)
	}
	return "", nil
}

// firstWord splits at the first space; the rest has its leading spaces
// dropped. The game takes the first word as the player's name, whatever it
// holds (a quote, an accent, 70 characters).
func firstWord(s string) (word, rest string) {
	word, rest, _ = strings.Cut(s, " ")
	return word, strings.TrimLeft(rest, " ")
}

// nameList joins names as the game does: "a", "a and b", "a, b and c".
func nameList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// Player names match exactly (case-sensitive): how the real game treats
// upper and lower case in names was not captured.
func (in *instance) banIndex(name string) int {
	return slices.IndexFunc(in.bans, func(b banEntry) bool { return b.name == name })
}

// leave removes an online player who was kicked or banned. The line the
// game logs for that was not captured (no client could join); this one is
// the plain leave line.
func (in *instance) leave(player string) []string {
	if !slices.Contains(in.players, player) {
		return nil
	}
	in.players = slices.DeleteFunc(in.players, func(p string) bool { return p == player })
	return []string{"[LEAVE] " + player + " left the game"}
}

func (in *instance) kick(player, reason string) (string, []string) {
	if !slices.Contains(in.players, player) {
		return "Player " + player + " doesn't exist.\n", nil // nothing is logged
	}
	if reason == "" {
		// "unspecified" is a guess, not captured from a real game.
		reason = "unspecified"
	}
	// The kick line for a player who is on was not captured; this follows
	// the ban line's wording.
	logs := []string{"[KICK] " + player + " was kicked by <server>. Reason: " + reason + "."}
	return "", append(logs, in.leave(player)...)
}

func (in *instance) ban(player, reason string) (string, []string) {
	if player == "" {
		return banUsage, nil
	}
	shown := reason
	if shown == "" {
		shown = "unspecified"
	}
	where := " (not on map)"
	var after []string
	if slices.Contains(in.players, player) {
		// The ban line for a player who is on was not captured; it is
		// the offline line without "(not on map)".
		where = ""
		after = in.leave(player)
	}
	logs := append([]string{"[BAN] " + player + where + " was banned by <server>. Reason: " + shown + "."}, after...)
	if in.banIndex(player) >= 0 {
		return player + " is already banned.\n", logs // logged all the same
	}
	in.bans = append(in.bans, banEntry{name: player, reason: reason})
	// The captures list bans by name, not in the order they were added.
	slices.SortFunc(in.bans, func(a, b banEntry) int { return strings.Compare(a.name, b.name) })
	in.saveBans()
	return "Added " + player + " to the banlist.\n", logs
}

func (in *instance) unban(player string) (string, []string) {
	logs := []string{"[UNBANNED] " + player + " was unbanned by <server>."} // even when not banned
	i := in.banIndex(player)
	if i < 0 {
		return player + " wasn't banned.\n", logs
	}
	in.bans = slices.Delete(in.bans, i, i+1)
	in.saveBans()
	return "Unbanned " + player + ".\n", logs
}

func (in *instance) banned() string {
	if len(in.bans) == 0 {
		return "The banlist is empty.\n"
	}
	names := make([]string, len(in.bans))
	for i, b := range in.bans {
		names[i] = b.name
	}
	return "Banned players: " + nameList(names) + "\n"
}

// adminName is how the admin list stores a name; 2.x drops accents. The
// log line shows the name as typed.
func (in *instance) adminName(player string) string {
	if strings.HasPrefix(in.v.name, "1.") {
		return player
	}
	return foldName(player)
}

func (in *instance) promote(player string) (string, []string) {
	if player == "" {
		return "", nil
	}
	// Case handling as in banIndex: not captured from a real game.
	stored := in.adminName(player)
	if slices.Contains(in.admins, stored) {
		return player + " is already in the admin list and will be promoted upon joining the game.\n", nil
	}
	in.admins = append(in.admins, stored)
	in.saveAdmins()
	return "", []string{"[PROMOTE] " + player + " was added to the admin list by <server> and will be promoted upon joining the game."}
}

func (in *instance) demote(player string) (string, []string) {
	if player == "" {
		return "", nil
	}
	i := slices.Index(in.admins, in.adminName(player))
	if i < 0 {
		return player + " is not in the admin list.\n", nil
	}
	in.admins = slices.Delete(in.admins, i, i+1)
	in.saveAdmins()
	return "", []string{"[DEMOTE] " + player + " was removed from the admin list by <server> and will no longer be promoted upon joining the game."}
}

// whitelistCommand does not log: the game printed nothing for any of these.
// enable and disable reply nothing and change nothing the fake shows; the
// game's own enabled state cannot be read over RCON.
func (in *instance) whitelistCommand(sub, rest string) (string, []string) {
	player, _ := firstWord(rest)
	switch sub {
	case "enable", "disable":
		return "", nil
	case "get":
		if len(in.whitelist) == 0 {
			return "The whitelist is empty.\n", nil
		}
		return "Whitelisted players: " + nameList(in.whitelist) + "\n", nil
	case "clear":
		if len(in.whitelist) == 0 {
			return "The whitelist is already empty.\n", nil
		}
		in.whitelist = nil
		in.saveWhitelist()
		return "The whitelist has been cleared and disabled.\n", nil
	case "add":
		if player == "" {
			return listUsage, nil
		}
		// Case-sensitive match; see banIndex.
		if slices.Contains(in.whitelist, player) {
			return player + " is already whitelisted.\n", nil
		}
		in.whitelist = append(in.whitelist, player)
		in.saveWhitelist()
		return "Added " + player + " to the whitelist.\n", nil
	case "remove":
		if player == "" {
			return listUsage, nil
		}
		// Case-sensitive match; see banIndex.
		i := slices.Index(in.whitelist, player)
		if i < 0 {
			return player + " wasn't in the whitelist.\n", nil
		}
		in.whitelist = slices.Delete(in.whitelist, i, i+1)
		in.saveWhitelist()
		return "Removed " + player + " from the whitelist.\n", nil
	}
	return listUsage, nil
}

var rconPrint = regexp.MustCompile(`^\s*rcon\.print\(\s*"((?:[^"\\]|\\.)*)"\s*\)\s*$`)

// lua runs a Lua console command. The first one on a save only warns that
// achievements will be disabled; the next runs, and the save stays disabled.
// The fake runs only rcon.print with a string literal. A silent command
// logs nothing when it runs; whether it warns first was not captured.
func (in *instance) lua(logged bool, code string) (string, []string) {
	in.g.mu.Lock()
	b := in.g.behave[in.name]
	b.luaCommands++
	in.g.behave[in.name] = b
	first := b.luaCommands == 1
	in.g.mu.Unlock()

	if first {
		return "", []string{
			`[WARNING] Player <server> tried using the command "` + code + `".`,
			luaWarning,
		}
	}
	var logs []string
	if logged {
		logs = []string{"[COMMAND] <server> (command): " + code}
	}
	if m := rconPrint.FindStringSubmatch(code); m != nil {
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[1]) + "\n", logs
	}
	return "", logs
}
