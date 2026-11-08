package web

import (
	"context"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Switching worlds (SPEC 7.9, 7.10, DESIGN 7.4). The confirmation page
// belongs to the world to switch to; the world to switch from is the active
// one and travels in the form, so a stale form is refused.

func switchPath(id string) string { return gamePath(id, "switch") }

func (s *server) switchRoutes() {
	s.mux.HandleFunc("GET "+PathGameSwitch, s.requireUser(s.handleSwitchForm))
	s.mux.HandleFunc("POST "+PathGameSwitch, s.requireUser(s.handleSwitch))
}

// The choices the confirmation offers when the new world's start checks
// find problems (SPEC 7.4, 7.9): start anyway, or switch and leave the game
// stopped, so the problems wait for the next Start.
const (
	choiceAnyway  = "anyway"
	choiceStopped = "stopped"
)

// SwitchPage confirms a switch (SPEC 7.5, 7.6, 7.9). It runs the switch's
// own checks, so any problem shows here, before the admin confirms
// (SPEC 17.8), and lists every step, including a download of the new
// world's game image when this host lacks it (SPEC 3.7).
type SwitchPage struct {
	From, To world.Summary
	Plan     game.SwitchPlan
	// ImageSize is the download's size in bytes, or 0 when not known.
	ImageSize uint64
	// StartAfter is the "Start after switching" box, offered when the game
	// is stopped; it is ticked by default.
	StartAfter bool
	// Lead and CheckProblems word the new world's pre-start problems, and
	// Fix offers to match its mods to its save (SPEC 9.8, 17.8). Seen
	// fingerprints them for "Switch and start anyway".
	Lead          i18n.Msg
	CheckProblems []i18n.Msg
	Fix           *Offer
	Seen          string
	CountdownForm
	// Changed is set when the switch was not done because what the page
	// showed is no longer true.
	Changed bool
	// Refusal replaces the form when the switch cannot go ahead.
	Refusal i18n.Msg
}

func (SwitchPage) templateName() string { return "switch" }
func (SwitchPage) navItem() string      { return PathWorlds }
func (p SwitchPage) hasErrors() bool    { return !p.CountdownError.IsZero() }

// Problems lists the countdown field's error for the summary.
func (p SwitchPage) Problems() []Problem {
	return problems(Problem{"countdown", p.CountdownError})
}

// Paths the page posts to and links to.
func (p SwitchPage) Path() string       { return switchPath(p.To.ID) }
func (p SwitchPage) WorldPath() string  { return worldPath(p.To.ID) }
func (p SwitchPage) ActivePath() string { return worldPath(p.From.ID) }

// HasProblems reports whether the new world's start checks found problems,
// so the page offers its two choices in place of a plain switch.
func (p SwitchPage) HasProblems() bool { return !p.Plan.Check.OK() }

// CanAnyway reports whether every problem allows a start anyway (SPEC 7.4).
func (p SwitchPage) CanAnyway() bool { return p.HasProblems() && p.Plan.Check.CanStartAnyway() }

// ChangedLead says why a confirmed switch was not done.
func (SwitchPage) ChangedLead() i18n.Msg { return changedLead }

// switchProblemsLead opens the list of the new world's problems. When the
// only problems are mods made for another Factorio, the save is not what
// fails, so the lead says the world cannot run instead.
func switchProblemsLead(check world.PreCheck, name string) i18n.Msg {
	if !onlyModVersions(check) {
		return i18n.M("server.switch.problems_lead", "world", name)
	}
	return i18n.M("server.switch.cannot_run_lead", "world", name)
}

// imageSize returns the download size of a version's game image, or 0 when
// it cannot be had; the page then leaves the size out.
func (s *server) imageSize(ctx context.Context, version string) uint64 {
	if s.releases == nil {
		return 0
	}
	n, err := s.releases.ImageSize(ctx, version)
	if err != nil {
		return 0
	}
	return uint64(n) //nolint:gosec // the Releases contract: a size is never negative
}

// fillSwitchPage asks what only a rendered confirmation shows: the size of
// the image to download and who is online. A switch that goes ahead asks for
// neither.
func (s *server) fillSwitchPage(ctx context.Context, p *SwitchPage) {
	if !p.Refusal.IsZero() {
		return
	}
	if p.Plan.Download {
		p.ImageSize = s.imageSize(ctx, p.To.GameVersion)
	}
	if p.Plan.Running {
		p.Online = s.online(ctx, p.From.ID)
	}
}

// switchBusy reports whether an operation is running on this world or the
// active one, so a switch would be refused.
func (s *server) switchBusy(ctx context.Context, id string) bool {
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

// switchPage builds the confirmation for a switch to to from the
// controller's plan, which holds every rule that could refuse it. from is
// the world the form named, or "" for the active one.
func (s *server) switchPage(ctx context.Context, from string, to world.Summary, form CountdownForm) (SwitchPage, error) {
	p := SwitchPage{To: to, CountdownForm: form, StartAfter: true}
	active, _, err := s.activeWorld(ctx)
	if err != nil {
		return p, err
	}
	p.From = active
	if from == "" {
		from = active.ID
	}
	p.Plan, err = s.game.PlanSwitch(ctx, from, to.ID)
	if err != nil {
		status, msg := gameError(err)
		if status == http.StatusInternalServerError {
			return p, err
		}
		p.Refusal = msg
		return p, nil
	}
	if p.HasProblems() {
		p.Lead = switchProblemsLead(p.Plan.Check, to.Name)
		p.CheckProblems = problemMsgs(p.Plan.Check.Problems)
		p.Fix = syncOffer(p.Plan.Check, to.ID)
		p.Seen = game.AnywayFingerprint(p.Plan.Check, p.Plan.Running)
	}
	return p, nil
}

func (s *server) handleSwitchForm(w http.ResponseWriter, r *http.Request, u store.User) {
	to, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	p, err := s.switchPage(r.Context(), "", to, defaultCountdown())
	if err != nil {
		s.fail(w, r, "build the switch page", err)
		return
	}
	s.fillSwitchPage(r.Context(), &p)
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handleSwitch makes the world in the path the active one (SPEC 7.9). The
// form's "from" is the world the admin saw as active. The checks run again
// first: a problem the page did not show sends the page back with it, so
// the admin decides once, on the confirmation (SPEC 17.8).
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
			Detail: i18n.M("server.switch.no_from")})
		return
	}
	form, stop := readCountdown(r)
	p, err := s.switchPage(r.Context(), from, to, form)
	if err != nil {
		s.fail(w, r, "build the switch page", err)
		return
	}
	// A running game's switch starts the new world (SPEC 7.9); a stopped
	// one's starts it when the box was ticked.
	p.StartAfter = r.PostFormValue("start") == "1"
	if !p.Refusal.IsZero() {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if !form.CountdownError.IsZero() {
		s.fillSwitchPage(r.Context(), &p)
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	// The page's steps were for the game's state it showed (SPEC 17.8); a
	// form without the field counts as stopped.
	shownRunning := r.PostFormValue("running") == "1"
	start, opts := p.Plan.Running || p.StartAfter, game.StartOptions{}
	choice := r.PostFormValue("choice")
	switch choice {
	case choiceStopped:
		start = false
	case choiceAnyway:
		start, opts = true, game.StartOptions{Anyway: true, Seen: r.PostFormValue("seen")}
		p.Changed = !p.CanAnyway() || opts.Seen != p.Seen
	default:
		p.Changed = start && p.HasProblems()
	}
	if shownRunning != p.Plan.Running {
		p.Changed = true
		// A game that was running is wanted running again after the switch,
		// unless the admin chose to leave it stopped.
		start = start || shownRunning && choice != choiceStopped
	}
	if p.Changed {
		p.StartAfter = start
		s.fillSwitchPage(r.Context(), &p)
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	by := actor(u)
	s.beginOn(w, r, []string{from, to.ID}, true, func(ctx context.Context) actionResult {
		err := s.game.Switch(ctx, from, to.ID, by, stop, opts, start)
		res := s.finish(from, game.AuditSwitch, err)
		res.Target, res.From = to.ID, from
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
	p.WorldNames = map[string]string{}
	if sw != nil {
		p.WorldNames[sw.From], p.WorldNames[sw.To] = s.worldName(sw.From), s.worldName(sw.To)
	}
	// A switch the checks refused never began an operation, but its box
	// still names the world it would switch to.
	for _, id := range []string{p.Result.Target, p.Result.From} {
		if p.HaveResult && id != "" && p.WorldNames[id] == "" {
			p.WorldNames[id] = s.worldName(id)
		}
	}
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

// SwitchNote says where a failed switch left the worlds, or is zero.
func (p ServerPage) SwitchNote() i18n.Msg {
	if p.switchRefused() {
		return i18n.M("server.switch.note_refused", "world", p.World.Name)
	}
	o := p.Status.Operation
	if o == nil || o.Switch == nil || !o.Done || o.Error == "" || p.opSuperseded() {
		return i18n.Msg{}
	}
	if o.Switch.Presented {
		return i18n.M("server.switch.note_presented", "world", p.name(o.Switch.To))
	}
	return i18n.M("server.switch.note_none", "world", p.name(o.Switch.From))
}

// Notice words an unfinished switch found at startup (SPEC 7.10), or is
// zero: where it stopped and which world is active.
func (p ServerPage) Notice() i18n.Msg {
	n := p.Status.Notice
	if n == nil {
		return i18n.Msg{}
	}
	from, to := p.name(n.From), p.name(n.To)
	if n.Presented {
		return i18n.M("server.notice.presented", "to", to)
	}
	switch n.Step {
	case store.StepBegun:
		return i18n.M("server.notice.at_stop", "from", from, "to", to)
	case store.StepStopped:
		return i18n.M("server.notice.at_snapshots", "from", from, "to", to)
	case store.StepSnapshotted:
		return i18n.M("server.notice.at_check", "from", from, "to", to)
	case store.StepChecked:
		return i18n.M("server.notice.at_present", "from", from, "to", to)
	}
	return i18n.M("server.notice.at_step", "from", from, "to", to, "step", n.Step)
}

// movedSentence is what the reloaded page reads out: which world is now the
// active one, then how the action ended when that is known.
func (p ServerPage) movedSentence() i18n.Msg {
	name := p.World.Name
	if sw := p.Status.Operation.Switch; sw != nil {
		name = p.name(sw.To)
	}
	moved := i18n.M("server.switch.moved", "world", name)
	if said := resultAnnouncement(p); !said.IsZero() {
		return join(moved, said)
	}
	return moved
}

// Moved reports whether this page's world stopped being the active one in a
// switch, so the page must load the Server page again to show the right
// world. It
// waits until the operation has ended and the result is stored, so the
// sentence the next page reads out always says how the action ended. Until
// then both worlds' pages show the same step list, so the old page is not
// stale.
func (p ServerPage) Moved() bool {
	o := p.Status.Operation
	if o == nil || !o.Done || !p.HaveResult {
		return false
	}
	return o.Switch != nil && o.Switch.Presented && o.Switch.From == p.World.ID
}
