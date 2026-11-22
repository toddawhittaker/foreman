package web

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The players of a save on a world's Overview page (SPEC 8.14, 8.16). The
// world page renders the panel on the server, so it works without script; a
// read it starts runs in the background and never holds the page back. With
// script, the panel asks again every few seconds while a read runs.
const (
	PathWorldPlayers     = "/worlds/{id}/players"
	PathWorldPlayersRead = "/worlds/{id}/players/read"
	// savePlayersAnchor is the id of the panel, so the Server page's "N
	// players in this save" link can point at it.
	savePlayersAnchor = "save-players"
)

func playersPath(id string) string     { return worldPath(id) + "/players" }
func playersReadPath(id string) string { return worldPath(id) + "/players/read" }

// savePlayersRoutes registers the panel and its "Read again" button.
func (s *server) savePlayersRoutes() {
	s.mux.HandleFunc("GET "+PathWorldPlayers, s.requireUser(s.handleSavePlayers))
	s.mux.HandleFunc("POST "+PathWorldPlayersRead, s.requireUser(s.handleSavePlayersRead))
}

// The kinds of panel, one for each thing the page can say.
const (
	playersUnavailable = "unavailable" // no server control, or the view could not be read
	playersReading     = "reading"     // a read runs now
	playersReady       = "ready"       // a record matches the save
	playersTracked     = "tracked"     // a read failed: tracked since a date
	playersNever       = "never"       // there is a save, and nothing has read it
	playersNoSave      = "no-save"     // there is no save to read
	playersUnsupported = "unsupported" // the game version cannot be read
)

// SavePlayersPanel is the players of a save, as the Overview page shows them.
type SavePlayersPanel struct {
	World world.Summary
	// Kind is one of the players* kinds.
	Kind string
	// Failure words why a read failed, for the tracked kind.
	Failure i18n.Msg
	// Inexact says a crash or a gap may have hidden time (SPEC 8.14, drift).
	Inexact      bool
	TrackedSince time.Time
	// Total is the save's play time, when Known (SPEC 8.15).
	Total      i18n.Msg
	TotalKnown bool
	Rows       []SavePlayerRow
	// CanRead offers the read button. First says it is the first read of
	// this save, which the button's words follow.
	CanRead bool
	First   bool
	// Running is true while a game runs on this Foreman, so a read needs
	// the memory note; Memory is what the game uses now, when HaveMemory.
	Running bool
	// Live says this world's game runs, so the panel asks again every few
	// seconds for players who joined or left.
	Live       bool
	Memory     uint64
	HaveMemory bool
	// Refused says why a click on the read button was refused.
	Refused i18n.Msg
	// Started says the admin's click began the read this panel shows, so the
	// poll that finds it ended says so once (see Announce). Nothing takes
	// focus: a focus that every poll's swap restores would be read out each
	// time.
	Started bool
	// Announce is said once in the page's live region, which no swap
	// replaces: the read began, or how it ended.
	Announce i18n.Msg
}

// SavePlayerRow is one player of the save, ready to show.
type SavePlayerRow struct {
	Name          string
	PlayTime      i18n.Msg
	Online        bool
	LastSeen      time.Time
	BeforeForeman bool
}

func (SavePlayersPanel) templateName() string { return "world" }

// Paths the panel loads from and posts to.
func (p SavePlayersPanel) PlayersPath() string     { return playersPath(p.World.ID) }
func (p SavePlayersPanel) PlayersReadPath() string { return playersReadPath(p.World.ID) }

// Count is the number of players listed.
func (p SavePlayersPanel) Count() int { return len(p.Rows) }

// Polls says the panel asks again, because a read runs.
func (p SavePlayersPanel) Polls() bool { return p.Kind == playersReading }

// Version says what the panel shows of the players, so a poll that finds
// the same answer can be told nothing changed.
func (p SavePlayersPanel) Version() string {
	var b strings.Builder
	fmt.Fprint(&b, p.Kind, p.Total, p.TotalKnown, p.Inexact)
	for _, r := range p.Rows {
		fmt.Fprint(&b, "|", r.Name, r.PlayTime, r.Online, r.LastSeen.UnixNano(), r.BeforeForeman)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(b.String())) // a hash never fails to write
	return fmt.Sprintf("%x", h.Sum64())
}

// unchangedSince says a poll that carried version v finds what it shows.
func (p SavePlayersPanel) unchangedSince(v string) bool {
	return v != "" && p.Kind != playersReading && v == p.Version()
}

// LivePollPath is where a running world's panel asks, with the version it shows.
func (p SavePlayersPanel) LivePollPath() string {
	return p.PlayersPath() + "?v=" + p.Version()
}

// PollPath is where the poll asks. A read the admin started carries that, so
// the poll that sees it end can say so.
func (p SavePlayersPanel) PollPath() string {
	if p.Started {
		return p.PlayersPath() + "?started=1"
	}
	return p.PlayersPath()
}

// endOfRead is the words for a read the admin started that has ended, or zero
// while it runs or when it left nothing to say.
func (p SavePlayersPanel) endOfRead() i18n.Msg {
	switch p.Kind {
	case playersReady:
		return i18n.M("saveplayers.announce.done")
	case playersTracked:
		return p.Failure
	}
	return i18n.Msg{}
}

// ShowTable says the players are listed: a read worked, or only tracked time
// counts.
func (p SavePlayersPanel) ShowTable() bool { return len(p.Rows) > 0 }

// playTime says d as hours and minutes (SPEC 8.15).
func playTime(d time.Duration) i18n.Msg {
	d = max(d, 0)
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	if h == 0 {
		return i18n.M("saveplayers.time.minutes", "minutes", m)
	}
	return i18n.M("saveplayers.time.hours", "hours", h, "minutes", m)
}

// failureWords words a failure id of a read (SPEC 8.16).
func failureWords(id string) i18n.Msg {
	switch id {
	case saveplayers.ResultImageMissing:
		return i18n.M("saveplayers.failure.image_missing")
	case saveplayers.ResultOutOfMemory:
		return i18n.M("saveplayers.failure.out_of_memory")
	case saveplayers.ResultTimeout:
		return i18n.M("saveplayers.failure.timeout")
	case saveplayers.ResultModsDiffer:
		return i18n.M("saveplayers.failure.mods_differ")
	case saveplayers.ResultSaveNewer:
		return i18n.M("saveplayers.failure.save_newer")
	case saveplayers.ResultBadOutput:
		return i18n.M("saveplayers.failure.bad_output")
	}
	return i18n.M("saveplayers.failure.other")
}

// readError words an error of a read that did not start, or ok false when
// the error is none of the read's own.
func readError(err error) (i18n.Msg, bool) {
	switch {
	case errors.Is(err, game.ErrSaveCopyImageMissing):
		return failureWords(saveplayers.ResultImageMissing), true
	case errors.Is(err, game.ErrSaveCopyOutOfMemory):
		return failureWords(saveplayers.ResultOutOfMemory), true
	case errors.Is(err, game.ErrSaveCopyTimeout):
		return failureWords(saveplayers.ResultTimeout), true
	case errors.Is(err, game.ErrSaveCopyModsDiffer):
		return failureWords(saveplayers.ResultModsDiffer), true
	case errors.Is(err, game.ErrSaveCopySaveNewer):
		return failureWords(saveplayers.ResultSaveNewer), true
	case errors.Is(err, game.ErrSaveCopyBadOutput):
		return failureWords(saveplayers.ResultBadOutput), true
	}
	return i18n.Msg{}, false
}

// panelOf makes the panel of a view. It is pure, so every state of the page
// is tested without a game. hasSave comes from the world's files
// (the view does not say it); running and memory from the game.
func panelOf(sum world.Summary, v game.SavePlayersView, hasSave, running bool, memory uint64) SavePlayersPanel {
	p := SavePlayersPanel{World: sum, TrackedSince: v.TrackedSince, Running: running, Memory: memory, HaveMemory: running && memory > 0}
	if v.GameTimeKnown {
		p.Total, p.TotalKnown = playTime(v.GameTime), true
	}
	for _, pl := range v.Players {
		p.Rows = append(p.Rows, SavePlayerRow{Name: pl.Name, PlayTime: playTime(pl.PlayTime),
			Online: pl.Online, LastSeen: pl.LastSeen, BeforeForeman: pl.BeforeForeman})
	}
	switch v.State {
	case game.SavePlayersReading:
		p.Kind = playersReading
	case game.SavePlayersReady:
		p.Kind, p.CanRead, p.Inexact = playersReady, true, v.Inexact
	case game.SavePlayersTrackedOnly:
		p.Kind, p.CanRead, p.Failure = playersTracked, true, failureWords(v.Failure)
	case game.SavePlayersNotSupported:
		p.Kind = playersUnsupported
	case game.SavePlayersNone:
		// "none" is also what a world with a save that nothing read yet
		// answers while a game runs, so the folder says which.
		if hasSave {
			p.Kind, p.CanRead, p.First = playersNever, true, true
		} else {
			p.Kind = playersNoSave
		}
	default:
		p.Kind = playersUnavailable
	}
	return p
}

// fillSavePlayers puts the panel on a world's page: only a world this
// Foreman can read, and only with server control (SPEC 8.14).
func (s *server) fillSavePlayers(ctx context.Context, p *WorldPage) {
	if s.game == nil || p.World.Newer || p.World.Unreadable {
		return
	}
	panel := s.savePlayersPanel(ctx, p.World)
	p.Players = &panel
}

// savePlayersPanel reads what the panel needs. A view that cannot be read
// is an unavailable panel, never an error page: the Overview page works
// without it.
func (s *server) savePlayersPanel(ctx context.Context, sum world.Summary) SavePlayersPanel {
	if s.game == nil {
		return SavePlayersPanel{World: sum, Kind: playersUnavailable}
	}
	v, err := s.game.SavePlayers(ctx, sum.ID)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("read the players of a save", "world", sum.ID, "error", err)
		}
		return SavePlayersPanel{World: sum, Kind: playersUnavailable}
	}
	_, hasSave, _ := s.worlds.LoadTarget(sum.ID)
	running, memory, activeID := s.runningMemory(ctx)
	p := panelOf(sum, v, hasSave, running, memory)
	p.Live = running && activeID == sum.ID
	return p
}

// runningMemory says whether a game runs on this Foreman, and the memory it
// uses now, which a read beside it needs about as much of (SPEC 8.14).
func (s *server) runningMemory(ctx context.Context) (running bool, memory uint64, activeID string) {
	active, found, err := s.activeWorld(ctx)
	if err != nil || !found {
		return false, 0, ""
	}
	st := s.game.Status(active.ID)
	switch st.State {
	case game.StateRunning, game.StateStarting, game.StateStopping:
		return true, st.MemoryBytes, active.ID
	}
	return false, 0, ""
}

// handleSavePlayers answers the panel, which the world page asks for.
func (s *server) handleSavePlayers(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	if !isHTMX(r) {
		http.Redirect(w, r, worldPath(sum.ID)+"#"+savePlayersAnchor, http.StatusSeeOther)
		return
	}
	p := s.savePlayersPanel(r.Context(), sum)
	// A live poll that finds what the page shows changes nothing: no swap,
	// so focus and the screen reader's place stay.
	if p.unchangedSince(r.URL.Query().Get("v")) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// A poll of a read the admin started: keep asking while it runs, and say
	// once how it ended.
	if r.URL.Query().Get("started") == "1" {
		if p.Kind == playersReading {
			p.Started = true
		} else {
			p.Announce = p.endOfRead()
		}
	}
	s.fragment(w, r, p, "save-players-body")
}

// handleSavePlayersRead reads the newest save again, on an admin's click
// (SPEC 8.14). It is not audited: a read changes no file the game uses.
func (s *server) handleSavePlayersRead(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	if s.game == nil {
		s.notFound(w, r, u)
		return
	}
	var refused i18n.Msg
	if err := s.game.ReadSavePlayersAgain(r.Context(), sum.ID, actor(u)); err != nil {
		msg, own := readError(err)
		if !own {
			_, m, ok := s.refusal(w, r, u, "read the players of a save", err)
			if !ok {
				return
			}
			msg = m
		}
		refused = msg
	}
	if !isHTMX(r) {
		if refused.IsZero() {
			http.Redirect(w, r, worldPath(sum.ID)+"#"+savePlayersAnchor, http.StatusSeeOther)
			return
		}
		// Without script the reason has no fragment to land in: show the page.
		page := s.worldPage(r.Context(), sum, sum.Name, i18n.Msg{})
		s.fillSavePlayers(r.Context(), &page)
		if page.Players != nil {
			page.Players.Refused = refused
		}
		s.page(w, r, http.StatusOK, u.Username, page)
		return
	}
	p := s.savePlayersPanel(r.Context(), sum)
	p.Refused = refused
	if p.Kind == playersReading {
		p.Started, p.Announce = true, i18n.M("saveplayers.announce.started")
	}
	s.fragment(w, r, p, "save-players-body")
}
