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

// Import paths (SPEC 5.5). A world's own pages hang off PathWorlds/{id}.
const (
	PathImport       = "/import"
	PathImportFolder = "/import/folder"
	PathImportUpload = "/import/upload"
	PathWorldDelete  = "/worlds/{id}/delete"
	PathDeleted      = "/deleted"
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
	// Done is the message after a rename.
	Done string
	// Rollback, when set, offers going back to the version before the last
	// update (SPEC 10.4); ReleasesPath loads the release notice (SPEC 10.2).
	Rollback     *RollbackOffer
	ReleasesPath string
}

// ImportPage offers both ways to import a world (SPEC 5.5).
type ImportPage struct {
	InboxDir string
	Inbox    []world.InboxFolder
	Folder   ImportForm
	Upload   ImportForm
}

// Importable reports whether any inbox folder can be imported: it has a
// save at its top level or in saves/, and no save name in both (SPEC 5.5).
func (p ImportPage) Importable() bool {
	for _, f := range p.Inbox {
		if f.Importable() {
			return true
		}
	}
	return false
}

// ImportForm is one of the two import forms. Source is the chosen inbox
// folder or the uploaded file.
type ImportForm struct {
	Source       string
	Name         string
	GameVersion  string
	SpaceAge     bool
	SourceError  string
	NameError    string
	VersionError string
	// IDPrefix keeps the two forms' field ids apart on one page.
	IDPrefix string
}

func (WorldPage) templateName() string  { return "world" }
func (ImportPage) templateName() string { return "import" }
func (WorldPage) navItem() string       { return PathWorlds }
func (ImportPage) navItem() string      { return PathWorlds }

func (p WorldPage) hasErrors() bool  { return p.NameError != "" }
func (p ImportPage) hasErrors() bool { return len(p.Folder.Problems())+len(p.Upload.Problems()) > 0 }

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
// sentences that end with a full stop, or returns "" when none runs. names
// maps world ids to display names.
func busyText(op *game.Operation, names map[string]string) string {
	if op == nil || op.Done {
		return ""
	}
	sw := op.Switch
	if sw == nil {
		return "An operation is running on this world: " + op.Action + "."
	}
	text := "Switching from " + nameOf(names, sw.From) + " to " + nameOf(names, sw.To) + "."
	for _, st := range op.Steps {
		if st.State == game.StepProgress {
			return text + " Now: " + st.Name + "."
		}
	}
	return text
}

func nameOf(names map[string]string, id string) string {
	if n := names[id]; n != "" {
		return n
	}
	return id
}

// worldNames maps each world's id to its display name.
func worldNames(list []world.Summary) map[string]string {
	names := make(map[string]string, len(list))
	for _, w := range list {
		names[w.ID] = w.Name
	}
	return names
}

// worldBusy says what runs on the world now, naming the worlds of a
// switch, or "".
func (s *server) worldBusy(ctx context.Context, id string) string {
	list, err := s.worlds.List(ctx)
	if err != nil {
		return s.busyOn(id, nil)
	}
	return s.busyOn(id, worldNames(list))
}

// busyOn says what runs on the world now, or "". Without server control
// nothing runs.
func (s *server) busyOn(id string, names map[string]string) string {
	if s.game == nil {
		return ""
	}
	return busyText(s.game.Status(id).Operation, names)
}

// FolderPath and UploadPath are where the two forms post.
func (ImportPage) FolderPath() string { return PathImportFolder }
func (ImportPage) UploadPath() string { return PathImportUpload }

// Problems lists the form's field errors in the order the fields appear.
// The upload form's file field comes last, so its text fields are read
// before the file (see receiveFileWithFields).
func (f ImportForm) Problems() []Problem {
	if f.IDPrefix == "upload" {
		return problems(
			Problem{"upload-name", f.NameError},
			Problem{"upload-version", f.VersionError},
			Problem{"upload-source", f.SourceError},
		)
	}
	return problems(
		Problem{"folder-source-0", f.SourceError},
		Problem{"folder-name", f.NameError},
		Problem{"folder-version", f.VersionError},
	)
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
	names := worldNames(p.Worlds)
	p.Busy = map[string]string{}
	p.Switchable = map[string]bool{}
	activeBusy := false
	for _, w := range p.Worlds {
		if text := s.busyOn(w.ID, names); text != "" {
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
	p.Busy = s.worldBusy(ctx, sum.ID)
	if s.game != nil && !sum.Unreadable && !sum.Newer {
		p.ReleasesPath = releasesPath(sum.ID)
		p.Rollback = s.rollbackOffer(sum.ID)
	}
	switch {
	case s.game == nil || sum.Unreadable || sum.Newer:
	case sum.Active:
		p.SwitchNote = "This is the active world."
	case s.switchBusy(ctx, sum.ID):
		p.SwitchNote, p.SwitchBusy = "An operation is running on the server, such as a switch.", true
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

func (s *server) handleImportForm(w http.ResponseWriter, r *http.Request, u store.User) {
	s.importPage(w, r, u, http.StatusOK, ImportForm{}, ImportForm{})
}

// importPage renders the import page with the given state of each form.
func (s *server) importPage(w http.ResponseWriter, r *http.Request, u store.User, status int, folder, upload ImportForm) {
	inbox, err := s.worlds.Inbox()
	if err != nil {
		s.fail(w, r, "list import folder", err)
		return
	}
	// A fresh form with one folder to choose starts with it chosen, named
	// after it, and filled in from its save.
	if only, ok := onlyImportable(inbox); ok && status == http.StatusOK && folder == (ImportForm{}) {
		folder = ImportForm{Source: only.Name, Name: only.Name}
		if only.Read {
			folder.GameVersion, folder.SpaceAge = only.GameVersion, only.SpaceAge
		}
	}
	folder.IDPrefix, upload.IDPrefix = "folder", "upload"
	s.page(w, r, status, u.Username, ImportPage{
		InboxDir: s.worlds.Dirs().Imports,
		Inbox:    inbox,
		Folder:   folder,
		Upload:   upload,
	})
}

// onlyImportable returns the one inbox folder that can be imported, when
// there is exactly one.
func onlyImportable(inbox []world.InboxFolder) (world.InboxFolder, bool) {
	var only world.InboxFolder
	n := 0
	for _, f := range inbox {
		if f.Importable() {
			only, n = f, n+1
		}
	}
	return only, n == 1
}

// importFields reads and checks the fields both import forms share.
func importFields(name, version, spaceAge string) ImportForm {
	f := ImportForm{Name: name, GameVersion: strings.TrimSpace(version), SpaceAge: spaceAge == "on"}
	if strings.TrimSpace(name) == "" {
		f.NameError = sentence(world.ErrNameEmpty)
	}
	// An empty version is filled in from the save later (fillFromSave).
	if f.GameVersion != "" && !world.SupportedGameVersion(f.GameVersion) {
		f.VersionError = sentence(world.ErrBadGameVersion)
	}
	return f
}

// fillFromSave takes the version from the save when the admin left it
// empty. Space Age then is on if the save uses it or the box is ticked. A
// save that could not be read leaves the version required.
func (f *ImportForm) fillFromSave(info world.SaveInfo, ok bool) {
	if f.GameVersion != "" || f.VersionError != "" {
		return
	}
	if !ok {
		f.VersionError = sentence(world.ErrBadGameVersion)
		return
	}
	f.GameVersion = info.GameVersion
	f.SpaceAge = f.SpaceAge || info.SpaceAge
}

func (f ImportForm) failed() bool {
	return f.SourceError != "" || f.NameError != "" || f.VersionError != ""
}

// place puts a refusal's words on the field it is about.
func (f *ImportForm) place(err error, msg string) {
	switch {
	case errors.Is(err, world.ErrNameEmpty), errors.Is(err, world.ErrNameTaken):
		f.NameError = msg
	case errors.Is(err, world.ErrBadGameVersion):
		f.VersionError = msg
	default:
		f.SourceError = msg
	}
}

// handleImportFolder copies an inbox folder into a new world (SPEC 5.5).
func (s *server) handleImportFolder(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	f := importFields(r.PostFormValue("name"), chosenVersion(r.PostFormValue("game_version"), r.PostFormValue("game_version_typed")), r.PostFormValue("space_age"))
	f.Source = r.PostFormValue("folder")
	if f.Source == "" {
		f.SourceError = sentence(world.ErrNoFolder)
	}
	if f.GameVersion == "" && f.Source != "" {
		// A folder that is gone is reported by the import below.
		folder, err := s.worlds.InboxEntry(f.Source)
		if err != nil && !errors.Is(err, world.ErrNoFolder) {
			s.fail(w, r, "read import folder", err)
			return
		}
		f.fillFromSave(world.SaveInfo{GameVersion: folder.GameVersion, SpaceAge: folder.SpaceAge}, folder.Read)
	}
	if f.failed() {
		s.importPage(w, r, u, http.StatusBadRequest, f, ImportForm{})
		return
	}
	id, err := s.worlds.ImportFolder(r.Context(), f.Source, f.Name, f.GameVersion, f.SpaceAge)
	if err != nil {
		status, msg, ok := s.refusal(w, r, u, "import folder", err)
		if !ok {
			return
		}
		f.place(err, msg)
		s.importPage(w, r, u, status, f, ImportForm{})
		return
	}
	http.Redirect(w, r, worldPath(id), http.StatusSeeOther)
}

// handleImportUpload makes a new world from one uploaded save (SPEC 5.5).
// A refused upload leaves no world. Syncing the save's mods from the
// portal comes with Epic 5.
func (s *server) handleImportUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	id, f, err := s.receiveImport(r)
	switch {
	case err != nil:
		status, msg, ok := s.refusal(w, r, u, "import upload", err)
		if !ok {
			return
		}
		f.place(err, msg)
		s.importPage(w, r, u, status, ImportForm{}, f)
	case f.failed():
		s.importPage(w, r, u, http.StatusBadRequest, ImportForm{}, f)
	default:
		http.Redirect(w, r, worldPath(id), http.StatusSeeOther)
	}
}

// receiveImport receives the upload form into a temp file in the worlds
// folder and makes the world. It returns the new world's id, or the form
// with its field errors, or an error.
func (s *server) receiveImport(r *http.Request) (string, ImportForm, error) {
	worldsDir := s.worlds.Dirs().Worlds
	root, err := s.worlds.OpenWorlds()
	if err != nil {
		return "", ImportForm{}, err
	}
	defer func() { _ = root.Close() }()

	values := map[string]string{"name": "", "game_version": "", "game_version_typed": "", "space_age": ""}
	up, err := receiveFileWithFields(r, root, ".", "save", worldsDir, MaxSaveUpload, values)
	f := importFields(values["name"], chosenVersion(values["game_version"], values["game_version_typed"]), values["space_age"])
	if err != nil {
		return "", f, err
	}
	f.Source = up.FileName
	if !world.ValidSaveName(up.FileName) {
		f.SourceError = "Choose a save file whose name ends in .zip and does not start with a dot."
	} else if err := checkZip(root, up.Temp, maxZipRatio*uint64(up.Size)); err != nil { //nolint:gosec // a size is never negative
		_ = root.Remove(up.Temp)
		return "", f, err
	}
	if f.SourceError == "" {
		info, ok := world.ReadSaveInfo(root, up.Temp)
		f.fillFromSave(info, ok)
	}
	if f.failed() {
		_ = root.Remove(up.Temp)
		return "", f, nil
	}
	// ImportSave removes the temp file whatever happens.
	id, err := s.worlds.ImportSave(r.Context(), f.Name, f.GameVersion, f.SpaceAge, up.FileName, up.Temp)
	if err != nil {
		return "", f, err
	}
	return id, f, nil
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
	s.page(w, r, http.StatusOK, u.Username, DeletePage{World: sum, FinalSnapshot: true, Busy: s.worldBusy(r.Context(), sum.ID)})
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
		Busy:          s.worldBusy(r.Context(), sum.ID),
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
