package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Import paths (SPEC 5.5).
const (
	PathImport       = "/import"
	PathImportFolder = "/import/folder"
	PathImportUpload = "/import/upload"
)

// Query keys the world page reads after an import.
const (
	importedQuery  = "imported"
	uncheckedQuery = "unchecked"
)

// folderQuery pre-ticks one inbox folder on the Import page.
const folderQuery = "folder"

// ImportPage offers both ways to import a world (SPEC 5.5).
type ImportPage struct {
	// InboxDir is the import folder as the admin reaches it: on the Docker
	// host when that path is known (SPEC 3.5).
	InboxDir string
	Inbox    []world.InboxFolder
	// Missing holds the game versions whose image this host lacks.
	Missing map[string]bool
	Folder  ImportForm
	Upload  ImportForm
	// Result says what a bulk import did; nil otherwise.
	Result *BulkResult
}

// ImportRow is one importable inbox folder as the form shows it.
type ImportRow struct {
	world.InboxFolder
	ID           string
	Checked      bool
	Invalid      bool
	MissingImage bool
}

// WorldPath links to the world the folder was already imported as.
func (r ImportRow) WorldPath() string {
	if r.Imported == nil {
		return ""
	}
	return worldPath(r.Imported.WorldID)
}

// BulkResult reports an import of several folders, folder by folder.
type BulkResult struct {
	Imported []BulkImported
	Skipped  []BulkSkipped
	// Downloads are the game versions whose image is being downloaded.
	Downloads []string
}

// BulkImported is one folder that became a world.
type BulkImported struct{ Folder, WorldID, WorldName string }

// BulkSkipped is one folder that was not imported, and why. Alone is true
// when importing the folder on its own can fix it (SPEC 17.9).
type BulkSkipped struct {
	Folder, Reason string
	Alone          bool
}

// AlonePath is the import form with only this folder ticked.
func (b BulkSkipped) AlonePath() string {
	return PathImport + "?" + folderQuery + "=" + url.QueryEscape(b.Folder)
}

// ImportForm is one of the two import forms. Source is the uploaded file;
// Folders are the ticked inbox folders.
type ImportForm struct {
	Source       string
	Folders      []string
	Name         string
	GameVersion  string
	SpaceAge     bool
	SourceError  string
	NameError    string
	VersionError string
	// SpaceAgeError says why Space Age cannot be on for this version.
	SpaceAgeError string
	// SaveRead, SaveVersion and SaveSpaceAge describe the one chosen
	// folder's save, for "Same as the save" and the Space Age warning.
	SaveRead     bool
	SaveVersion  string
	SaveSpaceAge bool
	// CanDownload offers the image download; Download is whether it is
	// ticked (SPEC 3.7).
	CanDownload bool
	Download    bool
	// AskVersion asks for the typed version and puts focus there: the folder
	// ticked by a link has a save whose version Foreman could not read.
	AskVersion bool
	// IDPrefix keeps the two forms' field ids apart on one page.
	IDPrefix string
}

func (ImportPage) templateName() string { return "import" }
func (ImportPage) navItem() string      { return PathWorlds }
func (p ImportPage) hasErrors() bool    { return len(p.Folder.Problems())+len(p.Upload.Problems()) > 0 }

// FolderPath and UploadPath are where the two forms post.
func (ImportPage) FolderPath() string { return PathImportFolder }
func (ImportPage) UploadPath() string { return PathImportUpload }

// WorldPath links to a world, for the already imported folders.
func (ImportPage) WorldPath(id string) string { return worldPath(id) }

// Rows lists the folders that can be imported: those waiting first, then
// those already imported, numbered in that order (SPEC 5.5).
func (p ImportPage) Rows() (waiting, imported []ImportRow) {
	for _, f := range p.Inbox {
		if !f.Importable() {
			continue
		}
		row := ImportRow{InboxFolder: f, Checked: slices.Contains(p.Folder.Folders, f.Name),
			Invalid: p.Folder.SourceError != "", MissingImage: f.Read && p.Missing[f.GameVersion]}
		if f.Imported == nil {
			waiting = append(waiting, row)
		} else {
			imported = append(imported, row)
		}
	}
	for i := range waiting {
		waiting[i].ID = fmt.Sprintf("folder-source-%d", i)
	}
	for i := range imported {
		imported[i].ID = fmt.Sprintf("folder-source-%d", len(waiting)+i)
	}
	return waiting, imported
}

// Waiting lists the folders not yet imported.
func (p ImportPage) Waiting() []ImportRow { w, _ := p.Rows(); return w }

// Imported lists the folders already imported with their newest save.
func (p ImportPage) Imported() []ImportRow { _, i := p.Rows(); return i }

// Unimportable lists the folders that cannot be imported.
func (p ImportPage) Unimportable() []world.InboxFolder {
	var out []world.InboxFolder
	for _, f := range p.Inbox {
		if !f.Importable() {
			out = append(out, f)
		}
	}
	return out
}

// Importable reports whether any inbox folder can be imported.
func (p ImportPage) Importable() bool {
	return slices.ContainsFunc(p.Inbox, world.InboxFolder.Importable)
}

// SpaceAgeWarning says what the Space Age choice does to the chosen
// folder's save when it differs from how the save was made, or "".
func (f ImportForm) SpaceAgeWarning() string {
	if !f.SaveRead || f.SpaceAge == f.SaveSpaceAge {
		return ""
	}
	if f.SpaceAge {
		return spaceAgeOnWarning
	}
	return spaceAgeOffWarning
}

// The Space Age warnings. The page hands them to import.js as data
// attributes, so the words live only here.
const (
	spaceAgeOnWarning  = "The save was made without Space Age. Factorio converts it to Space Age when it first starts, and that cannot be undone."
	spaceAgeOffWarning = "The save was made with Space Age. Without it, Factorio may not load the save, or may remove its Space Age content."
)

// SpaceAgeOnWarning, SpaceAgeOffWarning and SpaceAgeOldWarning are the three
// words import.js can show.
func (ImportForm) SpaceAgeOnWarning() string  { return spaceAgeOnWarning }
func (ImportForm) SpaceAgeOffWarning() string { return spaceAgeOffWarning }
func (ImportForm) SpaceAgeOldWarning() string { return sentence(world.ErrSpaceAgeOld) }

// Problems lists the form's field errors in the order the fields appear.
// The upload form's file field comes last, so its text fields are read
// before the file (see receiveFileWithFields).
func (f ImportForm) Problems() []Problem {
	if f.IDPrefix == "upload" {
		return problems(
			Problem{"upload-name", i18n.Raw(f.NameError)},
			Problem{"upload-version", i18n.Raw(f.VersionError)},
			Problem{"upload-space-age", i18n.Raw(f.SpaceAgeError)},
			Problem{"upload-source", i18n.Raw(f.SourceError)},
		)
	}
	return problems(
		Problem{"folder-source-0", i18n.Raw(f.SourceError)},
		Problem{"folder-name", i18n.Raw(f.NameError)},
		Problem{"folder-version", i18n.Raw(f.VersionError)},
		Problem{"folder-space-age", i18n.Raw(f.SpaceAgeError)},
	)
}

func (s *server) handleImportForm(w http.ResponseWriter, r *http.Request, u store.User) {
	folder := ImportForm{Download: true}
	// A link from a skipped folder ticks it. A name that is not an importable
	// inbox folder ticks nothing.
	if name := r.URL.Query().Get(folderQuery); name != "" {
		f, err := s.worlds.InboxEntry(name)
		if err != nil && !errors.Is(err, world.ErrNoFolder) {
			s.fail(w, r, "read import folder", err)
			return
		}
		if err == nil && f.Importable() {
			folder.Folders, folder.Name, folder.SpaceAge = []string{f.Name}, f.SuggestedName(), f.SpaceAge
			folder.AskVersion = !f.Read
		}
	}
	s.importPage(w, r, u, http.StatusOK, folder, ImportForm{Download: true}, nil)
}

// importPage renders the import page with the given state of each form.
func (s *server) importPage(w http.ResponseWriter, r *http.Request, u store.User, status int, folder, upload ImportForm, result *BulkResult) {
	inbox, err := s.worlds.Inbox(r.Context())
	if err != nil {
		s.fail(w, r, "list import folder", err)
		return
	}
	// A fresh form with one folder waiting starts with it chosen and named.
	if only, ok := onlyWaiting(inbox); ok && status == http.StatusOK && result == nil && len(folder.Folders) == 0 {
		folder.Folders, folder.Name, folder.SpaceAge = []string{only.Name}, only.SuggestedName(), only.SpaceAge
	}
	if len(folder.Folders) == 1 {
		for _, f := range inbox {
			if f.Name == folder.Folders[0] {
				folder.SaveRead, folder.SaveVersion, folder.SaveSpaceAge = f.Read, f.GameVersion, f.SpaceAge
			}
		}
	}
	folder.IDPrefix, upload.IDPrefix = "folder", "upload"
	folder.CanDownload, upload.CanDownload = s.game != nil, s.game != nil
	dir := s.worlds.Dirs().Imports
	if s.hostData != "" {
		dir = datadir.Imports(s.hostData)
	}
	s.page(w, r, status, u.Username, ImportPage{
		InboxDir: dir,
		Inbox:    inbox,
		Missing:  s.missingImages(r.Context(), inbox),
		Folder:   folder,
		Upload:   upload,
		Result:   result,
	})
}

// missingImages asks Docker which inbox folders' game versions have no
// image here. Without server control, or when Docker cannot answer, none
// are marked.
func (s *server) missingImages(ctx context.Context, inbox []world.InboxFolder) map[string]bool {
	out := map[string]bool{}
	if s.game == nil {
		return out
	}
	for _, f := range inbox {
		if !f.Read || !world.SupportedGameVersion(f.GameVersion) {
			continue
		}
		if _, asked := out[f.GameVersion]; asked {
			continue
		}
		has, err := s.game.HasImage(ctx, f.GameVersion)
		out[f.GameVersion] = err == nil && !has
	}
	return out
}

// onlyWaiting returns the one inbox folder waiting to be imported, when
// there is exactly one.
func onlyWaiting(inbox []world.InboxFolder) (world.InboxFolder, bool) {
	var only world.InboxFolder
	n := 0
	for _, f := range inbox {
		if f.Importable() && f.Imported == nil {
			only, n = f, n+1
		}
	}
	return only, n == 1
}

// importFields reads and checks the fields both import forms share.
func importFields(name, version, spaceAge, download string) ImportForm {
	f := ImportForm{Name: name, GameVersion: strings.TrimSpace(version), SpaceAge: spaceAge == "on", Download: download == "on"}
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

// checkSpaceAge refuses Space Age for a version before 2.0.
func (f *ImportForm) checkSpaceAge() {
	if f.SpaceAge && f.VersionError == "" && strings.HasPrefix(f.GameVersion, "1.") {
		f.SpaceAgeError = sentence(world.ErrSpaceAgeOld)
	}
}

func (f ImportForm) failed() bool {
	return f.SourceError != "" || f.NameError != "" || f.VersionError != "" || f.SpaceAgeError != ""
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

// checkTypedVersion applies to a version the admin chose rather than the
// save's: it must be a downloaded image or on Docker Hub's list. When Docker
// Hub cannot be asked, it is let through and unchecked is true.
func (s *server) checkTypedVersion(ctx context.Context, f *ImportForm) (unchecked bool) {
	v := f.GameVersion
	if s.game != nil {
		if has, err := s.game.HasImage(ctx, v); err == nil && has {
			return false
		}
	}
	if s.releases == nil {
		return true
	}
	versions, err := s.releases.Versions(ctx)
	if err != nil {
		return true
	}
	if !slices.Contains(versions, v) {
		f.VersionError = "Factorio " + v + " is not a downloaded game image and is not on Docker Hub's list of versions. Check the number."
	}
	return false
}

// handleImportFolder copies the ticked inbox folders into new worlds
// (SPEC 5.5). One folder uses the form's fields; several use each folder's
// own name and save.
func (s *server) handleImportFolder(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	folders := r.PostForm["folder"]
	if len(folders) > 1 {
		res := s.importMany(r.Context(), folders, r.PostFormValue("download") == "on", actor(u))
		s.importPage(w, r, u, http.StatusOK, ImportForm{Download: true}, ImportForm{Download: true}, res)
		return
	}
	typed := chosenVersion(r.PostFormValue("game_version"), r.PostFormValue("game_version_typed"))
	f := importFields(r.PostFormValue("name"), typed, r.PostFormValue("space_age"), r.PostFormValue("download"))
	f.Folders = folders
	if len(folders) == 0 {
		f.SourceError = sentence(world.ErrNoFolder)
	}
	if f.GameVersion == "" && len(folders) == 1 {
		// A folder that is gone is reported by the import below.
		folder, err := s.worlds.InboxEntry(folders[0])
		if err != nil && !errors.Is(err, world.ErrNoFolder) {
			s.fail(w, r, "read import folder", err)
			return
		}
		f.fillFromSave(world.SaveInfo{GameVersion: folder.GameVersion, SpaceAge: folder.SpaceAge}, folder.Read)
	}
	f.checkSpaceAge()
	unchecked := false
	if typed != "" && !f.failed() {
		unchecked = s.checkTypedVersion(r.Context(), &f)
	}
	if f.failed() {
		s.importPage(w, r, u, http.StatusBadRequest, f, ImportForm{Download: true}, nil)
		return
	}
	id, err := s.worlds.ImportFolder(r.Context(), folders[0], f.Name, f.GameVersion, f.SpaceAge)
	if err != nil {
		status, msg, ok := s.refusal(w, r, u, "import folder", err)
		if !ok {
			return
		}
		f.place(err, msg)
		s.importPage(w, r, u, status, f, ImportForm{Download: true}, nil)
		return
	}
	if f.Download {
		s.downloadFor(r.Context(), map[string]string{f.GameVersion: id}, actor(u))
	}
	http.Redirect(w, r, importedPath(id, unchecked), http.StatusSeeOther) //nolint:gosec // id is a world id Foreman made, a local path segment
}

// importedPath is the new world's page, with its Done message and, when
// the version could not be checked, that warning.
func importedPath(id string, unchecked bool) string {
	p := worldPath(id) + "?" + importedQuery + "=1"
	if unchecked {
		p += "&" + uncheckedQuery + "=1"
	}
	return p
}

// importMany imports each folder as its own world, named after the folder,
// with its save's version and Space Age (SPEC 5.5). A folder that cannot
// be imported is skipped with its reason; the others go ahead.
func (s *server) importMany(ctx context.Context, folders []string, download bool, by game.Actor) *BulkResult {
	res := &BulkResult{}
	firstOf := map[string]string{} // game version to the first world made with it
	for _, name := range folders {
		skip := func(reason string, alone bool) { res.Skipped = append(res.Skipped, BulkSkipped{name, reason, alone}) }
		f, err := s.worlds.InboxEntry(name)
		switch {
		case errors.Is(err, world.ErrNoFolder):
			skip("It is no longer in the import folder.", false)
			continue
		case err != nil:
			s.logUnlessCancelled("read import folder", name, err)
			skip("Foreman could not read it.", false)
			continue
		case f.Clash != nil:
			skip(sentence(f.Clash), false)
			continue
		case f.Saves == 0:
			skip(sentence(world.ErrNoSaves), false)
			continue
		case !f.Read:
			skip("Foreman could not read its save, so its game version is unknown. Import it on its own and enter the version.", true)
			continue
		}
		id, err := s.worlds.ImportFolder(ctx, name, name, f.GameVersion, f.SpaceAge)
		if err != nil {
			status, msg := worldError(err)
			if status == http.StatusInternalServerError {
				s.logUnlessCancelled("import folder", name, err)
				msg = "Foreman could not import it."
			}
			skip(msg, true)
			continue
		}
		res.Imported = append(res.Imported, BulkImported{Folder: name, WorldID: id, WorldName: name})
		if _, ok := firstOf[f.GameVersion]; !ok {
			firstOf[f.GameVersion] = id
		}
	}
	if download {
		res.Downloads = s.downloadFor(ctx, firstOf, by)
	}
	return res
}

// logUnlessCancelled logs a bulk import failure at ERROR. A browser that left
// mid-request is not a server fault, so a cancelled request logs nothing.
func (s *server) logUnlessCancelled(what, folder string, err error) {
	if !errors.Is(err, context.Canceled) {
		s.log.Error(what, "folder", folder, "error", err)
	}
}

// downloadFor starts one image download for each version this host lacks,
// through one world that runs it (SPEC 3.7). It reuses the world's own
// download action, so it is audited and shown like one. It returns the
// versions it started, sorted by version.
func (s *server) downloadFor(ctx context.Context, worlds map[string]string, by game.Actor) []string {
	if s.game == nil {
		return nil
	}
	var started []string
	for version, id := range worlds {
		if has, err := s.game.HasImage(ctx, version); err != nil || has {
			continue
		}
		if s.runs.runOn([]string{id}, false, func(ctx context.Context) actionResult {
			return s.finish(id, game.AuditImageDownload, s.game.PullImage(ctx, id, by))
		}) {
			started = append(started, version)
		}
	}
	slices.Sort(started)
	return started
}

// handleImportUpload makes a new world from one uploaded save (SPEC 5.5).
// A refused upload leaves no world. Syncing the save's mods from the
// portal comes with Epic 5.
func (s *server) handleImportUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	id, f, unchecked, err := s.receiveImport(r)
	switch {
	case err != nil:
		status, msg, ok := s.refusal(w, r, u, "import upload", err)
		if !ok {
			return
		}
		f.place(err, msg)
		s.importPage(w, r, u, status, ImportForm{Download: true}, f, nil)
	case f.failed():
		s.importPage(w, r, u, http.StatusBadRequest, ImportForm{Download: true}, f, nil)
	default:
		if f.Download {
			s.downloadFor(r.Context(), map[string]string{f.GameVersion: id}, actor(u))
		}
		http.Redirect(w, r, importedPath(id, unchecked), http.StatusSeeOther)
	}
}

// receiveImport receives the upload form into a temp file in the worlds
// folder and makes the world. It returns the new world's id, or the form
// with its field errors, or an error. unchecked is true when the typed
// version could not be checked against Docker Hub.
func (s *server) receiveImport(r *http.Request) (id string, f ImportForm, unchecked bool, err error) {
	worldsDir := s.worlds.Dirs().Worlds
	root, err := s.worlds.OpenWorlds()
	if err != nil {
		return "", ImportForm{}, false, err
	}
	defer func() { _ = root.Close() }()

	values := map[string]string{"name": "", "game_version": "", "game_version_typed": "", "space_age": "", "download": ""}
	up, err := receiveFileWithFields(r, root, ".", "save", worldsDir, MaxSaveUpload, values)
	typed := chosenVersion(values["game_version"], values["game_version_typed"])
	f = importFields(values["name"], typed, values["space_age"], values["download"])
	if err != nil {
		return "", f, false, err
	}
	f.Source = up.FileName
	if !world.ValidSaveName(up.FileName) {
		f.SourceError = "Choose a save file whose name ends in .zip and does not start with a dot."
	} else if err := checkZip(root, up.Temp, maxZipRatio*uint64(up.Size)); err != nil { //nolint:gosec // a size is never negative
		_ = root.Remove(up.Temp)
		return "", f, false, err
	}
	if f.SourceError == "" {
		info, ok := world.ReadSaveInfo(root, up.Temp)
		f.fillFromSave(info, ok)
	}
	f.checkSpaceAge()
	if typed != "" && !f.failed() {
		unchecked = s.checkTypedVersion(r.Context(), &f)
	}
	if f.failed() {
		_ = root.Remove(up.Temp)
		return "", f, false, nil
	}
	// ImportSave removes the temp file whatever happens.
	id, err = s.worlds.ImportSave(r.Context(), f.Name, f.GameVersion, f.SpaceAge, up.FileName, up.Temp)
	if err != nil {
		return "", f, false, err
	}
	return id, f, unchecked, nil
}

// importFacts adds to a world's page where it was imported from, the Done
// message and version warning after an import, and the state of an image
// download (SPEC 3.7, 5.5).
func (s *server) importFacts(p *WorldPage, r *http.Request) {
	q := r.URL.Query()
	if q.Get(importedQuery) != "" {
		p.Done = "Imported " + p.World.Name + "."
		if q.Get(uncheckedQuery) != "" {
			p.Warning = "Foreman could not reach Docker Hub to check that Factorio " + p.World.GameVersion +
				" exists. If it does not, the world will not start."
		}
	}
	if s.game == nil {
		return
	}
	d := s.game.Status(p.World.ID).Download
	switch {
	case d == nil:
	case !d.Done:
		p.Download = "running"
	case d.Error != "":
		// A later download, by a switch for one, may have got the image.
		if has, err := s.game.HasImage(r.Context(), p.World.GameVersion); err == nil && !has {
			p.Download = "failed"
		}
	}
}
