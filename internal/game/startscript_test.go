package game

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// runScript runs startScript against a fake world folder, with the game
// replaced by printf, and returns the arguments the game would get.
func runScript(t *testing.T, files ...string) []string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, world.ConfigDir)
	if err := os.MkdirAll(cfg, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range append(files, "rconpw") {
		if err := os.WriteFile(filepath.Join(cfg, f), []byte("pw-from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The binary's path contains the mount's, so it goes first.
	script := strings.ReplaceAll(startScript, "exec "+gameBinary, "exec /usr/bin/printf '%s\\n'")
	script = strings.ReplaceAll(script, gameMount, dir)
	out, err := exec.Command("/bin/sh", "-c", script).Output() //nolint:gosec // a fixed script in a test
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
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
