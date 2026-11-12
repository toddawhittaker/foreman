package fakegame_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The flags Foreman's start script gives the game (SPEC 4.3). The whitelist
// line is the script's: passed only when the file exists. The second form
// adds --use-server-whitelist.
const (
	scriptLists = `set -- --start-server-load-latest --rcon-port 27015 --server-adminlist /factorio/config/server-adminlist.json --server-banlist /factorio/config/server-banlist.json
`
	scriptWhitelist    = `if [ -f /factorio/config/server-whitelist.json ]; then set -- "$@" --server-whitelist /factorio/config/server-whitelist.json; fi` + "\n"
	scriptWhitelistUse = `if [ -f /factorio/config/server-whitelist.json ]; then set -- "$@" --server-whitelist /factorio/config/server-whitelist.json --use-server-whitelist; fi` + "\n"
	scriptExec         = `exec /opt/factorio/bin/x64/factorio "$@"`
)

// startScript starts a container whose command is the given script text.
func (r *rig) startScript(name, img, policy, script string) string {
	r.t.Helper()
	ctx := context.Background()
	spec := r.spec(img, policy)
	spec.Cmd = []string{"-c", script, "sh", "mysave.zip"}
	id, err := r.docker.CreateContainer(ctx, name, spec)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *rig) config(file string) string { return filepath.Join(r.world, world.ConfigDir, file) }

// restart is a stop and a start of the same container, as Foreman does it.
func (r *rig) restart(id string) {
	r.t.Helper()
	if err := r.docker.StopContainer(context.Background(), id, 120); err != nil {
		r.t.Fatal(err)
	}
	if err := r.docker.StartContainer(context.Background(), id); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) read(file string) string {
	r.t.Helper()
	b, err := os.ReadFile(r.config(file))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func (r *rig) exists(file string) bool {
	_, err := os.Stat(r.config(file))
	return err == nil
}

var (
	elapsedField = regexp.MustCompile(`^ *[0-9.]+ (.*)$`)
	tagged       = regexp.MustCompile(`^(?:\S+ )?\d{4}-\d\d-\d\d \d\d:\d\d:\d\d (\[(?:CHAT|BAN|UNBANNED|PROMOTE|DEMOTE|COMMAND|WARNING)\] .*)$`)
	capturedFile = regexp.MustCompile(`^(server-\S+\.json) (absent|\d\d:\d\d:\d\d)$`)
)

// taggedLines are the lines of a log that carry one of the chat and
// command tags, without the date.
func taggedLines(log string) []string {
	var out []string
	for l := range strings.SplitSeq(log, "\n") {
		if m := tagged.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a fixed test file
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Replaying the commands of a real capture must give its replies, its list
// files and its log lines, byte for byte, in every pinned version.
func TestReplayOfTheCapturedPlayerCommands(t *testing.T) {
	for _, ver := range []string{"1.1.110", "2.0.77", "2.1.24"} {
		t.Run(ver, func(t *testing.T) {
			dir := filepath.Join("..", "game", "testdata", ver)
			capture := strings.Split(readFile(t, filepath.Join(dir, "rcon-players.log")), "\n")
			img := "factoriotools/factorio:" + ver
			r := newRig(t, fakegame.Config{Images: []string{img}})
			id := r.startScript("foreman-w1", img, "unless-stopped", scriptLists+scriptWhitelist+scriptExec)

			commands, files := 0, 0
			for i := 0; i < len(capture); i++ {
				switch l := capture[i]; {
				case strings.HasPrefix(l, "> "):
					cmd, err := strconv.Unquote(l[2:])
					if err != nil {
						t.Fatal(err)
					}
					i++
					want, err := strconv.Unquote(strings.TrimPrefix(capture[i], "< "))
					if err != nil {
						t.Fatal(err)
					}
					if got := r.exec(id, cmd); got != want {
						t.Errorf("%s: reply %q, want %q", strconv.Quote(cmd), got, want)
					}
					commands++
				case strings.HasPrefix(l, "-- files in config/"):
					for i++; i < len(capture) && !strings.HasPrefix(capture[i], "-- write-data"); i++ {
						m := capturedFile.FindStringSubmatch(capture[i])
						if m == nil {
							t.Fatalf("unexpected capture line %q", capture[i])
						}
						var content []string
						for ; i+1 < len(capture) && strings.HasPrefix(capture[i+1], "    "); i++ {
							content = append(content, strings.TrimPrefix(capture[i+1], "    "))
						}
						files++
						if m[2] == "absent" {
							if r.exists(m[1]) {
								t.Errorf("%s exists, the game had not written it yet", m[1])
							}
							continue
						}
						if got, want := strings.TrimRight(r.read(m[1]), "\n"), strings.Join(content, "\n"); got != want {
							t.Errorf("%s =\n%q\nwant\n%q", m[1], got, want)
						}
					}
				}
			}
			if commands < 50 || files < 20 {
				t.Fatalf("replayed %d commands and %d files; the capture was not read", commands, files)
			}

			want := taggedLines(readFile(t, filepath.Join(dir, "run-players.log")))
			got := taggedLines(r.log(id))
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("log lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if len(want) < 10 {
				t.Fatalf("only %d tagged log lines in the capture", len(want))
			}
		})
	}
}

func TestWithoutAWhitelistFlagTheListIsLostOnRestart(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptWhitelist+scriptExec)
	r.exec(id, "/whitelist enable")
	r.exec(id, "/whitelist add dave")
	if got := r.exec(id, "/whitelist get"); got != "Whitelisted players: dave\n" {
		t.Fatalf("get = %q", got)
	}
	r.restart(id)
	if got := r.exec(id, "/whitelist get"); got != "The whitelist is empty.\n" {
		t.Errorf("after the restart: %q", got)
	}
	if r.exists("server-whitelist.json") {
		t.Error("the game wrote a whitelist file without --server-whitelist")
	}
}

func TestBothWhitelistFlagsWriteAtOnceAndSurviveARestart(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	if err := os.WriteFile(r.config("server-whitelist.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptWhitelistUse+scriptExec)
	r.exec(id, "/whitelist add hank")
	if got, want := r.read("server-whitelist.json"), "\n[\n  \"hank\"\n]"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	r.exec(id, "/whitelist add ivy")
	r.exec(id, "/whitelist disable")
	if got, want := r.read("server-whitelist.json"), "\n[\n  \"hank\",\n  \"ivy\"\n]"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	r.restart(id)
	if got := r.exec(id, "/whitelist get"); got != "Whitelisted players: hank and ivy\n" {
		t.Errorf("after the restart: %q", got)
	}
	r.exec(id, "/whitelist remove hank")
	if got, want := r.read("server-whitelist.json"), "\n[\n  \"ivy\"\n]"; got != want {
		t.Errorf("file after remove = %q, want %q", got, want)
	}
	if got := r.exec(id, "/whitelist clear"); got != "The whitelist has been cleared and disabled.\n" {
		t.Errorf("clear = %q", got)
	}
	if got, want := r.read("server-whitelist.json"), "\n[]"; got != want {
		t.Errorf("file after clear = %q, want %q", got, want)
	}
}

func TestWhitelistFlagWithoutTheUseFlagExitsWithTheCapturedError(t *testing.T) {
	for _, ver := range []string{"1.1.110", "2.0.77", "2.1.24"} {
		t.Run(ver, func(t *testing.T) {
			img := "factoriotools/factorio:" + ver
			r := newRig(t, fakegame.Config{Images: []string{img}})
			if err := os.WriteFile(r.config("server-whitelist.json"), []byte("[]"), 0o600); err != nil {
				t.Fatal(err)
			}
			id := r.startScript("foreman-w1", img, "no", scriptLists+scriptWhitelist+scriptExec)
			eventually(t, "the game to exit", func() bool { return r.status(id) == "exited" })
			if c := r.inspect(id); c.State.ExitCode != 1 {
				t.Errorf("exit code %d, want 1", c.State.ExitCode)
			}

			// The capture's lines from the error to "Goodbye", text only.
			var want []string
			in := false
			for l := range strings.SplitSeq(readFile(t, filepath.Join("..", "game", "testdata", ver, "run-whitelist-flag-missing.log")), "\n") {
				_, rest, _ := strings.Cut(l, " ")
				if m := elapsedField.FindStringSubmatch(rest); m != nil {
					in = in || strings.HasPrefix(m[1], "Error CommandLineMultiplayer.cpp:")
					if in {
						want = append(want, m[1])
					}
				}
			}
			var got []string
			for l := range strings.SplitSeq(r.log(id), "\n") {
				if m := elapsedField.FindStringSubmatch(l); m != nil {
					got = append(got, m[1])
				}
			}
			tail := strings.Join(want, "\n")
			if len(want) < 3 || !strings.HasSuffix(strings.Join(got, "\n"), tail) {
				t.Errorf("the log does not end with the captured error:\n%s\nwant it to end with:\n%s", strings.Join(got, "\n"), tail)
			}
			if !strings.HasSuffix(tail, "Goodbye") || strings.Contains(r.log(id), "Loading map") {
				t.Errorf("the game must stop before it loads the map")
			}
		})
	}
}

func TestChatPlainTextIsSaidByTheServer(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptExec)
	if got := r.exec(id, "hello from rcon"); got != "" {
		t.Errorf("reply = %q, want empty", got)
	}
	if !strings.Contains(r.log(id), " [CHAT] <server>: hello from rcon\n") {
		t.Errorf("log lacks the chat line:\n%s", r.log(id))
	}
}

// RCON strips leading spaces, so a chat message with spaces then "/" is a
// command, as the real game ran it.
func TestLeadingSpacesBeforeASlashRunTheCommand(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptExec)
	if got := r.exec(id, "   /ban zed via-leading-space"); got != "Added zed to the banlist.\n" {
		t.Errorf("reply = %q", got)
	}
	if got := r.exec(id, "  /bans"); got != "Banned players: zed\n" {
		t.Errorf("bans = %q", got)
	}
	if strings.Contains(r.log(id), "[CHAT]") {
		t.Error("a command was said in chat")
	}
	// The fake keeps the bytes it received, so a test can see what was sent.
	var sent []string
	for _, c := range r.game.State().Containers {
		sent = c.Commands
	}
	if len(sent) == 0 || sent[0] != "   /ban zed via-leading-space" {
		t.Errorf("commands = %q", sent)
	}
}

func TestLuaCommandWarnsFirstThenRunsAndDisablesAchievements(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptExec)
	achievements := func() bool { return r.game.State().Containers[0].AchievementsDisabled }

	if got := r.exec(id, ` /c rcon.print("one")`); got != "" || achievements() {
		t.Errorf("first /c: reply %q, achievements disabled %v", got, achievements())
	}
	if got := r.exec(id, ` /c rcon.print("two")`); got != "two\n" || !achievements() {
		t.Errorf("second /c: reply %q, achievements disabled %v", got, achievements())
	}
	want := []string{
		`[WARNING] Player <server> tried using the command "rcon.print("one")".`,
		`[WARNING] Using Lua console commands will disable achievements. Please repeat the command to proceed.`,
		`[COMMAND] <server> (command): rcon.print("two")`,
	}
	if got := taggedLines(r.log(id)); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("log lines = %q, want %q", got, want)
	}
}

func TestBanListAndAdminListAreLoadedAtStart(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	if err := os.WriteFile(r.config("server-banlist.json"), []byte(`["carl",{"username":"zed","reason":"x"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.config("server-adminlist.json"), []byte(`["carol"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptExec)
	if got := r.exec(id, "/bans"); got != "Banned players: carl and zed\n" {
		t.Errorf("bans = %q", got)
	}
	if got := r.exec(id, "/promote carol"); !strings.HasPrefix(got, "carol is already in the admin list") {
		t.Errorf("promote = %q", got)
	}
}

func TestNoListFlagsMeansNoListFiles(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.start("foreman-w1", image21, "unless-stopped")
	r.exec(id, "/ban bob x")
	r.exec(id, "/promote carol")
	if r.exists("server-banlist.json") || r.exists("server-adminlist.json") {
		t.Error("the game wrote a list file it was not told about")
	}
	if got := r.exec(id, "/bans"); got != "Banned players: bob\n" {
		t.Errorf("the ban list in memory: %q", got)
	}
}

func TestKickAndBanOfAnOnlinePlayerRemoveThem(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptExec)
	for _, p := range []string{"alice", "bob"} {
		if err := r.game.Join("foreman-w1", p); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.exec(id, "/kick alice griefing"); got != "" {
		t.Errorf("kick = %q", got)
	}
	if got := r.exec(id, "/players online"); got != "Online players (1):\n  bob (online)\n" {
		t.Errorf("after kick: %q", got)
	}
	if got := r.exec(id, "/ban bob spam"); got != "Added bob to the banlist.\n" {
		t.Errorf("ban = %q", got)
	}
	if got := r.exec(id, "/players online"); got != "Online players (0):\n" {
		t.Errorf("after ban: %q", got)
	}
	log := r.log(id)
	if !strings.Contains(log, "[BAN] bob was banned by <server>. Reason: spam.") {
		t.Errorf("log lacks the ban line:\n%s", log)
	}
	if got := r.exec(id, "/kick alice"); got != "Player alice doesn't exist.\n" {
		t.Errorf("kick of a player no longer on: %q", got)
	}
}

func TestChatFromAClient(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	id := r.startScript("foreman-w1", image21, "unless-stopped", scriptLists+scriptExec)
	if err := r.game.Chat("foreman-w1", "alice", "hi"); err == nil {
		t.Error("a player who is not on said something")
	}
	if err := r.game.Join("foreman-w1", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.game.Chat("foreman-w1", "alice", "hi there"); err != nil {
		t.Fatal(err)
	}
	if got := taggedLines(r.log(id)); len(got) != 1 || got[0] != "[CHAT] alice: hi there" {
		t.Errorf("tagged lines = %q", got)
	}
	for name, err := range map[string]error{
		"no game":     r.game.Chat("foreman-nope", "alice", "x"),
		"no player":   r.game.Chat("foreman-w1", "", "x"),
		"empty text":  r.game.Chat("foreman-w1", "alice", ""),
		"newline":     r.game.Chat("foreman-w1", "alice", "a\nb"),
		"unknown man": r.game.Chat("foreman-w1", "carl", "x"),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestOutOfMemoryKillsTheGameWhileLoading(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	r.game.SetOOM("foreman-w1", true)
	id := r.start("foreman-w1", image21, "no")
	eventually(t, "the game to be killed", func() bool { return r.status(id) == "exited" })
	c := r.inspect(id)
	if c.State.ExitCode != 137 || !c.State.OOMKilled {
		t.Errorf("state = %+v, want exit 137 and OOMKilled", c.State)
	}
	log := r.log(id)
	if !strings.Contains(log, "Loading mod base 2.1.24 (data.lua)") || strings.Contains(log, "Loading map") ||
		strings.Contains(log, "Goodbye") {
		t.Errorf("the log must stop in the middle of loading mods:\n%s", log)
	}
	if !r.game.State().Containers[0].OOM {
		t.Error("State does not show the OOM setting")
	}

	r.game.SetOOM("foreman-w1", false)
	if err := r.docker.StartContainer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.exec(id, "/players online")
	if c := r.inspect(id); c.State.OOMKilled {
		t.Error("OOMKilled is still set on a running game")
	}
}

func TestOutOfMemoryRestartsUnderUnlessStopped(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	r.game.SetOOM("foreman-w1", true)
	id := r.start("foreman-w1", image21, "unless-stopped")
	eventually(t, "a restart", func() bool { return r.inspect(id).RestartCount >= 1 })
	r.game.SetOOM("foreman-w1", false)
	eventually(t, "the game to load", func() bool { return r.inspect(id).State.Running && r.game.State().Containers[0].IP != "" })
}

func TestPortInUseFailsTheFirstStartOnly(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	r.game.FailStartPortInUse("foreman-w1", "34197")
	ctx := context.Background()
	id, err := r.docker.CreateContainer(ctx, "foreman-w1", r.spec(image21, "unless-stopped"))
	if err != nil {
		t.Fatal(err)
	}
	err = r.docker.StartContainer(ctx, id)
	if err == nil || !strings.Contains(err.Error(), "Bind for 0.0.0.0:34197 failed: port is already allocated") {
		t.Fatalf("start error = %v", err)
	}
	if c := r.inspect(id); c.State.Status != "created" || c.State.ExitCode != 128 ||
		!strings.Contains(c.State.Error, "port is already allocated") {
		t.Errorf("state = %+v", c.State)
	}
	if log := r.log(id); log != "" {
		t.Errorf("a game booted behind the failed start:\n%s", log)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatalf("the second start: %v", err)
	}
	r.exec(id, "/players online")
}
