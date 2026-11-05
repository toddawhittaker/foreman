package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Routes for every world's snapshots and for deleting one (SPEC 6.12).
const (
	PathSnapshots      = "/snapshots"
	PathSnapshotDelete = "/worlds/{id}/snapshots/{snap}/delete"
)

// AuditSnapshotDelete is the audit action for a deleted snapshot (SPEC 6.12).
const AuditSnapshotDelete = "snapshot-delete"

// fromAll marks a delete started from the Snapshots tab, so it returns there.
const fromAll = "all"

func snapshotDeletePath(id, snap string) string {
	return snapshotsPath(id) + "/" + snap + "/delete"
}

// SnapshotRow is one snapshot on the Snapshots tab.
type SnapshotRow struct {
	world.SnapshotUse
	// Ticked is true when the snapshot's box starts ticked.
	Ticked bool
}

// SnapshotGroup is one world's rows on the Snapshots tab.
type SnapshotGroup struct {
	WorldID   string
	WorldName string
	// Known is false for a deleted world, which has no page to link to.
	Known bool
	// Adds is the space this world's snapshots add (SPEC 6.6).
	Adds uint64
	Rows []SnapshotRow
}

// AllSnapshotsPage lists every world's snapshots and their disk use
// (SPEC 6.12).
type AllSnapshotsPage struct {
	Groups []SnapshotGroup
	// AddBytes is the space all snapshots add: what deleting every one of
	// them would free.
	AddBytes  uint64
	FreeBytes uint64
	// Done says what the last action did.
	Done string
}

func (AllSnapshotsPage) templateName() string { return "allsnapshots" }
func (AllSnapshotsPage) navItem() string      { return PathSnapshots }

// Reason says why a snapshot was taken, or "" when its label already says so.
func (AllSnapshotsPage) Reason(reason, label string) string { return showReason(reason, label) }

// DeletePath is the confirmation page for one snapshot.
func (AllSnapshotsPage) DeletePath(id, snap string) string {
	return snapshotDeletePath(id, snap) + "?from=" + fromAll
}

// RestorePath is the confirmation page for restoring one snapshot.
func (AllSnapshotsPage) RestorePath(id, snap string) string { return restorePath(id, snap) }

// WorldPath is the world's page.
func (AllSnapshotsPage) WorldPath(id string) string { return worldPath(id) }

// BulkValue is what a snapshot's checkbox submits.
func (AllSnapshotsPage) BulkValue(id, snap string) string { return refValue(id, snap) }

// SelectPath is where the shortcut buttons post back to.
func (AllSnapshotsPage) SelectPath() string { return PathSnapshots }

// BulkPath is the confirmation page for the ticked snapshots.
func (AllSnapshotsPage) BulkPath() string { return PathSnapshotsDelete }

// SelectWorld is the value of one world's "select all automatic" button.
func (AllSnapshotsPage) SelectWorld(id string) string { return selectAutomatic + ":" + id }

// SelectAll is the value of the button that ticks every world's automatic snapshots.
func (AllSnapshotsPage) SelectAll() string { return selectAutomatic }

// SnapshotDeletePage confirms deleting one snapshot (SPEC 6.12).
type SnapshotDeletePage struct {
	WorldID   string
	WorldName string
	Snapshot  world.Snapshot
	Frees     uint64
	// Rollback is the game version rollback would go back to, set when this
	// is the snapshot rollback restores (SPEC 10.4).
	Rollback string
	// FromAll is true when the delete started on the Snapshots tab.
	FromAll bool
	Error   string
}

func (SnapshotDeletePage) templateName() string { return "snapshotdelete" }
func (p SnapshotDeletePage) navItem() string {
	if p.FromAll {
		return PathSnapshots
	}
	return PathWorlds
}
func (p SnapshotDeletePage) hasErrors() bool { return p.Error != "" }

// Path is where the confirmation posts.
func (p SnapshotDeletePage) Path() string { return snapshotDeletePath(p.WorldID, p.Snapshot.Name) }

// BackPath is where cancelling goes.
func (p SnapshotDeletePage) BackPath() string {
	if p.FromAll {
		return PathSnapshots
	}
	return snapshotsPath(p.WorldID)
}

// DeletePath is the delete confirmation for one snapshot of this world.
func (p SnapshotsPage) DeletePath(snap string) string { return snapshotDeletePath(p.WorldID, snap) }

// SelectPath is where the shortcut button posts back to.
func (p SnapshotsPage) SelectPath() string { return snapshotsPath(p.WorldID) }

// BulkPath is the confirmation page for the ticked snapshots.
func (SnapshotsPage) BulkPath() string { return PathSnapshotsDelete }

// SelectAll is the value of the button that ticks this world's automatic snapshots.
func (SnapshotsPage) SelectAll() string { return selectAutomatic }

func (s *server) allSnapshotRoutes() {
	s.mux.HandleFunc("GET "+PathSnapshots, s.requireUser(s.handleAllSnapshots))
	s.mux.HandleFunc("GET "+PathSnapshotDelete, s.requireUser(s.handleSnapshotDeleteForm))
	s.mux.HandleFunc("POST "+PathSnapshotDelete, s.requireUser(s.handleSnapshotDelete))
	s.mux.HandleFunc("GET "+PathSnapshotsDelete, s.requireUser(s.handleSnapshotsDeleteForm))
	s.mux.HandleFunc("POST "+PathSnapshotsDelete, s.requireUser(s.handleSnapshotsDelete))
}

func (s *server) handleAllSnapshots(w http.ResponseWriter, r *http.Request, u store.User) {
	count, err := s.worlds.CountSnapshots()
	if err != nil {
		s.fail(w, r, "list snapshots", err)
		return
	}
	free, err := world.FreeBytes(s.worlds.Dirs().Snapshots)
	if err != nil {
		s.fail(w, r, "read free space", err)
		return
	}
	names := map[string]string{}
	if worlds, err := s.worlds.List(r.Context()); err == nil {
		for _, sum := range worlds {
			names[sum.ID] = sum.Name
		}
	}
	sel, adds := readSelection(r), count.WorldAdds()
	p := AllSnapshotsPage{AddBytes: count.Total(), FreeBytes: free}
	for _, sn := range count.Snapshots() {
		if n := len(p.Groups); n == 0 || p.Groups[n-1].WorldID != sn.WorldID {
			name, known := names[sn.WorldID]
			if !known {
				// A deleted world's snapshots stay (SPEC 6.6).
				name = s.worlds.SnapshotWorldName(sn.WorldID, sn.Name)
			}
			p.Groups = append(p.Groups, SnapshotGroup{WorldID: sn.WorldID, WorldName: name, Known: known, Adds: adds[sn.WorldID]})
		}
		g := &p.Groups[len(p.Groups)-1]
		g.Rows = append(g.Rows, SnapshotRow{SnapshotUse: sn, Ticked: sel.ticks(sn.WorldID, sn.Snapshot)})
	}
	p.Done = deletedText(r.URL.Query()["deleted"])
	s.page(w, r, http.StatusOK, u.Username, p)
}

// snapshotDeleteTarget reads the world and snapshot in the path, answering
// the request itself with 404 when either is missing or malformed.
func (s *server) snapshotDeleteTarget(w http.ResponseWriter, r *http.Request, u store.User) (SnapshotDeletePage, bool) {
	id, name := r.PathValue("id"), r.PathValue("snap")
	if !world.ValidID(id) || !world.ValidSnapshotName(name) {
		s.notFound(w, r, u)
		return SnapshotDeletePage{}, false
	}
	snap, err := s.worlds.Snapshot(id, name)
	if err != nil {
		s.notFoundOrFail(w, r, u, "read snapshot", err)
		return SnapshotDeletePage{}, false
	}
	p := SnapshotDeletePage{WorldID: id, Snapshot: snap, FromAll: r.FormValue("from") == fromAll}
	sum, err := s.worlds.Get(r.Context(), id)
	if err == nil {
		p.WorldName = sum.Name
		if target, version, err := s.worlds.RollbackTarget(id); err == nil && target.Name == name {
			p.Rollback = version
		}
	} else {
		p.WorldName = s.worlds.SnapshotWorldName(id, name)
	}
	count, err := s.worlds.CountWorldSnapshots(id)
	if err != nil {
		s.fail(w, r, "read snapshot size", err)
		return SnapshotDeletePage{}, false
	}
	p.Frees = count.Frees([]world.SnapshotRef{{WorldID: id, Name: name}})
	return p, true
}

func (s *server) handleSnapshotDeleteForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.snapshotDeleteTarget(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// errOperationRunning refuses a delete while server control works on the
// world, since a restore or update there may need the snapshot.
var errOperationRunning = errors.New("an operation is running on this world. Wait for it to finish, then try again")

func (s *server) operationRunning(id string) bool {
	if s.game == nil {
		return false
	}
	op := s.game.Status(id).Operation
	return op != nil && !op.Done
}

// deleteSnapshot deletes one snapshot of a world. The audit entry is
// written before anything is removed, and if it cannot be written nothing
// is removed (SPEC 6.12, 12.9).
func (s *server) deleteSnapshot(ctx context.Context, u store.User, id, name string) error {
	if s.operationRunning(id) {
		return errOperationRunning
	}
	return s.worlds.DeleteSnapshot(id, name, func(sn world.Snapshot) error {
		return s.store.AddAudit(ctx, store.AuditEntry{
			Time: s.now(), UserID: u.ID, Username: u.Username, Action: AuditSnapshotDelete,
			WorldID: id, Detail: fmt.Sprintf("%s (%s, %s)", sn.Name, sn.Label, sn.Reason),
		})
	})
}

// handleSnapshotDelete deletes one snapshot after the confirmation
// (SPEC 6.12).
func (s *server) handleSnapshotDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	p, ok := s.snapshotDeleteTarget(w, r, u)
	if !ok {
		return
	}
	err := s.deleteSnapshot(r.Context(), u, p.WorldID, p.Snapshot.Name)
	switch {
	case err == nil:
		to := snapshotsPath(p.WorldID)
		if p.FromAll {
			to = PathSnapshots
		}
		http.Redirect(w, r, to+"?"+deletedQuery(p.Snapshot.Name), http.StatusSeeOther)
	case errors.Is(err, errOperationRunning), errors.Is(err, world.ErrBusy):
		p.Error = "Nothing was deleted: " + sentence(err)
		s.page(w, r, http.StatusConflict, u.Username, p)
	default:
		s.notFoundOrFail(w, r, u, "delete snapshot", err)
	}
}

// deletedQuery names the deleted snapshots for the page the delete returns
// to, which says what went from their folder names alone.
func deletedQuery(names ...string) string {
	return url.Values{"deleted": names}.Encode()
}

// deletedText says which snapshots a delete removed, from their folder
// names alone, since the snapshots themselves are gone.
func deletedText(names []string) string {
	var valid []string
	for _, name := range names {
		if world.ValidSnapshotName(name) {
			valid = append(valid, name)
		}
	}
	switch len(valid) {
	case 0:
		return ""
	case 1:
		t, _ := world.SnapshotNameTime(valid[0])
		return "Foreman deleted the snapshot taken " + t.Format("2006-01-02 15:04:05 UTC") + "."
	}
	return fmt.Sprintf("Foreman deleted %d snapshots.", len(valid))
}
