package web

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The world file import (SPEC 5.14): upload, a preview of what the file
// holds, then confirm. The upload is staged in the worlds folder, and the
// preview carries only the staged file's name in a hidden field, never in
// an address (SPEC 17.17).
const (
	// worldFileField is the file field of the upload form.
	worldFileField = "world_file"
	// worldFileZoneID is the file input's id.
	worldFileZoneID = "world-file"
	// worldFileStagedField is the hidden field that names the staged file.
	worldFileStagedField = "staged"
	// worldFileBodyRoom is what a request may hold beyond the file itself:
	// the form's headers and framing.
	worldFileBodyRoom = 64 << 10
	// maxNameRunes is the length the name field takes, as the other import
	// forms' name fields do.
	maxNameRunes = 100
	// maxStagedName caps a posted staged name.
	maxStagedName = 100
	// maxVersionText caps the Foreman version a file claims, which is shown
	// and audited.
	maxVersionText = 40
)

// WorldFilePath is where the Import page's world file form posts.
func (ImportPage) WorldFilePath() string { return PathWorldFileImport }

// WorldFileZone is the world file form's file field (DESIGN 7.10), with the
// refusal of the last upload when the form can fix it.
func (p ImportPage) WorldFileZone() DropZone {
	return DropZone{ID: worldFileZoneID, Name: worldFileField, Label: i18n.M("worldimport.zone.label"),
		Accept: ".zip", MaxBytes: zoneLimit(world.MaxWorldFile), Error: p.WorldFileError}
}

// WorldFileProblems is the world file form's error summary.
func (p ImportPage) WorldFileProblems() []Problem {
	return problems(Problem{worldFileZoneID, p.WorldFileError})
}

// WorldFilePage previews a staged world file and asks for the world's name
// before anything is made (SPEC 5.14).
type WorldFilePage struct {
	Info world.WorldFileInfo
	// Staged is the staged file's name, posted back by the confirm form.
	Staged string
	Name   string
	// NameError is a name that is empty or in use. Error is another refusal
	// the form can fix, such as too little disk space. The staged file is
	// kept for either.
	NameError i18n.Msg
	Error     i18n.Msg
	// CanDownload says Foreman can fetch game images; ImageHere says the
	// file's game version already has one; Download is the checkbox.
	CanDownload bool
	ImageHere   bool
	Download    bool
}

func (WorldFilePage) templateName() string { return "worldfile" }
func (WorldFilePage) navItem() string      { return PathWorlds }
func (p WorldFilePage) hasErrors() bool    { return !p.NameError.IsZero() || !p.Error.IsZero() }

// Problems lists the form's field errors.
func (p WorldFilePage) Problems() []Problem {
	return problems(Problem{"world-file-name", p.NameError})
}

// Path is where the preview posts; ImportPath is the Import page, which
// cancelling returns to.
func (WorldFilePage) Path() string       { return PathWorldFileConfirm }
func (WorldFilePage) ImportPath() string { return PathImport }

// StagedField is the hidden field's name.
func (WorldFilePage) StagedField() string { return worldFileStagedField }

// Size is the file's unpacked size, for the bytes formatter.
func (p WorldFilePage) Size() uint64 { return uint64(max(p.Info.Bytes, 0)) }

// ForemanVersion is the Foreman version that made the file, or "" when it
// did not say a version that is safe to show.
func (p WorldFilePage) ForemanVersion() string { return cleanVersion(p.Info.ForemanVersion) }

// cleanVersion keeps a version a file claims only when it looks like one:
// the file is untrusted, and the text is shown and audited.
func cleanVersion(v string) string {
	if v == "" || len(v) > maxVersionText {
		return ""
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && !strings.ContainsRune(".+-_", c) {
			return ""
		}
	}
	return v
}

// stagedName reports whether name could be a staged world file's name. The
// store checks it again; this refuses a name that is not one before it
// reaches a path.
func stagedName(name string) bool {
	return len(name) <= maxStagedName && strings.HasPrefix(name, world.PrefixWorldFile) &&
		!strings.ContainsAny(name, `/\`) && filepath.IsLocal(name)
}

func (s *server) worldImportRoutes() {
	s.handleUpload("POST "+PathWorldFileImport, fixedLimit(world.MaxWorldFile+worldFileBodyRoom),
		s.requireUser(s.guardUpload(s.handleWorldFileUpload)))
	// Extracting a large world takes a while, so the confirm is long too.
	s.handleLong("POST "+PathWorldFileConfirm, s.requireUser(s.handleWorldFileConfirm))
}

// worldFileLimits are the per-entry limits of an import: the save and mod
// upload settings (SPEC 14.6).
func (s *server) worldFileLimits(ctx context.Context) world.WorldFileLimits {
	return world.WorldFileLimits{Save: s.saveUploadLimit(ctx), Mod: s.modUploadLimit(ctx)}
}

// handleWorldFileUpload stages the uploaded file, reads what it holds and
// shows the preview. Nothing becomes a world yet (SPEC 5.14).
func (s *server) handleWorldFileUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	root, err := s.worlds.OpenWorlds()
	if err != nil {
		s.fail(w, r, "open worlds folder", err)
		return
	}
	defer func() { _ = root.Close() }()
	up, err := receiveInto(r, root, s.worlds.CreateWorldFileTemp, worldFileField, s.worlds.Dirs().Worlds, world.MaxWorldFile, map[string]string{})
	if err != nil {
		if msg := worldFileFormError(err); !msg.IsZero() {
			s.importPageWith(w, r, u, http.StatusUnprocessableEntity, ImportForm{Download: true}, ImportForm{Download: true}, nil, msg)
			return
		}
		s.worldFileRefused(w, r, u, "receive a world file", err)
		return
	}
	info, err := s.worlds.ReadWorldFile(up.Temp, s.worldFileLimits(r.Context()))
	if err != nil {
		_ = root.Remove(up.Temp)
		s.worldFileRefused(w, r, u, "read a world file", err)
		return
	}
	s.worldFilePreview(w, r, u, http.StatusOK, WorldFilePage{
		Info: info, Staged: up.Temp, Name: clipName(info.Name), Download: true,
	})
}

// worldFileFormError words the two upload refusals the person fixes on the
// Import page itself: no file chosen, and a file over the limit. It is zero
// for any other error.
func worldFileFormError(err error) i18n.Msg {
	ue, ok := errors.AsType[uploadError](err)
	if ok && (ue.msg.Key == "worlds.upload.choose_file" || ue.msg.Key == "worlds.upload.too_large") {
		return ue.msg
	}
	return i18n.Msg{}
}

// clipName cuts a world name from a file to the length the name field takes.
func clipName(name string) string {
	if utf8.RuneCountInString(name) <= maxNameRunes {
		return name
	}
	return string([]rune(name)[:maxNameRunes])
}

// worldFilePreview renders the preview with what this host knows about the
// file's game image.
func (s *server) worldFilePreview(w http.ResponseWriter, r *http.Request, u store.User, status int, p WorldFilePage) {
	p.CanDownload = s.game != nil
	if s.game != nil {
		has, err := s.game.HasImage(r.Context(), p.Info.GameVersion)
		p.ImageHere = err == nil && has
	}
	s.page(w, r, status, u.Username, p)
}

// handleWorldFileConfirm makes the world from the staged file under the
// name the person chose (SPEC 5.14). A name in use or too little disk space
// shows the preview again and keeps the staged file.
func (s *server) handleWorldFileConfirm(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	staged := r.PostFormValue(worldFileStagedField)
	if !stagedName(staged) || !s.stagedExists(staged) {
		s.worldFileGone(w, r, u)
		return
	}
	lim := s.worldFileLimits(r.Context())
	info, err := s.worlds.ReadWorldFile(staged, lim)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.worldFileGone(w, r, u)
		return
	case err != nil:
		s.removeStagedWorldFile(staged)
		s.worldFileRefused(w, r, u, "read a world file", err)
		return
	}
	name := r.PostFormValue("name")
	p := WorldFilePage{Info: info, Staged: staged, Name: name, Download: r.PostFormValue("download") == "on"}
	if strings.TrimSpace(name) == "" {
		p.NameError = msgNameEmpty()
		s.worldFilePreview(w, r, u, http.StatusBadRequest, p)
		return
	}
	id, err := s.worlds.ImportWorldFile(r.Context(), staged, name, lim)
	if err != nil {
		s.worldFileImportFailed(w, r, u, p, err)
		return
	}
	detail := "world file"
	if v := cleanVersion(info.ForemanVersion); v != "" {
		detail = "world file made by Foreman " + v
	}
	s.audit(r.Context(), actor(u), AuditWorldImport, id, detail)
	if p.Download && s.game != nil {
		s.downloadFor(r.Context(), map[string]string{info.GameVersion: id}, actor(u))
	}
	http.Redirect(w, r, importedPath(id, false), http.StatusSeeOther)
}

// worldFileImportFailed answers a refused import: the preview again for a
// name or disk space the person can fix, otherwise a page of its own.
func (s *server) worldFileImportFailed(w http.ResponseWriter, r *http.Request, u store.User, p WorldFilePage, err error) {
	status, msg := worldError(err)
	_, noSpace := errors.AsType[*world.NoSpaceError](err)
	switch {
	case errors.Is(err, world.ErrNameTaken), errors.Is(err, world.ErrNameEmpty):
		p.NameError = msg
	case noSpace:
		p.Error = msg
	default:
		s.worldFileRefused(w, r, u, "import a world file", err)
		return
	}
	s.worldFilePreview(w, r, u, status, p)
}

// worldFileRefused answers a refusal that ends the attempt with a page that
// says why and offers the Import page again. The file's own content never
// reaches the words.
func (s *server) worldFileRefused(w http.ResponseWriter, r *http.Request, u store.User, what string, err error) {
	status := http.StatusUnprocessableEntity
	var msg i18n.Msg
	switch {
	case errors.Is(err, world.ErrWorldFileNewer):
		msg = i18n.M("worldimport.error.newer")
	case errors.Is(err, world.ErrWorldFileBad):
		msg = i18n.M("worldimport.error.bad")
	default:
		var ok bool
		if status, msg, ok = s.refusal(w, r, u, what, err); !ok {
			return
		}
	}
	s.page(w, r, status, u.Username, ErrorPage{Status: status, Title: i18n.M("worldimport.refused.title"), Detail: msg,
		ActionLabel: i18n.M("worldimport.retry"), ActionPath: PathImport})
}

// worldFileGone answers a confirm whose staged file is not there: it was
// removed after an hour, or never existed.
func (s *server) worldFileGone(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusGone, u.Username, ErrorPage{Status: http.StatusGone, Title: i18n.M("worldimport.gone.title"),
		Detail: i18n.M("worldimport.gone"), ActionLabel: i18n.M("worldimport.retry"), ActionPath: PathImport})
}

// stagedExists reports whether the staged file is still in the worlds folder.
func (s *server) stagedExists(name string) bool {
	root, err := s.worlds.OpenWorlds()
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	st, err := root.Lstat(name)
	return err == nil && st.Mode().IsRegular()
}

// removeStagedWorldFile removes a staged file that cannot be imported. A
// failure leaves it for the hourly cleanup.
func (s *server) removeStagedWorldFile(name string) {
	root, err := s.worlds.OpenWorlds()
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.log.Warn("could not remove a staged world file", "error", fmt.Sprint(err))
	}
}
