package web

import (
	"errors"
	"net/http"
	"slices"
	"sync"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Player actions (SPEC 8.2, DESIGN 8.12). Every action posts to one path
// with the player's name in a "name" field, so the name never needs escaping
// in a path; kick and ban confirm first at the same path.
const PathPlayerAction = "/worlds/{id}/players/{action}"

// returnField names the form and query value that says where a player action
// goes back to (checked as SPEC 14.17).
const returnField = "return"

func playerPath(id, action string) string { return worldPath(id) + "/players/" + action }

func (s *server) playersRoutes() {
	s.playerResults = &playerResults{byUser: map[int64]PlayerResult{}}
	s.mux.HandleFunc("GET "+PathPlayerAction, s.requireUser(s.handlePlayerConfirm))
	s.mux.HandleFunc("POST "+PathPlayerAction, s.requireUser(s.handlePlayerAction))
}

// PlayerResult is the outcome of one player action, shown once on the page
// the action went back to (DESIGN 8.12, 8.13).
type PlayerResult struct {
	WorldID, WorldName string
	Action             game.PlayerAction
	Name               string
	// Reason is, after an unban, the reason the ban had, so Undo bans again
	// with it.
	Reason string
	// Error is zero when the action was done.
	Error i18n.Msg
	// Reply is the game's own answer to a command it refused, in English.
	Reply string
	// NameError is true when the name failed the check of SPEC 14.9, so the
	// page shows it as a field error.
	NameError bool
}

// Done is the result sentence of a done action.
func (p PlayerResult) Done() i18n.Msg {
	n, w := p.Name, p.WorldName
	switch p.Action {
	case game.PlayerKick:
		return i18n.M("players.done.kicked", "name", n, "world", w)
	case game.PlayerBan:
		return i18n.M("players.done.banned", "name", n, "world", w)
	case game.PlayerUnban:
		return i18n.M("players.done.unbanned", "name", n, "world", w)
	case game.PlayerPromote:
		return i18n.M("players.done.promoted", "name", n, "world", w)
	case game.PlayerDemote:
		return i18n.M("players.done.demoted", "name", n, "world", w)
	case game.PlayerWhitelistAdd:
		return i18n.M("players.done.whitelist_added", "name", n, "world", w)
	}
	return i18n.M("players.done.whitelist_removed", "name", n, "world", w)
}

// UndoAction is the action that undoes this one, "" when there is none.
func (p PlayerResult) UndoAction() string {
	switch p.Action {
	case game.PlayerPromote:
		return string(game.PlayerDemote)
	case game.PlayerDemote:
		return string(game.PlayerPromote)
	case game.PlayerBan:
		return string(game.PlayerUnban)
	case game.PlayerUnban:
		return string(game.PlayerBan)
	case game.PlayerWhitelistAdd:
		return string(game.PlayerWhitelistRemove)
	case game.PlayerWhitelistRemove:
		return string(game.PlayerWhitelistAdd)
	}
	return ""
}

// UndoHidden is the Undo button's whole name, which says what it does.
func (p PlayerResult) UndoHidden() i18n.Msg {
	switch p.Action {
	case game.PlayerPromote:
		return i18n.M("players.undo.demote", "name", p.Name)
	case game.PlayerDemote:
		return i18n.M("players.undo.promote", "name", p.Name)
	case game.PlayerUnban:
		return i18n.M("players.undo.ban", "name", p.Name)
	case game.PlayerWhitelistAdd:
		return i18n.M("players.undo.whitelist_remove", "name", p.Name)
	}
	return i18n.M("players.undo.whitelist_add", "name", p.Name)
}

// UndoPath is where Undo posts.
func (p PlayerResult) UndoPath() string { return playerPath(p.WorldID, p.UndoAction()) }

// IsBan is true for a ban, whose undo is shown as "Unban" rather than Undo.
func (p PlayerResult) IsBan() bool { return p.Action == game.PlayerBan }

// playerResults keeps each user's last player action result until a page
// shows it, so a reload drops it.
type playerResults struct {
	mu     sync.Mutex
	byUser map[int64]PlayerResult
}

func (p *playerResults) put(userID int64, r PlayerResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byUser[userID] = r
}

// take returns and forgets the user's result for the world, or nil.
func (p *playerResults) take(userID int64, worldID string) *PlayerResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.byUser[userID]
	if !ok || r.WorldID != worldID {
		return nil
	}
	delete(p.byUser, userID)
	return &r
}

// PlayerActionPage confirms a kick or a ban (SPEC 8.2, DESIGN 8.12).
type PlayerActionPage struct {
	World  world.Summary
	Action game.PlayerAction
	Name   string
	Reason string
	// Return is the checked page Cancel and the result go back to; empty
	// means the Server page.
	Return string
	// Online is true when the player is known to be in the game, Gone when
	// they are known not to be. Neither when the game cannot say.
	Online, Gone bool
	// Error says why nothing can be sent now, for one that the game is not
	// running; the page then has no button.
	Error       i18n.Msg
	ReasonError i18n.Msg
}

func (PlayerActionPage) templateName() string { return "playeraction" }
func (PlayerActionPage) navItem() string      { return PathServer }
func (p PlayerActionPage) hasErrors() bool    { return !p.ReasonError.IsZero() || !p.Error.IsZero() }

// Problems lists the reason field's error for the summary.
func (p PlayerActionPage) Problems() []Problem {
	return problems(Problem{"player-reason", p.ReasonError})
}

func (p PlayerActionPage) Path() string { return playerPath(p.World.ID, string(p.Action)) }
func (p PlayerActionPage) IsBan() bool  { return p.Action == game.PlayerBan }

// Back is where Cancel goes.
func (p PlayerActionPage) Back() string {
	if p.Return != "" {
		return p.Return
	}
	return PathServer
}

func (PlayerActionPage) MaxReason() int { return gamesettings.MaxReasonChars }

// playerAction reads the action from the path, answering 404 itself for one
// that is not a player action, or, when confirm is set, one that needs no
// confirmation.
func (s *server) playerAction(w http.ResponseWriter, r *http.Request, u store.User, confirm bool) (game.PlayerAction, bool) {
	a := game.PlayerAction(r.PathValue("action"))
	if !a.Known() || (confirm && !a.TakesReason()) || s.game == nil {
		s.notFound(w, r, u)
		return "", false
	}
	return a, true
}

// handlePlayerConfirm shows the kick or ban confirmation. It runs the checks
// the action would, so a problem shows here and not after the click
// (SPEC 17.8). A name that fails the check goes back to the page it came
// from as a field error, never sent.
func (s *server) handlePlayerConfirm(w http.ResponseWriter, r *http.Request, u store.User) {
	action, ok := s.playerAction(w, r, u, true)
	if !ok {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	q := r.URL.Query()
	p := PlayerActionPage{World: sum, Action: action, Name: q.Get("name"), Return: returnPath(q.Get(returnField))}
	if err := game.CheckPlayerAction(action, p.Name, ""); err != nil {
		s.playerResults.put(u.ID, PlayerResult{WorldID: sum.ID, WorldName: sum.Name, Action: action, Name: p.Name,
			Error: i18n.M("players.error.name", "max", gamesettings.MaxNameBytes), NameError: true})
		http.Redirect(w, r, p.Back(), http.StatusSeeOther)
		return
	}
	if s.game.Status(sum.ID).State != game.StateRunning {
		p.Error = i18n.M("players.error.not_running", "world", sum.Name)
	} else if o := s.online(r.Context(), sum.ID); o.Asked && !o.Failed {
		p.Online = slices.Contains(o.Names, p.Name)
		p.Gone = !p.Online
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handlePlayerAction sends one player action to the running game and goes
// back to the page it came from, which shows the result once (DESIGN 8.12).
// A reason refused on the confirmation shows that page again.
func (s *server) handlePlayerAction(w http.ResponseWriter, r *http.Request, u store.User) {
	action, ok := s.playerAction(w, r, u, false)
	if !ok {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	name, reason := r.PostFormValue("name"), r.PostFormValue("reason")
	back := returnPath(r.PostFormValue(returnField))
	res := PlayerResult{WorldID: sum.ID, WorldName: sum.Name, Action: action, Name: name}
	if action == game.PlayerUnban {
		res.Reason = s.banReason(sum.ID, name)
	}
	err := s.game.Player(r.Context(), sum.ID, actor(u), action, name, reason)
	if errors.Is(err, game.ErrPlayerReason) && action.TakesReason() {
		p := PlayerActionPage{World: sum, Action: action, Name: name, Reason: reason, Return: back,
			ReasonError: i18n.M("players.error.reason", "max", gamesettings.MaxReasonChars)}
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	if err != nil {
		res.Error, res.Reply, res.NameError = s.playerError(r, sum, name, err)
	}
	s.playerResults.put(u.ID, res)
	if back == "" {
		back = PathServer
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// banReason is the reason the ban list gives name, or "" when it has none
// that could be sent again.
func (s *server) banReason(id, name string) string {
	read, err := s.worlds.ReadConfig(id, world.ServerBanListFile)
	if err != nil || !read.Found {
		return ""
	}
	bans, err := gamesettings.ParseBanList(read.Data)
	if err != nil {
		return ""
	}
	for _, b := range bans {
		if b.Name == name && gamesettings.ValidateReason(b.Reason) == nil {
			return b.Reason
		}
	}
	return ""
}

// playerError words a player action that was not done.
func (s *server) playerError(r *http.Request, sum world.Summary, name string, err error) (msg i18n.Msg, reply string, nameErr bool) {
	if refused, ok := errors.AsType[*game.PlayerRefusedError](err); ok {
		return i18n.M("players.error.refused", "name", name, "world", sum.Name), refused.Reply, false
	}
	switch {
	case errors.Is(err, game.ErrPlayerName):
		return i18n.M("players.error.name", "max", gamesettings.MaxNameBytes), "", true
	case errors.Is(err, game.ErrPlayerReason):
		return i18n.M("players.error.reason", "max", gamesettings.MaxReasonChars), "", false
	case errors.Is(err, game.ErrWhitelistOff):
		return i18n.M("players.error.whitelist_off", "world", sum.Name), "", false
	case errors.Is(err, game.ErrRCONUnreachable):
		return i18n.M("players.error.rcon", "name", name, "world", sum.Name), "", false
	case errors.Is(err, game.ErrNotRunning):
		return i18n.M("players.error.not_running", "world", sum.Name), "", false
	}
	status, msg := gameError(err)
	if status == http.StatusInternalServerError || msg.IsZero() {
		s.log.Error("player action", "path", r.URL.Path, "world", sum.ID, "error", err)
		msg = i18n.M("players.error.failed", "world", sum.Name)
	}
	return msg, "", false
}
