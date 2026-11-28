package web

import (
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// The result messages of the worlds and snapshots pages float over the page
// and take no room, so nothing the admin used moves (DESIGN 6, "Toasts").
// What a result offers next, a link or a list, stays in the page. A toast
// built from a result parameter in the address names it in Drop, so the
// address loses it once shown and a reload does not show it again.

// Parameters that carry a result to the page after a redirect.
var (
	dropDeleted     = []string{"deleted", "removed"}
	dropWorldResult = []string{"renamed", "archived", importedQuery, uncheckedQuery, clonedQuery}
)

func (p WorldsPage) toasts() []Toast {
	switch {
	case p.Deleted == "":
		return nil
	case p.DeletedKept:
		return []Toast{{Text: i18n.M("worlds.list.deleted_kept", "name", p.Deleted), Drop: dropDeleted}}
	}
	return []Toast{{Text: i18n.M("worlds.list.deleted_gone", "name", p.Deleted), Drop: dropDeleted}}
}

func (p FavoritesPage) toasts() []Toast {
	var ts []Toast
	if p.Deleted != "" {
		ts = append(ts, Toast{Text: i18n.M("favorites.deleted", "name", p.Deleted)})
	}
	if !p.Renamed.IsZero() {
		ts = append(ts, Toast{Text: p.Renamed, Drop: []string{"renamed"}})
	}
	if p.Imported != "" {
		ts = append(ts, Toast{Text: i18n.M("favorites.import.done", "name", p.Imported)})
	}
	return ts
}

func (p SavesPage) toasts() []Toast {
	var ts []Toast
	if p.Uploaded != "" {
		ts = append(ts, Toast{Text: i18n.M("worlds.saves.uploaded", "file", i18n.Code(p.Uploaded)), Drop: []string{"uploaded"}})
	}
	if p.Chosen {
		chosen := Toast{Text: i18n.M("worlds.saves.chosen_newest"), Drop: []string{"chosen"}}
		if p.LoadSave != "" {
			chosen.Text = i18n.M("worlds.saves.chosen", "file", i18n.Code(p.LoadSave))
		}
		ts = append(ts, chosen)
	}
	return ts
}

// deletedToast sums up the snapshots a delete removed: the one by its time,
// or how many, and removed is the count when the times are not known.
func deletedToast(deleted []time.Time, removed int) []Toast {
	switch {
	case len(deleted) == 1:
		return []Toast{{Text: i18n.M("worlds.snapshots.deleted_one", "when", momentUTCSeconds(deleted[0])), Drop: dropDeleted}}
	case len(deleted) > 1:
		return []Toast{{Text: i18n.M("worlds.snapshots.deleted_many", "count", len(deleted)), Drop: dropDeleted}}
	case removed > 0:
		return []Toast{{Text: i18n.M("worlds.snapshots.deleted_many", "count", removed), Drop: dropDeleted}}
	}
	return nil
}

func (p SnapshotsPage) toasts() []Toast {
	var ts []Toast
	if !p.Done.IsZero() {
		t := Toast{Text: p.Done}
		if p.DoneParam != "" {
			t.Drop = []string{p.DoneParam}
		}
		ts = append(ts, t)
	}
	ts = append(ts, deletedToast(p.Deleted, p.Removed)...)
	if !p.Error.IsZero() {
		ts = append(ts, Toast{Error: true, Text: p.Error})
	}
	return ts
}

func (p AllSnapshotsPage) toasts() []Toast { return deletedToast(p.Deleted, p.Removed) }

func (p SnapshotBulkDeletePage) toasts() []Toast {
	ts := deletedToast(p.Deleted, 0)
	if !p.Error.IsZero() {
		ts = append(ts, Toast{Error: true, Text: p.Error})
	}
	return ts
}

// errorToast is the one error a confirmation or form page sums up, if any.
func errorToast(m i18n.Msg) []Toast {
	if m.IsZero() {
		return nil
	}
	return []Toast{{Error: true, Text: m}}
}

func (p SnapshotDeletePage) toasts() []Toast { return errorToast(p.Error) }
func (p RestorePage) toasts() []Toast        { return errorToast(p.Error) }
func (p RollbackPage) toasts() []Toast       { return errorToast(p.Error) }
func (p UpdatePage) toasts() []Toast         { return errorToast(p.Refusal) }
func (p DeletePage) toasts() []Toast         { return errorToast(p.Error) }
func (p ClonePage) toasts() []Toast          { return errorToast(p.Error) }
func (p WorldFilePage) toasts() []Toast      { return errorToast(p.Error) }
func (p FactoryMapPage) toasts() []Toast     { return errorToast(p.Refused) }

func (p WorldPage) toasts() []Toast {
	var ts []Toast
	if !p.Done.IsZero() {
		ts = append(ts, Toast{Text: p.Done, Drop: dropWorldResult})
	}
	if !p.UpdateDone.IsZero() {
		ts = append(ts, Toast{Text: p.UpdateDone, Drop: []string{"result"}}) // ownResultQuery
	}
	if p.FavMade != nil {
		ts = append(ts, Toast{Text: i18n.M("favorites.made", "name", p.FavMade.Name), Drop: []string{favoriteQuery}})
	}
	return append(ts, errorToast(p.ArchiveError)...)
}

func (p ImportPage) toasts() []Toast {
	if p.Result == nil || len(p.Result.Imported) == 0 {
		return nil
	}
	return []Toast{{Text: i18n.M("worlds.import.result.imported", "count", len(p.Result.Imported))}}
}
