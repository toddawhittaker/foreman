package game

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// These tests pin what an update and a rollback must hold (SPEC 10.3 to
// 10.5, 6.2, 7.9, and rulings 3 to 5 of the plan):
//
//   - every refusal happens before the game is stopped, takes no snapshot
//     and changes nothing on disk;
//   - only the active world is updated;
//   - a missing image is downloaded before the stop;
//   - an update stops gracefully, snapshots the old version, sets the new
//     one and restarts only a game that was running;
//   - rollback goes back one update, version and save together, with a
//     "Before restore" undo.

// versionWorld makes a world at version with one save from the save
// reader's fixtures, and makes sure the images it needs are present.
func (r *rig) versionWorld(name, version, fixture string) string {
	r.t.Helper()
	id, err := r.worlds.Create(context.Background(), name, version, false)
	if err != nil {
		r.t.Fatal(err)
	}
	r.putSave(id, fixture, "base.zip", time.Now().Add(-time.Hour))
	r.game.SetImage(ImageRef(version), true)
	return id
}

func (r *rig) metaVersion(id string) string {
	r.t.Helper()
	root, err := r.worlds.OpenWorld(id)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil {
		r.t.Fatal(err)
	}
	return meta.GameVersion
}

func (r *rig) snapshotVersion(id, name string) string {
	r.t.Helper()
	root, err := os.OpenRoot(filepath.Join(r.worlds.Dirs().Snapshots, id, name))
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil {
		r.t.Fatal(err)
	}
	return meta.GameVersion
}

func (r *rig) snapshots(id string) []world.Snapshot {
	r.t.Helper()
	list, err := r.worlds.Snapshots(id)
	if err != nil {
		r.t.Fatal(err)
	}
	return list
}

// putModZip writes an enabled mod zip whose info.json declares factorio.
func (r *rig) putModZip(id, name, factorio string) {
	r.t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(name + "_1.0.0/info.json")
	if err != nil {
		r.t.Fatal(err)
	}
	if _, err := f.Write([]byte(`{"name":"` + name + `","version":"1.0.0","factorio_version":"` + factorio + `"}`)); err != nil {
		r.t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		r.t.Fatal(err)
	}
	r.writeFile(id, filepath.Join(world.ModsDir, name+"_1.0.0.zip"), buf.Bytes())
}

// untouched records what a refused update must leave as it was.
type untouched struct {
	r         *rig
	id        string
	files     map[string]string
	snapshots int
	container string // the running container's ID, or "" when stopped
	saves     int    // /server-save commands so far
	pulls     int
}

func (r *rig) record(id string, running bool) untouched {
	r.t.Helper()
	u := untouched{r: r, id: id, files: hashDir(r.t, r.worldDir(id)), snapshots: len(r.snapshots(id)),
		saves: countOf(r.commands(id), "/server-save"), pulls: len(r.game.Daemon().Pulls())}
	if running {
		u.container = r.container(id).ID
	}
	return u
}

func countOf(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}

func (u untouched) check(label string) {
	t := u.r.t
	t.Helper()
	if got := hashDir(t, u.r.worldDir(u.id)); !maps.Equal(got, u.files) {
		t.Errorf("%s: the world's files changed", label)
	}
	if n := len(u.r.snapshots(u.id)); n != u.snapshots {
		t.Errorf("%s: %d snapshots, was %d", label, n, u.snapshots)
	}
	if n := countOf(u.r.commands(u.id), "/server-save"); n != u.saves {
		t.Errorf("%s: the game was saved for a stop", label)
	}
	if n := len(u.r.game.Daemon().Pulls()); n != u.pulls {
		t.Errorf("%s: an image was downloaded: %v", label, u.r.game.Daemon().Pulls()[u.pulls:])
	}
	if u.container != "" {
		cs := u.r.container(u.id)
		if cs.ID != u.container || cs.Status != "running" {
			t.Errorf("%s: the game was stopped (container %s %s, was %s)", label, cs.ID, cs.Status, u.container)
		}
	} else if len(u.r.calls("POST /containers/create")) != 0 {
		t.Errorf("%s: a stopped world was started", label)
	}
}

func TestUpdateConstants(t *testing.T) {
	if AuditUpdate != "game-update" || AuditUpdateRefused != "game-update-refused" || AuditRollback != "rollback" {
		t.Errorf("audit actions %q %q %q", AuditUpdate, AuditUpdateRefused, AuditRollback)
	}
	if ImageRef("2.1.24") != "factoriotools/factorio:2.1.24" {
		t.Errorf("ImageRef = %q", ImageRef("2.1.24"))
	}
}

// SPEC 10.3: an older, equal, unsupported or malformed version is refused
// before anything happens.
func TestUpdateRefusesAVersionThatIsNotNewer(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Refuse", "2.0.77", "2.0.77-no-space-age.zip")
	r.start(id)
	cases := map[string]error{
		"2.0.77":  world.ErrVersionNotNewer,
		"2.0.70":  world.ErrVersionNotNewer,
		"1.1.110": world.ErrVersionNotNewer,
		"3.0.0":   world.ErrVersionUnsupported,
		"2.2.0":   world.ErrVersionUnsupported,
	}
	for target, want := range cases {
		u := r.record(id, true)
		err := r.c.Update(context.Background(), id, target, admin, StopOptions{}, StartOptions{})
		if !errors.Is(err, want) {
			t.Errorf("Update to %s = %v, want %v", target, err, want)
		}
		u.check("update to " + target)
	}
	for _, target := range []string{"", "2.1", "latest", "../2.1.24", "2.1.24 "} {
		u := r.record(id, true)
		err := r.c.Update(context.Background(), id, target, admin, StopOptions{}, StartOptions{})
		if !errors.Is(err, world.ErrVersionNotNewer) && !errors.Is(err, world.ErrVersionUnsupported) {
			t.Errorf("Update to %q = %v, want a version refusal", target, err)
		}
		u.check("update to " + target)
	}
	if v := r.metaVersion(id); v != "2.0.77" {
		t.Errorf("world at %s after refusals", v)
	}
	if slices.Contains(r.actions(), AuditUpdate) {
		t.Errorf("a refused update was audited as done: %v", r.actions())
	}
}

// Ruling 3 of the plan: only the active world is updated.
func TestUpdateRefusesAWorldThatIsNotActive(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.versionWorld("Active", "2.0.77", "2.0.77-no-space-age.zip")
	other := r.versionWorld("Other", "2.0.77", "2.0.77-no-space-age.zip")
	u := r.record(other, false)
	if err := r.c.Update(context.Background(), other, "2.1.24", admin, StopOptions{}, StartOptions{}); err == nil {
		t.Fatal("updated a world that is not active")
	}
	u.check("update of a world that is not active")
	if v := r.metaVersion(other); v != "2.0.77" {
		t.Errorf("world at %s", v)
	}
}

// SPEC 10.3, 7.4: a mod made for the old line is a pre-start problem at
// the new version, refused while the game still runs.
func TestUpdateRefusesAModMadeForTheOldLine(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Mods", "2.0.77", "2.0.77-no-space-age.zip")
	r.putModZip(id, "oldmod", "2.0")
	r.start(id)
	u := r.record(id, true)
	err := r.c.Update(context.Background(), id, "2.1.24", admin, StopOptions{}, StartOptions{})
	var pe *PrecheckError
	if !errors.As(err, &pe) {
		t.Fatalf("Update = %v, want a *PrecheckError", err)
	}
	if !slices.ContainsFunc(pe.Check.Problems, func(p world.Problem) bool { return p.Kind == world.ProblemModGameVersion }) {
		t.Errorf("problems %+v, want a mod version problem", pe.Check.Problems)
	}
	u.check("update refused by the pre-start check")
	if v := r.metaVersion(id); v != "2.0.77" {
		t.Errorf("world at %s", v)
	}
	if !slices.Contains(r.actions(), AuditUpdateRefused) {
		t.Errorf("audit %v, want %s", r.actions(), AuditUpdateRefused)
	}
}

// A save made by a newer game than the target is refused before anything,
// including the image download.
func TestUpdateRefusesASaveNewerThanTheTarget(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Newer save", "2.0.77", "2.1.24-no-space-age.zip")
	r.game.SetImage(ImageRef("2.1.20"), false)
	u := r.record(id, false)
	err := r.c.Update(context.Background(), id, "2.1.20", admin, StopOptions{}, StartOptions{})
	var pe *PrecheckError
	if !errors.As(err, &pe) {
		t.Fatalf("Update = %v, want a *PrecheckError", err)
	}
	if !slices.ContainsFunc(pe.Check.Problems, func(p world.Problem) bool { return p.Kind == world.ProblemSaveNewer }) {
		t.Errorf("problems %+v, want a newer save", pe.Check.Problems)
	}
	u.check("update refused for a newer save")
}

// SPEC 10.3, 6.2, 7.6: a running world is stopped gracefully, snapshotted
// at its old version, set to the new one and started on its image.
func TestUpdateRunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Running", "2.0.77", "2.0.77-no-space-age.zip")
	r.game.SetImage(ImageRef("2.1.24"), true)
	r.start(id)
	first := r.container(id).ID
	if err := r.c.Update(context.Background(), id, "2.1.24", admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.commands(id), "/server-save") {
		t.Error("the update did not stop gracefully")
	}
	snaps := r.snapshots(id)
	if len(snaps) == 0 || snaps[0].Reason != world.ReasonGameUpdate || snaps[0].Label != world.LabelBeforeUpdate {
		t.Fatalf("no Before update snapshot: %+v", snaps)
	}
	if v := r.snapshotVersion(id, snaps[0].Name); v != "2.0.77" {
		t.Errorf("the snapshot holds %s, want the old 2.0.77", v)
	}
	if v := r.metaVersion(id); v != "2.1.24" {
		t.Errorf("world at %s, want 2.1.24", v)
	}
	cs := r.container(id)
	if cs.ID == first || cs.Status != "running" || cs.Image != ImageRef("2.1.24") {
		t.Errorf("container %s %s on %s; want a new running one on %s", cs.ID, cs.Status, cs.Image, ImageRef("2.1.24"))
	}
	if st := r.c.Status(id); st.State != StateRunning || st.Operation.Error != "" {
		t.Errorf("state %s, op %+v", st.State, st.Operation)
	}
	if !slices.Contains(r.actions(), AuditUpdate) {
		t.Errorf("audit %v, want %s", r.actions(), AuditUpdate)
	}
}

// Ruling 4 of the plan: a stopped world is updated and stays stopped.
func TestUpdateStoppedWorldStaysStopped(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Stopped", "2.0.77", "2.0.77-no-space-age.zip")
	r.game.SetImage(ImageRef("2.1.24"), true)
	if err := r.c.Update(context.Background(), id, "2.1.24", admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("updating a stopped world started it")
	}
	snaps := r.snapshots(id)
	if len(snaps) == 0 || snaps[0].Reason != world.ReasonGameUpdate {
		t.Fatalf("no Before update snapshot: %+v", snaps)
	}
	if v := r.snapshotVersion(id, snaps[0].Name); v != "2.0.77" {
		t.Errorf("the snapshot holds %s", v)
	}
	if v := r.metaVersion(id); v != "2.1.24" {
		t.Errorf("world at %s", v)
	}
}

// The plan's update order: a missing image is downloaded before the game
// is stopped, so a failed download costs no downtime.
func TestUpdateDownloadsAMissingImageBeforeTheStop(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Download", "2.0.77", "2.0.77-no-space-age.zip")
	target := ImageRef("2.1.20")
	r.game.SetImage(target, false)
	r.start(id)
	if err := r.c.Update(context.Background(), id, "2.1.20", admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.game.Daemon().Pulls(), target) {
		t.Fatalf("pulls %v, want %s", r.game.Daemon().Pulls(), target)
	}
	calls := r.game.State().Calls
	pull := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, "POST /images/create") })
	stop := slices.IndexFunc(calls, func(c string) bool {
		return strings.HasPrefix(c, "POST /containers/") && (strings.HasSuffix(c, "/stop") || strings.HasSuffix(c, "/kill"))
	})
	if pull < 0 || stop < 0 || pull > stop {
		t.Errorf("download at call %d, stop at call %d; want the download first", pull, stop)
	}
	if r.container(id).Image != target {
		t.Errorf("running on %s", r.container(id).Image)
	}
}

func TestUpdateFailedDownloadKeepsTheGameRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("No download", "2.0.77", "2.0.77-no-space-age.zip")
	target := ImageRef("2.1.20")
	r.game.SetImage(target, false)
	r.game.Daemon().FailPull(target, "manifest unknown")
	r.start(id)
	u := r.record(id, true)
	if err := r.c.Update(context.Background(), id, "2.1.20", admin, StopOptions{}, StartOptions{}); err == nil {
		t.Fatal("the update went on without its image")
	}
	u.pulls++ // the attempt itself
	u.check("update whose download failed")
	if v := r.metaVersion(id); v != "2.0.77" {
		t.Errorf("world at %s", v)
	}
}

// SPEC 10.4: rollback restores the "Before update" snapshot, old version
// and save together, takes a "Before restore" undo and starts the game
// again on the old image.
func TestRollbackRunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Rollback", "2.0.77", "2.0.77-no-space-age.zip")
	r.game.SetImage(ImageRef("2.1.24"), true)
	r.start(id)
	if err := r.c.Update(context.Background(), id, "2.1.24", admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	snaps := r.snapshots(id)
	if len(snaps) == 0 {
		t.Fatal("no Before update snapshot")
	}
	before := snaps[0]
	saved := hashDir(t, filepath.Join(r.worlds.Dirs().Snapshots, id, before.Name, world.SavesDir))
	// Play after the update, which a rollback gives up.
	r.writeFile(id, filepath.Join(world.SavesDir, "later.zip"), []byte("played on 2.1.24"))

	undo, err := r.c.Rollback(context.Background(), id, admin, StopOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if undo.Reason != world.ReasonBeforeRestore || undo.Label != world.LabelBeforeRestore {
		t.Errorf("undo snapshot %+v", undo.Manifest)
	}
	if v := r.snapshotVersion(id, undo.Name); v != "2.1.24" {
		t.Errorf("the undo snapshot holds %s, want 2.1.24", v)
	}
	if v := r.metaVersion(id); v != "2.0.77" {
		t.Errorf("world at %s after rollback, want 2.0.77", v)
	}
	if got := hashDir(t, filepath.Join(r.worldDir(id), world.SavesDir)); !maps.Equal(got, saved) {
		t.Errorf("saves after rollback %v, want the snapshot's %v", got, saved)
	}
	cs := r.container(id)
	if cs.Status != "running" || cs.Image != ImageRef("2.0.77") {
		t.Errorf("container %s on %s, want running on %s", cs.Status, cs.Image, ImageRef("2.0.77"))
	}
	if !slices.Contains(r.actions(), AuditRollback) {
		t.Errorf("audit %v, want %s", r.actions(), AuditRollback)
	}
}

// Ruling 5 of the plan: across several updates, each rollback goes back one.
func TestRollbackStepsBackOneUpdateAtATime(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Steps", "2.0.77", "2.0.77-no-space-age.zip")
	for _, v := range []string{"2.1.23", "2.1.24"} {
		r.game.SetImage(ImageRef(v), true)
		if err := r.c.Update(context.Background(), id, v, admin, StopOptions{}, StartOptions{}); err != nil {
			t.Fatalf("update to %s: %v", v, err)
		}
	}
	for _, want := range []string{"2.1.23", "2.0.77"} {
		if _, err := r.c.Rollback(context.Background(), id, admin, StopOptions{}); err != nil {
			t.Fatalf("rollback to %s: %v", want, err)
		}
		if v := r.metaVersion(id); v != want {
			t.Fatalf("world at %s, want %s", v, want)
		}
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("a stopped world was started")
	}
	u := r.record(id, false)
	if _, err := r.c.Rollback(context.Background(), id, admin, StopOptions{}); !errors.Is(err, world.ErrNoRollback) {
		t.Fatalf("Rollback past the first update = %v, want ErrNoRollback", err)
	}
	u.check("rollback with nothing to go back to")
}

// With no update to undo, rollback is refused and the game keeps running.
func TestRollbackWithoutUpdateKeepsTheGameRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Never updated")
	r.start(id)
	u := r.record(id, true)
	if _, err := r.c.Rollback(context.Background(), id, admin, StopOptions{}); !errors.Is(err, world.ErrNoRollback) {
		t.Fatalf("Rollback = %v, want ErrNoRollback", err)
	}
	u.check("rollback with no update")
}
