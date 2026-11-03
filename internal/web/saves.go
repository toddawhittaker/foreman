package web

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Routes of the saves screens (SPEC 5.11). A world's own page is
// /worlds/{id}.
const (
	patternSaves    = "/worlds/{id}/saves"
	patternSave     = "/worlds/{id}/saves/{name}"
	patternLoadSave = "/worlds/{id}/load-save"
)

// savesShown is how many saves the list shows before "Show older" (R14).
const savesShown = 50

// Names of the form fields: the file on the upload form and the radio group
// on the load form.
const (
	saveUploadField = "save"
	saveLoadField   = "load"
)

func (s *server) saveRoutes() {
	s.mux.HandleFunc("GET "+patternSaves, s.requireUser(s.handleSaves))
	s.handleUpload("POST "+patternSaves, MaxSaveUpload, s.requireUser(s.handleSaveUpload))
	s.handleLong("GET "+patternSave, s.requireUser(s.handleSaveDownload))
	s.mux.HandleFunc("POST "+patternLoadSave, s.requireUser(s.handleLoadSave))
}

// SaveRow is one row of the saves table.
type SaveRow struct {
	Name     string
	Href     string
	Size     uint64
	Saved    time.Time
	Loads    bool // the save the game loads next
	Selected bool // checked in the load choice
}

// SavesPage is a world's saves (SPEC 5.11, DESIGN 8.3).
type SavesPage struct {
	WorldName string
	WorldHref string
	SavesHref string
	LoadHref  string
	// Newer is true when a newer Foreman wrote the world: the saves can be
	// downloaded but nothing can be changed.
	Newer bool
	Rows  []SaveRow
	// OlderHref is set when more saves exist than the table shows.
	OlderHref string
	// LoadSave is the chosen save's name, empty for the newest.
	LoadSave string
	// LoadMissing is true when the chosen save is no longer in the folder.
	LoadMissing bool
	// NewestSelected checks the "newest save" choice.
	NewestSelected bool
	UploadLimit    uint64
	// Uploaded and Chosen report what the last request did.
	Uploaded string
	Chosen   bool

	UploadError string
	LoadError   string
}

func (SavesPage) templateName() string { return "saves" }

func (p SavesPage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists the form errors in the order the forms appear.
func (p SavesPage) Problems() []Problem {
	return problems(
		Problem{"save-file", p.UploadError},
		Problem{"load-save", p.LoadError},
	)
}

func saveHref(id, name string) string {
	return "/worlds/" + url.PathEscape(id) + "/saves/" + url.PathEscape(name)
}

// buildSavesPage reads a world and lays out its page. all shows every save
// instead of the newest savesShown.
func (s *server) buildSavesPage(id string, all bool) (SavesPage, error) {
	list, err := s.worlds.Saves(id)
	if err != nil {
		return SavesPage{}, err
	}
	base := "/worlds/" + id
	p := SavesPage{
		WorldName:   list.WorldName,
		WorldHref:   base,
		SavesHref:   base + "/saves",
		LoadHref:    base + "/load-save",
		Newer:       list.Newer,
		LoadSave:    list.LoadSave,
		UploadLimit: MaxSaveUpload,
	}
	chosen := slices.IndexFunc(list.Saves, func(sv world.Save) bool { return sv.Name == list.LoadSave })
	p.LoadMissing = list.LoadSave != "" && chosen < 0
	p.NewestSelected = list.LoadSave == "" || p.LoadMissing
	// The save that loads next: the chosen one, else the newest.
	loads := max(chosen, 0)
	shown := list.Saves
	if !all && len(shown) > savesShown {
		shown = shown[:savesShown]
		p.OlderHref = p.SavesHref + "?all=1"
	}
	for i, sv := range shown {
		p.Rows = append(p.Rows, SaveRow{
			Name:     sv.Name,
			Href:     saveHref(id, sv.Name),
			Size:     uint64(sv.Size), //nolint:gosec // a file size is never negative
			Saved:    sv.ModTime,
			Loads:    i == loads,
			Selected: i == chosen,
		})
	}
	return p, nil
}

// savesWorldError answers a failure to reach the world: 404 when there is
// no such world, otherwise a server fault.
func (s *server) savesWorldError(w http.ResponseWriter, r *http.Request, u store.User, what string, err error) {
	if errors.Is(err, fs.ErrNotExist) {
		s.page(w, r, http.StatusNotFound, u.Username, ErrorPage{Status: http.StatusNotFound})
		return
	}
	s.fail(w, r, what, err)
}

// showSaves renders a world's saves page. edit adds what the request did.
func (s *server) showSaves(w http.ResponseWriter, r *http.Request, u store.User, status int, edit func(*SavesPage)) {
	p, err := s.buildSavesPage(r.PathValue("id"), r.URL.Query().Get("all") != "")
	if err != nil {
		s.savesWorldError(w, r, u, "list saves", err)
		return
	}
	if edit != nil {
		edit(&p)
	}
	s.page(w, r, status, u.Username, p)
}

// handleSaves lists a world's saves (SPEC 5.11).
func (s *server) handleSaves(w http.ResponseWriter, r *http.Request, u store.User) {
	s.showSaves(w, r, u, http.StatusOK, func(p *SavesPage) {
		q := r.URL.Query()
		if name := q.Get("uploaded"); slices.ContainsFunc(p.Rows, func(row SaveRow) bool { return row.Name == name }) {
			p.Uploaded = name
		}
		p.Chosen = q.Get("chosen") != ""
	})
}

// changeRefusal turns a refused change into words for the admin and a
// status. ok is false for an error with no words, which is a server fault.
func changeRefusal(err error) (msg string, status int, ok bool) {
	var ue uploadError
	switch {
	case errors.As(err, &ue):
		return ue.msg, http.StatusUnprocessableEntity, true
	case errors.Is(err, world.ErrSaveName), errors.Is(err, world.ErrSaveNotFound):
		return err.Error(), http.StatusUnprocessableEntity, true
	case errors.Is(err, world.ErrSaveExists), errors.Is(err, world.ErrBusy), errors.Is(err, world.ErrNewerFormat):
		return err.Error(), http.StatusConflict, true
	case errors.Is(err, world.ErrNoSpace):
		return err.Error(), http.StatusInsufficientStorage, true
	}
	return "", 0, false
}

// handleSaveUpload adds an uploaded save to a world (SPEC 5.11). It never
// replaces a save that has the name.
func (s *server) handleSaveUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	id := r.PathValue("id")
	refuse := func(err error) {
		msg, status, ok := changeRefusal(err)
		if !ok {
			s.fail(w, r, "upload save", err)
			return
		}
		s.showSaves(w, r, u, status, func(p *SavesPage) { p.UploadError = msg })
	}

	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		s.savesWorldError(w, r, u, "open world", err)
		return
	}
	defer func() { _ = root.Close() }()
	if list, err := s.worlds.Saves(id); err == nil && list.Newer {
		refuse(world.ErrNewerFormat)
		return
	}
	if r.ContentLength > 0 {
		if err := world.CheckSpace(root.Name(), uint64(r.ContentLength)); err != nil {
			refuse(err)
			return
		}
	}

	up, err := receiveFile(r, root, world.SavesDir, saveUploadField, root.Name(), MaxSaveUpload)
	if err != nil {
		refuse(err)
		return
	}
	// AddSave takes over the temp file once it is called.
	if !world.ValidSaveName(up.FileName) {
		_ = root.Remove(up.Temp)
		refuse(world.ErrSaveName)
		return
	}
	if err := checkZip(root, up.Temp, maxZipRatio*uint64(up.Size)); err != nil { //nolint:gosec // a file size is never negative
		_ = root.Remove(up.Temp)
		refuse(err)
		return
	}
	if err := s.worlds.AddSave(id, up.FileName, up.Temp); err != nil {
		refuse(err)
		return
	}
	http.Redirect(w, r, "/worlds/"+id+"/saves?uploaded="+url.QueryEscape(up.FileName), http.StatusSeeOther)
}

// handleSaveDownload sends a save as a file, with the read and write
// deadlines already lifted by the route (SPEC 5.11).
func (s *server) handleSaveDownload(w http.ResponseWriter, r *http.Request, u store.User) {
	f, info, err := s.worlds.OpenSave(r.PathValue("id"), r.PathValue("name"))
	if errors.Is(err, world.ErrSaveNotFound) {
		err = fs.ErrNotExist
	}
	if err != nil {
		s.savesWorldError(w, r, u, "open save", err)
		return
	}
	defer func() { _ = f.Close() }()
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// handleLoadSave sets which save the game loads: a named one, or the
// newest (SPEC 7.11). It changes only world.json.
func (s *server) handleLoadSave(w http.ResponseWriter, r *http.Request, u store.User) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		s.showSaves(w, r, u, http.StatusBadRequest, func(p *SavesPage) { p.LoadError = "Foreman could not read the form. Try again." })
		return
	}
	choice, sent := r.PostForm[saveLoadField]
	if !sent {
		s.showSaves(w, r, u, http.StatusUnprocessableEntity, func(p *SavesPage) { p.LoadError = "Choose which save loads." })
		return
	}
	if err := s.worlds.SetLoadSave(id, choice[0]); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.savesWorldError(w, r, u, "choose save", err)
			return
		}
		msg, status, ok := changeRefusal(err)
		if !ok {
			s.fail(w, r, "choose save", err)
			return
		}
		s.showSaves(w, r, u, status, func(p *SavesPage) { p.LoadError = msg })
		return
	}
	http.Redirect(w, r, "/worlds/"+id+"/saves?chosen=1", http.StatusSeeOther)
}
