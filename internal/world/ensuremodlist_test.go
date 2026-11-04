package world

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The game's own mod-list.json for a new world, as captured from each
// image: the Space Age flag is on, so every built-in mod is enabled.
func gameModList(t *testing.T, version string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "game", "testdata", version, "mod-list.json")) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	return parseModList(t, b)
}

func parseModList(t *testing.T, b []byte) map[string]bool {
	t.Helper()
	var doc struct {
		Mods []modListEntry `json:"mods"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("not the game's shape: %v\n%s", err, b)
	}
	out := map[string]bool{}
	for i, e := range doc.Mods {
		if i == 0 && e.Name != "base" {
			t.Fatalf("base must come first: %s", b)
		}
		if _, dup := out[e.Name]; dup {
			t.Fatalf("%s listed twice", e.Name)
		}
		out[e.Name] = e.Enabled
	}
	return out
}

func TestEnsureModListMatchesTheGame(t *testing.T) {
	cases := []struct {
		fixtureDir string
		version    string
		spaceAge   bool
		want       map[string]bool
	}{
		{"1.1.110", "1.1.110", false, map[string]bool{"base": true}},
		{"2.0.77", "2.0.77", true, nil}, // nil: equal to the game's file
		{"2.0.77", "2.0.77", false, map[string]bool{"base": true, "elevated-rails": false, "quality": false, "space-age": false}},
		{"2.1.24", "2.1.24", true, nil},
		{"2.1.24", "2.1.24", false, map[string]bool{"base": true, "elevated-rails": false, "quality": false, "recycler": true, "space-age": false}},
	}
	for _, c := range cases {
		name := c.version
		if c.spaceAge {
			name += "-space-age"
		}
		t.Run(name, func(t *testing.T) {
			m, id, dir := checkWorld(t, c.version, c.spaceAge)
			if err := m.EnsureModList(id); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, ModsDir, ModListFile)
			b, err := os.ReadFile(p) //nolint:gosec // a test temp folder
			if err != nil {
				t.Fatal(err)
			}
			got := parseModList(t, b)
			want := c.want
			if want == nil {
				want = gameModList(t, c.fixtureDir)
			}
			if len(got) != len(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			for k, v := range want {
				if on, ok := got[k]; !ok || on != v {
					t.Fatalf("got %v, want %v", got, want)
				}
			}
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != FileMode {
				t.Fatalf("mode %v, want %v", info.Mode().Perm(), FileMode)
			}
		})
	}
}

func TestEnsureModListLeavesAnExistingFile(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	p := filepath.Join(dir, ModsDir, ModListFile)
	const mine = `{"mods":[{"name":"base","enabled":true},{"name":"space-age","enabled":true}]}`
	writeFile(t, p, mine)
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != mine {
		t.Fatalf("file changed: %s", b)
	}
}

func TestEnsureModListCreatesTheModsFolder(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if err := os.Remove(filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ModsDir, ModListFile)); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureModListRefusesASymlink(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	p := filepath.Join(dir, ModsDir, ModListFile)
	// A dangling link would otherwise be written through.
	if err := os.Symlink(outside, p); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureModList(id); err == nil {
		t.Fatal("a symlink at the mod list path must be refused")
	}
	if _, err := os.Lstat(outside); err == nil {
		t.Fatal("wrote through the link")
	}
	if info, err := os.Lstat(p); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v", err)
	}
}

func TestEnsureModListRefusesAnUnsupportedVersion(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	metaPath := filepath.Join(dir, MetaFile)
	b, err := os.ReadFile(metaPath) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(b), "2.0.77", "3.0.1", 1)
	if err := os.WriteFile(metaPath, []byte(changed), 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := m.EnsureModList(id); err == nil {
		t.Fatal("want an error")
	} else if !strings.Contains(err.Error(), "game version") {
		t.Fatalf("wrong error: %v", err)
	}
}

// A new world with Space Age off used to be refused for its missing list.
func TestPreStartCheckPassesAfterEnsureModList(t *testing.T) {
	cases := []struct{ version, fixture string }{
		{"2.0.77", "2.0.77-no-space-age.zip"},
		{"2.1.24", "2.1.24-no-space-age.zip"},
	}
	for _, c := range cases {
		t.Run(c.version, func(t *testing.T) {
			m, id, dir := checkWorld(t, c.version, false)
			putFixture(t, dir, c.fixture, "a.zip", t0)
			if p := runCheck(t, m, id); len(p.Problems) == 0 || p.Problems[0].Kind != ProblemModListSpaceAge {
				t.Fatalf("want the problem before the fix: %+v", p.Problems)
			}
			if err := m.EnsureModList(id); err != nil {
				t.Fatal(err)
			}
			wantKinds(t, runCheck(t, m, id))
		})
	}
}

func TestPreStartCheckPassesAfterEnsureModListSpaceAgeOn(t *testing.T) {
	m, id, dir := checkWorld(t, "2.1.24", true)
	putFixture(t, dir, "2.1.24-space-age.zip", "a.zip", t0)
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	wantKinds(t, runCheck(t, m, id))
}
