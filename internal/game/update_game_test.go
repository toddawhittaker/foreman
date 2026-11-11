//go:build game

package game

// These tests run a game version update and its rollback against the real
// Docker daemon and the real factoriotools/factorio images (SPEC 10.3, 10.4,
// 9.7). They are run by `make test-game`, never by `make verify`. The images
// are used by plain tag, as the rest of the real-game suite does, and none is
// downloaded: Foreman never downloads at start (SPEC 3.7).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/world"
)

const inGameLine = "changing state from(CreatingGame) to(InGame)"

// updateCases are the updates under test. The real-game tests run the
// latest patch of each line only, so there is no same-line update; the
// cross-line case starts from the real 2.0.77 save in the save reader's
// fixtures.
var updateCases = []struct {
	name, from, to string
	fixture        string   // a fixture file in the save reader's testdata
	tags           []string // the FOREMAN_TEST_GAME_TAG values that include this case
}{
	{"cross-line 2.0.77 to 2.1.24", "2.0.77", "2.1.24", "2.0.77-no-space-age.zip", []string{"2.0", "2.1"}},
}

func TestRealGameUpdate(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	total := time.Now()
	for _, tc := range updateCases {
		if only != "" && !slices.Contains(tc.tags, only) {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			e.requireImage(t, ImageRef(tc.from))
			e.requireImage(t, ImageRef(tc.to))
			e.runUpdate(t, tc.from, tc.to, tc.fixture)
			t.Logf("TIMING %s: %s", tc.name, time.Since(start).Round(time.Second))
		})
	}
	t.Logf("TIMING update total: %s", time.Since(total).Round(time.Second))
}

func (e *gameEnv) runUpdate(t *testing.T, from, to, fixture string) {
	ctx := context.Background()
	c, _ := e.controller(t)

	save, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	id := e.newWorld(t, "upd"+from, from, "main", "", nil)
	e.putSave(t, id, "main.zip", save, time.Now().Add(-time.Hour))

	step := func(name string, f func(t *testing.T)) {
		t.Helper()
		begin := time.Now()
		ok := t.Run(name, f)
		t.Logf("TIMING %s %s: %s", from, name, time.Since(begin).Round(100*time.Millisecond))
		if !ok {
			t.FailNow() // later steps depend on this one
		}
	}
	reachesInGame := func(t *testing.T, version string) {
		t.Helper()
		st := c.Status(id)
		if st.State != StateRunning || st.GameVersion != version {
			t.Fatalf("state %s (%s), game version %q, want running on %s", st.State, st.Reason, st.GameVersion, version)
		}
		if cont := e.inspect(t, id); !cont.State.Running || cont.Image != ImageRef(version) {
			t.Fatalf("container running %v on image %q, want %s", cont.State.Running, cont.Image, ImageRef(version))
		}
		if !logHas(c, id, inGameLine) {
			t.Fatalf("the game's log has no %q line for this run", inGameLine)
		}
		if _, reply, err := c.Console(ctx, id, admin, "/version", "/version"); err != nil || !strings.Contains(reply, version) {
			t.Fatalf("/version reply %q, err %v, want it to name %s", reply, err, version)
		}
	}
	var beforeUpdate world.Snapshot
	var beforeHash string

	step("1 the old version starts and reaches InGame", func(t *testing.T) {
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		reachesInGame(t, from)
		t.Logf("mod-list.json on %s: %s", from, e.readModList(t, id))
	})

	step("2 update reaches InGame on the new version", func(t *testing.T) {
		began := time.Now()
		if err := c.Update(ctx, id, to, admin, StopOptions{Countdown: 2 * time.Second}, StartOptions{}); err != nil {
			t.Fatalf("update: %v", err)
		}
		t.Logf("update took %s", time.Since(began).Round(100*time.Millisecond))
		reachesInGame(t, to)
		if v := e.metaVersion(t, id); v != to {
			t.Fatalf("world.json says %s, want %s", v, to)
		}
		beforeUpdate = e.onlySnapshot(t, id, world.ReasonGameUpdate)
		if beforeUpdate.Label != world.LabelBeforeUpdate {
			t.Fatalf("snapshot label %q, want %q", beforeUpdate.Label, world.LabelBeforeUpdate)
		}
		beforeHash = e.snapshotSaveHash(t, id, beforeUpdate, "main.zip")
		if want := e.manifestHash(t, beforeUpdate, "main.zip"); want != "" && want != beforeHash {
			t.Fatalf("the snapshot's save hashes to %s, its manifest says %s", beforeHash, want)
		}
		t.Logf("mod-list.json after the update to %s: %s", to, e.readModList(t, id))
		e.logModLines(t, c, id)
		// Built-in mods that stay on without Space Age, such as 2.1's recycler, are
		// loaded even when the old line's mod-list.json lacks them (SPEC 9.7).
		for _, m := range world.BuiltinMods(to) {
			if slices.Contains(world.SpaceAgeMods(to), m) {
				continue
			}
			if !logHas(c, id, "Loading mod "+m+" "+to) {
				t.Errorf("the game did not load the built-in mod %s", m)
			}
		}
	})

	step("3 rollback reaches InGame on the old version with the save as it was", func(t *testing.T) {
		began := time.Now()
		undo, err := c.Rollback(ctx, id, admin, StopOptions{Countdown: 2 * time.Second})
		if err != nil {
			t.Fatalf("rollback: %v", err)
		}
		t.Logf("rollback took %s", time.Since(began).Round(100*time.Millisecond))
		reachesInGame(t, from)
		if v := e.metaVersion(t, id); v != from {
			t.Fatalf("world.json says %s, want %s", v, from)
		}
		if got := e.liveSaveHash(t, id, "main.zip"); got != beforeHash {
			t.Fatalf("the live save hashes to %s after rollback, the Before update snapshot's save to %s", got, beforeHash)
		}
		// The new version saved the world when Rollback stopped it, so the undo
		// snapshot holds a different save: the comparison above proves something.
		if undoHash := e.snapshotSaveHash(t, id, undo, "main.zip"); undoHash == beforeHash {
			t.Fatal("the undo snapshot's save equals the pre-update save, so the new version never changed it")
		}
		if undo.Label != world.LabelBeforeRestore {
			t.Fatalf("undo snapshot label %q, want %q", undo.Label, world.LabelBeforeRestore)
		}
		// The snapshot is hard-linked to the live save; the game must not have changed it (SPEC 6.7).
		if got := e.snapshotSaveHash(t, id, beforeUpdate, "main.zip"); got != beforeHash {
			t.Fatalf("the Before update snapshot's save changed to %s from %s", got, beforeHash)
		}
	})

	step("4 stop", func(t *testing.T) {
		if err := c.Stop(ctx, id, admin, StopOptions{Countdown: 2 * time.Second}); err != nil {
			t.Fatalf("stop: %v", err)
		}
	})
}

func (e *gameEnv) metaVersion(t *testing.T, id string) string {
	t.Helper()
	root, err := e.worlds.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	return meta.GameVersion
}

func (e *gameEnv) readModList(t *testing.T, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.worldDir(id), world.ModsDir, world.ModListFile)) //nolint:gosec // the test's own folder
	if err != nil {
		return "(unreadable: " + err.Error() + ")"
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

// logModLines records what the new version's log says about mods, as
// evidence for how it copes with a mod-list.json from an older line (SPEC 9.7).
func (e *gameEnv) logModLines(t *testing.T, c *Controller, id string) {
	t.Helper()
	for _, l := range logLines(c, id) {
		if strings.Contains(l, " Error ") {
			t.Errorf("the game logged an error after the update: %s", l)
		}
		low := strings.ToLower(l)
		if strings.Contains(low, "recycler") || strings.Contains(low, "error") ||
			strings.Contains(low, "warning") || strings.Contains(low, "mod-list") {
			t.Logf("GAME LOG: %s", l)
		}
	}
}

func (e *gameEnv) onlySnapshot(t *testing.T, id, reason string) world.Snapshot {
	t.Helper()
	list, err := e.worlds.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	var found []world.Snapshot
	for _, s := range list {
		if s.Reason == reason {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d snapshots with reason %q, want 1", len(found), reason)
	}
	return found[0]
}

func (e *gameEnv) manifestHash(t *testing.T, s world.Snapshot, name string) string {
	t.Helper()
	for _, f := range s.Files {
		if f.Path == filepath.ToSlash(filepath.Join(world.SavesDir, name)) {
			return f.SHA256
		}
	}
	t.Fatalf("snapshot %s has no %s in its manifest", s.Name, name)
	return ""
}

func (e *gameEnv) snapshotSaveHash(t *testing.T, id string, s world.Snapshot, name string) string {
	t.Helper()
	return fileHash(t, filepath.Join(datadir.Snapshots(e.data), id, s.Name, world.SavesDir, name))
}

func (e *gameEnv) liveSaveHash(t *testing.T, id, name string) string {
	t.Helper()
	return fileHash(t, filepath.Join(e.worldDir(id), world.SavesDir, name))
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a path inside the test's own data folder
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
