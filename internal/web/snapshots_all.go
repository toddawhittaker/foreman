package web

import (
	"errors"
	"fmt"
	"net/http"

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
	WorldName string
	world.SnapshotUse
}

// AllSnapshotsPage lists every world's snapshots and their disk use
// (SPEC 6.12).
type AllSnapshotsPage struct {
	Rows      []SnapshotRow
	UsedBytes uint64
	FreeBytes uint64
	// Done says what the last action did.
	Done string
}

func (AllSnapshotsPage) templateName() string { return "allsnapshots" }
func (AllSnapshotsPage) navItem() string      { return PathSnapshots }

// Reason says why a snapshot was taken.
func (AllSnapshotsPage) Reason(reason string) string { return reasonText(reason) }

// DeletePath is the confirmation page for one snapshot.
func (AllSnapshotsPage) DeletePath(id, snap string) string {
	return snapshotDeletePath(id, snap) + "?from=" + fromAll
}

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

func (s *server) allSnapshotRoutes() {
	s.mux.HandleFunc("GET "+PathSnapshots, s.requireUser(s.handleAllSnapshots))
	s.mux.HandleFunc("GET "+PathSnapshotDelete, s.requireUser(s.handleSnapshotDeleteForm))
	s.mux.HandleFunc("POST "+PathSnapshotDelete, s.requireUser(s.handleSnapshotDelete))
}

func (s *server) handleAllSnapshots(w http.ResponseWriter, r *http.Request, u store.User) {
	list, used, err := s.worlds.AllSnapshots()
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
	p := AllSnapshotsPage{UsedBytes: used, FreeBytes: free}
	for _, sn := range list {
		name, ok := names[sn.WorldID]
		if !ok {
			// A deleted world's snapshots stay (SPEC 6.6).
			name = s.worlds.SnapshotWorldName(sn.WorldID, sn.Name)
		}
		p.Rows = append(p.Rows, SnapshotRow{WorldName: name, SnapshotUse: sn})
	}
	p.Done = deletedText(r.URL.Query().Get("deleted"))
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
	if p.Frees, err = s.worlds.SnapshotFrees(id, snap); err != nil {
		s.fail(w, r, "read snapshot size", err)
		return SnapshotDeletePage{}, false
	}
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

// handleSnapshotDelete deletes one snapshot after the confirmation
// (SPEC 6.12). The audit entry is written before anything is removed, and
// if it cannot be written nothing is removed (SPEC 12.9).
func (s *server) handleSnapshotDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	p, ok := s.snapshotDeleteTarget(w, r, u)
	if !ok {
		return
	}
	var err error
	if s.game != nil {
		if op := s.game.Status(p.WorldID).Operation; op != nil && !op.Done {
			err = errOperationRunning
		}
	}
	if err == nil {
		err = s.worlds.DeleteSnapshot(p.WorldID, p.Snapshot.Name, func(sn world.Snapshot) error {
			return s.store.AddAudit(r.Context(), store.AuditEntry{
				Time: s.now(), UserID: u.ID, Username: u.Username, Action: AuditSnapshotDelete,
				WorldID: p.WorldID, Detail: fmt.Sprintf("%s (%s, %s)", sn.Name, sn.Label, sn.Reason),
			})
		})
	}
	switch {
	case err == nil:
		to := snapshotsPath(p.WorldID)
		if p.FromAll {
			to = PathSnapshots
		}
		http.Redirect(w, r, to+"?deleted="+p.Snapshot.Name, http.StatusSeeOther)
	case errors.Is(err, errOperationRunning), errors.Is(err, world.ErrBusy):
		p.Error = "Nothing was deleted: " + sentence(err)
		s.page(w, r, http.StatusConflict, u.Username, p)
	default:
		s.notFoundOrFail(w, r, u, "delete snapshot", err)
	}
}

// deletedText says which snapshot a delete removed, from its folder name
// alone, since the snapshot itself is gone.
func deletedText(name string) string {
	t, ok := world.SnapshotNameTime(name)
	if !ok {
		return ""
	}
	return "Foreman deleted the snapshot taken " + t.Format("2006-01-02 15:04:05 UTC") + "."
}
