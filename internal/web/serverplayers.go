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

// PlayTimeText says the row's play time in hours and minutes.
func (r ServerPlayer) PlayTimeText() i18n.Msg { return playTime(r.PlayTime) }

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
	if !p.HaveSavePlayers {
		return nil
	}
	switch p.SavePlayers.State {
	case game.SavePlayersReady, game.SavePlayersTrackedOnly, game.SavePlayersNotSupported:
		return p.SavePlayers.Players
	}
	return nil
}

// AllPlayers is every row the panel could show, in its order.
func (p ServerPage) AllPlayers() []ServerPlayer {
	return mergePlayers(p.PlayerRows(), p.savePlayersListed())
}

// ShownPlayers is the panel's rows, at most serverPlayersShown.
func (p ServerPage) ShownPlayers() []ServerPlayer {
	all := p.AllPlayers()
	return all[:min(len(all), serverPlayersShown)]
}

// PlayersCount is how many rows there are in all, for the "All N players"
// link; MorePlayers says some are not shown.
func (p ServerPage) PlayersCount() int { return len(p.AllPlayers()) }
func (p ServerPage) MorePlayers() bool { return p.PlayersCount() > serverPlayersShown }

// PlayersCaption names the table: who is online when anyone is, or else how
// many players the save has.
func (p ServerPage) PlayersCaption() i18n.Msg {
	if t := p.PlayerTable(); t != nil {
		return t.Caption
	}
	return i18n.M("server.playtime.players", "count", p.PlayersCount())
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
	was := map[string]bool{}
	for _, n := range before {
		was[n] = true
	}
	is := map[string]bool{}
	var joined, left []string
	for _, n := range after {
		is[n] = true
		if !was[n] {
			joined = append(joined, n)
		}
	}
	for _, n := range before {
		if !is[n] {
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
