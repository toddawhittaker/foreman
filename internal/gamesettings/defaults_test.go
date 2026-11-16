package gamesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func exampleServerSettings(t *testing.T, line string) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", line+"-server-settings.example.json")) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// SPEC 11.1: an unset field shows the game's own value, so the table must
// match what the game's example file holds on every supported line.
func TestGameDefaultsMatchExampleFiles(t *testing.T) {
	for _, line := range []string{Line11, Line20, Line21} {
		ex := exampleServerSettings(t, line)
		for key, want := range gameWholes {
			var got uint32
			if raw, ok := ex[key]; !ok || json.Unmarshal(raw, &got) != nil || got != want {
				t.Errorf("%s: %s is %s in the example, the table says %d", line, key, ex[key], want)
			}
		}
		for key, want := range gameFlags {
			raw, ok := ex[key]
			if key == "auto_pause_when_players_connect" && !HasPauseWhenConnect(line) {
				if ok {
					t.Errorf("%s: the example has %s, so the line must offer it", line, key)
				}
				continue
			}
			var got bool
			if !ok || json.Unmarshal(raw, &got) != nil || got != want {
				t.Errorf("%s: %s is %s in the example, the table says %v", line, key, raw, want)
			}
		}
		var cmds string
		if json.Unmarshal(ex["allow_commands"], &cmds) != nil || cmds != GameDefaultCommands {
			t.Errorf("%s: allow_commands is %s in the example, the table says %q", line, ex["allow_commands"], GameDefaultCommands)
		}
	}
}

// The raw editor must know auto_pause_when_players_connect on 2.0 and later
// and treat it as unknown on 1.1, where the game has no such key.
func TestPauseWhenConnectKnownFrom20(t *testing.T) {
	for line, unknown := range map[string]bool{Line11: true, Line20: false, Line21: false} {
		if got := UnknownKey(FileServerSettings, line, "auto_pause_when_players_connect"); got != unknown {
			t.Errorf("%s: UnknownKey = %v, want %v", line, got, unknown)
		}
	}
}

// SPEC 11.1: a file Foreman creates passes the checks of every line and
// carries the three keys the game needs, and 20 autosave slots.
func TestNewServerSettings(t *testing.T) {
	text := NewServerSettings(`My "odd" <world>`)
	for _, line := range []string{Line11, Line20, Line21} {
		if errs := Validate(FileServerSettings, line, text); len(errs) != 0 {
			t.Errorf("%s: %+v", line, errs)
		}
	}
	var m struct {
		Name  string `json:"name"`
		Slots int    `json:"autosave_slots"`
	}
	if err := json.Unmarshal(text, &m); err != nil || m.Name != `My "odd" <world>` || m.Slots != 20 {
		t.Errorf("created file = %+v, %v", m, err)
	}
	if v, ok := ReadVisibility(text); !ok || v != VisibilityHidden {
		t.Errorf("visibility = %q, %v; want hidden", v, ok)
	}
	for _, key := range []string{"description", "auto_pause"} {
		if !HasKey(text, key) {
			t.Errorf("created file lacks %s", key)
		}
	}
}

func TestWithAutosaveSlots(t *testing.T) {
	got := WithAutosaveSlots([]byte("{\n  \"name\": \"a\"\n}\n"))
	if !HasKey(got, "autosave_slots") || !HasKey(got, "name") {
		t.Errorf("slots not added: %s", got)
	}
	kept := []byte(`{"autosave_slots": 3}`)
	if string(WithAutosaveSlots(kept)) != string(kept) {
		t.Error("a file that sets slots was changed")
	}
	bad := []byte(`[1]`)
	if string(WithAutosaveSlots(bad)) != string(bad) {
		t.Error("input that is not an object was changed")
	}
}
