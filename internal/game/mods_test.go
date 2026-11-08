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
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

// These tests pin what a mod change must hold (SPEC 9.2 to 9.6, 6.2, 7.4,
// 10.3, 12.9, and 9.13: a change on a running world is one operation like a
// game update):
//
//   - the downloads come first, and a failed one leaves the game running
//     and the world, its snapshots and its staged files untouched;
//   - a running game is stopped gracefully, the "Before mod change"
//     snapshot holds the old mods folder, and the game starts again;
//   - a stopped world is changed and stays stopped;
//   - a pre-start check that refuses the new mods leaves the world stopped
//     with the snapshot to undo;
//   - a plan the mods folder no longer fits is refused before anything;
//   - the audit log gets one entry per change.

// modZip builds a mod zip whose info.json names the mod, as the portal serves.
func modZip(t *testing.T, name, version, factorio string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(name + "_" + version + "/info.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(`{"name":"` + name + `","version":"` + version + `","factorio_version":"` + factorio + `"}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeFetcher serves zips by file name, fails chosen ones, and can wait.
type fakeFetcher struct {
	mu     sync.Mutex
	files  map[string][]byte
	errs   map[string]error
	calls  []string
	slow   chan struct{}     // when set, Download signals on it, then waits for the context
	during func(name string) // runs inside each Download
}

func newFetcher() *fakeFetcher {
	return &fakeFetcher{files: map[string][]byte{}, errs: map[string]error{}}
}

func (f *fakeFetcher) Download(ctx context.Context, r modportal.Release, dst *os.File) error {
	f.mu.Lock()
	f.calls = append(f.calls, r.FileName)
	body, fail, slow, during := f.files[r.FileName], f.errs[r.FileName], f.slow, f.during
	f.mu.Unlock()
	if during != nil {
		during(r.FileName)
	}
	if slow != nil {
		slow <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	if fail != nil {
		// A client truncates the file on a failure and leaves it to the caller.
		_, _ = dst.Write([]byte("partial"))
		return fail
	}
	_, err := dst.Write(body)
	return err
}

func (f *fakeFetcher) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// offer serves a mod's zip and returns the download that asks for it.
func (r *rig) offer(f *fakeFetcher, name, version, factorio string) ModDownload {
	r.t.Helper()
	file := name + "_" + version + ".zip"
	f.files[file] = modZip(r.t, name, version, factorio)
	return ModDownload{Mod: name, Release: modportal.Release{Version: version, FileName: file, FactorioVersion: "2.1"}}
}

func (r *rig) fingerprint(id string) string {
	r.t.Helper()
	fp, err := r.worlds.ModsFingerprint(id)
	if err != nil {
		r.t.Fatal(err)
	}
	return fp
}

// modOp makes the change a planner would for these downloads.
func (r *rig) modOp(id string, summary string, downloads ...ModDownload) ModOp {
	return ModOp{Downloads: downloads, Fingerprint: r.fingerprint(id), Summary: summary}
}

func (r *rig) applyMods(id string, op ModOp) error {
	return r.c.ApplyMods(context.Background(), id, admin, op, StopOptions{})
}

// stagedFiles lists the temp files left at the top of the worlds folder.
func (r *rig) stagedFiles() []string {
	r.t.Helper()
	entries, err := os.ReadDir(r.worlds.Dirs().Worlds)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), world.PrefixPartial) {
			out = append(out, e.Name())
		}
	}
	return out
}

func (r *rig) hasModZip(id, file string) bool {
	_, err := os.Stat(filepath.Join(r.worldDir(id), world.ModsDir, file))
	return err == nil
}

func (r *rig) modAudit() (done, refused []string) {
	for _, e := range r.audit.list() {
		switch e.Action {
		case AuditMods:
			done = append(done, e.Detail)
		case AuditModsRefused:
			refused = append(refused, e.Detail)
		}
	}
	return done, refused
}

func TestModAuditConstants(t *testing.T) {
	if AuditMods != "mods" || AuditModsRefused != "mods-refused" {
		t.Errorf("audit actions %q %q", AuditMods, AuditModsRefused)
	}
}

// SPEC 9.2, 10.3, 6.2: a running world's mods are downloaded while the game
// runs, then it is stopped gracefully, snapshotted with the old mods, given
// the new ones and started again.
func TestApplyModsRunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Running")
	r.putModZip(id, "oldmod", "2.1")
	r.start(id)
	first := r.container(id).ID
	oldMods := hashDir(t, filepath.Join(r.worldDir(id), world.ModsDir))

	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	f.during = func(string) {
		if cs := r.container(id); cs.ID != first || cs.Status != "running" {
			t.Errorf("the game was %s while the mod downloaded", cs.Status)
		}
	}
	r.c.SetModFetcher(f)
	if err := r.applyMods(id, r.modOp(id, "install flib 0.16.0", dl)); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(r.commands(id), "/server-save") {
		t.Error("the change did not stop gracefully")
	}
	snaps := r.snapshots(id)
	if len(snaps) == 0 || snaps[0].Reason != world.ReasonMods || snaps[0].Label != world.LabelBeforeMods {
		t.Fatalf("no Before mod change snapshot: %+v", snaps)
	}
	if got := hashDir(t, filepath.Join(r.worlds.Dirs().Snapshots, id, snaps[0].Name, world.ModsDir)); !maps.Equal(got, oldMods) {
		t.Errorf("the snapshot's mods %v, want the old %v", got, oldMods)
	}
	if !r.hasModZip(id, "flib_0.16.0.zip") {
		t.Error("the mod was not placed")
	}
	list, err := os.ReadFile(filepath.Join(r.worldDir(id), world.ModsDir, world.ModListFile)) //nolint:gosec // a test's own temp folder
	if err != nil || !strings.Contains(string(list), `"flib"`) {
		t.Errorf("mod-list.json = %q, %v; want flib listed", list, err)
	}
	cs := r.container(id)
	if cs.ID == first || cs.Status != "running" {
		t.Errorf("container %s %s, want a new running one", cs.ID, cs.Status)
	}
	if st := r.c.Status(id); st.State != StateRunning || st.Operation.Error != "" {
		t.Errorf("state %s, op %+v", st.State, st.Operation)
	}
	if left := r.stagedFiles(); len(left) != 0 {
		t.Errorf("staged files left: %v", left)
	}
	done, refused := r.modAudit()
	if len(done) != 1 || done[0] != "install flib 0.16.0" || len(refused) != 0 {
		t.Errorf("audit done %v refused %v, want one entry", done, refused)
	}
}

// SPEC 10.3: a stopped world gets the same change with no stop and no start.
func TestApplyModsStoppedWorldStaysStopped(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stopped")
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	if err := r.applyMods(id, r.modOp(id, "install flib", dl)); err != nil {
		t.Fatal(err)
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("a stopped world was started")
	}
	if !r.hasModZip(id, "flib_0.16.0.zip") {
		t.Error("the mod was not placed")
	}
	snaps := r.snapshots(id)
	if len(snaps) == 0 || snaps[0].Reason != world.ReasonMods {
		t.Fatalf("no Before mod change snapshot: %+v", snaps)
	}
	if len(r.stagedFiles()) != 0 {
		t.Errorf("staged files left: %v", r.stagedFiles())
	}
}

// A world that is not the active one is changed without touching the game
// that runs.
func TestApplyModsWorldThatIsNotActive(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.start(a)
	running := r.record(a, true)
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	if err := r.applyMods(b, r.modOp(b, "install flib", dl)); err != nil {
		t.Fatal(err)
	}
	running.check("mod change of a world that is not active")
	if !r.hasModZip(b, "flib_0.16.0.zip") {
		t.Error("the mod was not placed")
	}
}

// Changes with nothing to download (enable, disable, remove) need no fetcher.
func TestApplyModsWithoutDownloads(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Disable")
	r.putModZip(id, "oldmod", "2.1")
	op := r.modOp(id, "disable oldmod")
	op.Change.Enabled = map[string]bool{"oldmod": false}
	if err := r.applyMods(id, op); err != nil {
		t.Fatal(err)
	}
	mods, err := r.worlds.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(mods, func(m world.InstalledMod) bool { return m.Name == "oldmod" }); i < 0 || mods[i].Enabled {
		t.Errorf("mods %+v, want oldmod disabled", mods)
	}
}

// An uploaded zip staged by the page is placed beside the downloads.
func TestApplyModsKeepsAnUploadInChangeAdd(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Upload")
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	sf, name, err := r.worlds.StageMod()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sf.Write(modZip(t, "mine", "1.0.0", "2.1"))
	_ = sf.Close()
	op := r.modOp(id, "install flib, upload mine", dl)
	op.Change.Add = []string{name}
	if err := r.applyMods(id, op); err != nil {
		t.Fatal(err)
	}
	if !r.hasModZip(id, "flib_0.16.0.zip") || !r.hasModZip(id, "mine_1.0.0.zip") {
		t.Error("the downloaded and the uploaded mod should both be placed")
	}
	if len(op.Change.Add) != 1 {
		t.Errorf("the caller's ModOp changed: %v", op.Change.Add)
	}
}

// SPEC 9.12: a download that fails, here on a checksum after a
// good first one, leaves the running game, the files, the snapshots and the
// staged files exactly as they were.
func TestApplyModsFailedDownloadLeavesTheGameUntouched(t *testing.T) {
	for name, cause := range map[string]error{
		"checksum":    modportal.ErrChecksum,
		"unreachable": modportal.ErrUnreachable,
		"refused":     modportal.ErrUnauthorized,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			id := r.newWorld("Failing")
			r.start(id)
			f := newFetcher()
			good := r.offer(f, "flib", "0.16.0", "2.1")
			bad := r.offer(f, "other", "1.0.0", "2.1")
			f.errs[bad.Release.FileName] = cause
			r.c.SetModFetcher(f)
			u := r.record(id, true)
			err := r.applyMods(id, r.modOp(id, "install both", good, bad))
			if !errors.Is(err, cause) {
				t.Fatalf("ApplyMods = %v, want %v", err, cause)
			}
			// The page names the mod that failed (SPEC 17.9).
			if de, ok := errors.AsType[*ModDownloadError](err); !ok || de.Mod != "other" || de.Version != "1.0.0" {
				t.Errorf("ApplyMods = %#v, want a ModDownloadError naming other 1.0.0", err)
			}
			if got := f.called(); len(got) != 2 {
				t.Errorf("downloads %v, want both tried", got)
			}
			u.check("change whose download failed")
			if left := r.stagedFiles(); len(left) != 0 {
				t.Errorf("staged files left: %v", left)
			}
			if st := r.c.Status(id); st.State != StateRunning {
				t.Errorf("state %s, want running", st.State)
			}
		})
	}
}

// A zip that is not the mod and version asked for is a failed download.
func TestApplyModsRefusesAZipThatIsNotTheRelease(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Wrong zip")
	r.start(id)
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	f.files[dl.Release.FileName] = modZip(t, "evil", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	u := r.record(id, true)
	if err := r.applyMods(id, r.modOp(id, "install flib", dl)); err == nil {
		t.Fatal("a zip of another mod was accepted")
	}
	u.check("change with the wrong zip")
	if len(r.stagedFiles()) != 0 {
		t.Errorf("staged files left: %v", r.stagedFiles())
	}
	f.files[dl.Release.FileName] = []byte("not a zip")
	if err := r.applyMods(id, r.modOp(id, "install flib", dl)); err == nil {
		t.Fatal("a file that is not a zip was accepted")
	}
	u.check("change with a file that is not a zip")
}

// ForceStop ends a slow download: nothing is applied, no snapshot is taken,
// no staged file is left.
func TestApplyModsCancelledDownload(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Cancel")
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	f.slow = make(chan struct{})
	r.c.SetModFetcher(f)
	u := r.record(id, false)
	done := make(chan error, 1)
	op := r.modOp(id, "install flib", dl)
	go func() { done <- r.applyMods(id, op) }()
	<-f.slow
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrCancelled) {
		t.Fatalf("ApplyMods = %v, want ErrCancelled", err)
	}
	u.check("cancelled download")
	if len(r.stagedFiles()) != 0 {
		t.Errorf("staged files left: %v", r.stagedFiles())
	}
}

// SPEC 9.11: with no way to download, a change that needs one is refused
// before anything, and the refusal is audited.
func TestApplyModsRefusesDownloadsWithoutAFetcher(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("No fetcher")
	r.start(id)
	u := r.record(id, true)
	dl := ModDownload{Mod: "flib", Release: modportal.Release{Version: "0.16.0", FileName: "flib_0.16.0.zip"}}
	err := r.applyMods(id, r.modOp(id, "install flib", dl))
	if !errors.Is(err, ErrNoModFetcher) {
		t.Fatalf("ApplyMods = %v, want ErrNoModFetcher", err)
	}
	u.check("change without a fetcher")
	if _, refused := r.modAudit(); len(refused) != 1 {
		t.Errorf("refusals audited: %v", refused)
	}
}

// SPEC 6.11: a disk too full for the reserve fails the download step, with
// the game untouched.
func TestApplyModsFullDiskLeavesTheGameUntouched(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Full")
	r.start(id)
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	op := r.modOp(id, "install flib", dl)
	u := r.record(id, true)
	t.Cleanup(world.SetSpaceReserveForTest(1 << 62))
	if err := r.applyMods(id, op); !errors.Is(err, world.ErrNoSpace) {
		t.Fatalf("ApplyMods = %v, want ErrNoSpace", err)
	}
	u.check("change on a full disk")
	if len(f.called()) != 0 || len(r.stagedFiles()) != 0 {
		t.Errorf("downloads %v, staged %v; want neither", f.called(), r.stagedFiles())
	}
}

// SPEC 9.2, 17.8: a plan made against another mods folder is refused as
// "the plan changed", before any download or stop.
func TestApplyModsRefusesAPlanThatChanged(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Changed")
	r.start(id)
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	op := r.modOp(id, "install flib", dl)
	// Someone else changed the folder after the plan was made.
	r.putModZip(id, "newcomer", "2.1")
	u := r.record(id, true)
	err := r.applyMods(id, op)
	if !errors.Is(err, world.ErrModsChanged) || !IsRefusal(err) {
		t.Fatalf("ApplyMods = %v, want a refusal that is ErrModsChanged", err)
	}
	u.check("change of a plan that changed")
	if len(f.called()) != 0 {
		t.Errorf("downloads %v after a refusal", f.called())
	}
	done, refused := r.modAudit()
	if len(done) != 0 || len(refused) != 1 {
		t.Errorf("audit done %v refused %v, want one refusal", done, refused)
	}
}

// SPEC 7.4, 6.2: the pre-start check runs on the new mods. One it refuses
// leaves the world stopped, with the change applied and the snapshot that
// undoes it.
func TestApplyModsPreStartCheckFailureLeavesTheWorldStopped(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Bad mod")
	r.start(id)
	oldMods := hashDir(t, filepath.Join(r.worldDir(id), world.ModsDir))
	f := newFetcher()
	dl := r.offer(f, "oldline", "1.0.0", "1.1") // made for another game line
	r.c.SetModFetcher(f)
	creates := len(r.calls("POST /containers/create"))
	err := r.applyMods(id, r.modOp(id, "install oldline", dl))
	pe, ok := errors.AsType[*PrecheckError](err)
	if !ok {
		t.Fatalf("ApplyMods = %v, want a *PrecheckError", err)
	}
	if !slices.ContainsFunc(pe.Check.Problems, func(p world.Problem) bool { return p.Kind == world.ProblemModGameVersion }) {
		t.Errorf("problems %+v, want a mod version problem", pe.Check.Problems)
	}
	if cs := r.container(id); cs.Status == "running" {
		t.Error("the game was started past a refused check")
	}
	if n := len(r.calls("POST /containers/create")); n != creates {
		t.Errorf("%d containers created, was %d", n, creates)
	}
	if !r.hasModZip(id, "oldline_1.0.0.zip") {
		t.Error("the change should stay applied, to be undone from the snapshot")
	}
	snaps := r.snapshots(id)
	if len(snaps) == 0 || snaps[0].Reason != world.ReasonMods {
		t.Fatalf("no Before mod change snapshot: %+v", snaps)
	}
	if got := hashDir(t, filepath.Join(r.worlds.Dirs().Snapshots, id, snaps[0].Name, world.ModsDir)); !maps.Equal(got, oldMods) {
		t.Errorf("the snapshot's mods %v, want the old %v", got, oldMods)
	}
	if done, _ := r.modAudit(); len(done) != 1 {
		t.Errorf("audit entries %v, want one", done)
	}
}

// A game the stop would refuse is refused before the download, as an
// update's is.
func TestApplyModsRefusesAPausedGameBeforeTheDownload(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Paused")
	r.game.Daemon().Seed(dockertest.Container{Name: ContainerName(id), Status: "paused",
		Spec: docker.ContainerSpec{Labels: map[string]string{LabelWorld: id, LabelInstall: r.data}}})
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	if err := r.applyMods(id, r.modOp(id, "install flib", dl)); !errors.Is(err, ErrPaused) {
		t.Fatalf("ApplyMods = %v, want ErrPaused", err)
	}
	if len(f.called()) != 0 {
		t.Errorf("downloads %v for a game that cannot be stopped", f.called())
	}
}

// A staged upload the page handed in is removed too when the change fails
// before it is placed.
func TestApplyModsFailureRemovesAnUploadStagedByThePage(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Upload fails")
	f := newFetcher()
	bad := r.offer(f, "flib", "0.16.0", "2.1")
	f.errs[bad.Release.FileName] = modportal.ErrChecksum
	r.c.SetModFetcher(f)
	sf, name, err := r.worlds.StageMod()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sf.Write(modZip(t, "mine", "1.0.0", "2.1"))
	_ = sf.Close()
	op := r.modOp(id, "install flib, upload mine", bad)
	op.Change.Add = []string{name}
	u := r.record(id, false)
	if err := r.applyMods(id, op); !errors.Is(err, modportal.ErrChecksum) {
		t.Fatalf("ApplyMods = %v, want ErrChecksum", err)
	}
	u.check("failed change with an upload")
	if len(r.stagedFiles()) != 0 {
		t.Errorf("staged files left: %v", r.stagedFiles())
	}
}

// The countdown limit is the stop's own.
func TestApplyModsRejectsABadCountdown(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Countdown")
	err := r.c.ApplyMods(context.Background(), id, admin, r.modOp(id, "x"), StopOptions{Countdown: -1})
	if !errors.Is(err, ErrCountdown) {
		t.Errorf("ApplyMods = %v, want ErrCountdown", err)
	}
}

// dropWorld makes a running Space Age world whose newest save uses the mod
// foreman-test, which the world has installed and on.
func (r *rig) dropWorld() string {
	r.t.Helper()
	id, err := r.worlds.Create(context.Background(), "Drops", "2.1.24", true)
	if err != nil {
		r.t.Fatal(err)
	}
	r.putSave(id, "2.1.24-space-age-testmod.zip", "base.zip", time.Now().Add(-time.Hour))
	r.writeFile(id, filepath.Join(world.ModsDir, "foreman-test_0.1.0.zip"), modZip(r.t, "foreman-test", "0.1.0", "2.1"))
	r.start(id)
	return id
}

func (r *rig) problemKinds(err error) []world.ProblemKind {
	r.t.Helper()
	pe, ok := errors.AsType[*PrecheckError](err)
	if !ok {
		r.t.Fatalf("ApplyMods = %v, want a *PrecheckError", err)
	}
	var kinds []world.ProblemKind
	for _, p := range pe.Check.Problems {
		kinds = append(kinds, p.Kind)
	}
	return kinds
}

// SPEC 9.4, 7.4: turning off a mod the loading save uses, after the admin
// confirmed the game deletes its content, ends with the game running.
func TestApplyModsConfirmedDropRestartsTheGame(t *testing.T) {
	for name, change := range map[string]world.ModChange{
		"disable": {Enabled: map[string]bool{"foreman-test": false}},
		"remove":  {Enabled: map[string]bool{"foreman-test": false}, Remove: []string{"foreman-test_0.1.0.zip"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			id := r.dropWorld()
			op := r.modOp(id, name+" foreman-test")
			op.Change, op.ConfirmedDrops = change, []string{"foreman-test"}
			if err := r.applyMods(id, op); err != nil {
				t.Fatalf("ApplyMods = %v, want the world running", err)
			}
			if st := r.c.Status(id); st.State != StateRunning || st.Operation.Error != "" {
				t.Errorf("state %s, op %+v", st.State, st.Operation)
			}
			if snaps := r.snapshots(id); len(snaps) == 0 || snaps[0].Label != world.LabelBeforeMods {
				t.Errorf("no Before mod change snapshot: %+v", snaps)
			}
			done, _ := r.modAudit()
			if len(done) != 1 || !strings.Contains(done[0], "foreman-test") || !strings.Contains(done[0], "confirmed") {
				t.Errorf("audit %v, want the confirmed drop recorded", done)
			}
		})
	}
}

// Without the confirmation the refusal stands, as before.
func TestApplyModsUnconfirmedDropStillRefuses(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.dropWorld()
	op := r.modOp(id, "disable foreman-test")
	op.Change = world.ModChange{Enabled: map[string]bool{"foreman-test": false}}
	err := r.applyMods(id, op)
	if kinds := r.problemKinds(err); !slices.Equal(kinds, []world.ProblemKind{world.ProblemModDisabled}) {
		t.Errorf("problems %v, want mod-disabled", kinds)
	}
	if st := r.c.Status(id); st.State == StateRunning {
		t.Error("the game was started past a refused check")
	}
}

// A confirmation for another mod, or one next to a different problem, does
// not pass the check.
func TestApplyModsPartlyConfirmedStillRefuses(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.dropWorld()
	f := newFetcher()
	dl := r.offer(f, "oldline", "1.0.0", "1.1") // made for another game line
	r.c.SetModFetcher(f)
	op := r.modOp(id, "disable foreman-test, install oldline", dl)
	op.Change = world.ModChange{Enabled: map[string]bool{"foreman-test": false}}
	op.ConfirmedDrops = []string{"foreman-test"}
	kinds := r.problemKinds(r.applyMods(id, op))
	if !slices.Equal(kinds, []world.ProblemKind{world.ProblemModGameVersion}) {
		t.Errorf("problems %v, want only the mod version problem left", kinds)
	}

	r2 := newRig(t, rigOptions{})
	id = r2.dropWorld()
	op = r2.modOp(id, "disable foreman-test")
	op.Change = world.ModChange{Enabled: map[string]bool{"foreman-test": false}}
	op.ConfirmedDrops = []string{"some-other-mod"}
	if kinds := r2.problemKinds(r2.applyMods(id, op)); !slices.Equal(kinds, []world.ProblemKind{world.ProblemModDisabled}) {
		t.Errorf("problems %v, want mod-disabled", kinds)
	}
}

// One problem naming two mods passes only when both are confirmed.
func TestConfirmedDropNeedsEveryNamedMod(t *testing.T) {
	two := world.Problem{Kind: world.ProblemModDisabled, Mods: []string{"a", "b"}}
	cases := []struct {
		p     world.Problem
		drops []string
		want  bool
	}{
		{two, []string{"a", "b"}, true},
		{two, []string{"a"}, false},
		{two, nil, false},
		{world.Problem{Kind: world.ProblemModMissing, Mods: []string{"a"}}, []string{"a"}, true},
		{world.Problem{Kind: world.ProblemSpaceAgeOff, Names: []string{"a"}}, []string{"a"}, false},
		{world.Problem{Kind: world.ProblemModDisabled}, []string{"a"}, false},
	}
	for _, c := range cases {
		if got := confirmedDrop(c.p, c.drops); got != c.want {
			t.Errorf("confirmedDrop(%+v, %v) = %v, want %v", c.p, c.drops, got, c.want)
		}
	}
}

func (r *rig) modSnapshotCount(id string) int {
	n := 0
	for _, s := range r.snapshots(id) {
		if s.Reason == world.ReasonMods {
			n++
		}
	}
	return n
}

// SPEC 9.4: a run of changes on a stopped world shares one snapshot until
// the game launches.
func TestApplyModsStoppedRunSharesOneSnapshotUntilALaunch(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Run")
	r.putModZip(id, "oldmod", "2.1")
	for _, on := range []bool{false, true, false} {
		op := r.modOp(id, "toggle oldmod")
		op.Change.Enabled = map[string]bool{"oldmod": on}
		if err := r.applyMods(id, op); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.modSnapshotCount(id); n != 1 {
		t.Fatalf("%d mod snapshots after three stopped changes, want 1", n)
	}
	r.start(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	op := r.modOp(id, "enable oldmod")
	op.Change.Enabled = map[string]bool{"oldmod": true}
	if err := r.applyMods(id, op); err != nil {
		t.Fatal(err)
	}
	if n := r.modSnapshotCount(id); n != 2 {
		t.Fatalf("%d mod snapshots after a launch and another change, want 2", n)
	}
}

// SPEC 9.4, ADR 0014: a start the pre-start check refuses never launches
// the game, so it does not end the run; the next change reuses its snapshot.
func TestARefusedStartDoesNotEndTheRun(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Refused")
	f := newFetcher()
	r.c.SetModFetcher(f)
	dl := r.offer(f, "oldline", "1.0.0", "1.1") // made for another game line
	if err := r.applyMods(id, r.modOp(id, "install oldline", dl)); err != nil {
		t.Fatal(err)
	}
	if _, ok := errors.AsType[*PrecheckError](r.c.Start(context.Background(), id, admin, StartOptions{})); !ok {
		t.Fatal("the start was not refused by the pre-start check")
	}
	op := r.modOp(id, "disable oldline")
	op.Change.Enabled = map[string]bool{"oldline": false}
	if err := r.applyMods(id, op); err != nil {
		t.Fatal(err)
	}
	if n := r.modSnapshotCount(id); n != 1 {
		t.Fatalf("%d mod snapshots after a refused start, want 1", n)
	}
}
