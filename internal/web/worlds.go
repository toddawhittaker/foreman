package web

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
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
)

// maxZipRatio caps a zip's stated unpacked size at this many times its
// own size (the Epic 2 zip check).
const maxZipRatio = 50

func worldPath(id string) string { return PathWorlds + "/" + id }

func (s *server) worldRoutes() {
	s.mux.HandleFunc("GET "+PathWorlds, s.requireUser(s.handleWorlds))
	s.mux.HandleFunc("GET "+PathWorlds+"/{id}", s.requireUser(s.handleWorld))
	s.mux.HandleFunc("POST "+PathWorlds+"/{id}/rename", s.requireUser(s.handleRename))
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
// The save lands in a staging folder, so a refused upload leaves no world.
// Syncing the save's mods from the portal comes with Epic 5.
func (s *server) handleImportUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	staging, err := s.worlds.NewStaging()
	if err != nil {
		s.fail(w, r, "make staging folder", err)
		return
	}
	id, f, err := s.receiveImport(r, staging)
	if id == "" {
		// Removing an already-removed staging folder is harmless.
		if derr := s.worlds.DiscardStaging(staging); derr != nil && err == nil {
			err = derr
		}
	}
	switch {
	case err != nil && !f.placeError(err):
		s.fail(w, r, "import upload", err)
	case err != nil || f.failed():
		s.importPage(w, r, u, http.StatusBadRequest, ImportForm{}, f)
	default:
		http.Redirect(w, r, worldPath(id), http.StatusSeeOther)
	}
}

// receiveImport receives the upload form into staging and makes the world.
// It returns the new world's id, or the form with its field errors, or an
// error.
func (s *server) receiveImport(r *http.Request, staging string) (string, ImportForm, error) {
	worldsDir := s.worlds.Dirs().Worlds
	root, err := os.OpenRoot(worldsDir)
	if err != nil {
		return "", ImportForm{}, err
	}
	defer func() { _ = root.Close() }()

	values := map[string]string{"name": "", "game_version": "", "space_age": ""}
	saves := path.Join(staging, world.SavesDir)
	up, err := receiveFileWithFields(r, root, saves, "save", worldsDir, MaxSaveUpload, values)
	f := importFields(values["name"], values["game_version"], values["space_age"])
	if err != nil {
		return "", f, err
	}
	f.Source = up.FileName
	if !world.ValidSaveName(up.FileName) {
		f.SourceError = "Choose a save file whose name ends in .zip and does not start with a dot."
	} else if err := checkZip(root, up.Temp, maxZipRatio*uint64(up.Size)); err != nil { //nolint:gosec // a size is never negative
		return "", f, err
	}
	if f.failed() {
		return "", f, nil
	}
	if err := world.Commit(root, up.Temp, path.Join(saves, up.FileName)); err != nil {
		return "", f, err
	}
	id, err := s.worlds.CreateFromStaging(r.Context(), staging, f.Name, f.GameVersion, f.SpaceAge)
	if err != nil {
		return "", f, err
	}
	return id, f, nil
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
