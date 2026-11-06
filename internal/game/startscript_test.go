package game

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// fakeBinary stands in for the game. Each run prints its arguments one per
// line and then END. Run with --create, it makes the file it was given, as
// the game does, unless FAKE_CREATE_FAILS is set, when it exits 3.
const fakeBinary = `#!/bin/sh
printf '%s\n' "$@"
printf 'END\n'
if [ "$1" = "--create" ]; then
  [ -n "$FAKE_CREATE_FAILS" ] && exit 3
  : > "$2"
fi
`

// scriptRun is what startScript did against a fake world folder.
type scriptRun struct {
	dir   string
	calls [][]string // the arguments of each run of the game, in order
	err   error      // the script's own exit
}

// runScriptEnv runs startScript against a fake world folder, with the game
// replaced by fakeBinary. files are made in config/, saves in saves/, and
// env is added to the script's environment.
func runScriptEnv(t *testing.T, env []string, saves []string, files ...string) scriptRun {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{world.ConfigDir, world.SavesDir, world.ModsDir} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range append(files, "rconpw") {
		if err := os.WriteFile(filepath.Join(dir, world.ConfigDir, f), []byte("pw-from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range saves {
		if err := os.WriteFile(filepath.Join(dir, world.SavesDir, s), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(t.TempDir(), "game")
	if err := os.WriteFile(bin, []byte(fakeBinary), 0o700); err != nil { //nolint:gosec // a test's own script
		t.Fatal(err)
	}
	// The binary replaces its path first, since that path holds the mount's.
	script := strings.ReplaceAll(startScript, gameBinary, bin)
	script = strings.ReplaceAll(script, gameMount, dir)
	cmd := exec.Command("/bin/sh", "-c", script) //nolint:gosec // a fixed script in a test
	cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, env...)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("script did not run: %v", err)
	}
	run := scriptRun{dir: dir, err: err}
	var args []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l == "END" {
			run.calls = append(run.calls, args)
			args = nil
			continue
		}
		args = append(args, l)
	}
	return run
}

// runScript runs the script with no environment and returns the arguments
// of the one game run it makes.
func runScript(t *testing.T, files ...string) []string {
	t.Helper()
	run := runScriptEnv(t, nil, nil, files...)
	if run.err != nil || len(run.calls) != 1 {
		t.Fatalf("want one clean run of the game, got %v, %v", run.calls, run.err)
	}
	return run.calls[0]
}

// flagValue returns the argument after flag, or "" when flag is absent.
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// SPEC 5.1, 5.5: the game is told where the admin and ban lists live
// whether or not they exist yet, so /promote and /ban persist in the
// world's folder. Settings and the whitelist are passed only when present,
// and the whitelist is never enforced (SPEC 11).
func TestStartScriptFlagsForConfigFiles(t *testing.T) {
	cfg := func(dir, f string) string { return filepath.Join(dir, world.ConfigDir, f) }
	t.Run("an empty config folder", func(t *testing.T) {
		args := runScript(t)
		for flag, file := range map[string]string{
			"--server-adminlist": world.ServerAdminListFile,
			"--server-banlist":   world.ServerBanListFile,
		} {
			if got := flagValue(args, flag); filepath.Base(got) != file || filepath.Base(filepath.Dir(got)) != world.ConfigDir {
				t.Errorf("%s is %q, want .../config/%s", flag, got, file)
			}
		}
		for _, flag := range []string{"--server-settings", "--server-whitelist", "--use-server-whitelist"} {
			if got := flagValue(args, flag); got != "" {
				t.Errorf("%s passed as %q with no file", flag, got)
			}
		}
	})
	t.Run("every config file present", func(t *testing.T) {
		args := runScript(t, world.ServerSettingsFile, world.ServerWhiteListFile, world.ServerAdminListFile, world.ServerBanListFile)
		for flag, file := range map[string]string{
			"--server-settings":  world.ServerSettingsFile,
			"--server-whitelist": world.ServerWhiteListFile,
			"--server-adminlist": world.ServerAdminListFile,
			"--server-banlist":   world.ServerBanListFile,
		} {
			if got := flagValue(args, flag); filepath.Base(got) != file || cfg(filepath.Dir(filepath.Dir(got)), file) != got {
				t.Errorf("%s is %q, want .../config/%s", flag, got, file)
			}
		}
		if strings.Contains(strings.Join(args, " "), "--use-server-whitelist") {
			t.Error("the whitelist is enforced; that is a game setting (SPEC 11)")
		}
	})
}

// SPEC 4.3: a world with no save and new_map set gets its map made by the
// script, before the server starts; the map-gen options are passed only when
// they are set, and the settings files only when they exist.
func TestStartScriptCreatesTheMap(t *testing.T) {
	newMap := []string{gameenv.NewMap + "=1", gameenv.WorldID + "=my-world"}

	t.Run("a bare new map", func(t *testing.T) {
		run := runScriptEnv(t, newMap, nil)
		if run.err != nil || len(run.calls) != 2 {
			t.Fatalf("want a create run and a server run, got %v, %v", run.calls, run.err)
		}
		create, server := run.calls[0], run.calls[1]
		if create[0] != "--create" || create[1] != filepath.Join(run.dir, world.SavesDir, "my-world.zip") {
			t.Errorf("create arguments %v", create)
		}
		if got := flagValue(create, "--mod-directory"); got != filepath.Join(run.dir, world.ModsDir) {
			t.Errorf("--mod-directory is %q", got)
		}
		for _, flag := range []string{"--preset", "--map-gen-seed", "--map-gen-settings", "--map-settings"} {
			if hasArg(create, flag) {
				t.Errorf("%s passed though nothing sets it", flag)
			}
		}
		if server[0] != "--start-server-load-latest" {
			t.Errorf("the server did not start after the create: %v", server)
		}
		if _, err := os.Stat(filepath.Join(run.dir, world.SavesDir, "my-world.zip")); err != nil {
			t.Errorf("the create step made no save: %v", err)
		}
	})

	t.Run("preset, seed and both settings files", func(t *testing.T) {
		env := append([]string{gameenv.Preset + "=rich-resources", gameenv.Seed + "=4294967295"}, newMap...)
		run := runScriptEnv(t, env, nil, world.MapGenSettingsFile, world.MapSettingsFile)
		create := run.calls[0]
		if flagValue(create, "--preset") != "rich-resources" || flagValue(create, "--map-gen-seed") != "4294967295" {
			t.Errorf("preset or seed missing: %v", create)
		}
		for flag, file := range map[string]string{
			"--map-gen-settings": world.MapGenSettingsFile,
			"--map-settings":     world.MapSettingsFile,
		} {
			if got := flagValue(create, flag); got != filepath.Join(run.dir, world.ConfigDir, file) {
				t.Errorf("%s is %q", flag, got)
			}
		}
		// The settings files belong to the create step only.
		if hasArg(run.calls[1], "--map-gen-settings") || hasArg(run.calls[1], "--map-settings") {
			t.Errorf("the server was given map settings: %v", run.calls[1])
		}
	})

	t.Run("a save exists, so a Docker restart makes no map", func(t *testing.T) {
		run := runScriptEnv(t, newMap, []string{"first.zip"})
		if run.err != nil || len(run.calls) != 1 || run.calls[0][0] != "--start-server-load-latest" {
			t.Fatalf("want only the server run, got %v, %v", run.calls, run.err)
		}
	})

	t.Run("without new_map no map is made", func(t *testing.T) {
		run := runScriptEnv(t, nil, nil)
		if len(run.calls) != 1 || hasArg(run.calls[0], "--create") {
			t.Fatalf("calls %v", run.calls)
		}
	})

	t.Run("a failed create ends the script before the server", func(t *testing.T) {
		run := runScriptEnv(t, append([]string{"FAKE_CREATE_FAILS=1"}, newMap...), nil)
		var exit *exec.ExitError
		if !errors.As(run.err, &exit) || exit.ExitCode() != 1 {
			t.Errorf("script exit %v, want 1", run.err)
		}
		if len(run.calls) != 1 || run.calls[0][0] != "--create" {
			t.Errorf("calls %v, want only the create", run.calls)
		}
	})

	t.Run("values are data, never shell", func(t *testing.T) {
		hostile := "x; touch pwned #"
		env := append([]string{gameenv.Preset + "=" + hostile}, newMap...)
		run := runScriptEnv(t, env, nil)
		if got := flagValue(run.calls[0], "--preset"); got != hostile {
			t.Errorf("preset arrived as %q", got)
		}
		if _, err := os.Stat("pwned"); err == nil {
			t.Error("the preset ran as a command")
		}
	})
}
