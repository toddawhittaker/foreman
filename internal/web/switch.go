package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Switching worlds (SPEC 7.9, 7.10, DESIGN 7.4). The confirmation page
// belongs to the world to switch to; the world to switch from is the active
// one and travels in the form, so a stale form is refused.

func switchPath(id string) string { return gamePath(id, "switch") }

// switchAnywayPath is the confirmation for a switch the pre-start check
// refused but allows to go ahead (SPEC 7.4).
func switchAnywayPath(id string) string { return switchPath(id) + "?anyway=1" }

func (s *server) switchRoutes() {
	s.mux.HandleFunc("GET "+PathGameSwitch, s.requireUser(s.handleSwitchForm))
	s.mux.HandleFunc("POST "+PathGameSwitch, s.requireUser(s.handleSwitch))
}

// SwitchPage confirms a switch and asks how to warn players (SPEC 7.5,
// 7.6, 7.9). With Anyway it also confirms the switch despite the pre-start
// check's problems (7.4).
type SwitchPage struct {
	From, To world.Summary
	// Running is true when the game runs in From, so a switch stops it and
	// starts To; otherwise the worlds are switched and the game stays stopped.
	Running bool
	CountdownForm
	Anyway bool
	// CheckProblems are the pre-start check's findings, for Anyway.
	CheckProblems []string
	// Refusal replaces the form when the switch cannot go ahead.
	Refusal string
}

func (SwitchPage) templateName() string { return "switch" }
func (SwitchPage) navItem() string      { return PathWorlds }
func (p SwitchPage) hasErrors() bool    { return p.CountdownError != "" }

// Problems lists the countdown field's error for the summary.
func (p SwitchPage) Problems() []Problem {
	return problems(Problem{"countdown", p.CountdownError})
}

// Paths the page posts to and links to.
func (p SwitchPage) Path() string       { return switchPath(p.To.ID) }
func (p SwitchPage) WorldPath() string  { return worldPath(p.To.ID) }
func (p SwitchPage) ActivePath() string { return worldPath(p.From.ID) }

// switchRefusal says in words why a switch from the active world to to
// cannot go ahead, or returns "". The controller checks the same things
// again when the switch runs; this keeps a button that would be refused off
// the page (DESIGN 7.1).
func (s *server) switchRefusal(ctx context.Context, from string, to world.Summary) (refusal string, running bool, err error) {
	active, found, err := s.activeWorld(ctx)
	if err != nil {
		return "", false, err
	}
	switch {
	case to.Active:
		return "This is already the active world.", false, nil
	case !found:
		return "No world is active, so there is nothing to switch from.", false, nil
	case from != "" && from != active.ID:
		return sentence(game.ErrSwitchNotActive), false, nil
	case to.Unreadable:
		return "Foreman cannot read this world's world.json, so it cannot start this world.", false, nil
	case to.Newer:
		return "A newer version of Foreman wrote this world, so this version cannot start it.", false, nil
	}
	cur, next := s.game.Status(active.ID), s.game.Status(to.ID)
	switch {
	case cur.Operation != nil && !cur.Operation.Done, next.Operation != nil && !next.Operation.Done:
		return "Another operation is running on one of these worlds. Wait for it to finish, then try again.", false, nil
	case next.State != game.StateStopped:
		return sentence(game.ErrSwitchTargetRunning), false, nil
	}
	switch cur.State {
	case game.StateRunning, game.StateCrashed:
		return "", true, nil
	case game.StateStopped, game.StateCrashLooping:
		return "", false, nil
	}
	return "The game is starting or stopping, or Foreman cannot tell its state. Wait until it is running or stopped, or use Force stop.", false, nil
}

// switchBusy reports whether an adoption is running, or an operation is
// running on this world or the active one, so a switch would be refused.
func (s *server) switchBusy(ctx context.Context, id string) bool {
	if _, _, adopting := s.adoption.get(); adopting {
		return true
	}
	ids := []string{id}
	if active, found, err := s.activeWorld(ctx); err == nil && found {
		ids = append(ids, active.ID)
	}
	for _, id := range ids {
		if op := s.game.Status(id).Operation; op != nil && !op.Done {
			return true
		}
	}
	return false
}

// switchPage builds the confirmation for a switch to to. from is the
// world the form named, or "" for the active one.
func (s *server) switchPage(ctx context.Context, from string, to world.Summary, form CountdownForm, anyway bool) (SwitchPage, error) {
	p := SwitchPage{To: to, CountdownForm: form, Anyway: anyway}
	active, found, err := s.activeWorld(ctx)
	if err != nil {
		return p, err
	}
	p.From = active
	if p.Refusal, p.Running, err = s.switchRefusal(ctx, from, to); err != nil || p.Refusal != "" || !found {
		return p, err
	}
	if anyway {
		problems, refusal, err := s.anywayCheck(to.ID)
		p.CheckProblems, p.Refusal = problems, refusal
		if err != nil {
			s.log.Error("pre-start check", "world", to.ID, "error", err)
			p.Refusal = "Foreman could not run the pre-start check. The details are in Foreman's log."
		}
	}
	return p, nil
}

func (s *server) handleSwitchForm(w http.ResponseWriter, r *http.Request, u store.User) {
	to, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	p, err := s.switchPage(r.Context(), "", to, defaultCountdown(), r.URL.Query().Get("anyway") == "1")
	if err != nil {
		s.fail(w, r, "build the switch page", err)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handleSwitch makes the world in the path the active one (SPEC 7.9). The
// form's "from" is the world the admin saw as active.
func (s *server) handleSwitch(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	to, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	from := r.PostFormValue("from")
	if from == "" {
		// A form without it cannot say what the admin was shown.
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest,
			Detail: "The form does not say which world is active. Go back, reload the page and try again."})
		return
	}
	form, stop := readCountdown(r)
	anyway := r.PostFormValue("anyway") == "1"
	p, err := s.switchPage(r.Context(), from, to, form, anyway)
	if err != nil {
		s.fail(w, r, "build the switch page", err)
		return
	}
	if p.Refusal != "" {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if form.CountdownError != "" {
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	by := actor(u)
	s.beginOn(w, r, []string{from, to.ID}, true, func(ctx context.Context) actionResult {
		err := s.game.Switch(ctx, from, to.ID, by, stop, game.StartOptions{Anyway: anyway})
		res := s.finish(from, game.AuditSwitch, err)
		res.Target = to.ID
		return res
	})
}

// worldName returns a world's name for a sentence, or its id when the name
// cannot be read.
func (s *server) worldName(id string) string {
	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		return id
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil || meta.Name == "" {
		return id
	}
	return meta.Name
}

// switchNames fills in the names of the worlds the page speaks of.
func (s *server) switchNames(p *ServerPage) {
	var sw *game.SwitchInfo
	if o := p.Status.Operation; o != nil && o.Switch != nil {
		sw = o.Switch
	} else if n := p.Status.Notice; n != nil {
		sw = &n.SwitchInfo
	}
	if sw == nil {
		return
	}
	p.WorldNames = map[string]string{sw.From: s.worldName(sw.From), sw.To: s.worldName(sw.To)}
}

// name returns a world's name for a sentence, or its id when unknown.
func (p ServerPage) name(id string) string {
	if n := p.WorldNames[id]; n != "" {
		return n
	}
	return id
}

// switchInfo returns the switch the page offers a way back from: the failed
// one still shown, or else the unfinished one found at startup. Nil when
// there is none.
func (p ServerPage) switchInfo() *game.SwitchInfo {
	if o := p.Status.Operation; o != nil && o.Switch != nil && o.Done && o.Error != "" {
		return o.Switch
	}
	if n := p.Status.Notice; n != nil {
		return &n.SwitchInfo
	}
	return nil
}

// SwitchOffer is a link to switch to a world.
type SwitchOffer struct {
	Path, Name string
}

// SwitchBack offers the switch back to the world a failed or unfinished
// switch left, when the new world had already become active (SPEC 7.9). When
// it had not, the old world is still active and its Start button is the way
// on.
func (p ServerPage) SwitchBack() *SwitchOffer {
	sw := p.switchInfo()
	if sw == nil || !sw.Presented || p.Busy() {
		return nil
	}
	return &SwitchOffer{Path: switchPath(sw.From), Name: p.name(sw.From)}
}

// SwitchNote says where a failed switch left the worlds. An unfinished
// switch found at startup brings its own words in the notice.
func (p ServerPage) SwitchNote() string {
	o := p.Status.Operation
	if o == nil || o.Switch == nil || !o.Done || o.Error == "" {
		return ""
	}
	if o.Switch.Presented {
		return p.name(o.Switch.To) + " is now the active world, but the switch did not finish."
	}
	return "Nothing was switched. " + p.name(o.Switch.From) + " is still the active world."
}

// movedSentence is what the reloaded page reads out: which world is now the
// active one, then how the action ended when that is known.
func (p ServerPage) movedSentence() string {
	name := p.World.Name
	if sw := p.Status.Operation.Switch; !p.Adopting && sw != nil {
		name = p.name(sw.To)
	}
	return strings.TrimSpace(name + " is now the active world. " + resultAnnouncement(p))
}

// Moved reports whether this page's world stopped being the active one in a
// switch, or the world a first adoption was building became the active one,
// so the page must load the Server page again to show the right world. It
// waits until the operation has ended and the result is stored, so the
// sentence the next page reads out always says how the action ended. Until
// then both worlds' pages show the same step list, so the old page is not
// stale.
func (p ServerPage) Moved() bool {
	o := p.Status.Operation
	if o == nil || !o.Done || !p.HaveResult {
		return false
	}
	if p.Adopting {
		return o.Action == game.AuditAdopt && o.Error == ""
	}
	return o.Switch != nil && o.Switch.Presented && o.Switch.From == p.World.ID
}
