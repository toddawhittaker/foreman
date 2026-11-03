package web

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

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
	s.handleUpload("POST "+PathImportUpload, MaxSaveUpload, s.requireUser(s.handleImportUpload))
}

// WorldPage is one world's page (SPEC 5.4, 5.8; DESIGN 8.2).
type WorldPage struct {
	World world.Summary
	// Name refills the rename field.
	Name      string
	NameError string
}

// ImportPage offers both ways to import a world (SPEC 5.5).
type ImportPage struct {
	InboxDir string
	Inbox    []world.InboxFolder
	Folder   ImportForm
	Upload   ImportForm
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
	s.page(w, r, http.StatusOK, u.Username, WorldsPage{Worlds: list})
}

// findWorld returns the world with the path's id, or sends the 404 page.
func (s *server) findWorld(w http.ResponseWriter, r *http.Request) (world.Summary, bool) {
	id := r.PathValue("id")
	list, err := s.worlds.List(r.Context())
	if err != nil {
		s.fail(w, r, "list worlds", err)
		return world.Summary{}, false
	}
	for _, sum := range list {
		if sum.ID == id {
			return sum, true
		}
	}
	s.page(w, r, http.StatusNotFound, "", ErrorPage{Status: http.StatusNotFound})
	return world.Summary{}, false
}

func (s *server) handleWorld(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, WorldPage{World: sum, Name: sum.Name})
}

// handleRename changes a world's display name only (SPEC 5.8). It is not a
// risky operation, so it takes no snapshot (SPEC 6.2).
func (s *server) handleRename(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	err := s.worlds.Rename(r.Context(), sum.ID, name)
	switch {
	case err == nil:
		http.Redirect(w, r, worldPath(sum.ID), http.StatusSeeOther)
	case errors.Is(err, world.ErrNameEmpty), errors.Is(err, world.ErrNameTaken),
		errors.Is(err, world.ErrNewerFormat), errors.Is(err, world.ErrBusy):
		s.page(w, r, http.StatusBadRequest, u.Username, WorldPage{World: sum, Name: name, NameError: sentence(err)})
	case errors.Is(err, fs.ErrNotExist):
		s.page(w, r, http.StatusNotFound, "", ErrorPage{Status: http.StatusNotFound})
	default:
		s.fail(w, r, "rename world", err)
	}
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
	folder.IDPrefix, upload.IDPrefix = "folder", "upload"
	s.page(w, r, status, u.Username, ImportPage{
		InboxDir: s.worlds.Dirs().Imports,
		Inbox:    inbox,
		Folder:   folder,
		Upload:   upload,
	})
}

// importFields reads and checks the fields both import forms share.
func importFields(name, version, spaceAge string) ImportForm {
	f := ImportForm{Name: name, GameVersion: strings.TrimSpace(version), SpaceAge: spaceAge == "on"}
	if strings.TrimSpace(name) == "" {
		f.NameError = sentence(world.ErrNameEmpty)
	}
	if !world.ValidGameVersion(f.GameVersion) {
		f.VersionError = sentence(world.ErrBadGameVersion)
	}
	return f
}

func (f ImportForm) failed() bool {
	return f.SourceError != "" || f.NameError != "" || f.VersionError != ""
}

// placeError puts a refusal from the world manager on the field it is
// about. It returns false for an error that is not the admin's to fix.
func (f *ImportForm) placeError(err error) bool {
	switch {
	case errors.Is(err, world.ErrNameEmpty), errors.Is(err, world.ErrNameTaken):
		f.NameError = sentence(err)
	case errors.Is(err, world.ErrBadGameVersion):
		f.VersionError = sentence(err)
	case errors.Is(err, world.ErrNoFolder), errors.Is(err, world.ErrNoSaves), errors.Is(err, world.ErrNoSpace):
		f.SourceError = sentence(err)
	default:
		var ue uploadError
		if !errors.As(err, &ue) {
			return false
		}
		f.SourceError = ue.msg
	}
	return true
}

// handleImportFolder copies an inbox folder into a new world (SPEC 5.5).
func (s *server) handleImportFolder(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	f := importFields(r.PostFormValue("name"), r.PostFormValue("game_version"), r.PostFormValue("space_age"))
	f.Source = r.PostFormValue("folder")
	if f.Source == "" {
		f.SourceError = sentence(world.ErrNoFolder)
	}
	if f.failed() {
		s.importPage(w, r, u, http.StatusBadRequest, f, ImportForm{})
		return
	}
	id, err := s.worlds.ImportFolder(r.Context(), f.Source, f.Name, f.GameVersion, f.SpaceAge)
	if err != nil {
		if !f.placeError(err) {
			s.fail(w, r, "import folder", err)
			return
		}
		s.importPage(w, r, u, http.StatusBadRequest, f, ImportForm{})
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
	case err != nil && !f.placeError(err):
		s.fail(w, r, "import upload", err)
	case err != nil || f.failed():
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
	root, err := os.OpenRoot(worldsDir)
	if err != nil {
		return "", ImportForm{}, err
	}
	defer func() { _ = root.Close() }()

	values := map[string]string{"name": "", "game_version": "", "space_age": ""}
	up, err := receiveFileWithFields(r, root, ".", "save", worldsDir, MaxSaveUpload, values)
	f := importFields(values["name"], values["game_version"], values["space_age"])
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
}

// DeletedPage lists deleted worlds, each with the snapshots that remain
// (SPEC 6.6, DESIGN 8.3).
type DeletedPage struct {
	Worlds []world.DeletedWorld
	// More is true when rows past maxListRows are hidden for some world.
	More bool
	// Done says what the delete that led here did.
	Done string
}

func (DeletePage) templateName() string  { return "delete" }
func (DeletedPage) templateName() string { return "deleted" }
func (DeletePage) navItem() string       { return PathWorlds }
func (DeletedPage) navItem() string      { return PathWorlds }

func (p DeletePage) hasErrors() bool { return p.NameError != "" || p.Error != "" }

// Problems lists the name field's error for the summary.
func (p DeletePage) Problems() []Problem { return problems(Problem{"confirm-name", p.NameError}) }

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

// Reason says why a snapshot was taken, in words.
func (DeletedPage) Reason(reason string) string { return reasonText(reason) }

func (s *server) handleDeleteForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, DeletePage{World: sum, FinalSnapshot: true})
}

// handleDelete deletes a world after its final snapshot (SPEC 5.10). A
// refusal shows the confirmation page again with the reason.
func (s *server) handleDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r)
	if !ok {
		return
	}
	p := DeletePage{
		World:         sum,
		FinalSnapshot: r.PostFormValue("final_snapshot") == "on",
		ConfirmName:   r.PostFormValue("confirm_name"),
	}
	err := s.worlds.Delete(r.Context(), sum.ID, p.ConfirmName, p.FinalSnapshot, u.Username)
	if err == nil {
		http.Redirect(w, r, PathDeleted+"?deleted="+sum.ID, http.StatusSeeOther)
		return
	}
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, world.ErrWrongName):
		p.NameError = "Type " + sum.Name + " exactly, or cancel"
	case errors.Is(err, fs.ErrNotExist):
		s.page(w, r, http.StatusNotFound, u.Username, ErrorPage{Status: http.StatusNotFound})
		return
	default:
		var msg string
		if status, msg = actionError(err); status == http.StatusInternalServerError {
			s.fail(w, r, "delete world", err)
			return
		}
		p.Error = msg
	}
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
	if id := r.URL.Query().Get("deleted"); world.ValidID(id) {
		// The message is only true while the world is gone.
		_, found, err := s.worldName(id)
		if err != nil {
			s.fail(w, r, "read world", err)
			return
		}
		if !found {
			p.Done = deletedMessage(id, list)
		}
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// deletedMessage says what deleting world id did. A world that kept no
// snapshot is not in the list, so it is named by its id.
func deletedMessage(id string, list []world.DeletedWorld) string {
	for _, d := range list {
		if d.ID == id {
			return fmt.Sprintf("The world \"%s\" is deleted. Its snapshots stay, listed below, and can be restored.", d.Name)
		}
	}
	return fmt.Sprintf("The world %s is deleted. It had no snapshots, so it cannot be restored.", id)
}

// sentence turns a world error into a sentence for a field error.
func sentence(err error) string {
	msg := err.Error()
	r, n := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[n:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}
