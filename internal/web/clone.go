package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// PathClone is a world's clone page (SPEC 5.7).
const PathClone = "/worlds/{id}/clone"

// AuditClone is the audit action of a world made by Clone (SPEC 12.9).
const AuditClone = "clone"

// clonedQuery names the world a clone was made from, for the Done message
// on the new world's page.
const clonedQuery = "cloned"

// The two ways to clone.
const (
	cloneKeep  = "keep"
	cloneFresh = "fresh"
)

// ClonePage is the clone form (SPEC 5.7, DESIGN 8.9).
type ClonePage struct {
	World world.Summary
	Name  string
	// Choice is "keep" or "fresh". SaveName and SaveTime name the save that
	// "keep" copies; SaveName is empty when the world has none.
	Choice      string
	SaveName    string
	SaveTime    time.Time
	NameError   i18n.Msg
	ChoiceError i18n.Msg
	// Error is a refusal that belongs to no field; ErrorBusy adds a link to
	// the Server page.
	Error     i18n.Msg
	ErrorBusy bool
	// RunningNote is set while the world's game runs, whose newest save
	// may be behind.
	RunningNote bool
	// FreshOnlyWhy says why only a fresh map is offered.
	FreshOnlyWhy i18n.Msg
}

func (ClonePage) templateName() string { return "clone" }
func (ClonePage) navItem() string      { return PathWorlds }
func (p ClonePage) hasErrors() bool    { return len(p.Problems()) > 0 || !p.Error.IsZero() }

// Problems lists the form's errors in the order the fields appear.
func (p ClonePage) Problems() []Problem {
	choice := "clone-fresh"
	if p.SaveName != "" {
		choice = "clone-keep"
	}
	return problems(Problem{"clone-name", p.NameError}, Problem{choice, p.ChoiceError})
}

// Paths the form uses.
func (p ClonePage) Path() string      { return worldPath(p.World.ID) + "/clone" }
func (p ClonePage) WorldPath() string { return worldPath(p.World.ID) }

// Tabs are the world's pages; Clone is reached from Overview (DESIGN 6).
func (p ClonePage) Tabs() WorldTabs {
	t := worldTabs(p.World, "overview")
	t.Under = true
	return t
}

// ClonePath is where a world's Clone button goes.
func (p WorldPage) ClonePath() string { return worldPath(p.World.ID) + "/clone" }

func (s *server) cloneRoutes() {
	s.mux.HandleFunc("GET "+PathClone, s.requireUser(s.handleCloneForm))
	s.handleLong("POST "+PathClone, s.requireUser(s.handleClone))
}

// cloneRefusal says why a world cannot be cloned, or is zero when it can.
func cloneRefusal(sum world.Summary) i18n.Msg {
	switch {
	case sum.Unreadable:
		return i18n.M("worlds.error.unreadable")
	case sum.Newer:
		return i18n.M("worlds.error.newer_format")
	}
	return i18n.Msg{}
}

// clonable answers the request itself when the world cannot be cloned.
func (s *server) clonable(w http.ResponseWriter, r *http.Request, u store.User) (world.Summary, bool) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return sum, false
	}
	why := cloneRefusal(sum)
	if why.IsZero() {
		return sum, true
	}
	s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict, Detail: why})
	return sum, false
}

// clonePage fills the save that "keep" would copy. A chosen save that is
// gone leaves only a fresh map, and says so.
func (s *server) clonePage(sum world.Summary, name, choice string) (ClonePage, error) {
	p := ClonePage{World: sum, Name: name, Choice: choice}
	saveName, saveTime, err := s.worlds.CloneSource(sum.ID)
	switch {
	case errors.Is(err, world.ErrSaveNotFound):
		p.FreshOnlyWhy = i18n.M("worlds.clone.save_gone")
	case err != nil:
		return p, err
	case saveName == "":
		p.FreshOnlyWhy = i18n.M("worlds.clone.no_save")
	default:
		p.SaveName, p.SaveTime = saveName, saveTime
	}
	switch {
	case p.SaveName == "":
		p.Choice = cloneFresh
	case p.Choice != cloneFresh:
		p.Choice = cloneKeep
	}
	// A running game writes its save now and then, so the copy may be a
	// little behind what players see.
	if s.game != nil && sum.Active {
		switch s.game.Status(sum.ID).State {
		case game.StateRunning, game.StateStarting, game.StateStopping:
			p.RunningNote = true
		}
	}
	return p, nil
}

// copyName is the clone's suggested name, cut so it fits the name field.
func copyName(name string) string {
	const suffix = " (copy)"
	if utf8.RuneCountInString(name)+utf8.RuneCountInString(suffix) <= 100 {
		return name + suffix
	}
	r := []rune(name)
	return string(r[:100-utf8.RuneCountInString(suffix)]) + suffix
}

func (s *server) handleCloneForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.clonable(w, r, u)
	if !ok {
		return
	}
	p, err := s.clonePage(sum, copyName(sum.Name), "")
	if err != nil {
		s.notFoundOrFail(w, r, u, "read clone source", err)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// checkForm puts the form's own errors on its fields.
func (p *ClonePage) checkForm(name, choice string) {
	if strings.TrimSpace(name) == "" {
		p.NameError = msgNameEmpty()
	}
	if choice != cloneKeep && choice != cloneFresh {
		p.ChoiceError = i18n.M("worlds.clone.error.choice")
	}
	if choice == cloneKeep && p.SaveName == "" {
		p.ChoiceError = i18n.M("worlds.clone.error.no_save")
	}
}

// errCloneForm says the clone form has errors, which are on the page.
var errCloneForm = errors.New("the clone form has errors")

// cloneWorld clones a world as the Clone page does (SPEC 5.7) and audits
// it. A field the form shows wrongly is set on the returned page with
// errCloneForm; a refusal from the clone itself is returned as it is, for
// the caller to word.
func (s *server) cloneWorld(ctx context.Context, by game.Actor, sum world.Summary, name, choice string) (string, ClonePage, error) {
	p, err := s.clonePage(sum, name, choice)
	if err != nil {
		// An empty page tells the caller the source could not be read.
		return "", ClonePage{}, err
	}
	p.checkForm(name, choice)
	if p.hasErrors() {
		return "", p, errCloneForm
	}
	var seed *uint32
	if choice == cloneFresh {
		v := newSeed()
		seed = &v
	}
	id, err := s.worlds.Clone(ctx, sum.ID, name, choice == cloneKeep, seed)
	if err != nil {
		return "", p, err
	}
	s.audit(ctx, by, AuditClone, id, fmt.Sprintf("from %s, %s", sum.ID, choice))
	return id, p, nil
}

func (s *server) handleClone(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.clonable(w, r, u)
	if !ok {
		return
	}
	id, p, err := s.cloneWorld(r.Context(), actor(u), sum, r.PostFormValue("name"), r.PostFormValue("map"))
	switch {
	case err == nil:
		http.Redirect(w, r, worldPath(id)+"?"+clonedQuery+"="+url.QueryEscape(sum.ID), http.StatusSeeOther) //nolint:gosec // id is a world id Foreman made, a local path segment
		return
	case errors.Is(err, errCloneForm):
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	case p.World.ID == "":
		s.notFoundOrFail(w, r, u, "read clone source", err)
		return
	}
	status, msg, ok := s.refusal(w, r, u, "clone world", err)
	if !ok {
		return
	}
	switch {
	case errors.Is(err, world.ErrNameEmpty), errors.Is(err, world.ErrNameTaken):
		p.NameError = msg
	case errors.Is(err, world.ErrCloneNoSave):
		p.ChoiceError = i18n.M("worlds.clone.error.no_save")
	default:
		p.Error = msg
		p.ErrorBusy = errors.Is(err, world.ErrBusy)
	}
	s.page(w, r, status, u.Username, p)
}

// clonedFacts adds the Done message after a clone to the new world's page.
// It names the source only while that world still exists, so a made-up link
// cannot put words on the page.
func (s *server) clonedFacts(p *WorldPage, r *http.Request) {
	from := r.URL.Query().Get(clonedQuery)
	if from == "" || !world.ValidID(from) {
		return
	}
	src, err := s.worlds.Get(r.Context(), from)
	if err != nil {
		return
	}
	p.Done = i18n.M("worlds.world.cloned", "name", p.World.Name, "from", src.Name)
}
