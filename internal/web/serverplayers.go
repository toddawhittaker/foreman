package web

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

// serverPlayersShown is how many players the Server page lists before it
// links to the full list on the world's Overview page (SPEC 8.1).
const serverPlayersShown = 10

// ServerPlayer is one row of the Server page's Players panel: a player online
// now, with their actions, or a player of the save who is not (SPEC 8.1,
// 8.14).
type ServerPlayer struct {
	PlayerRow
	// Online marks a player the game lists now; only they get the chip, the
	// session length and the actions (SPEC 8.2).
	Online bool
	// PlayTime is the save's play time, when HavePlayTime.
	PlayTime      time.Duration
	HavePlayTime  bool
	LastSeen      time.Time
	BeforeForeman bool
}

// PlayTimeText says the save's play time in hours and minutes.
func (r ServerPlayer) PlayTimeText() i18n.Msg { return playTime(r.PlayTime) }

// SessionText says how long an online player has been on (SPEC 8.1).
func (r ServerPlayer) SessionText() i18n.Msg {
	if r.Joined.IsZero() {
		return i18n.M("server.players.unknown")
	}
	return durationText(r.Duration)
}

// TotalText says the save's play time for an online player the record lists.
func (r ServerPlayer) TotalText() i18n.Msg {
	return i18n.M("server.players.total_time", "total", playTime(r.PlayTime))
}

// mergePlayers lists who is online first, longest session first, then the
// rest of the save by play time, most first (SPEC 8.1). A name in both lists
// is one row, online, carrying the save's figures.
func mergePlayers(online []PlayerRow, save []game.SavePlayer) []ServerPlayer {
	out := make([]ServerPlayer, 0, len(online)+len(save))
	at := map[string]int{}
	for _, r := range online {
		at[strings.ToLower(r.Name)] = len(out)
		out = append(out, ServerPlayer{PlayerRow: r, Online: true})
	}
	for _, sp := range save {
		if i, ok := at[strings.ToLower(sp.Name)]; ok {
			out[i].PlayTime, out[i].HavePlayTime, out[i].LastSeen, out[i].BeforeForeman = sp.PlayTime, true, sp.LastSeen, sp.BeforeForeman
			continue
		}
		out = append(out, ServerPlayer{PlayerRow: PlayerRow{Player: game.Player{Name: sp.Name}},
			PlayTime: sp.PlayTime, HavePlayTime: true, LastSeen: sp.LastSeen, BeforeForeman: sp.BeforeForeman})
	}
	slices.SortStableFunc(out, func(a, b ServerPlayer) int {
		switch {
		case a.Online != b.Online:
			if a.Online {
				return -1
			}
			return 1
		case a.Online:
			// A session of unknown length sorts after the known ones.
			if c := cmp.Compare(b.sessionKey(), a.sessionKey()); c != 0 {
				return c
			}
		default:
			if c := cmp.Compare(b.PlayTime, a.PlayTime); c != 0 {
				return c
			}
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

func (r ServerPlayer) sessionKey() time.Duration {
	if r.Joined.IsZero() {
		return -1
	}
	return r.Duration
}

// savePlayersListed is the save's players when the view has a list of them.
func (p ServerPage) savePlayersListed() []game.SavePlayer {
	if !p.HaveSavePlayers || !p.savePlayersKnown() {
		return nil
	}
	return p.SavePlayers.Players
}

// mergedPlayers is every player the panel could show: the save's, and anyone
// online who is not in its record yet, in the panel's order.
func (p ServerPage) mergedPlayers() []ServerPlayer {
	return mergePlayers(p.PlayerRows(), p.savePlayersListed())
}

// PlayersPanel is the Players panel's rows and counts, worked out once per
// render (SPEC 8.1).
type PlayersPanel struct {
	// Rows are the rows shown: everyone online, then the others until there
	// are serverPlayersShown rows.
	Rows []ServerPlayer
	// Total is every player of the save and everyone online, shown or not.
	Total, Online int
	Caption       i18n.Msg
}

// More says some players are not shown, so the panel links to the full list.
func (v PlayersPanel) More() bool { return v.Total > len(v.Rows) }

// PlayersPanel builds the panel. An online player's actions live only in
// their row (SPEC 8.2), so the cap never drops one.
func (p ServerPage) PlayersPanel() PlayersPanel {
	all := p.mergedPlayers()
	v := PlayersPanel{Total: len(all)}
	for _, r := range all {
		if r.Online {
			v.Online++
		}
	}
	v.Rows = all[:min(len(all), max(serverPlayersShown, v.Online))]
	v.Caption = p.playersCaption(v)
	return v
}

// playersCaption names the table by how many are online and how many the
// save has. While a stop warns players, the online ones are who was online
// when it began (SPEC 7.6). With no record to count, only who is online.
func (p ServerPage) playersCaption(v PlayersPanel) i18n.Msg {
	listed := p.HaveSavePlayers && p.savePlayersKnown()
	stop := !p.PlayersKnown
	switch {
	case v.Online == 0:
		return i18n.M("server.players.table_caption_saved", "total", v.Total)
	case !listed && stop:
		return i18n.M("server.players.table_caption_online_stop", "online", v.Online)
	case !listed:
		return i18n.M("server.players.table_caption_online", "online", v.Online)
	case stop:
		return i18n.M("server.players.table_caption_stop", "online", v.Online, "total", v.Total)
	}
	return i18n.M("server.players.table_caption", "online", v.Online, "total", v.Total)
}

// NobodyYet says the save is known to have no players at all, so the panel
// says no one has played rather than why nobody is online (SPEC 8.1).
func (p ServerPage) NobodyYet() bool {
	return p.HaveSavePlayers && p.savePlayersKnown() && len(p.SavePlayers.Players) == 0 && p.PlayerTable() == nil
}

// savePlayersKnown says the view lists the save's players, even if none.
func (p ServerPage) savePlayersKnown() bool {
	switch p.SavePlayers.State {
	case game.SavePlayersReady, game.SavePlayersTrackedOnly, game.SavePlayersNotSupported:
		return true
	}
	return false
}

// PlayersInexact and PlayersTrackedSince carry the notes of SPEC 8.14 into
// the panel, compactly; the read button stays on the world's page.
func (p ServerPage) PlayersInexact() bool {
	return p.HaveSavePlayers && p.SavePlayers.State == game.SavePlayersReady && p.SavePlayers.Inexact
}

func (p ServerPage) PlayersTrackedSince() time.Time {
	if !p.HaveSavePlayers {
		return time.Time{}
	}
	switch p.SavePlayers.State {
	case game.SavePlayersTrackedOnly, game.SavePlayersNotSupported:
		return p.SavePlayers.TrackedSince
	}
	return time.Time{}
}

// onlineNames is who the panel marks online, for the live announcement.
func (p ServerPage) onlineNames() []string {
	var names []string
	for _, r := range p.PlayerRows() {
		names = append(names, r.Name)
	}
	return names
}

// playersChanged words who joined and who left between two lists of who is
// online, or zero when nobody did.
func playersChanged(before, after []string) i18n.Msg {
	// Factorio names ignore case, as mergePlayers does.
	was := map[string]bool{}
	for _, n := range before {
		was[strings.ToLower(n)] = true
	}
	is := map[string]bool{}
	var joined, left []string
	for _, n := range after {
		is[strings.ToLower(n)] = true
		if !was[strings.ToLower(n)] {
			joined = append(joined, n)
		}
	}
	for _, n := range before {
		if !is[strings.ToLower(n)] {
			left = append(left, n)
		}
	}
	var m i18n.Msg
	if len(joined) > 0 {
		m = i18n.M("server.players.announce_joined", "names", strings.Join(joined, ", "))
	}
	if len(left) > 0 {
		l := i18n.M("server.players.announce_left", "names", strings.Join(left, ", "))
		if m.IsZero() {
			return l
		}
		return join(m, l)
	}
	return m
}
