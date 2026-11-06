package world

import (
	"context"
	"strings"
	"testing"
)

// SPEC 4.3: the start that holds the world's lock clears new_map itself.
func TestClearNewMapHeldWorksUnderTheLock(t *testing.T) {
	m, _ := newTestManager(t)
	id, err := m.Generate(context.Background(), NewWorld{Name: "Held", GameVersion: "2.0.77", Seed: seedPtr(5)})
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := m.ClearNewMapHeld(id); err != nil {
		t.Fatal(err)
	}
	if readWorldMetaFile(t, m, id).NewMap != nil {
		t.Error("new_map is still set")
	}
}

// SPEC 7.4, 9.7: a preview's mod list turns Space Age on or off as a world's
// does.
func TestDefaultModListFollowsSpaceAge(t *testing.T) {
	on, off := string(DefaultModList("2.0.77", true)), string(DefaultModList("2.0.77", false))
	if on == off {
		t.Fatal("the Space Age flag changes nothing")
	}
	if !strings.Contains(on, "\"space-age\",\n      \"enabled\": true") || !strings.Contains(off, "\"space-age\",\n      \"enabled\": false") {
		t.Errorf("on:\n%s\noff:\n%s", on, off)
	}
}
