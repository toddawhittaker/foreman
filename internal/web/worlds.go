package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// A world's own pages hang off PathWorlds/{id}.
const (
	PathWorldDelete = "/worlds/{id}/delete"
	PathDeleted     = "/deleted"
)

// WorldsPage is the world list (SPEC 5.4, DESIGN 8.2).
type WorldsPage struct {
	Worlds []world.Summary
	// Deleted names the world a delete just removed, for the message, and
	// DeletedKept says whether its snapshots remain under Deleted worlds.
	Deleted     string
	DeletedKept bool
	// Busy says what runs on a world, by world id, for the worlds a running
	// operation is busy on. Switchable marks the worlds that get a Switch
	// button. SwitchWait is set when a world has no button only because an
	// operation is running. All are empty without server control.
	Busy       map[string]i18n.Msg
	Switchable map[string]bool
	SwitchWait bool
	// SnapshotAdds is the space each world's snapshots add, by world id
	// (SPEC 6.6); a world with none has no entry. AddsUnknown is set when
	// the count failed, and the list then says "unknown".
	SnapshotAdds map[string]uint64
	AddsUnknown  bool
	// ActiveState is the game's state, shown beside the active world when
	// HaveState (only with server control, DESIGN 8.2).
	ActiveState game.State
	HaveState   bool
}

// StateWord is the active world's state badge word (DESIGN 7.5).
func (p WorldsPage) StateWord() i18n.Msg { return stateWord(p.ActiveState) }

// SwitchPath is where a world's Switch button goes.
func (WorldsPage) SwitchPath(id string) string { return switchPath(id) }

func (WorldsPage) templateName() string { return "worlds" }

func (WorldsPage) navItem() string { return PathWorlds + navPage }

func worldPath(id string) string { return PathWorlds + "/" + id }

// WorldTabs are the links between a world's pages; Current is the one shown
// ("overview", "saves", "snapshots" or "settings"), and Settings is false for
// a world whose settings cannot be read (DESIGN 6, "Sub-navigation").
type WorldTabs struct {
	Path, Current string
	Settings      bool
	// Under marks Current as the section the page belongs to, not the page
	// itself (aria-current "true", not "page"; DESIGN 6).
	Under bool
}

func worldTabs(w world.Summary, current string) WorldTabs {
	return WorldTabs{Path: worldPath(w.ID), Current: current, Settings: !w.Newer && !w.Unreadable}
}

// settingsTabs are a world's pages from the settings pages. under is true
// on a page reached from Settings rather than Settings itself.
func settingsTabs(id string, under bool) WorldTabs {
	return WorldTabs{Path: worldPath(id), Current: "settings", Settings: true, Under: under}
}

func deletePath(id string) string { return worldPath(id) + "/delete" }

func (s *server) worldRoutes() {
	s.mux.HandleFunc("GET "+PathWorlds, s.requireUser(s.handleWorlds))
	s.mux.HandleFunc("GET "+PathWorlds+"/{id}", s.requireUser(s.handleWorld))
	s.mux.HandleFunc("POST "+PathWorlds+"/{id}/rename", s.requireUser(s.handleRename))
	s.mux.HandleFunc("GET "+PathWorldDelete, s.requireUser(s.handleDeleteForm))
	s.mux.HandleFunc("POST "+PathWorldDelete, s.requireUser(s.handleDelete))
	s.mux.HandleFunc("GET "+PathDeleted, s.requireUser(s.handleDeleted))
	s.mux.HandleFunc("GET "+PathImport, s.requireUser(s.handleImportForm))
	s.handleLong("POST "+PathImportFolder, s.requireUser(s.handleImportFolder))
	s.handleUpload("POST "+PathImportUpload, MaxSaveUpload, s.requireUser(s.guardUpload(s.handleImportUpload)))
}

// WorldPage is one world's page (SPEC 5.4, 5.8; DESIGN 8.2).
type WorldPage struct {
	World world.Summary
	// Name refills the rename field.
	Name      string
	NameError i18n.Msg
	// CanSwitch offers "Switch to this world": this mode has server control
	// and the world is not the active one (SPEC 7.9). SwitchBusy says an
	// operation stops it, with a link to the Server page (DESIGN 7.1).
	CanSwitch  bool
	SwitchBusy bool
	// MissingImage is the game image this host lacks for the world, asked
	// of the local Docker only when it is offered for a switch (SPEC 3.7).
	MissingImage string
	// Busy says what runs on this world now, so both worlds of a switch
	// show it (SPEC 7.9); it is zero when nothing does.
	Busy i18n.Msg
	// Done is the message after a rename or an import; Warning follows an
	// import whose version could not be checked.
	Done    i18n.Msg
	Warning i18n.Msg
	// Download is "running" or "failed" while the world's last game image
	// download is so, else "" (SPEC 3.7).
	Download string
	// Rollback, when set, offers going back to the version before the last
	// update (SPEC 10.4); ReleasesPath loads the release notice (SPEC 10.2).
	Rollback     *RollbackOffer
	ReleasesPath string
	// UpdateDone and UpdateFailure say how the last update of a world that
	// is not active ended, since the Server page shows only the active
	// world's (SPEC 10.3). At most one is set, and only on the page the
	// update sent the admin to, which takes focus and reads it out
	// (DESIGN 7.6).
	UpdateDone    i18n.Msg
	UpdateFailure *Failure
	// ServerLink offers the Server page beside the busy note, which shows
	// the operation only for the active world or a switch.
	ServerLink bool
	// Working says an action runs on the world, so the page reloads the
	// block that shows what it does and how it ended.
	Working bool
}

func (WorldPage) templateName() string { return "world" }
func (WorldPage) navItem() string      { return PathWorlds }

func (p WorldPage) hasErrors() bool { return !p.NameError.IsZero() }

// ImportPath and GeneratePath are where the world list's import and generate
// links go.
func (WorldsPage) ImportPath() string   { return PathImport }
func (WorldsPage) GeneratePath() string { return PathGenerate }

// Problems lists the rename field's error.
func (p WorldPage) Problems() []Problem { return problems(Problem{"name", p.NameError}) }

// Paths the world page links and posts to.
func (p WorldPage) WorldPath() string     { return worldPath(p.World.ID) }
func (p WorldPage) RenamePath() string    { return worldPath(p.World.ID) + "/rename" }
func (p WorldPage) SnapshotsPath() string { return worldPath(p.World.ID) + "/snapshots" }
func (p WorldPage) DeletePath() string    { return deletePath(p.World.ID) }
func (p WorldPage) SwitchPath() string    { return switchPath(p.World.ID) }
func (p WorldPage) Tabs() WorldTabs       { return worldTabs(p.World, "overview") }

// DeleteRefusal says why the world cannot be deleted, or is zero when it can.
func (p WorldPage) DeleteRefusal() i18n.Msg { return deleteRefusal(p.World, p.Busy) }

// DeleteWaitsOnServer reports whether the delete refusal is a running
// operation, which the Server page follows (SPEC 17.9).
func (p WorldPage) DeleteWaitsOnServer() bool {
	return !p.Busy.IsZero() && !p.World.Active && !p.World.Newer && !p.World.Unreadable
}

// deleteRefusal says, in words, why a world cannot be deleted, so the page
// can show that instead of a button that would be refused (DESIGN 7.1).
// busy is what runs on the world now, or zero.
func deleteRefusal(w world.Summary, busy i18n.Msg) i18n.Msg {
	switch {
	case w.Active:
		return i18n.M("worlds.delete.refusal.active")
	case w.Newer:
		return i18n.M("worlds.delete.refusal.newer")
	case w.Unreadable:
		return i18n.M("worlds.delete.refusal.unreadable")
	case !busy.IsZero():
		return i18n.M("worlds.delete.refusal.busy", "busy", busy)
	}
	return i18n.Msg{}
}

// busyActions name each action in the note about what runs on a world.
var busyActions = map[string]i18n.Msg{
	game.AuditStart: i18n.M("worlds.busy.action.start"), game.AuditStop: i18n.M("worlds.busy.action.stop"),
	game.AuditRestart: i18n.M("worlds.busy.action.restart"), game.AuditForceStop: i18n.M("worlds.busy.action.force_stop"),
	game.AuditImageDownload: i18n.M("worlds.busy.action.image_download"), game.ActionRestore: i18n.M("worlds.busy.action.restore"),
	game.AuditSwitch: i18n.M("worlds.busy.action.switch"), game.AuditUpdate: i18n.M("worlds.busy.action.update"),
	game.AuditRollback: i18n.M("worlds.busy.action.rollback"), actionSnapshot: i18n.M("worlds.busy.action.snapshot"),
}

// busyText says what the running operation is doing to a world, or returns
// the zero Msg when none runs. name words a world's id for a sentence.
func busyText(op *game.Operation, name func(id string) string) i18n.Msg {
	if op == nil || op.Done {
		return i18n.Msg{}
	}
	sw := op.Switch
	if sw == nil {
		what, ok := busyActions[op.Action]
		if !ok {
			what = i18n.M("worlds.busy.action.other")
		}
		return i18n.M("worlds.busy.operation", "action", what)
	}
	from, to := name(sw.From), name(sw.To)
	for _, st := range op.Steps {
		if st.State == game.StepProgress {
			return i18n.M("worlds.busy.switch_step", "from", from, "to", to, "step", stepName(st.Name))
		}
	}
	return i18n.M("worlds.busy.switch", "from", from, "to", to)
}

// worldBusy says what runs on the world now, or the zero Msg. Without
// server control nothing runs. Only a switch reads world names, and only
// its two.
func (s *server) worldBusy(id string) i18n.Msg {
	if s.game == nil {
		return i18n.Msg{}
	}
	return busyText(s.game.Status(id).Operation, s.worldName)
}

// handleWorlds shows the world list (SPEC 5.4).
func (s *server) handleWorlds(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.worlds.List(r.Context())
	if err != nil {
		s.fail(w, r, "list worlds", err)
		return
	}
	p := WorldsPage{Worlds: list}
	if s.game != nil {
		for _, w := range list {
			if w.Active {
				p.ActiveState, p.HaveState = s.game.Status(w.ID).State, true
			}
		}
	}
	s.markSwitching(&p)
	s.markSnapshotAdds(&p)
	if id := r.URL.Query().Get("deleted"); world.ValidID(id) {
		p.Deleted, p.DeletedKept, err = s.deletedWorld(id, list)
		if err != nil {
			s.fail(w, r, "list deleted worlds", err)
			return
		}
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// markSnapshotAdds fills in each world's "Snapshots add" figure from one
// counting pass, the same one the Snapshots pages use, so the figures agree.
// A failed count is logged once and the list still shows.
func (s *server) markSnapshotAdds(p *WorldsPage) {
	if len(p.Worlds) == 0 {
		return
	}
	count, err := s.worlds.CountSnapshots()
	if err != nil {
		s.log.Error("count snapshots", "error", err)
		p.AddsUnknown = true
		return
	}
	p.SnapshotAdds = count.WorldAdds()
}

// markSwitching fills in what the list shows about switching: a Switch
// button on each world that can be switched to, and what runs on the worlds
// an operation is busy on. While one runs on the active world or the row's
// world, the button is left off and the page says why, as switchBusy does
// for one world.
func (s *server) markSwitching(p *WorldsPage) {
	if s.game == nil {
		return
	}
	p.Busy = map[string]i18n.Msg{}
	p.Switchable = map[string]bool{}
	activeBusy := false
	for _, w := range p.Worlds {
		if text := s.worldBusy(w.ID); !text.IsZero() {
			p.Busy[w.ID] = text
			activeBusy = activeBusy || w.Active
		}
	}
	for _, w := range p.Worlds {
		switch {
		case w.Active || w.Unreadable || w.Newer:
		case activeBusy || !p.Busy[w.ID].IsZero():
			p.SwitchWait = true
		default:
			p.Switchable[w.ID] = true
		}
	}
}

// deletedWorld names the world a delete just removed, for the message on
// the list, and says whether it kept snapshots. A world that kept none is
// named by its id. The name is empty while the world exists, so a crafted
// link cannot claim a live world was deleted.
func (s *server) deletedWorld(id string, live []world.Summary) (name string, kept bool, err error) {
	for _, w := range live {
		if w.ID == id {
			return "", false, nil
		}
	}
	gone, err := s.worlds.Deleted()
	if err != nil {
		return "", false, err
	}
	for _, d := range gone {
		if d.ID == id {
			return d.Name, true, nil
		}
	}
	return id, false, nil
}

// findWorld returns the world with the path's id, or answers the request
// itself with 404 or 500.
func (s *server) findWorld(w http.ResponseWriter, r *http.Request, u store.User) (world.Summary, bool) {
	sum, err := s.worlds.Get(r.Context(), r.PathValue("id"))
	if err == nil {
		return sum, true
	}
	s.notFoundOrFail(w, r, u, "read world", err)
	return world.Summary{}, false
}

func (s *server) handleWorld(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	p := s.worldPage(r.Context(), sum, sum.Name, i18n.Msg{})
	// The page reloads its activity block while an action runs; when the
	// action has ended, it reloads whole, so the version and the rollback
	// offer are current.
	// Only the activity block sends htmx requests for this page.
	if r.Header.Get("HX-Request") != "" && !p.Working {
		w.Header().Set("HX-Refresh", "true")
	}
	if followsOwnAction(r) {
		p.UpdateDone, p.UpdateFailure = s.updateOutcome(sum)
	}
	p.Done = renameDone(r.URL.Query().Get("renamed"), sum.Name)
	s.importFacts(&p, r)
	s.generatedFacts(&p, r)
	s.clonedFacts(&p, r)
	s.page(w, r, http.StatusOK, u.Username, p)
}

// renameDone is the message after a rename. The link carries the new name,
// and the message shows only while that is still the world's name, so a
// crafted link cannot put words on the page.
func renameDone(renamed, current string) i18n.Msg {
	if renamed == "" || renamed != current {
		return i18n.Msg{}
	}
	return i18n.M("worlds.world.renamed", "name", current)
}

// worldPage builds a world's page, with its Switch panel's state. A world
// this Foreman cannot start gets no panel, because the page already says
// why above.
func (s *server) worldPage(ctx context.Context, sum world.Summary, name string, nameError i18n.Msg) WorldPage {
	p := WorldPage{World: sum, Name: name, NameError: nameError}
	if s.game != nil {
		op := s.game.Status(sum.ID).Operation
		p.Busy = busyText(op, s.worldName)
		if !p.Busy.IsZero() {
			p.ServerLink = sum.Active || op.Switch != nil
			if !p.ServerLink && op.Action == game.AuditUpdate && op.Version != "" {
				p.Busy = i18n.M("worlds.world.updating", "version", op.Version)
			}
		}
	}
	p.Working = !p.Busy.IsZero() || s.game != nil && s.runs.inFlight(sum.ID)
	if s.game != nil && !sum.Unreadable && !sum.Newer {
		p.ReleasesPath = releasesPath(sum.ID)
		p.Rollback = s.rollbackOffer(sum.ID)
	}
	switch {
	case s.game == nil || sum.Unreadable || sum.Newer || sum.Active:
	case s.switchBusy(ctx, sum.ID):
		// When the operation is on this world, Busy already says so.
		p.SwitchBusy = p.Busy.IsZero()
	default:
		p.CanSwitch = true
		if ref, present, err := s.game.ImageFor(ctx, sum.ID); err == nil && !present {
			p.MissingImage = ref
		}
	}
	return p
}

// handleRename changes a world's display name only (SPEC 5.8). It is not a
// risky operation, so it takes no snapshot (SPEC 6.2).
func (s *server) handleRename(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	err := s.worlds.Rename(r.Context(), sum.ID, name)
	if err == nil {
		http.Redirect(w, r, worldPath(sum.ID)+"?renamed="+url.QueryEscape(strings.TrimSpace(name)), http.StatusSeeOther)
		return
	}
	status, msg, ok := s.refusal(w, r, u, "rename world", err)
	if !ok {
		return
	}
	s.page(w, r, status, u.Username, s.worldPage(r.Context(), sum, name, msg))
}

// DeletePage confirms deleting a world (SPEC 5.10, DESIGN 7.4).
type DeletePage struct {
	World world.Summary
	// FinalSnapshot is whether the box is ticked; it starts ticked.
	FinalSnapshot bool
	// ConfirmName refills the typed name after a mistake.
	ConfirmName string
	NameError   i18n.Msg
	// Error says why the delete was refused, in plain words.
	Error i18n.Msg
	// Busy says what runs on the world now; it makes the page a refusal.
	Busy i18n.Msg
}

// DeletedPage lists deleted worlds, each with the snapshots that remain
// (SPEC 6.6, DESIGN 8.3).
type DeletedPage struct {
	Worlds []world.DeletedWorld
	// More is true when rows past maxListRows are hidden for some world.
	More bool
}

func (DeletePage) templateName() string  { return "delete" }
func (DeletedPage) templateName() string { return "deleted" }
func (DeletePage) navItem() string       { return PathWorlds }
func (DeletedPage) navItem() string      { return PathWorlds }

func (p DeletePage) hasErrors() bool { return !p.NameError.IsZero() || !p.Error.IsZero() }

// Problems lists the name field's error for the summary.
func (p DeletePage) Problems() []Problem {
	return problems(Problem{"confirm-name", p.NameError})
}

// Refusal says why the world cannot be deleted, or is zero when it can.
func (p DeletePage) Refusal() i18n.Msg { return deleteRefusal(p.World, p.Busy) }

// Path is where the confirmation posts.
func (p DeletePage) Path() string { return deletePath(p.World.ID) }

// WorldPath is where cancelling goes.
func (p DeletePage) WorldPath() string { return worldPath(p.World.ID) }

// Tabs are the world's pages; Delete is reached from Overview (DESIGN 6).
func (p DeletePage) Tabs() WorldTabs {
	t := worldTabs(p.World, "overview")
	t.Under = true
	return t
}

// WorldsPath links back to the world list.
func (DeletedPage) WorldsPath() string { return PathWorlds }

// AllPath shows every snapshot.
func (DeletedPage) AllPath() string { return PathDeleted + "?all=1" }

// RestorePath is the confirmation page for one snapshot.
func (DeletedPage) RestorePath(id, snap string) string { return restorePath(id, snap) }

func (s *server) handleDeleteForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, DeletePage{World: sum, FinalSnapshot: true, Busy: s.worldBusy(sum.ID)})
}

// handleDelete deletes a world after its final snapshot (SPEC 5.10). A
// refusal shows the confirmation page again with the reason.
func (s *server) handleDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	p := DeletePage{
		World:         sum,
		FinalSnapshot: r.PostFormValue("final_snapshot") == "on",
		ConfirmName:   r.PostFormValue("confirm_name"),
		Busy:          s.worldBusy(sum.ID),
	}
	err := s.worlds.Delete(r.Context(), sum.ID, p.ConfirmName, p.FinalSnapshot, u.Username)
	if err == nil {
		http.Redirect(w, r, PathWorlds+"?deleted="+sum.ID, http.StatusSeeOther)
		return
	}
	if errors.Is(err, world.ErrWrongName) {
		p.NameError = i18n.M("worlds.delete.wrong_name", "name", sum.Name)
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	status, msg, ok := s.refusal(w, r, u, "delete world", err)
	if !ok {
		return
	}
	p.Error = msg
	s.page(w, r, status, u.Username, p)
}

func (s *server) handleDeleted(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.worlds.Deleted()
	if err != nil {
		s.fail(w, r, "list deleted worlds", err)
		return
	}
	p := DeletedPage{Worlds: list}
	if r.URL.Query().Get("all") == "" {
		for i := range p.Worlds {
			if len(p.Worlds[i].Snapshots) > maxListRows {
				p.Worlds[i].Snapshots, p.More = p.Worlds[i].Snapshots[:maxListRows], true
			}
		}
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// rollbackOffer returns the rollback the world page offers, or nil when
// there is none (SPEC 10.4).
func (s *server) rollbackOffer(id string) *RollbackOffer {
	snap, version, err := s.worlds.RollbackTarget(id)
	if err != nil {
		if !errors.Is(err, world.ErrNoRollback) {
			s.log.Error("find the rollback snapshot", "world", id, "error", err)
		}
		return nil
	}
	return &RollbackOffer{Snapshot: snap, Version: version, Path: rollbackPath(id)}
}
