package web

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamedir"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Adopting the game folder (SPEC 4.8, DESIGN 7.4). The Server page links
// here while the folder still holds its own saves, mods and config.

// PathAdopt is the adoption form and where it posts.
const PathAdopt = "/adopt"

func (s *server) adoptRoutes() {
	s.mux.HandleFunc("GET "+PathAdopt, s.requireUser(s.handleAdoptForm))
	s.mux.HandleFunc("POST "+PathAdopt, s.requireUser(s.handleAdopt))
}

// adoptionRecord remembers the adoption this process started last. The new
// world has no folder, and is not the active world, until the adoption is
// well under way, so the Server page needs it to show the steps from the
// start and to show how a failed adoption ended.
type adoptionRecord struct {
	mu      sync.Mutex
	id      string
	name    string
	running bool
}

func (a *adoptionRecord) start(id, name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.id, a.name, a.running = id, name, true
}

func (a *adoptionRecord) finish() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.running = false
}

func (a *adoptionRecord) is(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id != "" && a.id == id
}

func (a *adoptionRecord) get() (id, name string, running bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id, a.name, a.running
}

// AdoptPage is the adoption form with its danger box (DESIGN 7.4).
type AdoptPage struct {
	// Form holds the name, game version and Space Age fields.
	Form ImportForm
	// Running is true when the game runs now, so the adoption stops it,
	// warns players first, and starts it again.
	Running bool
	CountdownForm
	// Refusal replaces the form when the adoption cannot go ahead.
	Refusal string
}

func (AdoptPage) templateName() string { return "adopt" }
func (AdoptPage) navItem() string      { return PathServer }
func (p AdoptPage) hasErrors() bool    { return p.Refusal != "" || len(p.Problems()) > 0 }

// Problems lists the field errors in the order the fields appear.
func (p AdoptPage) Problems() []Problem {
	return problems(
		Problem{"adopt-name", p.Form.NameError},
		Problem{"adopt-version", p.Form.VersionError},
		Problem{"countdown", p.CountdownError},
	)
}

// Path is where the form posts, and where "check again" goes.
func (AdoptPage) Path() string { return PathAdopt }

// adoptSpaceAge is the Space Age flag a world of this version can have.
func adoptSpaceAge(version string, spaceAge bool) bool {
	return spaceAge && strings.HasPrefix(version, "2.")
}

// folderLayout is the game folder's shape at the monitor's last look, for
// the pages that have no world to ask about. The controller answers for any
// id in adopted mode.
func (s *server) folderLayout() gamedir.State {
	return s.game.Status("").Layout
}

// adoptRefusal words why the game folder cannot be adopted, or "" when it
// can: the folder is already adopted, or it is broken.
func (s *server) adoptRefusal(defaults game.AdoptionDefaults) string {
	if defaults.Unadopted {
		return ""
	}
	if l := s.folderLayout(); l.Kind == gamedir.Broken {
		return sentence(&game.LayoutError{Path: l.Path, Reason: l.Reason})
	}
	return sentence(game.ErrAlreadyAdopted)
}

// adoptPage starts a page from the container's defaults. ok is false when
// the request was answered.
func (s *server) adoptPage(w http.ResponseWriter, r *http.Request, u store.User) (AdoptPage, bool) {
	d, err := s.game.AdoptionDefaults(r.Context())
	p := AdoptPage{Form: ImportForm{IDPrefix: "adopt", GameVersion: d.GameVersion, SpaceAge: adoptSpaceAge(d.GameVersion, d.SpaceAge)},
		Running: d.Running, CountdownForm: defaultCountdown()}
	switch {
	case errors.Is(err, game.ErrNotAdoptedMode):
		s.notFound(w, r, u)
		return p, false
	case err != nil:
		_, msg, ok := s.gameRefusal(w, r, u, "read the adoption defaults", err)
		if !ok {
			return p, false
		}
		p.Refusal = msg
	default:
		p.Refusal = s.adoptRefusal(d)
	}
	return p, true
}

func (s *server) handleAdoptForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.adoptPage(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handleAdopt adopts the game folder as a new world (SPEC 4.8). It checks
// everything that can be checked without changing anything, then runs the
// adoption in the background.
func (s *server) handleAdopt(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	p, ok := s.adoptPage(w, r, u)
	if !ok {
		return
	}
	f := importFields(r.PostFormValue("name"), r.PostFormValue("game_version"), r.PostFormValue("space_age"))
	f.IDPrefix, f.SpaceAge = "adopt", adoptSpaceAge(f.GameVersion, f.SpaceAge)
	form, stop := readCountdown(r)
	p.Form, p.CountdownForm = f, form
	switch {
	case p.Refusal != "":
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	case f.failed() || form.CountdownError != "":
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	case s.adoptionRunning():
		p.Refusal = "An adoption is already running. Wait for it to finish."
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	a, err := s.game.ReserveAdoption(r.Context(), f.Name, f.GameVersion, f.SpaceAge)
	if err != nil {
		s.adoptFailed(w, r, u, p, err)
		return
	}
	by := actor(u)
	s.adoption.start(a.ID, a.Name)
	s.beginOn(w, r, []string{a.ID}, true, func(ctx context.Context) actionResult {
		defer s.adoption.finish()
		return s.finish(a.ID, game.AuditAdopt, s.game.Adopt(ctx, a, by, stop))
	})
}

func (s *server) adoptionRunning() bool {
	_, _, running := s.adoption.get()
	return running
}

// adoptFailed shows why ReserveAdoption refused: on the field it is about,
// or in place of the form.
func (s *server) adoptFailed(w http.ResponseWriter, r *http.Request, u store.User, p AdoptPage, err error) {
	if errors.Is(err, game.ErrNotAdoptedMode) {
		s.notFound(w, r, u)
		return
	}
	status, msg, ok := s.gameRefusal(w, r, u, "reserve the adoption", err)
	if !ok {
		return
	}
	switch {
	case errors.Is(err, world.ErrNameEmpty), errors.Is(err, world.ErrNameTaken):
		p.Form.NameError = msg
	case errors.Is(err, world.ErrBadGameVersion):
		p.Form.VersionError = msg
	default:
		p.Refusal = msg
	}
	s.page(w, r, status, u.Username, p)
}

// serverWorld is the world the Server page shows: the active one, or else
// the world a first adoption is building while it runs or after it failed,
// since that adoption's steps are the news.
func (s *server) serverWorld(ctx context.Context) (sum world.Summary, found bool, err error) {
	if id, name, running := s.adoption.get(); id != "" {
		op := s.game.Status(id).Operation
		if running || op != nil && op.Error != "" {
			adopting, err := s.worlds.Get(ctx, id)
			switch {
			case err == nil && !adopting.Active:
				return adopting, true, nil
			case errors.Is(err, fs.ErrNotExist):
				return world.Summary{ID: id, Name: name}, true, nil
			case err != nil:
				return world.Summary{}, false, err
			}
		}
	}
	return s.activeWorld(ctx)
}

// adoptingParam marks a live stream for a page that shows an adoption.
const adoptingParam = "adopting"

// watchedWorld is the world a live page follows: a world on disk, or the
// one a first adoption is still building. A page that showed the adoption
// follows the world as that page saw it, not yet active, so the stream can
// tell it to load again once the world is.
func (s *server) watchedWorld(w http.ResponseWriter, r *http.Request, u store.User) (world.Summary, bool) {
	id := r.PathValue("id")
	sum, err := s.worlds.Get(r.Context(), id)
	if err == nil {
		if r.URL.Query().Get(adoptingParam) == "1" && s.adoption.is(id) {
			sum.Active = false
		}
		return sum, true
	}
	if aid, name, _ := s.adoption.get(); aid == id && errors.Is(err, fs.ErrNotExist) {
		return world.Summary{ID: id, Name: name}, true
	}
	s.notFoundOrFail(w, r, u, "read world", err)
	return world.Summary{}, false
}

// noWorldPage is the Server page when no world is active. In adopted mode
// it also says what the game folder needs.
func (s *server) noWorldPage() ServerPage {
	p := ServerPage{}
	if s.cfg.Mode == config.ModeAdopted {
		p.Status = s.game.Status("")
	}
	return p
}
