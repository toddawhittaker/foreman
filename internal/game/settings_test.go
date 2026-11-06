package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	oldSettings = `{"name": "Old name", "description": "", "visibility": {"public": false, "lan": false}, "game_password": "old-secret"}`
	newSettings = `{"name": "New name", "description": "", "visibility": {"public": false, "lan": false}, "game_password": "new-secret"}`
)

// seen reads the file's hash as the settings page does when it shows the diff.
func (r *rig) seen(id, file string) string {
	r.t.Helper()
	read, err := r.worlds.ReadConfig(id, file)
	if err != nil {
		r.t.Fatal(err)
	}
	return read.Hash
}

func (r *rig) saveSettings(id, file, data string) (SettingsResult, error) {
	r.t.Helper()
	return r.c.SaveSettings(context.Background(), id, admin, []SettingsChange{{
		File: file, Data: []byte(data), SeenHash: r.seen(id, file),
	}})
}

func (r *rig) config(id, file string) string {
	b, err := os.ReadFile(filepath.Join(r.worldDir(id), world.ConfigDir, file)) //nolint:gosec // a test's own temp folder
	if err != nil {
		return ""
	}
	return string(b)
}

func (r *rig) settingsSnapshots(id string) []world.Snapshot {
	var out []world.Snapshot
	for _, s := range r.snapshots(id) {
		if s.Reason == world.ReasonSettings {
			out = append(out, s)
		}
	}
	return out
}

// settingsWorld is a world whose server settings hold oldSettings.
func (r *rig) settingsWorld(name string) string {
	id := r.newWorld(name)
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerSettingsFile), []byte(oldSettings))
	return id
}

// SPEC 11.3, 6.2: a settings save on a stopped world takes the automatic
// snapshot first, writes the file, and records the save without any value.
func TestSaveSettingsOnAStoppedWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.settingsWorld("Stopped")
	res, err := r.saveSettings(id, world.ServerSettingsFile, newSettings)
	if err != nil {
		t.Fatal(err)
	}
	if res.RestartNeeded {
		t.Error("a stopped world needs no restart")
	}
	if got := r.config(id, world.ServerSettingsFile); got != newSettings {
		t.Errorf("file is %q", got)
	}
	snaps := r.settingsSnapshots(id)
	if len(snaps) != 1 || snaps[0].Label != world.LabelBeforeSettings || snaps[0].TakenBy != admin.Username {
		t.Fatalf("snapshots %+v", snaps)
	}
	// The snapshot holds the file as it was.
	old, err := os.ReadFile(filepath.Join(r.worlds.Dirs().Snapshots, id, snaps[0].Name, world.ConfigDir, world.ServerSettingsFile)) //nolint:gosec // a test's own temp folder
	if err != nil || string(old) != oldSettings {
		t.Errorf("snapshot holds %q, %v", old, err)
	}
	entries := r.audit.list()
	if len(entries) != 1 || entries[0].Action != AuditSettingsSave || entries[0].WorldID != id || entries[0].Detail != world.ServerSettingsFile {
		t.Fatalf("audit %+v", entries)
	}
	for _, e := range entries {
		if strings.Contains(e.Detail, "secret") || strings.Contains(e.Action, "secret") {
			t.Errorf("the audit entry holds a value: %+v", e)
		}
	}
	st, _ := os.Stat(filepath.Join(r.worldDir(id), world.ConfigDir, world.ServerSettingsFile))
	if st.Mode().Perm() != 0o640 {
		t.Errorf("file mode %v, want 0640 (SPEC 14.8)", st.Mode().Perm())
	}
}

// SPEC 11.3, 6.10: on a running world the game is saved first, the snapshot
// is taken, and the page is told a restart is needed.
func TestSaveSettingsOnARunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.settingsWorld("Running")
	r.start(id)
	res, err := r.saveSettings(id, world.ServerSettingsFile, newSettings)
	if err != nil {
		t.Fatal(err)
	}
	if !res.RestartNeeded {
		t.Error("a running world needs a restart for server settings")
	}
	if !slices.Contains(r.commands(id), "/server-save") {
		t.Errorf("the game was not saved first: %v", r.commands(id))
	}
	if len(r.settingsSnapshots(id)) != 1 || r.config(id, world.ServerSettingsFile) != newSettings {
		t.Error("no snapshot, or the file was not written")
	}
}

// SPEC 11.3: the steps run in order, and a failure stops the later ones. Each
// case makes one step fail and checks that nothing after it was done.
func TestSaveSettingsOrder(t *testing.T) {
	type outcome struct {
		snapshot, written, audited bool
	}
	cases := []struct {
		name    string
		running bool
		setup   func(r *rig, id string)
		want    error
		done    outcome
	}{
		{"the world is busy", false, func(r *rig, id string) {
			unlock, err := r.worlds.Lock(id)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(unlock)
		}, world.ErrBusy, outcome{}},
		{"the game cannot be saved", true, func(r *rig, id string) {
			r.game.SetSaveHangs(ContainerName(id), true)
		}, ErrSaveTimeout, outcome{}},
		{"the snapshot fails", false, func(r *rig, id string) {
			// A snapshot refuses a world that holds a link (SPEC 6.8).
			if err := os.Symlink("/etc/hostname", filepath.Join(r.worldDir(id), "link")); err != nil {
				t.Fatal(err)
			}
		}, nil, outcome{}},
		{"the file changed since the preview", false, func(r *rig, id string) {
			r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerSettingsFile), []byte(`{"name": "Someone else"}`))
		}, world.ErrSettingsChanged, outcome{}},
		{"the audit log fails", false, func(r *rig, id string) {
			r.audit.setFail(true)
		}, nil, outcome{snapshot: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			id := r.settingsWorld("Order")
			if c.running {
				r.start(id)
			}
			// The hash the page saw is taken before the case changes anything.
			change := SettingsChange{File: world.ServerSettingsFile, Data: []byte(newSettings), SeenHash: r.seen(id, world.ServerSettingsFile)}
			c.setup(r, id)
			_, err := r.c.SaveSettings(context.Background(), id, admin, []SettingsChange{change})
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("error %v, want %v", err, c.want)
			}
			got := outcome{
				snapshot: len(r.settingsSnapshots(id)) > 0,
				written:  r.config(id, world.ServerSettingsFile) == newSettings,
				audited:  slices.Contains(r.actions(), AuditSettingsSave),
			}
			if got != c.done {
				t.Errorf("done %+v, want %+v", got, c.done)
			}
		})
	}
}

// SPEC 11.3: a file that changed since the preview is not overwritten, and
// the typed error lets the page show the new diff.
func TestSaveSettingsRefusesAChangedFile(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.settingsWorld("Changed")
	stale := r.seen(id, world.ServerSettingsFile)
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerSettingsFile), []byte(`{"name": "Edited elsewhere"}`))
	_, err := r.c.SaveSettings(context.Background(), id, admin, []SettingsChange{{
		File: world.ServerSettingsFile, Data: []byte(newSettings), SeenHash: stale,
	}})
	if !errors.Is(err, world.ErrSettingsChanged) {
		t.Fatalf("error %v, want world.ErrSettingsChanged", err)
	}
	if got := r.config(id, world.ServerSettingsFile); got != `{"name": "Edited elsewhere"}` {
		t.Errorf("the other edit was overwritten: %q", got)
	}
}

// SPEC 11.3: a file that did not exist at the preview and appeared since is
// a change too.
func TestSaveSettingsRefusesAFileThatAppeared(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Appeared")
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerSettingsFile), []byte(oldSettings))
	_, err := r.c.SaveSettings(context.Background(), id, admin, []SettingsChange{{
		File: world.ServerSettingsFile, Data: []byte(newSettings), SeenHash: "",
	}})
	if !errors.Is(err, world.ErrSettingsChanged) {
		t.Fatalf("error %v, want world.ErrSettingsChanged", err)
	}
}

// SPEC 11.2: input that fails the checks is refused before anything is done:
// no snapshot, no write, no audit entry.
func TestSaveSettingsRefusesInvalidInput(t *testing.T) {
	cases := []struct {
		name, file, data string
		want             error
	}{
		{"not JSON", world.ServerSettingsFile, `{"name": `, ErrSettingsInvalid},
		{"a wrong type", world.ServerSettingsFile, `{"max_players": "many"}`, ErrSettingsInvalid},
		{"an admin name with a line break", world.ServerAdminListFile, "[\"a\\nb\"]", ErrSettingsInvalid},
		{"a file that is not editable here", world.ServerBanListFile, `[]`, world.ErrConfigFile},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			id := r.settingsWorld("Invalid")
			_, err := r.saveSettings(id, c.file, c.data)
			if !errors.Is(err, c.want) {
				t.Fatalf("error %v, want %v", err, c.want)
			}
			if len(r.settingsSnapshots(id)) != 0 || len(r.audit.list()) != 0 || r.config(id, world.ServerSettingsFile) != oldSettings {
				t.Error("something was done")
			}
		})
	}
}

// SPEC 11.1: a stopped world's admin list is written as a file.
func TestSaveAdminListOnAStoppedWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Admins stopped")
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerAdminListFile), []byte(`["alice"]`))
	res, err := r.saveSettings(id, world.ServerAdminListFile, `["alice", "bob"]`)
	if err != nil {
		t.Fatal(err)
	}
	if res.RestartNeeded {
		t.Error("an admin list never needs a restart")
	}
	if got := r.config(id, world.ServerAdminListFile); got != `["alice", "bob"]` {
		t.Errorf("file is %q", got)
	}
}

// SPEC 11.1, 14.9: a running world's admin list changes through the game's
// own commands, which write the file; Foreman does not write it. Demotions go
// first, a name present in both lists is left alone, and a name is sent as it
// is: the console has no quoting, so a quote is part of the name.
func TestSaveAdminListOnARunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Admins running")
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerAdminListFile), []byte(`["alice", "old_admin"]`))
	r.start(id)
	before := r.config(id, world.ServerAdminListFile)

	res, err := r.saveSettings(id, world.ServerAdminListFile, `["ALICE", "o\"rly;"]`)
	if err != nil {
		t.Fatal(err)
	}
	if res.RestartNeeded {
		t.Error("an admin list never needs a restart")
	}
	var sent []string
	for _, c := range r.commands(id) {
		if strings.HasPrefix(c, "/promote") || strings.HasPrefix(c, "/demote") {
			sent = append(sent, c)
		}
	}
	want := []string{`/demote old_admin`, `/promote o"rly;`}
	if !slices.Equal(sent, want) {
		t.Errorf("commands %q, want %q", sent, want)
	}
	if r.config(id, world.ServerAdminListFile) != before {
		t.Error("Foreman wrote the admin list of a running game")
	}
	if len(r.settingsSnapshots(id)) != 1 {
		t.Error("no snapshot was taken")
	}
}

// SPEC 14.9: the game's console reads one word as the player's name, so
// "/promote bob smith" would promote bob. A name with a space, of any kind, is
// refused whether the game runs or not; nothing is sent and the file is not
// changed.
func TestAdminNamesWithSpacesAreRefused(t *testing.T) {
	lists := []string{`["alice", "bob smith"]`, `["alice", "bob smith"]`, `["alice", "bob　smith"]`}
	for _, running := range []bool{true, false} {
		for _, list := range lists {
			r := newRig(t, rigOptions{})
			id := r.newWorld("Admins spaces")
			r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerAdminListFile), []byte(`["alice"]`))
			if running {
				r.start(id)
			}
			if _, err := r.saveSettings(id, world.ServerAdminListFile, list); !errors.Is(err, ErrSettingsInvalid) {
				t.Fatalf("running %v, %s: error %v, want ErrSettingsInvalid", running, list, err)
			}
			for _, c := range r.commands(id) {
				if strings.HasPrefix(c, "/promote") || strings.HasPrefix(c, "/demote") {
					t.Errorf("sent %q", c)
				}
			}
			if r.config(id, world.ServerAdminListFile) != `["alice"]` || len(r.settingsSnapshots(id)) != 0 {
				t.Error("something was done")
			}
		}
	}
}

// SPEC 11.3: one confirmation of two files saves a running game once, takes
// one snapshot, records one audit entry naming both files, writes the
// settings file and sends the admin change over RCON. A second save takes its
// own snapshot, so a Done link names the right one.
func TestSaveSettingsOfSeveralFilesIsOneSave(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.settingsWorld("Both")
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerAdminListFile), []byte(`["alice"]`))
	r.start(id)
	two := func(name, admins string) SettingsResult {
		res, err := r.c.SaveSettings(context.Background(), id, admin, []SettingsChange{
			{File: world.ServerSettingsFile, Data: []byte(name), SeenHash: r.seen(id, world.ServerSettingsFile)},
			{File: world.ServerAdminListFile, Data: []byte(admins), SeenHash: r.seen(id, world.ServerAdminListFile)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	first := two(`{"name": "One", "description": "", "visibility": {"public": false, "lan": false}}`, `["alice", "bob"]`)
	if !first.RestartNeeded || first.Snapshot.Name == "" {
		t.Errorf("result %+v", first)
	}
	if n := len(r.settingsSnapshots(id)); n != 1 {
		t.Errorf("%d settings snapshots, want 1", n)
	}
	saves := 0
	for _, c := range r.commands(id) {
		if c == "/server-save" {
			saves++
		}
	}
	if saves != 1 {
		t.Errorf("the game was saved %d times, want 1", saves)
	}
	var entries []store.AuditEntry
	for _, e := range r.audit.list() {
		if e.Action == AuditSettingsSave {
			entries = append(entries, e)
		}
	}
	want := world.ServerSettingsFile + ", " + world.ServerAdminListFile
	if len(entries) != 1 || entries[0].Detail != want {
		t.Errorf("audit %+v", entries)
	}
	if got := r.config(id, world.ServerSettingsFile); got != `{"name": "One", "description": "", "visibility": {"public": false, "lan": false}}` {
		t.Errorf("settings file %q", got)
	}
	if !slices.Contains(r.commands(id), "/promote bob") {
		t.Errorf("commands %v", r.commands(id))
	}

	second := two(`{"name": "Two", "description": "", "visibility": {"public": false, "lan": false}}`, `["alice", "bob", "carol"]`)
	if second.Snapshot.Name == "" || second.Snapshot.Name == first.Snapshot.Name {
		t.Errorf("snapshots %q and %q", first.Snapshot.Name, second.Snapshot.Name)
	}
}

// SPEC 11.3: if one file is out of date, none of a many-file change is
// written and no snapshot is taken.
func TestSaveSettingsOfSeveralFilesIsAllOrNothing(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.settingsWorld("Moved")
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerAdminListFile), []byte(`["alice"]`))
	_, err := r.c.SaveSettings(context.Background(), id, admin, []SettingsChange{
		{File: world.ServerSettingsFile, Data: []byte(newSettings), SeenHash: r.seen(id, world.ServerSettingsFile)},
		{File: world.ServerAdminListFile, Data: []byte(`["bob"]`), SeenHash: "stale"},
	})
	if !errors.Is(err, world.ErrSettingsChanged) {
		t.Fatalf("error %v", err)
	}
	if r.config(id, world.ServerSettingsFile) != oldSettings || len(r.settingsSnapshots(id)) != 0 || len(r.audit.list()) != 0 {
		t.Error("something was done")
	}
}

// SPEC 11.1: with no change in names, a running world is sent nothing.
func TestSaveAdminListWithNoChangeSendsNothing(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Admins same")
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerAdminListFile), []byte(`["alice"]`))
	r.start(id)
	if _, err := r.saveSettings(id, world.ServerAdminListFile, `["Alice"]`); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.commands(id) {
		if strings.HasPrefix(c, "/promote") || strings.HasPrefix(c, "/demote") {
			t.Errorf("sent %q", c)
		}
	}
}
