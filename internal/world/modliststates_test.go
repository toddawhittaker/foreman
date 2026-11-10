package world

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

// ModListStates names each listed mod with whether the list enables it, and
// is nil when there is no list, so a caller can tell "unlisted" from "off".
func TestModListStates(t *testing.T) {
	write := func(t *testing.T, body string) *os.Root {
		t.Helper()
		dir := t.TempDir()
		if body != "" {
			if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ModsDir, ModListFile), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = root.Close() })
		return root
	}
	got, err := ModListStates(write(t, `{"mods":[{"name":"base","enabled":true},{"name":"space-age","enabled":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]bool{"base": true, "space-age": false}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, err := ModListStates(write(t, "")); err != nil || got != nil {
		t.Errorf("no list: got %v, %v; want nil, nil", got, err)
	}
	if _, err := ModListStates(write(t, `{"mods": 3}`)); err == nil {
		t.Error("a list of the wrong shape was read")
	}
}
