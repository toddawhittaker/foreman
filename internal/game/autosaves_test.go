package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// failTidyAudit refuses only the tidy's audit entry, so the start's own
// entry is still written.
type failTidyAudit struct{ AuditLog }

func (a failTidyAudit) AddAudit(ctx context.Context, e store.AuditEntry) error {
	if e.Action == AuditAutosaveTidy {
		return errors.New("the database is gone")
	}
	return a.AuditLog.AddAudit(ctx, e)
}

// tidyRig makes a world with autosaves 1 to n, 1 oldest, and the given
// slots and interval in server-settings.json.
func tidyRig(t *testing.T, n, slots, interval int, wrap func(*Controller)) (*rig, string) {
	t.Helper()
	r := newRig(t, rigOptions{noRun: true})
	if wrap != nil {
		wrap(r.c)
	}
	r.stop = r.run(r.c)
	id := r.newWorld("Tidy")
	b, err := gamesettings.SetFields(gamesettings.NewServerSettings("Tidy"), []gamesettings.Field{
		{Key: "autosave_slots", JSON: []byte(strconv.Itoa(slots))},
		{Key: "autosave_interval", JSON: []byte(strconv.Itoa(interval))},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.writeFile(id, filepath.Join(world.ConfigDir, world.ServerSettingsFile), b)
	base := time.Now().Add(-30 * time.Minute)
	for i := 1; i <= n; i++ {
		r.putSave(id, "2.1.24-no-space-age.zip", "_autosave"+strconv.Itoa(i)+".zip", base.Add(time.Duration(i)*time.Minute))
	}
	return r, id
}

func (r *rig) saveFiles(id string) map[string]time.Time {
	r.t.Helper()
	entries, err := os.ReadDir(filepath.Join(r.worldDir(id), world.SavesDir))
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]time.Time{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			r.t.Fatal(err)
		}
		out[e.Name()] = info.ModTime()
	}
	return out
}

func (r *rig) snapshotReasons(id string) []string {
	r.t.Helper()
	snaps, err := r.worlds.Snapshots(id)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []string
	for _, s := range snaps {
		out = append(out, s.Reason)
	}
	return out
}

// SPEC 11.6: a start after the slots went down keeps the newest S
// autosaves in rotation order, after a snapshot and an audit entry.
func TestStartTidiesAutosaves(t *testing.T) {
	r, id := tidyRig(t, 6, 3, 10, nil)
	before := r.saveFiles(id)
	r.start(id)

	after := r.saveFiles(id)
	// K = 6: 6, 5, 4 become 3, 2, 1 with their times.
	for from, to := range map[string]string{"_autosave6.zip": "_autosave3.zip", "_autosave5.zip": "_autosave2.zip", "_autosave4.zip": "_autosave1.zip"} {
		if !after[to].Equal(before[from]) {
			t.Errorf("%s has time %v, want %s's %v", to, after[to], from, before[from])
		}
	}
	if len(after) != 4 || after["base.zip"].IsZero() {
		t.Errorf("saves after the tidy: %v", after)
	}
	if !slices.Contains(r.snapshotReasons(id), world.ReasonAutosaves) {
		t.Errorf("no snapshot before the tidy: %v", r.snapshotReasons(id))
	}
	var tidy *store.AuditEntry
	for _, e := range r.audit.list() {
		if e.Action == AuditAutosaveTidy {
			tidy = &e
		}
	}
	if tidy == nil || tidy.WorldID != id || tidy.Detail != "kept 3, renamed 3, removed 3" {
		t.Errorf("audit %+v", r.audit.list())
	}
}

// SPEC 11.6: no tidy when the interval or the slots are 0, or when the
// snapshot or the audit entry fails; the start carries on regardless.
func TestStartSkipsTheTidy(t *testing.T) {
	cases := []struct {
		name            string
		slots, interval int
		wrap            func(*Controller)
		setup           func(*rig, string)
	}{
		{"interval 0", 3, 0, nil, nil},
		{"slots 0", 0, 10, nil, nil},
		{"the audit entry fails", 3, 10, func(c *Controller) { c.auditLog = failTidyAudit{c.auditLog} }, nil},
		{"the snapshot fails", 3, 10, nil, func(r *rig, id string) {
			// A snapshot refuses a world that holds a link (SPEC 6.8).
			if err := os.Symlink("/etc/hostname", filepath.Join(r.worldDir(id), "link")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, id := tidyRig(t, 6, c.slots, c.interval, c.wrap)
			if c.setup != nil {
				c.setup(r, id)
			}
			before := r.saveFiles(id)
			r.start(id)
			after := r.saveFiles(id)
			// The game wrote nothing new; the newest file may be re-touched
			// only by a load, which the fake game does not do.
			for name := range before {
				if _, ok := after[name]; !ok {
					t.Errorf("%s is gone", name)
				}
			}
			if len(after) != len(before) {
				t.Errorf("saves changed: %v", after)
			}
			for _, e := range r.audit.list() {
				if e.Action == AuditAutosaveTidy {
					t.Errorf("tidy audited: %+v", e)
				}
			}
		})
	}
}

// SPEC 11.6: the file the last run loaded is remembered after it ends.
func TestLoadedSaveKeptAfterTheRun(t *testing.T) {
	r, id := tidyRig(t, 2, 20, 10, nil)
	r.start(id)
	want := r.c.Status(id).Save
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	w := r.c.world(id)
	w.mu.Lock()
	loaded := w.lastLoaded
	w.mu.Unlock()
	if want == "" || loaded != want {
		t.Errorf("remembered %q, want %q", loaded, want)
	}
}
