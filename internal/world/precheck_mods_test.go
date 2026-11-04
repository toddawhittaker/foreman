package world

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

// putModZip writes mods/<file> holding <folder>/info.json with the content.
func putModZip(t *testing.T, dir, file, folder, info string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(folder + "/info.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(info)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ModsDir, file), buf.String())
}

// putModFor writes a mod zip declaring factorio_version for the mod name.
func putModFor(t *testing.T, dir, name, version, factorio string) {
	t.Helper()
	putModZip(t, dir, name+"_"+version+".zip", name+"_"+version,
		fmt.Sprintf(`{"name":%q,"version":%q,"factorio_version":%q}`, name, version, factorio))
}

// modCheckWorld makes a world whose mods folder holds one enabled mod zip
// for each name, declaring the given Factorio version.
func modCheckWorld(t *testing.T, version string, mods map[string]string) (*Manager, string, string) {
	t.Helper()
	m, id, dir := checkWorld(t, version, false)
	putFixture(t, dir, "2.0.77-no-space-age.zip", "a.zip", t0)
	enabled := []string{"base"}
	for name, factorio := range mods {
		putModFor(t, dir, name, "1.0.0", factorio)
		enabled = append(enabled, name)
	}
	putModList(t, dir, enabled...)
	return m, id, dir
}

func checkWith(t *testing.T, m *Manager, id string, run GameRun) PreCheck {
	t.Helper()
	p, err := m.PreStartCheck(id, run)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func modVersionProblems(p PreCheck) []Problem {
	var out []Problem
	for _, pr := range p.Problems {
		if pr.Kind == ProblemModGameVersion {
			out = append(out, pr)
		}
	}
	return out
}

func TestPreCheckModsForAnotherFactorioInAdoptedMode(t *testing.T) {
	mods := map[string]string{}
	for i := range 7 {
		mods[fmt.Sprintf("mod-%d", i)] = "2.0"
	}
	// The container's version decides, not the world's recorded one.
	m, id, _ := modCheckWorld(t, "2.0.77", mods)
	p := checkWith(t, m, id, GameRun{Adopted: true, Version: "2.1.24"})
	got := modVersionProblems(p)
	if len(got) != 1 || got[0].StartAnyway || p.CanStartAnyway() {
		t.Fatalf("%+v", p.Problems)
	}
	want := "Check cannot run on Factorio 2.1.24: 7 of its mods were made for Factorio 2.0, and the game refuses to load them: " +
		"mod-0, mod-1, mod-2, mod-3, mod-4 and 2 more. Your game container runs Factorio 2.1.24. " +
		"Updating the mods for 2.1 will come with mod management; until then this world cannot run in this container."
	if got[0].Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got[0].Text, want)
	}
}

func TestPreCheckModsForAnotherFactorioInManagedMode(t *testing.T) {
	m, id, _ := modCheckWorld(t, "2.1.24", map[string]string{"old-mod": "2.0"})
	p := checkWith(t, m, id, GameRun{})
	got := modVersionProblems(p)
	if len(got) != 1 {
		t.Fatalf("%+v", p.Problems)
	}
	want := "Check cannot run on Factorio 2.1.24: 1 of its mods was made for Factorio 2.0, and the game refuses to load it: old-mod. " +
		"Set the world's game version to 2.0.x, or update its mods."
	if got[0].Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got[0].Text, want)
	}
}

func TestPreCheckModsThatDisagreeAreGroupedLargestFirst(t *testing.T) {
	m, id, _ := modCheckWorld(t, "2.0.77", map[string]string{"a": "1.1", "b": "2.1", "c": "2.1", "d": "2.0"})
	p := checkWith(t, m, id, GameRun{})
	got := modVersionProblems(p)
	if len(got) != 1 {
		t.Fatalf("%+v", p.Problems)
	}
	want := "Check cannot run on Factorio 2.0.77: 2 of its mods were made for Factorio 2.1, and the game refuses to load them: b, c. " +
		"1 of its mods was made for Factorio 1.1, and the game refuses to load it: a. " +
		"Set the world's game version to 2.1.x, or update its mods."
	if got[0].Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got[0].Text, want)
	}
}

func TestPreCheckModsMatchingTheGameAreFine(t *testing.T) {
	m, id, _ := modCheckWorld(t, "2.0.77", map[string]string{"a": "2.0", "b": "2.0"})
	if got := modVersionProblems(checkWith(t, m, id, GameRun{})); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	// Adopted: the container's minor version is what counts.
	if got := modVersionProblems(checkWith(t, m, id, GameRun{Adopted: true, Version: "2.0.9"})); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPreCheckSkipsModVersionsWhenAdoptedVersionUnknown(t *testing.T) {
	m, id, _ := modCheckWorld(t, "2.0.77", map[string]string{"a": "1.1"})
	if got := modVersionProblems(checkWith(t, m, id, GameRun{Adopted: true})); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPreCheckModVersionIgnoresWhatItCannotRead(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.1.24", nil)
	// Not a zip, no info.json, no factorio_version, bad JSON, a mod with no
	// zip, and a mod that is turned off.
	writeFile(t, filepath.Join(dir, ModsDir, "junk_1.0.0.zip"), "not a zip")
	putModZip(t, dir, "noinfo_1.0.0.zip", "noinfo_1.0.0", "")
	putModZip(t, dir, "nover_1.0.0.zip", "nover_1.0.0", `{"name":"nover"}`)
	putModZip(t, dir, "badjson_1.0.0.zip", "badjson_1.0.0", `{"factorio_version":`)
	putModFor(t, dir, "disabled", "1.0.0", "1.1")
	putModListOff(t, dir, []string{"base", "junk", "noinfo", "nover", "badjson", "ghost-without-a-zip"}, "disabled")
	if got := modVersionProblems(checkWith(t, m, id, GameRun{})); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPreCheckModVersionReadsOnlyTheFirstBytesOfInfo(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.1.24", nil)
	// The version sits past the cap, so it is not seen.
	big := `{"description":"` + strings.Repeat("x", maxInfoJSON) + `","factorio_version":"1.1"}`
	putModZip(t, dir, "big_1.0.0.zip", "big_1.0.0", big)
	putModList(t, dir, "base", "big")
	if got := modVersionProblems(checkWith(t, m, id, GameRun{})); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPreCheckModVersionUsesTheNewestZip(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.0.77", nil)
	putModFor(t, dir, "multi", "1.9.0", "1.1")
	putModFor(t, dir, "multi", "1.10.0", "2.0")
	putModList(t, dir, "base", "multi")
	if got := modVersionProblems(checkWith(t, m, id, GameRun{})); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPreCheckBuiltinModsAreNotRead(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.0.77", nil)
	putModFor(t, dir, "space-age", "1.0.0", "1.1")
	putModList(t, dir, "base", "space-age")
	if got := modVersionProblems(checkWith(t, m, id, GameRun{})); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// In adopted mode the container decides the version, so the world's recorded
// one does not gate the save (SPEC 4.9); the controller checks the container.
func TestPreCheckAdoptedIgnoresWorldVersionForSave(t *testing.T) {
	m, id, dir := checkWorld(t, "2.1.9", false)
	putFixture(t, dir, "2.1.24-no-space-age.zip", "a.zip", t0)
	putModList(t, dir, "base", "recycler")
	wantKinds(t, checkWith(t, m, id, GameRun{Adopted: true, Version: "2.1.24"}))
	wantKinds(t, checkWith(t, m, id, GameRun{Adopted: true}))
	wantKinds(t, checkWith(t, m, id, GameRun{}), ProblemSaveNewer)
}

func TestPreCheckMissingModText(t *testing.T) {
	m, id, dir := checkWorld(t, "2.1.24", true)
	putFixture(t, dir, "2.1.24-space-age-testmod.zip", "a.zip", t0)
	putModList(t, dir, "base", "elevated-rails", "quality", "recycler", "space-age")
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemModMissing)
	want := "This world does not have the file of the mod the save a.zip was last played with: foreman-test. " +
		"Switching or starting anyway removes everything it built from the save."
	if p.Problems[0].Text != want || len(p.Problems[0].Mods) != 1 {
		t.Fatalf("%+v", p.Problems[0])
	}
}

func TestPreCheckDisabledModText(t *testing.T) {
	m, id, dir := checkWorld(t, "2.1.24", true)
	putFixture(t, dir, "2.1.24-space-age-testmod.zip", "a.zip", t0)
	writeFile(t, filepath.Join(dir, ModsDir, "foreman-test_0.1.0.zip"), "zip")
	putModListOff(t, dir, []string{"base", "elevated-rails", "quality", "recycler", "space-age"}, "foreman-test")
	p := runCheck(t, m, id)
	wantKinds(t, p, ProblemModDisabled)
	want := "The save a.zip was last played with foreman-test, which is turned off in this world. " +
		"Switching or starting anyway removes everything it built from the save."
	if p.Problems[0].Text != want || !p.CanStartAnyway() {
		t.Fatalf("%+v", p.Problems[0])
	}
}

func TestCompareSaveManyMissingAndDisabledModsUsePlurals(t *testing.T) {
	save := savefile.Save{Version: savefile.Version{Major: 2, Minor: 0, Patch: 77},
		Mods: []savefile.Mod{{Name: "base"}, {Name: "gone-a"}, {Name: "gone-b"}, {Name: "off-a"}, {Name: "off-b"}}}
	meta := Meta{GameVersion: "2.0.77"}
	mods := modList{found: true, enabled: map[string]bool{"base": true}, disabled: map[string]bool{"off-a": true, "off-b": true}}
	present := map[string]bool{"off-a": true, "off-b": true}
	got := compareSave(meta, mods, present, "a.zip", save, false)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	wantMissing := "This world does not have the files of the mods the save a.zip was last played with: gone-a, gone-b. " +
		"Switching or starting anyway removes everything they built from the save."
	wantOff := "The save a.zip was last played with off-a, off-b, which are turned off in this world. " +
		"Switching or starting anyway removes everything they built from the save."
	if got[0].Text != wantMissing || got[1].Text != wantOff {
		t.Fatalf("%q\n%q", got[0].Text, got[1].Text)
	}
}
