package web

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

func playersHTML(t *testing.T, p ServerPage) string {
	t.Helper()
	var b strings.Builder
	if err := newTestRenderer(t).Fragment(&b, i18n.English, p, "server-players"); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func names(rows []ServerPlayer) string {
	var out []string
	for _, r := range rows {
		n := r.Name
		if r.Online {
			n += "*"
		}
		out = append(out, n)
	}
	return strings.Join(out, ",")
}

// SPEC 8.1: online players first, longest session first, then the rest of the
// save by play time, most first. A player in both lists is one online row.
func TestMergePlayersOrdersOnlineThenPlayTime(t *testing.T) {
	now := time.Now()
	online := []PlayerRow{
		{Player: game.Player{Name: "short", Joined: now, Duration: time.Minute}},
		{Player: game.Player{Name: "unknown"}},
		{Player: game.Player{Name: "Long", Joined: now, Duration: time.Hour}},
	}
	save := []game.SavePlayer{
		{Name: "little", PlayTime: time.Hour},
		{Name: "long", PlayTime: 9 * time.Hour},
		{Name: "most", PlayTime: 50 * time.Hour},
		{Name: "some", PlayTime: 5 * time.Hour},
	}
	got := mergePlayers(online, save)
	if want := "Long*,short*,unknown*,most,some,little"; names(got) != want {
		t.Fatalf("order %s, want %s", names(got), want)
	}
	if !got[0].HavePlayTime || got[0].PlayTime != 9*time.Hour {
		t.Errorf("online row lost the save's figures: %+v", got[0])
	}
}

// SPEC 8.1: at most ten rows, then "All N players" with the full count.
func TestServerPlayersCapAtTenWithLink(t *testing.T) {
	var save []game.SavePlayer
	for i := range 12 {
		save = append(save, game.SavePlayer{Name: fmt.Sprintf("p%02d", i), PlayTime: time.Duration(i) * time.Hour})
	}
	p := serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, Players: save}))
	v := p.PlayersPanel()
	if len(v.Rows) != 10 {
		t.Fatalf("shown %d, want 10", len(v.Rows))
	}
	if v.Total != 12 || !v.More() {
		t.Fatalf("total %d more %v", v.Total, v.More())
	}
	html := playersHTML(t, p)
	if !strings.Contains(html, "All 12 players") || strings.Contains(html, "p00") || strings.Contains(html, "p01<") {
		t.Errorf("html: %s", html)
	}
	if strings.Contains(html, "badge--online") {
		t.Error("a stopped game marks nobody online")
	}
	p = serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, Players: save[:3]}))
	if strings.Contains(playersHTML(t, p), "act-all-players") {
		t.Error("a short list links to nothing more")
	}
}

// SPEC 8.1: the empty state only when the save has no players at all.
func TestServerPlayersEmptyOnlyWithoutPlayers(t *testing.T) {
	p := serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady}))
	if html := playersHTML(t, p); !strings.Contains(html, "No one has played this world yet.") {
		t.Errorf("empty save: %s", html)
	}
	p = serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady,
		Players: []game.SavePlayer{{Name: "alice", PlayTime: time.Hour, LastSeen: time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)}}}))
	html := playersHTML(t, p)
	if !strings.Contains(html, "alice") || !strings.Contains(html, "1 h 0 min") || !strings.Contains(html, "Last seen") {
		t.Errorf("stopped list: %s", html)
	}
}

// SPEC 8.1, 16.3: one sentence per update, naming who joined and who left.
func TestPlayersChangedIsOneSentence(t *testing.T) {
	if m := playersChanged([]string{"a"}, []string{"a"}); !m.IsZero() {
		t.Errorf("no change said %v", m)
	}
	got := i18n.English.Format(playersChanged([]string{"a", "b"}, []string{"b", "c"}))
	if got != "c joined. a left." {
		t.Errorf("got %q", got)
	}
}

func onlineRows(n int) []game.Player {
	var out []game.Player
	for i := range n {
		out = append(out, game.Player{Name: fmt.Sprintf("on%02d", i), Joined: time.Now(), Duration: time.Duration(i) * time.Minute})
	}
	return out
}

// SPEC 8.1, 8.2: the cap never drops an online player, whose actions live only
// in their row; offline players fill the rows up to ten.
func TestServerPlayersCapKeepsEveryOnlinePlayer(t *testing.T) {
	var save []game.SavePlayer
	for i := range 12 {
		save = append(save, game.SavePlayer{Name: fmt.Sprintf("off%02d", i), PlayTime: time.Hour})
	}
	for _, c := range []struct{ online, rows, total int }{{3, 10, 15}, {10, 10, 22}, {12, 12, 24}, {0, 10, 12}} {
		p := serverCase(game.StateRunning, func(p *ServerPage) {
			p.PlayersKnown, p.Players = true, onlineRows(c.online)
			withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, Players: save})(p)
		})
		v := p.PlayersPanel()
		if len(v.Rows) != c.rows || v.Total != c.total || v.Online != c.online || !v.More() {
			t.Errorf("%d online: %d rows, total %d, online %d, more %v; want %d rows, total %d", c.online, len(v.Rows), v.Total, v.Online, v.More(), c.rows, c.total)
		}
		for i, r := range v.Rows[:c.online] {
			if !r.Online {
				t.Errorf("%d online: row %d is offline", c.online, i)
			}
		}
	}
}

// SPEC 8.1: the caption names who is online and how many the save has; while a
// stop warns, the online part says when they were counted.
func TestServerPlayersCaption(t *testing.T) {
	save := game.SavePlayersView{State: game.SavePlayersReady, Players: []game.SavePlayer{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}, {Name: "f"}, {Name: "g"}}}
	two := []game.Player{{Name: "a"}, {Name: "b"}}
	for name, c := range map[string]struct {
		edit func(*ServerPage)
		want string
	}{
		"online":  {func(p *ServerPage) { p.PlayersKnown, p.Players = true, two; withSavePlayers(save)(p) }, "Players: 2 online, 7 in this save"},
		"stopped": {withSavePlayers(save), "Players: 7 in this save"},
		"not in save": {func(p *ServerPage) {
			p.PlayersKnown, p.Players = true, append(two, game.Player{Name: "new"})
			withSavePlayers(save)(p)
		}, "Players: 3 online, 8 in this save"},
		"no record": {func(p *ServerPage) { p.PlayersKnown, p.Players = true, two }, "Players: 2 online"},
		"stop warning": {func(p *ServerPage) {
			p.Status.Operation = &game.Operation{Players: two}
			withSavePlayers(save)(p)
		}, "Players: 2 online when the stop began, 7 in this save"},
	} {
		t.Run(name, func(t *testing.T) {
			state := game.StateRunning
			if name == "stopped" {
				state = game.StateStopped
			}
			p := serverCase(state, c.edit)
			if got := english(p.PlayersPanel().Caption); got != c.want {
				t.Errorf("caption %q, want %q", got, c.want)
			}
		})
	}
}

// SPEC 8.1: an online row says the session and, with the save's record, the
// total as a hint under the name (the time column stays one line, DESIGN 8.3);
// the others say the total in the column.
func TestServerPlayersTimeColumn(t *testing.T) {
	p := serverCase(game.StateRunning, func(p *ServerPage) {
		p.PlayersKnown = true
		p.Players = []game.Player{{Name: "both", Joined: time.Now(), Duration: 2 * time.Minute}, {Name: "fresh", Joined: time.Now(), Duration: 3 * time.Minute}, {Name: "nojoin"}}
		withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, Players: []game.SavePlayer{{Name: "both", PlayTime: 40 * time.Hour}, {Name: "away", PlayTime: 5 * time.Hour}}})(p)
	})
	html := playersHTML(t, p)
	for _, want := range []string{
		`<span class="hint player-actions__total">40 h 0 min total</span><span class="visually-hidden">`,
		`data-label="Online for">3 min</td>`,
		`data-label="Online for">unknown</td>`,
		`data-label="Play time">5 h 0 min</td>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q:\n%s", want, html)
		}
	}
}

// SPEC 8.1, 8.14: "All N players" and the facts' "N players in this save"
// are one count: the save's record plus anyone online it does not list yet.
func TestServerPlayersOneTotal(t *testing.T) {
	var save []game.SavePlayer
	for i := range 11 {
		save = append(save, game.SavePlayer{Name: fmt.Sprintf("p%02d", i), PlayTime: time.Hour})
	}
	p := serverCase(game.StateRunning, func(p *ServerPage) {
		p.PlayersKnown, p.Players = true, []game.Player{{Name: "P00"}, {Name: "newcomer"}}
		withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, Players: save})(p)
	})
	if v := p.PlayersPanel(); v.Total != 12 {
		t.Fatalf("panel total %d, want 12", v.Total)
	}
	if got := english(p.SavePlayersText()); got != "12 players in this save" {
		t.Errorf("facts say %q", got)
	}
	if html := playersHTML(t, p); !strings.Contains(html, "All 12 players") {
		t.Errorf("panel: %s", html)
	}
}

// Factorio names ignore case, so a name that changes only its case is the same
// player and nobody joined or left.
func TestPlayersChangedIgnoresCase(t *testing.T) {
	if m := playersChanged([]string{"Alice"}, []string{"alice"}); !m.IsZero() {
		t.Errorf("a case change said %q", english(m))
	}
	if got := english(playersChanged([]string{"Alice", "Bob"}, []string{"alice"})); got != "Bob left." {
		t.Errorf("got %q", got)
	}
}

// DESIGN 8.12: the Online chip sits in reading order beside the name, before
// the actions menu opens, not after its buttons.
func TestOnlineChipComesBeforeTheActions(t *testing.T) {
	p := liveOpsCase(func(p *ServerPage) {})
	html := playersHTML(t, p)
	chip, menu := strings.Index(html, "badge--online"), strings.Index(html, `class="actions"`)
	if chip < 0 || menu < 0 || chip > menu {
		t.Errorf("chip at %d, actions at %d:\n%s", chip, menu, html)
	}
}
