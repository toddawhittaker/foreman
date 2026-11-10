package web

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
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

// Names of the form fields: the file on the upload form and the button a
// row of the saves table submits.
const (
	saveUploadField = "save"
	saveLoadField   = "load"
)

// loadErrorID is the id of the load choice's error, where the error summary
// sends focus (DESIGN 7.2).
const loadErrorID = "load-save-error"

func (s *server) saveRoutes() {
	s.mux.HandleFunc("GET "+patternSaves, s.requireUser(s.handleSaves))
	s.handleUpload("POST "+patternSaves, MaxSaveUpload, s.requireUser(s.guardUpload(s.handleSaveUpload)))
	s.handleLong("GET "+patternSave, s.requireUser(s.handleSaveDownload))
	s.mux.HandleFunc("POST "+patternLoadSave, s.requireUser(s.handleLoadSave))
}

// SaveRow is one row of the saves table.
type SaveRow struct {
	Name  string
	Href  string
	Size  uint64
	Saved time.Time
	Loads bool // the save the game loads next
	// LoadValue is what the row's "Load next" button posts: the save's name,
	// or empty on the newest row, so later autosaves still win (SPEC 7.11).
	LoadValue string
}

// SavesPage is a world's saves (SPEC 5.11, DESIGN 8.3).
type SavesPage struct {
	WorldName string
	// Active marks the active world in the heading, as on its Overview.
	Active    bool
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
	UploadLimit uint64
	// Uploaded and Chosen report what the last request did.
	Uploaded string
	Chosen   bool

	UploadError i18n.Msg
	LoadError   i18n.Msg
}

func (SavesPage) templateName() string { return "saves" }
func (SavesPage) navItem() string      { return PathWorlds }

// Tabs links the world's pages; a world a newer Foreman wrote has no
// settings to show (DESIGN 6).
func (p SavesPage) Tabs() WorldTabs {
	return WorldTabs{Path: p.WorldHref, Current: "saves", Settings: !p.Newer}
}

func (p SavesPage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists the form errors in the order the forms appear.
func (p SavesPage) Problems() []Problem {
	return problems(
		Problem{"save-file", p.UploadError},
		Problem{loadErrorID, p.LoadError},
	)
}

func saveHref(id, name string) string {
	return "/worlds/" + url.PathEscape(id) + "/saves/" + url.PathEscape(name)
}

// buildSavesPage reads a world and lays out its page. all shows every save
// instead of the newest maxListRows.
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
	chosen := chosenIndex(list)
	p.LoadMissing = list.LoadSave != "" && chosen < 0
	// The save that loads next: the chosen one, else the newest.
	loads := max(chosen, 0)
	shown := list.Saves
	if !all && len(shown) > maxListRows {
		shown = shown[:maxListRows]
		p.OlderHref = p.SavesHref + "?all=1"
	}
	for i, sv := range shown {
		row := SaveRow{
			Name:  sv.Name,
			Href:  saveHref(id, sv.Name),
			Size:  uint64(sv.Size), //nolint:gosec // a file size is never negative
			Saved: sv.ModTime,
			Loads: i == loads,
		}
		if i > 0 {
			row.LoadValue = sv.Name
		}
		p.Rows = append(p.Rows, row)
	}
	return p, nil
}

// chosenIndex is the place of the chosen save in the list, or -1 when none
// is chosen or the chosen file is gone.
func chosenIndex(list world.SaveList) int {
	return slices.IndexFunc(list.Saves, func(sv world.Save) bool { return list.LoadSave != "" && sv.Name == list.LoadSave })
}

// loadsNext is the name of the save the game loads at its next start when
// that is not the newest, or "" when the newest loads: no choice is set,
// the newest is chosen, or the chosen file is gone (SPEC 7.11, 17.7).
func (s *server) loadsNext(id string) string {
	list, err := s.worlds.Saves(id)
	if err != nil {
		return ""
	}
	if i := chosenIndex(list); i > 0 {
		return list.Saves[i].Name
	}
	return ""
}

// showSaves renders a world's saves page. edit adds what the request did.
func (s *server) showSaves(w http.ResponseWriter, r *http.Request, u store.User, status int, edit func(*SavesPage)) {
	p, err := s.buildSavesPage(r.PathValue("id"), r.URL.Query().Get("all") != "")
	if err != nil {
		s.notFoundOrFail(w, r, u, "list saves", err)
		return
	}
	if sum, err := s.worlds.Get(r.Context(), r.PathValue("id")); err == nil {
		p.Active = sum.Active
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

// handleSaveUpload adds an uploaded save to a world (SPEC 5.11). It never
// replaces a save that has the name.
func (s *server) handleSaveUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	id := r.PathValue("id")
	refuse := func(err error) {
		status, msg, ok := s.refusal(w, r, u, "upload save", err)
		if ok {
			s.showSaves(w, r, u, status, func(p *SavesPage) { p.UploadError = msg })
		}
	}

	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		s.notFoundOrFail(w, r, u, "open world", err)
		return
	}
	defer func() { _ = root.Close() }()
	if list, err := s.worlds.Saves(id); err == nil && list.Newer {
		refuse(world.ErrNewerFormat)
		return
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
		s.notFoundOrFail(w, r, u, "open save", err)
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
	if !s.readForm(w, r) {
		return
	}
	choice, sent := r.PostForm[saveLoadField]
	if !sent {
		s.showSaves(w, r, u, http.StatusUnprocessableEntity, func(p *SavesPage) { p.LoadError = i18n.M("worlds.saves.choose") })
		return
	}
	if err := s.worlds.SetLoadSave(id, choice[0]); err != nil {
		status, msg, ok := s.refusal(w, r, u, "choose save", err)
		if !ok {
			return
		}
		if errors.Is(err, world.ErrSaveNotFound) {
			msg = i18n.M("worlds.saves.gone")
		}
		s.showSaves(w, r, u, status, func(p *SavesPage) { p.LoadError = msg })
		return
	}
	http.Redirect(w, r, "/worlds/"+id+"/saves?chosen=1", http.StatusSeeOther)
}
