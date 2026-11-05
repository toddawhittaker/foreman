package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Deleting several snapshots at once (SPEC 6.12).
const (
	PathSnapshotsDelete = "/snapshots/delete"

	// fieldSnap carries one ticked snapshot as "<world id>/<folder>";
	// fieldSelect carries the shortcut button that was pressed.
	fieldSnap   = "snap"
	fieldSelect = "select"
	// fieldWorld names the world's page a bulk delete started on; without
	// it the delete started on the Snapshots tab.
	fieldWorld = "world"

	// selectAutomatic ticks the snapshots retention may remove (SPEC 6.6),
	// in every world, or in one world as "automatic:<world id>".
	selectAutomatic = "automatic"
)

func refValue(id, snap string) string { return id + "/" + snap }

func parseRef(v string) (world.SnapshotRef, bool) {
	id, name, ok := strings.Cut(v, "/")
	if !ok || !world.ValidID(id) || !world.ValidSnapshotName(name) {
		return world.SnapshotRef{}, false
	}
	return world.SnapshotRef{WorldID: id, Name: name}, true
}

// selection is what a person ticked on a snapshot list, plus the shortcut
// button they pressed, if any.
type selection struct {
	refs      []world.SnapshotRef
	automatic string
}

// readSelection reads the ticked snapshots, leaving out repeats and values
// that name no snapshot.
func readSelection(r *http.Request) selection {
	_ = r.ParseForm() // a bad query leaves what parsed, which is all a selection needs
	sel := selection{automatic: r.Form.Get(fieldSelect)}
	for _, v := range r.Form[fieldSnap] {
		if ref, ok := parseRef(v); ok && !slices.Contains(sel.refs, ref) {
			sel.refs = append(sel.refs, ref)
		}
	}
	return sel
}

// ticks reports whether a snapshot's box starts ticked. The shortcut never
// ticks a manual or "Before delete" snapshot; only a hand tick does.
func (sel selection) ticks(id string, s world.Snapshot) bool {
	if slices.Contains(sel.refs, world.SnapshotRef{WorldID: id, Name: s.Name}) {
		return true
	}
	return s.Automatic() && (sel.automatic == selectAutomatic || sel.automatic == selectAutomatic+":"+id)
}

// DeleteItem is one snapshot on the bulk confirmation page.
type DeleteItem struct {
	WorldID   string
	WorldName string
	Snapshot  world.Snapshot
	// Rollback is the game version rollback would go back to, set when
	// this is the snapshot rollback restores (SPEC 10.4).
	Rollback string
	// Refusal says why the snapshot stays; empty when it can go.
	Refusal string
	// Failure says why a delete just attempted did not happen.
	Failure string
}

// TakenAt is when the snapshot was taken.
func (i DeleteItem) TakenAt() time.Time { return i.Snapshot.TakenAt }

// SnapshotBulkDeletePage confirms deleting the ticked snapshots (SPEC 6.12).
type SnapshotBulkDeletePage struct {
	Items []DeleteItem
	// Frees is what deleting the items that can go would free.
	Frees uint64
	// WorldID is the world's page the delete started on; empty when it
	// started on the Snapshots tab.
	WorldID string
	// Deleted holds when each snapshot just deleted was taken.
	Deleted []time.Time
	Error   string
}

func (SnapshotBulkDeletePage) templateName() string { return "snapshotbulkdelete" }
func (p SnapshotBulkDeletePage) navItem() string {
	if p.WorldID != "" {
		return PathWorlds
	}
	return PathSnapshots
}
func (p SnapshotBulkDeletePage) hasErrors() bool { return p.Error != "" }

// Path is where the confirmation posts.
func (SnapshotBulkDeletePage) Path() string { return PathSnapshotsDelete }

// BackPath is where cancelling goes.
func (p SnapshotBulkDeletePage) BackPath() string {
	if p.WorldID != "" {
		return snapshotsPath(p.WorldID)
	}
	return PathSnapshots
}

// Value is what a snapshot's hidden field submits.
func (SnapshotBulkDeletePage) Value(i DeleteItem) string { return refValue(i.WorldID, i.Snapshot.Name) }

// Going lists the items that can be deleted.
func (p SnapshotBulkDeletePage) Going() []DeleteItem {
	return slices.DeleteFunc(slices.Clone(p.Items), func(i DeleteItem) bool { return i.Refusal != "" })
}

// Staying lists the items that cannot be deleted now.
func (p SnapshotBulkDeletePage) Staying() []DeleteItem {
	return slices.DeleteFunc(slices.Clone(p.Items), func(i DeleteItem) bool { return i.Refusal == "" })
}

// Heading names what the page asks.
func (p SnapshotBulkDeletePage) Heading() string {
	switch n := len(p.Going()); n {
	case 0:
		return "Delete snapshots"
	case 1:
		return "Delete 1 snapshot"
	default:
		return fmt.Sprintf("Delete %d snapshots", n)
	}
}

// bulkPage examines the named snapshots and fills the confirmation page.
// It answers the request itself with 500 and returns false when the
// snapshots cannot be counted.
func (s *server) bulkPage(w http.ResponseWriter, r *http.Request, refs []world.SnapshotRef) (SnapshotBulkDeletePage, bool) {
	count, err := s.worlds.CountSnapshots()
	if err != nil {
		s.fail(w, r, "read snapshots", err)
		return SnapshotBulkDeletePage{}, false
	}
	have := map[world.SnapshotRef]world.Snapshot{}
	for _, u := range count.Snapshots() {
		have[world.SnapshotRef{WorldID: u.WorldID, Name: u.Name}] = u.Snapshot
	}
	// A world's name and rollback point are read once for all its snapshots.
	type worldInfo struct{ name, rollback, rollbackSnap string }
	worlds := map[string]worldInfo{}
	info := func(ref world.SnapshotRef) worldInfo {
		if wi, ok := worlds[ref.WorldID]; ok {
			return wi
		}
		var wi worldInfo
		if sum, err := s.worlds.Get(r.Context(), ref.WorldID); err == nil {
			wi.name = sum.Name
			if target, version, err := s.worlds.RollbackTarget(ref.WorldID); err == nil {
				wi.rollbackSnap, wi.rollback = target.Name, version
			}
		} else {
			wi.name = s.worlds.SnapshotWorldName(ref.WorldID, ref.Name)
		}
		worlds[ref.WorldID] = wi
		return wi
	}
	p := SnapshotBulkDeletePage{}
	if id := r.Form.Get(fieldWorld); world.ValidID(id) {
		p.WorldID = id
	}
	var going []world.SnapshotRef
	for _, ref := range refs {
		snap, found := have[ref]
		wi := info(ref)
		item := DeleteItem{WorldID: ref.WorldID, WorldName: wi.name, Snapshot: world.Snapshot{Name: ref.Name}}
		switch {
		case !found:
			item.Refusal = "The snapshot is not there any more."
		case s.operationRunning(ref.WorldID):
			item.Snapshot = snap
			item.Refusal = sentence(errOperationRunning)
		default:
			item.Snapshot = snap
			if wi.rollbackSnap == ref.Name {
				item.Rollback = wi.rollback
			}
			going = append(going, ref)
		}
		p.Items = append(p.Items, item)
	}
	p.Frees = count.Frees(going)
	return p, true
}

func (s *server) handleSnapshotsDeleteForm(w http.ResponseWriter, r *http.Request, u store.User) {
	refs := readSelection(r).refs
	p, ok := s.bulkPage(w, r, refs)
	if !ok {
		return
	}
	status := http.StatusOK
	if len(refs) == 0 {
		p.Error = "Nothing is ticked. Go back, tick the snapshots to delete, then choose Delete selected."
		status = http.StatusBadRequest
	}
	s.page(w, r, status, u.Username, p)
}

// failureText says in plain words why one delete did not happen, logging a
// fault whose detail the page must not carry.
func (s *server) failureText(r *http.Request, err error) (text string, fault bool) {
	switch {
	case errors.Is(err, errOperationRunning), errors.Is(err, world.ErrBusy):
		return sentence(err), false
	case errors.Is(err, world.ErrNoSnapshot):
		return "The snapshot is not there any more.", false
	}
	s.log.Error("delete snapshot", "method", r.Method, "path", r.URL.Path, "error", err)
	return "Foreman could not delete it. The log says why.", true
}

// handleSnapshotsDelete deletes the confirmed snapshots one at a time, each
// audited before it is removed. A failure does not undo the deletes already
// done; the page then lists what stayed and why (SPEC 6.12).
func (s *server) handleSnapshotsDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	refs := readSelection(r).refs
	p, ok := s.bulkPage(w, r, refs)
	if !ok {
		return
	}
	if len(refs) == 0 {
		p.Error = "Nothing was deleted: nothing is ticked."
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	var deleted []string
	var stayed []world.SnapshotRef
	failures := map[world.SnapshotRef]string{}
	fault := false
	for _, item := range p.Items {
		ref := world.SnapshotRef{WorldID: item.WorldID, Name: item.Snapshot.Name}
		if item.Refusal != "" {
			stayed = append(stayed, ref)
			continue
		}
		err := s.deleteSnapshot(r.Context(), u, ref.WorldID, ref.Name)
		if err == nil {
			deleted = append(deleted, ref.Name)
			continue
		}
		var bad bool
		failures[ref], bad = s.failureText(r, err)
		fault = fault || bad
		stayed = append(stayed, ref)
	}
	if len(stayed) == 0 {
		to := PathSnapshots
		if p.WorldID != "" {
			to = snapshotsPath(p.WorldID)
		}
		http.Redirect(w, r, to+"?"+deletedQuery(deleted...), http.StatusSeeOther)
		return
	}
	rest, ok := s.bulkPage(w, r, stayed)
	if !ok {
		return
	}
	for i, item := range rest.Items {
		rest.Items[i].Failure = failures[world.SnapshotRef{WorldID: item.WorldID, Name: item.Snapshot.Name}]
	}
	rest.Deleted = deletedTimes(deleted)
	rest.Error = fmt.Sprintf("%d of %d snapshots were not deleted. Each one below says why.", len(stayed), len(refs))
	if len(deleted) == 0 {
		rest.Error = "Nothing was deleted. Each snapshot below says why."
	}
	status := http.StatusConflict
	if fault {
		status = http.StatusInternalServerError
	}
	s.page(w, r, status, u.Username, rest)
}
