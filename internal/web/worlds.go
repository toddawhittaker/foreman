package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// A world's own pages hang off PathWorlds/{id}.
const (
	PathWorldDelete = "/worlds/{id}/delete"
	PathDeleted     = "/deleted"
)

func worldPath(id string) string  { return PathWorlds + "/" + id }
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
	NameError string
	// CanSwitch offers "Switch to this world": this mode has server control
	// and the world is not the active one (SPEC 7.9). SwitchNote says in a
	// line why it does not, when that is worth saying; SwitchBusy adds a
	// link to the Server page (DESIGN 7.1).
	CanSwitch  bool
	SwitchNote string
	SwitchBusy bool
	// MissingImage is the game image this host lacks for the world, asked
	// of the local Docker only when it is offered for a switch (SPEC 3.7).
	MissingImage string
	// Busy says what runs on this world now, so both worlds of a switch
	// show it (SPEC 7.9); it is "" when nothing does.
	Busy string
	// Done is the message after a rename or an import; Warning follows an
	// import whose version could not be checked.
	Done    string
	Warning string
	// ImportedFrom is where the world was imported from, when recorded.
	ImportedFrom *world.ImportedFrom
	// Download is "running" or "failed" while the world's last game image
	// download is so, else "" (SPEC 3.7).
	Download string
	// Rollback, when set, offers going back to the version before the last
	// update (SPEC 10.4); ReleasesPath loads the release notice (SPEC 10.2).
	Rollback     *RollbackOffer
	ReleasesPath string
}

func (WorldPage) templateName() string { return "world" }
func (WorldPage) navItem() string      { return PathWorlds }

func (p WorldPage) hasErrors() bool { return p.NameError != "" }

// ImportPath is where the world list's import link goes.
func (WorldsPage) ImportPath() string { return PathImport }

// Problems lists the rename field's error.
func (p WorldPage) Problems() []Problem { return problems(Problem{"name", p.NameError}) }

// Paths the world page links and posts to.
func (p WorldPage) RenamePath() string    { return worldPath(p.World.ID) + "/rename" }
func (p WorldPage) SavesPath() string     { return worldPath(p.World.ID) + "/saves" }
func (p WorldPage) SnapshotsPath() string { return worldPath(p.World.ID) + "/snapshots" }
func (p WorldPage) DeletePath() string    { return deletePath(p.World.ID) }
func (p WorldPage) SwitchPath() string    { return switchPath(p.World.ID) }

// DeleteRefusal says why the world cannot be deleted, or is "" when it can.
func (p WorldPage) DeleteRefusal() string { return deleteRefusal(p.World, p.Busy) }

// DeleteWaitsOnServer reports whether the delete refusal is a running
// operation, which the Server page follows (SPEC 17.9).
func (p WorldPage) DeleteWaitsOnServer() bool {
	return p.Busy != "" && !p.World.Active && !p.World.Newer && !p.World.Unreadable
}

// deleteRefusal says, in words, why a world cannot be deleted, so the page
// can show that instead of a button that would be refused (DESIGN 7.1).
// busy is what runs on the world now, or "".
func deleteRefusal(w world.Summary, busy string) string {
	switch {
	case w.Active:
		return "This is the active world. Switch to another world before deleting it."
	case w.Newer:
		return "This world is from a newer Foreman, so this version will not delete it."
	case w.Unreadable:
		return "Because its world.json cannot be read, Foreman will not delete this world."
	case busy != "":
		return busy + " This world cannot be deleted until it ends."
	}
	return ""
}

// busyText says what the running operation is doing to a world, in
// sentences that end with a full stop, or returns "" when none runs. name
// words a world's id for a sentence.
func busyText(op *game.Operation, name func(id string) string) string {
	if op == nil || op.Done {
		return ""
	}
	sw := op.Switch
	if sw == nil {
		what := actionNames[op.Action]
		if what == "" {
			what = "action"
		}
		return "An operation is running on this world: " + what + "."
	}
	text := "Switching from " + name(sw.From) + " to " + name(sw.To) + "."
	for _, st := range op.Steps {
		if st.State == game.StepProgress {
			return text + " Now: " + st.Name + "."
		}
	}
	return text
}

// worldBusy says what runs on the world now, or "". Without server control
// nothing runs. Only a switch reads world names, and only its two.
func (s *server) worldBusy(id string) string {
	if s.game == nil {
		return ""
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
	s.markSwitching(&p)
	if id := r.URL.Query().Get("deleted"); world.ValidID(id) {
		p.Deleted, p.DeletedKept, err = s.deletedWorld(id, list)
		if err != nil {
			s.fail(w, r, "list deleted worlds", err)
			return
		}
	}
	s.page(w, r, http.StatusOK, u.Username, p)
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
	p.Busy = map[string]string{}
	p.Switchable = map[string]bool{}
	activeBusy := false
	for _, w := range p.Worlds {
		if text := s.worldBusy(w.ID); text != "" {
			p.Busy[w.ID] = text
			activeBusy = activeBusy || w.Active
		}
	}
	for _, w := range p.Worlds {
		switch {
		case w.Active || w.Unreadable || w.Newer:
		case activeBusy || p.Busy[w.ID] != "":
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
	p := s.worldPage(r.Context(), sum, sum.Name, "")
	p.Done = renameDone(r.URL.Query().Get("renamed"), sum.Name)
	s.importFacts(&p, r)
	s.page(w, r, http.StatusOK, u.Username, p)
}

// renameDone is the message after a rename. The link carries the new name,
// and the message shows only while that is still the world's name, so a
// crafted link cannot put words on the page.
func renameDone(renamed, current string) string {
	if renamed == "" || renamed != current {
		return ""
	}
	return "Renamed to " + current + "."
}

// worldPage builds a world's page, with its Switch panel's state. A world
// this Foreman cannot start gets no panel, because the page already says
// why above.
func (s *server) worldPage(ctx context.Context, sum world.Summary, name, nameError string) WorldPage {
	p := WorldPage{World: sum, Name: name, NameError: nameError}
	p.Busy = s.worldBusy(sum.ID)
	if s.game != nil && !sum.Unreadable && !sum.Newer {
		p.ReleasesPath = releasesPath(sum.ID)
		p.Rollback = s.rollbackOffer(sum.ID)
	}
	switch {
	case s.game == nil || sum.Unreadable || sum.Newer:
	case sum.Active:
		p.SwitchNote = "This is the active world."
	case s.switchBusy(ctx, sum.ID):
		// When the operation is on this world, Busy already says so.
		if p.Busy == "" {
			p.SwitchNote, p.SwitchBusy = "An operation is running on the server, such as a switch.", true
		}
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
	NameError   string
	// Error says why the delete was refused, in plain words.
	Error string
	// Busy says what runs on the world now; it makes the page a refusal.
	Busy string
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

func (p DeletePage) hasErrors() bool { return p.NameError != "" || p.Error != "" }

// Problems lists the name field's error for the summary.
func (p DeletePage) Problems() []Problem { return problems(Problem{"confirm-name", p.NameError}) }

// Refusal says why the world cannot be deleted, or is "" when it can.
func (p DeletePage) Refusal() string { return deleteRefusal(p.World, p.Busy) }

// Path is where the confirmation posts.
func (p DeletePage) Path() string { return deletePath(p.World.ID) }

// WorldPath is where cancelling goes.
func (p DeletePage) WorldPath() string { return worldPath(p.World.ID) }

// WorldsPath links back to the world list.
func (DeletedPage) WorldsPath() string { return PathWorlds }

// AllPath shows every snapshot.
func (DeletedPage) AllPath() string { return PathDeleted + "?all=1" }

// RestorePath is the confirmation page for one snapshot.
func (DeletedPage) RestorePath(id, snap string) string { return restorePath(id, snap) }

// Reason says why a snapshot was taken, or "" when its label already says so.
func (DeletedPage) Reason(reason, label string) string { return showReason(reason, label) }

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
		p.NameError = "Type " + sum.Name + " exactly, or cancel"
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
