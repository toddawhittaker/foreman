package web

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Snapshot routes (SPEC 6).
const (
	PathWorldSnapshots  = "/worlds/{id}/snapshots"
	PathSnapshotRestore = "/worlds/{id}/snapshots/{snap}/restore"
)

const (
	maxLabelChars = 100
	// maxListRows is how many snapshots show before "Show older" (R14).
	maxListRows = 50
	// defaultLabel names a manual snapshot taken without a label.
	defaultLabel = "Manual snapshot"
)

func worldPath(id string) string     { return "/worlds/" + id }
func snapshotsPath(id string) string { return worldPath(id) + "/snapshots" }
func restorePath(id, snap string) string {
	return snapshotsPath(id) + "/" + snap + "/restore"
}

// SnapshotsPage lists a world's snapshots and takes a new one (SPEC 6.3,
// 6.4, 6.6, DESIGN 8.3).
type SnapshotsPage struct {
	WorldID   string
	WorldName string
	Snapshots []world.Snapshot
	// More is true when rows past maxListRows are hidden.
	More bool
	// Copied is true when any snapshot holds full copies of zips (SPEC 6.8).
	Copied    bool
	UsedBytes uint64
	FreeBytes uint64

	Label      string
	LabelError string
	// Error says why the last action failed, in plain words.
	Error string
	// Done says what the last action did; DoneSnap is the snapshot it names.
	Done     string
	DoneSnap string
}

// RestorePage confirms a restore (SPEC 6.5, DESIGN 7.4).
type RestorePage struct {
	WorldID   string
	WorldName string
	Snapshot  world.Snapshot
	Error     string
}

func (SnapshotsPage) templateName() string { return "snapshots" }
func (RestorePage) templateName() string   { return "restore" }

func (p SnapshotsPage) hasErrors() bool { return p.LabelError != "" || p.Error != "" }
func (p RestorePage) hasErrors() bool   { return p.Error != "" }

// Problems lists the field errors for the summary (DESIGN 7.2).
func (p SnapshotsPage) Problems() []Problem { return problems(Problem{"label", p.LabelError}) }

// WorldPath links back to the world page.
func (p SnapshotsPage) WorldPath() string { return worldPath(p.WorldID) }

// Path is where the take-snapshot form posts.
func (p SnapshotsPage) Path() string { return snapshotsPath(p.WorldID) }

// AllPath shows every snapshot.
func (p SnapshotsPage) AllPath() string { return snapshotsPath(p.WorldID) + "?all=1" }

// RestorePath is the confirmation page for one snapshot.
func (p SnapshotsPage) RestorePath(snap string) string { return restorePath(p.WorldID, snap) }

// Path is where the confirmation posts.
func (p RestorePage) Path() string { return restorePath(p.WorldID, p.Snapshot.Name) }

// SnapshotsPath is where cancelling goes.
func (p RestorePage) SnapshotsPath() string { return snapshotsPath(p.WorldID) }

// Reason says why a snapshot was taken, in words.
func (SnapshotsPage) Reason(reason string) string { return reasonText(reason) }

// reasonText says why a snapshot was taken, in words.
func reasonText(reason string) string {
	switch reason {
	case world.ReasonManual:
		return "Manual"
	case world.ReasonBeforeRestore:
		return world.LabelBeforeRestore
	case world.ReasonBeforeDelete:
		return world.LabelBeforeDelete
	}
	return "Before " + strings.ReplaceAll(strings.TrimPrefix(reason, "before-"), "-", " ")
}

func (s *server) snapshotRoutes() {
	s.mux.HandleFunc("GET "+PathWorldSnapshots, s.requireUser(s.handleSnapshots))
	s.mux.HandleFunc("POST "+PathWorldSnapshots, s.requireUser(s.handleTakeSnapshot))
	s.mux.HandleFunc("GET "+PathSnapshotRestore, s.requireUser(s.handleRestoreForm))
	s.handleLong("POST "+PathSnapshotRestore, s.requireUser(s.handleRestore))
}

// worldName returns the world's name, or its id when world.json cannot be
// read. found is false when there is no such world.
func (s *server) worldName(id string) (name string, found bool, err error) {
	w, err := s.worlds.OpenWorld(id)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer func() { _ = w.Close() }()
	meta, err := world.ReadMeta(w)
	if err != nil {
		return id, true, nil
	}
	return meta.Name, true, nil
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusNotFound, u.Username, ErrorPage{Status: http.StatusNotFound})
}

// snapshotsPage fills the list, disk use and free space for a world.
func (s *server) snapshotsPage(r *http.Request, id, name string) (SnapshotsPage, error) {
	list, err := s.worlds.Snapshots(id)
	if err != nil {
		return SnapshotsPage{}, err
	}
	used, free, err := s.worlds.SnapshotDiskUse(id, list)
	if err != nil {
		return SnapshotsPage{}, err
	}
	p := SnapshotsPage{WorldID: id, WorldName: name, UsedBytes: used, FreeBytes: free}
	for _, sn := range list {
		p.Copied = p.Copied || sn.Copied
	}
	if len(list) > maxListRows && r.URL.Query().Get("all") == "" {
		list, p.More = list[:maxListRows], true
	}
	p.Snapshots = list
	return p, nil
}

func (s *server) handleSnapshots(w http.ResponseWriter, r *http.Request, u store.User) {
	id := r.PathValue("id")
	name, found, err := s.worldName(id)
	if err != nil {
		s.fail(w, r, "read world", err)
		return
	}
	if !found {
		s.notFound(w, r, u)
		return
	}
	p, err := s.snapshotsPage(r, id, name)
	if err != nil {
		s.fail(w, r, "list snapshots", err)
		return
	}
	q := r.URL.Query()
	if sn, ok := findSnapshot(p.Snapshots, q.Get("taken")); ok {
		p.Done, p.DoneSnap = fmt.Sprintf("Foreman took the snapshot \"%s\".", sn.Label), sn.Name
	}
	if sn, ok := findSnapshot(p.Snapshots, q.Get("undo")); ok {
		p.Done, p.DoneSnap = fmt.Sprintf("The world is restored. Foreman took the snapshot \"%s\" first; to undo the restore, restore that snapshot.", sn.Label), sn.Name
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

func findSnapshot(list []world.Snapshot, name string) (world.Snapshot, bool) {
	for _, sn := range list {
		if name != "" && sn.Name == name {
			return sn, true
		}
	}
	return world.Snapshot{}, false
}

// checkLabel trims a label and returns a problem in plain words, or "".
func checkLabel(label string) (clean, problem string) {
	clean = strings.TrimSpace(label)
	switch {
	case utf8.RuneCountInString(clean) > maxLabelChars:
		return clean, fmt.Sprintf("Use at most %d characters", maxLabelChars)
	case strings.IndexFunc(clean, unicode.IsControl) >= 0:
		return clean, "Remove control characters from the label"
	case clean == "":
		return defaultLabel, ""
	}
	return clean, ""
}

// handleTakeSnapshot takes a manual snapshot (SPEC 6.3).
func (s *server) handleTakeSnapshot(w http.ResponseWriter, r *http.Request, u store.User) {
	id := r.PathValue("id")
	name, found, err := s.worldName(id)
	if err != nil {
		s.fail(w, r, "read world", err)
		return
	}
	if !found {
		s.notFound(w, r, u)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	label, problem := checkLabel(r.PostFormValue("label"))
	var snap world.Snapshot
	if problem == "" {
		snap, err = s.worlds.TakeSnapshot(id, label, u.Username)
	}
	if problem == "" && err == nil {
		http.Redirect(w, r, snapshotsPath(id)+"?taken="+snap.Name, http.StatusSeeOther)
		return
	}
	status, msg := http.StatusBadRequest, ""
	if err != nil {
		if status, msg = actionError(err); status == http.StatusInternalServerError {
			s.fail(w, r, "take snapshot", err)
			return
		}
	}
	p, perr := s.snapshotsPage(r, id, name)
	if perr != nil {
		s.fail(w, r, "list snapshots", perr)
		return
	}
	p.Label, p.LabelError, p.Error = r.PostFormValue("label"), problem, msg
	s.page(w, r, status, u.Username, p)
}

// actionError turns an error an admin can act on into a status and its
// message. Anything else is a 500 whose detail stays in the log.
func actionError(err error) (int, string) {
	switch {
	case errors.Is(err, world.ErrNoSpace):
		return http.StatusInsufficientStorage, "The world was not changed: " + err.Error()
	case errors.Is(err, world.ErrBusy), errors.Is(err, world.ErrNewerFormat):
		return http.StatusConflict, "The world was not changed: " + err.Error() + "."
	case errors.Is(err, world.ErrDamaged):
		return http.StatusConflict, "The world was not changed: " + world.ErrDamaged.Error() + "."
	}
	return http.StatusInternalServerError, ""
}

// restoreTarget reads the world and snapshot named in the path, answering
// the request itself with 404 when either is missing.
func (s *server) restoreTarget(w http.ResponseWriter, r *http.Request, u store.User) (RestorePage, bool) {
	id, snapName := r.PathValue("id"), r.PathValue("snap")
	if !world.ValidSnapshotName(snapName) {
		s.notFound(w, r, u)
		return RestorePage{}, false
	}
	name, found, err := s.worldName(id)
	if err != nil {
		s.fail(w, r, "read world", err)
		return RestorePage{}, false
	}
	if !found {
		s.notFound(w, r, u)
		return RestorePage{}, false
	}
	snap, err := s.worlds.Snapshot(id, snapName)
	if errors.Is(err, world.ErrNoSnapshot) {
		s.notFound(w, r, u)
		return RestorePage{}, false
	}
	if err != nil {
		s.fail(w, r, "read snapshot", err)
		return RestorePage{}, false
	}
	return RestorePage{WorldID: id, WorldName: name, Snapshot: snap}, true
}

func (s *server) handleRestoreForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.restoreTarget(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handleRestore restores a snapshot (SPEC 6.5). Nothing runs a world in
// this version, so there is no server to stop first.
func (s *server) handleRestore(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.restoreTarget(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	before, err := s.worlds.Restore(p.WorldID, p.Snapshot.Name, u.Username)
	if err != nil {
		status, msg := actionError(err)
		if status == http.StatusInternalServerError {
			s.fail(w, r, "restore snapshot", err)
			return
		}
		p.Error = msg
		s.page(w, r, status, u.Username, p)
		return
	}
	http.Redirect(w, r, snapshotsPath(p.WorldID)+"?undo="+before.Name, http.StatusSeeOther)
}
