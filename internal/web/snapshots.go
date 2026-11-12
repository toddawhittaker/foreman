package web

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Snapshot routes (SPEC 6).
const (
	PathWorldSnapshots  = "/worlds/{id}/snapshots"
	PathSnapshotRestore = "/worlds/{id}/snapshots/{snap}/restore"
)

const maxLabelChars = 100

func snapshotsPath(id string) string { return worldPath(id) + "/snapshots" }
func restorePath(id, snap string) string {
	return snapshotsPath(id) + "/" + snap + "/restore"
}

// SnapshotsPage lists a world's snapshots and takes a new one (SPEC 6.3,
// 6.4, 6.6, DESIGN 8.3).
type SnapshotsPage struct {
	WorldID   string
	WorldName string
	// Active marks the active world in the heading, as on its Overview.
	Active    bool
	Live      LiveState
	Snapshots []world.Snapshot
	// More is true when rows past maxListRows are hidden.
	More bool
	// Copied is true when any snapshot holds full copies of zips (SPEC 6.8).
	Copied bool
	// AddBytes is the space this world's snapshots add: what deleting all
	// of them would free (SPEC 6.6).
	AddBytes  uint64
	FreeBytes uint64
	// Ticked names the snapshots whose boxes start ticked.
	Ticked map[string]bool
	// TickNote says what a shortcut button ticked; zero when none was pressed.
	TickNote i18n.Msg

	Label      string
	LabelError i18n.Msg
	// Error says why the last action failed, in plain words.
	Error i18n.Msg
	// Done says what the last action did; DoneSnap is the snapshot it names.
	Done     i18n.Msg
	DoneSnap string
	// Deleted holds when each snapshot the last delete removed was taken.
	Deleted []time.Time
}

// RestorePage confirms a restore (SPEC 6.5, DESIGN 7.4).
type RestorePage struct {
	WorldID   string
	WorldName string
	Snapshot  world.Snapshot
	Error     i18n.Msg
	// Deleted is true when the world's folder is gone, so restoring makes
	// the world again and takes no "Before restore" snapshot.
	Deleted bool
	// Undo names the "Before restore" snapshot a failed restore took.
	Undo string
	// Running is true when the world's game may be running, so the restore
	// stops it gracefully first and starts it again after (SPEC 6.5).
	Running bool
	// Active is true when the world is the active one, whose restore shows
	// its result on the Server page (SPEC 17.7).
	Active bool
	// ModUndo says what restoring a "Before mod change" snapshot does to
	// the world's mods; nil for any other restore (SPEC 9.4, 17.5).
	ModUndo []i18n.Msg
	CountdownForm
}

func (SnapshotsPage) templateName() string { return "snapshots" }
func (SnapshotsPage) navItem() string      { return PathWorlds }

// Tabs links the world's pages (DESIGN 6).
func (p SnapshotsPage) Tabs() WorldTabs {
	return WorldTabs{Path: worldPath(p.WorldID), Current: "snapshots", Settings: true}
}
func (RestorePage) navItem() string { return PathWorlds }

// WorldPath links back to the world page.
func (p RestorePage) WorldPath() string  { return worldPath(p.WorldID) }
func (RestorePage) templateName() string { return "restore" }

func (p SnapshotsPage) hasErrors() bool { return !p.LabelError.IsZero() || !p.Error.IsZero() }
func (p RestorePage) hasErrors() bool   { return !p.Error.IsZero() || !p.CountdownError.IsZero() }

// Problems lists the countdown field's error for the summary.
func (p RestorePage) Problems() []Problem {
	return problems(Problem{"countdown", p.CountdownError})
}

// Problems lists the field errors for the summary (DESIGN 7.2).
func (p SnapshotsPage) Problems() []Problem {
	return problems(Problem{"label", p.LabelError})
}

// WorldPath links back to the world page.
func (p SnapshotsPage) WorldPath() string { return worldPath(p.WorldID) }

// Path is where the take-snapshot form posts.
func (p SnapshotsPage) Path() string { return snapshotsPath(p.WorldID) }

// AllPath shows every snapshot.
func (p SnapshotsPage) AllPath() string { return snapshotsPath(p.WorldID) + "?all=1" }

// BulkValue is what a snapshot's checkbox submits.
func (p SnapshotsPage) BulkValue(snap string) string { return refValue(p.WorldID, snap) }

// RestorePath is the confirmation page for one snapshot.
func (p SnapshotsPage) RestorePath(snap string) string { return restorePath(p.WorldID, snap) }

// Path is where the confirmation posts.
func (p RestorePage) Path() string { return restorePath(p.WorldID, p.Snapshot.Name) }

// UndoPath restores the "Before restore" snapshot a failed restore took.
func (p RestorePage) UndoPath() string { return restorePath(p.WorldID, p.Undo) }

// SnapshotsPath is where cancelling goes.
func (p RestorePage) SnapshotsPath() string { return snapshotsPath(p.WorldID) }

func (s *server) snapshotRoutes() {
	s.mux.HandleFunc("GET "+PathWorldSnapshots, s.requireUser(s.handleSnapshots))
	// A running game saves before its snapshot (SPEC 6.10), which can outlast
	// the usual deadlines.
	s.handleLong("POST "+PathWorldSnapshots, s.requireUser(s.handleTakeSnapshot))
	s.allSnapshotRoutes()
	s.mux.HandleFunc("GET "+PathSnapshotRestore, s.requireUser(s.handleRestoreForm))
	s.handleLong("POST "+PathSnapshotRestore, s.requireUser(s.handleRestore))
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusNotFound, u.Username, ErrorPage{Status: http.StatusNotFound})
}

// snapshotsPage fills the list, disk use and free space for a world.
func (s *server) snapshotsPage(r *http.Request, sum world.Summary) (SnapshotsPage, error) {
	id := sum.ID
	count, err := s.worlds.CountWorldSnapshots(id)
	if err != nil {
		return SnapshotsPage{}, err
	}
	free, err := world.FreeBytes(s.worlds.Dirs().Snapshots)
	if err != nil {
		return SnapshotsPage{}, err
	}
	sel := readSelection(r)
	p := SnapshotsPage{WorldID: id, WorldName: sum.Name, Active: sum.Active, Live: s.liveState(sum.ID, sum.Active), AddBytes: count.Total(), FreeBytes: free, Ticked: map[string]bool{}}
	var list []world.Snapshot
	automatic := 0
	for _, u := range count.Snapshots() {
		list = append(list, u.Snapshot)
		p.Copied = p.Copied || u.Copied
		p.Ticked[u.Name] = sel.ticks(id, u.Snapshot)
		if p.Ticked[u.Name] && u.Automatic() {
			automatic++
		}
	}
	p.TickNote = sel.tickNote(automatic)
	// A box ticked by the shortcut must be on screen to be reviewed.
	if len(list) > maxListRows && r.URL.Query().Get("all") == "" && sel.automatic == "" {
		list, p.More = list[:maxListRows], true
	}
	p.Snapshots = list
	return p, nil
}

func (s *server) handleSnapshots(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	p, err := s.snapshotsPage(r, sum)
	if err != nil {
		s.notFoundOrFail(w, r, u, "list snapshots", err)
		return
	}
	q := r.URL.Query()
	if sn, ok := findSnapshot(p.Snapshots, q.Get("taken")); ok {
		p.Done, p.DoneSnap = i18n.M("worlds.snapshots.done.taken", "label", snapshotLabelArg(sn)), sn.Name
	}
	if sn, ok := findSnapshot(p.Snapshots, q.Get("restored")); ok {
		p.Done, p.DoneSnap = i18n.M("worlds.snapshots.done.restored", "label", snapshotLabelArg(sn)), sn.Name
	}
	if sn, ok := findSnapshot(p.Snapshots, q.Get("undo")); ok {
		p.Done, p.DoneSnap = i18n.M("worlds.snapshots.done.undo", "label", snapshotLabelArg(sn)), sn.Name
	}
	if p.Deleted = deletedTimes(q["deleted"]); len(p.Deleted) > 0 {
		p.Done, p.DoneSnap = i18n.Msg{}, ""
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

// checkLabel trims a label and returns a problem in plain words, or the
// zero Msg.
func checkLabel(label string) (clean string, problem i18n.Msg) {
	clean = strings.TrimSpace(label)
	switch {
	case utf8.RuneCountInString(clean) > maxLabelChars:
		return clean, i18n.M("worlds.snapshots.label.too_long", "count", maxLabelChars)
	case strings.IndexFunc(clean, unicode.IsControl) >= 0:
		return clean, i18n.M("worlds.snapshots.label.control")
	case clean == "":
		return defaultLabel, i18n.Msg{}
	}
	return clean, i18n.Msg{}
}

// handleTakeSnapshot takes a manual snapshot (SPEC 6.3).
func (s *server) handleTakeSnapshot(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	id := sum.ID
	if !s.readForm(w, r) {
		return
	}
	label, problem := checkLabel(r.PostFormValue("label"))
	var snap world.Snapshot
	var err error
	switch {
	case !problem.IsZero():
	case s.game != nil:
		snap, err = s.game.Snapshot(r.Context(), id, actor(u), label)
	default:
		snap, err = s.worlds.TakeSnapshot(id, label, u.Username)
	}
	if problem.IsZero() && err == nil {
		http.Redirect(w, r, snapshotsPath(id)+"?taken="+snap.Name, http.StatusSeeOther)
		return
	}
	status, msg := http.StatusBadRequest, i18n.Msg{}
	if err != nil {
		var ok bool
		if status, msg, ok = s.gameRefusal(w, r, u, "take snapshot", err); !ok {
			return
		}
	}
	p, perr := s.snapshotsPage(r, sum)
	if perr != nil {
		s.notFoundOrFail(w, r, u, "list snapshots", perr)
		return
	}
	p.Label, p.LabelError, p.Error = r.PostFormValue("label"), problem, msg
	s.page(w, r, status, u.Username, p)
}

// actionSnapshot is a snapshot taken from the Server page.
const actionSnapshot = "snapshot"

// handleServerSnapshot takes a manual snapshot of the world from the Server
// page, in one click, and shows the result there (SPEC 6.3, 17.7, 17.10).
func (s *server) handleServerSnapshot(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	by := actor(u)
	s.beginTo(w, r, PathServerResult, []string{sum.ID}, true, func(ctx context.Context) actionResult {
		snap, err := s.game.Snapshot(ctx, sum.ID, by, defaultLabel)
		res := s.finish(sum.ID, actionSnapshot, err)
		res.Taken = snapshotLabelArg(snap)
		return res
	})
}

// restoreTarget reads the world and snapshot named in the path, answering
// the request itself with 404 when either is missing.
func (s *server) restoreTarget(w http.ResponseWriter, r *http.Request, u store.User) (RestorePage, bool) {
	id, snapName := r.PathValue("id"), r.PathValue("snap")
	if !world.ValidSnapshotName(snapName) {
		s.notFound(w, r, u)
		return RestorePage{}, false
	}
	sum, err := s.worlds.Get(r.Context(), id)
	found := err == nil
	if status, _ := worldError(err); err != nil && status != http.StatusNotFound {
		s.fail(w, r, "read world", err)
		return RestorePage{}, false
	}
	name := sum.Name
	snap, err := s.worlds.Snapshot(id, snapName)
	if err != nil {
		s.notFoundOrFail(w, r, u, "read snapshot", err)
		return RestorePage{}, false
	}
	if !found {
		// A deleted world's snapshots stay restorable (SPEC 6.6).
		name = s.worlds.SnapshotWorldName(id, snapName)
	}
	p := RestorePage{WorldID: id, WorldName: name, Snapshot: snap, Deleted: !found, CountdownForm: defaultCountdown()}
	p.Active = found && sum.Active
	if found {
		p.ModUndo = s.modUndo(id, snap)
	}
	if found && s.game != nil {
		switch s.game.Status(id).State {
		case game.StateRunning, game.StateStarting, game.StateStopping, game.StateCrashed:
			p.Running = true
			p.Online = s.online(r.Context(), id)
		}
	}
	return p, true
}

func (s *server) handleRestoreForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.restoreTarget(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handleRestore restores a snapshot (SPEC 6.5). A deleted world's snapshot
// recreates the world. With server control, a world whose game may be
// running, and the active world, restore in the background and the server
// page follows them; a running game is stopped gracefully first and started
// again after. Any other restore is done before the reply.
func (s *server) handleRestore(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.restoreTarget(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	var before world.Snapshot
	var err error
	switch {
	case s.game == nil:
		before, err = s.worlds.Restore(r.Context(), p.WorldID, p.Snapshot.Name, u.Username)
	case p.Running || p.Active:
		// A stopped game has nobody to warn, so it takes the default countdown.
		stop := game.StopOptions{Countdown: game.StopCountdown}
		if p.Running {
			var form CountdownForm
			form, stop = readCountdown(r)
			if !form.CountdownError.IsZero() {
				form.Online, p.CountdownForm = p.Online, form
				s.page(w, r, http.StatusBadRequest, u.Username, p)
				return
			}
		}
		id, snap, by := p.WorldID, p.Snapshot.Name, actor(u)
		s.begin(w, r, id, true, func(ctx context.Context) actionResult {
			before, err := s.game.Restore(ctx, id, by, snap, stop)
			res := s.finish(id, game.ActionRestore, err)
			res.Restored, res.Undo = snap, before.Name
			return res
		})
		return
	default:
		before, err = s.game.Restore(r.Context(), p.WorldID, actor(u), p.Snapshot.Name, game.StopOptions{Countdown: game.StopCountdown})
	}
	if err != nil {
		if before.Name == "" {
			if status, msg, ok := s.gameRefusal(w, r, u, "restore snapshot", err); ok {
				p.Error = msg
				s.page(w, r, status, u.Username, p)
			}
			return
		}
		// The undo snapshot exists, so the admin must hear of it whatever
		// went wrong (SPEC 6.5); a fault's detail stays in the log.
		status, msg := gameError(err)
		p.Error, p.Undo = msg, before.Name
		if status == http.StatusInternalServerError || status == http.StatusNotFound {
			s.log.Error("restore snapshot", "method", r.Method, "path", r.URL.Path, "error", err)
			status, p.Error = http.StatusInternalServerError, i18n.M("worlds.restore.failed")
		}
		s.page(w, r, status, u.Username, p)
		return
	}
	if p.Deleted {
		http.Redirect(w, r, snapshotsPath(p.WorldID)+"?restored="+p.Snapshot.Name, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, snapshotsPath(p.WorldID)+"?undo="+before.Name, http.StatusSeeOther)
}
