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
	if n := len(p.ShownPlayers()); n != 10 {
		t.Fatalf("shown %d, want 10", n)
	}
	if p.PlayersCount() != 12 || !p.MorePlayers() {
		t.Fatalf("count %d more %v", p.PlayersCount(), p.MorePlayers())
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
