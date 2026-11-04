package world

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

// checkWorld makes a world with the given version and Space Age flag.
func checkWorld(t *testing.T, version string, spaceAge bool) (*Manager, string, string) {
	t.Helper()
	m, _ := newTestManager(t)
	id, err := m.Create(context.Background(), "Check", version, spaceAge)
	if err != nil {
		t.Fatal(err)
	}
	return m, id, filepath.Join(m.Dirs().Worlds, id)
}

// putFixture copies a save from the save reader's test data into the world.
func putFixture(t *testing.T, dir, fixture, name string, at time.Time) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, SavesDir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// putModList writes mods/mod-list.json enabling the named mods.
func putModList(t *testing.T, dir string, enabled ...string) {
	t.Helper()
	var parts []string
	for _, n := range enabled {
		parts = append(parts, `{"name":"`+n+`","enabled":true}`)
	}
	parts = append(parts, `{"name":"off-mod","enabled":false}`)
	writeFile(t, filepath.Join(dir, ModsDir, ModListFile), `{"mods":[`+strings.Join(parts, ",")+`]}`)
}

// putModListOff writes mods/mod-list.json enabling some mods and listing
// others as off.
func putModListOff(t *testing.T, dir string, enabled []string, off ...string) {
	t.Helper()
	var parts []string
	for _, n := range enabled {
		parts = append(parts, `{"name":"`+n+`","enabled":true}`)
	}
	for _, n := range off {
		parts = append(parts, `{"name":"`+n+`","enabled":false}`)
	}
	writeFile(t, filepath.Join(dir, ModsDir, ModListFile), `{"mods":[`+strings.Join(parts, ",")+`]}`)
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func kinds(p PreCheck) []ProblemKind {
	var out []ProblemKind
	for _, pr := range p.Problems {
		out = append(out, pr.Kind)
	}
	return out
}

func runCheck(t *testing.T, m *Manager, id string) PreCheck {
	t.Helper()
	p, err := m.PreStartCheck(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func wantKinds(t *testing.T, p PreCheck, want ...ProblemKind) {
	t.Helper()
	got := kinds(p)
	if strings.Join(toStrings(got), ",") != strings.Join(toStrings(want), ",") {
		t.Fatalf("problems %v, want %v (%+v)", got, want, p.Problems)
	}
}

func toStrings(k []ProblemKind) []string {
	var out []string
	for _, x := range k {
		out = append(out, string(x))
	}
	return out
}

func TestPreCheckMatchingWorldPasses(t *testing.T) {
	cases := []struct {
		version  string
		spaceAge bool
		fixture  string
		mods     []string
	}{
		{"1.1.110", false, "1.1.110-vanilla.zip", []string{"base"}},
		{"2.0.77", true, "2.0.77-space-age.zip", []string{"base", "elevated-rails", "quality", "space-age"}},
		{"2.0.77", false, "2.0.77-no-space-age.zip", []string{"base"}},
		{"2.1.24", false, "2.1.24-no-space-age.zip", []string{"base", "recycler"}},
		{"2.1.24", true, "2.1.24-space-age.zip", []string{"base", "elevated-rails", "quality", "recycler", "space-age"}},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			m, id, dir := checkWorld(t, c.version, c.spaceAge)
			putFixture(t, dir, c.fixture, "a.zip", t0)
			putModList(t, dir, c.mods...)
			p := runCheck(t, m, id)
			wantKinds(t, p)
			if !p.OK() || p.CanStartAnyway() || p.Save != "a.zip" {
				t.Fatalf("%+v", p)
			}
		})
	}
}

func TestPreCheckSpaceAgeOffButSaveUsesIt(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putFixture(t, dir, "2.0.77-space-age.zip", "a.zip", t0)
	putModList(t, dir, "base")
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemSpaceAgeOff)
	if !p.CanStartAnyway() || !strings.Contains(p.Problems[0].Text, "delete") {
		t.Fatalf("%+v", p)
	}
}

func TestPreCheckMissingOrDisabledMod(t *testing.T) {
	// 2.1 with Space Age and a test mod; the mod's zip is absent.
	m, id, dir := checkWorld(t, "2.1.24", true)
	putFixture(t, dir, "2.1.24-space-age-testmod.zip", "a.zip", t0)
	putModList(t, dir, "base", "elevated-rails", "quality", "recycler", "space-age", "foreman-test")
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemModMissing)
	if !p.CanStartAnyway() || !strings.Contains(p.Problems[0].Text, "foreman-test") {
		t.Fatalf("%+v", p)
	}

	// Present but listed as off is still a problem.
	writeFile(t, filepath.Join(dir, ModsDir, "foreman-test_0.1.0.zip"), "zip")
	putModListOff(t, dir, []string{"base", "elevated-rails", "quality", "recycler", "space-age"}, "foreman-test")
	wantKinds(t, runCheck(t, m, id), ProblemModDisabled)

	// Present and not in the list: the game loads it and turns it on.
	putModList(t, dir, "base", "elevated-rails", "quality", "recycler", "space-age")
	wantKinds(t, runCheck(t, m, id))

	// Present and enabled passes.
	putModList(t, dir, "base", "elevated-rails", "quality", "recycler", "space-age", "foreman-test")
	wantKinds(t, runCheck(t, m, id))
}

func TestPreCheckModFolderCounts(t *testing.T) {
	m, id, dir := checkWorld(t, "1.1.110", false)
	putFixture(t, dir, "1.1.110-testmod.zip", "a.zip", t0)
	putModList(t, dir, "base", "foreman-test")
	if err := os.Mkdir(filepath.Join(dir, ModsDir, "foreman-test"), 0o750); err != nil {
		t.Fatal(err)
	}
	wantKinds(t, runCheck(t, m, id))
}

func TestPreCheckRecyclerIsNotSpaceAge(t *testing.T) {
	// A 2.1 save without Space Age still lists recycler; that is fine.
	m, id, dir := checkWorld(t, "2.1.24", false)
	putFixture(t, dir, "2.1.24-no-space-age.zip", "a.zip", t0)
	putModList(t, dir, "base", "recycler")
	wantKinds(t, runCheck(t, m, id))
}

func TestPreCheckSaveFromNewerGame(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putFixture(t, dir, "2.1.24-no-space-age.zip", "a.zip", t0)
	putModList(t, dir, "base")
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemSaveNewer)
	if p.CanStartAnyway() {
		t.Fatal("a newer save must be a plain refusal")
	}
}

func TestPreCheckOlderPatchLoads(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.80", false)
	putFixture(t, dir, "2.0.77-no-space-age.zip", "a.zip", t0)
	putModList(t, dir, "base")
	wantKinds(t, runCheck(t, m, id))
}

func TestPreCheckNoSave(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModList(t, dir, "base")
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemNoSave)
	if p.CanStartAnyway() || p.Save != "" {
		t.Fatalf("%+v", p)
	}
}

func TestPreCheckChosenSaveGone(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putFixture(t, dir, "2.0.77-no-space-age.zip", "a.zip", t0)
	putModList(t, dir, "base")
	if err := m.SetLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, SavesDir, "a.zip")); err != nil {
		t.Fatal(err)
	}
	putFixture(t, dir, "2.0.77-no-space-age.zip", "b.zip", t0)
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemNoSave)
	if p.CanStartAnyway() {
		t.Fatal("must be a plain refusal")
	}
}

func TestPreCheckReadsTheSaveThatLoads(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putFixture(t, dir, "2.0.77-no-space-age.zip", "old.zip", t0)
	putFixture(t, dir, "2.0.77-space-age.zip", "new.zip", t0.Add(time.Hour))
	putModList(t, dir, "base")

	// Newest by default.
	p := runCheck(t, m, id)
	if p.Save != "new.zip" {
		t.Fatalf("save %q", p.Save)
	}
	wantKinds(t, p, ProblemSpaceAgeOff)

	// The chosen save when set.
	if err := m.SetLoadSave(id, "old.zip"); err != nil {
		t.Fatal(err)
	}
	p = runCheck(t, m, id)
	if p.Save != "old.zip" {
		t.Fatalf("save %q", p.Save)
	}
	wantKinds(t, p)
}

func TestPreCheckUnreadableSave(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putSave(t, dir, "a.zip", "not a zip", t0)
	putModList(t, dir, "base")
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemSaveUnreadable)
	if !p.CanStartAnyway() || !strings.Contains(p.Problems[0].Text, "could not be checked") {
		t.Fatalf("%+v", p)
	}
	if p.SaveVersion != (savefile.Version{}) {
		t.Errorf("an unreadable save has version %s", p.SaveVersion)
	}
}

// The controller compares the save's version with the game image's.
func TestPreCheckReportsSaveVersion(t *testing.T) {
	m, id, dir := checkWorld(t, "2.1.24", false)
	putFixture(t, dir, "2.1.24-no-space-age.zip", "a.zip", t0)
	putModList(t, dir, "base", "recycler")
	p := runCheck(t, m, id)
	if got := p.SaveVersion.String(); got != "2.1.24" {
		t.Errorf("save version %q, want 2.1.24", got)
	}
}

func TestPreCheckWorldVersionUnsupported(t *testing.T) {
	m, id, dir := checkWorld(t, "1.0.0", false)
	putFixture(t, dir, "1.1.110-vanilla.zip", "a.zip", t0)
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemGameVersion)
	if p.CanStartAnyway() {
		t.Fatal("must be a plain refusal")
	}
}

func TestPreCheckModListDisagreesWithSpaceAge(t *testing.T) {
	cases := []struct {
		name     string
		spaceAge bool
		mods     []string // nil: no mod-list.json
	}{
		{"flag off, list on", false, []string{"base", "elevated-rails", "quality", "space-age"}},
		{"flag off, no list", false, nil},
		{"flag on, list off", true, []string{"base"}},
		{"flag on, list partly off", true, []string{"base", "space-age"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, id, dir := checkWorld(t, "2.0.77", c.spaceAge)
			putFixture(t, dir, "2.0.77-no-space-age.zip", "a.zip", t0)
			if c.mods != nil {
				putModList(t, dir, c.mods...)
			}
			p := runCheck(t, m, id)
			if len(p.Problems) == 0 || p.Problems[0].Kind != ProblemModListSpaceAge {
				t.Fatalf("%+v", p)
			}
			if p.CanStartAnyway() {
				t.Fatal("must be a plain refusal")
			}
		})
	}
}

func TestPreCheckNoModListOn11IsFine(t *testing.T) {
	m, id, dir := checkWorld(t, "1.1.110", false)
	putFixture(t, dir, "1.1.110-vanilla.zip", "a.zip", t0)
	wantKinds(t, runCheck(t, m, id))
}

func TestPreCheckHostileModList(t *testing.T) {
	m, id, dir := checkWorld(t, "1.1.110", false)
	putFixture(t, dir, "1.1.110-vanilla.zip", "a.zip", t0)
	writeFile(t, filepath.Join(dir, ModsDir, ModListFile), `{"mods":[`+strings.Repeat(" ", maxModList)+`]}`)
	if _, err := m.PreStartCheck(id); err == nil {
		t.Fatal("an oversized mod-list.json was accepted")
	}
	writeFile(t, filepath.Join(dir, ModsDir, ModListFile), `{"mods":`)
	if _, err := m.PreStartCheck(id); err == nil {
		t.Fatal("a broken mod-list.json was accepted")
	}
	if err := os.Remove(filepath.Join(dir, ModsDir, ModListFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../world.json", filepath.Join(dir, ModsDir, ModListFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PreStartCheck(id); err == nil {
		t.Fatal("a symlinked mod-list.json was followed")
	}
}

func TestBuiltinMods(t *testing.T) {
	cases := map[string]string{
		"1.1.110": "base",
		"2.0.77":  "base,elevated-rails,quality,space-age",
		"2.1.24":  "base,recycler,elevated-rails,quality,space-age",
		"1.0.0":   "",
		"3.0.0":   "",
	}
	for v, want := range cases {
		if got := strings.Join(BuiltinMods(v), ","); got != want {
			t.Errorf("%s: %q, want %q", v, got, want)
		}
	}
	if got := strings.Join(SpaceAgeMods("2.1.24"), ","); got != "elevated-rails,quality,space-age" {
		t.Errorf("2.1 Space Age mods %q", got)
	}
}
