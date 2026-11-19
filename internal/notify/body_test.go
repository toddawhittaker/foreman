package notify

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// sample is one message of each event, as the operator guide documents them.
func sample() []Message {
	at := time.Date(2026, 11, 12, 18, 30, 5, 0, time.FixedZone("EST", -5*3600))
	w := World{ID: "main-base", Name: "Main base"}
	return []Message{
		{Event: Start, Time: at, World: w},
		{Event: Stop, Time: at, World: w},
		{Event: Crash, Time: at, World: w, Reason: "save-unreadable"},
		{Event: CrashLoop, Time: at, World: w},
		{Event: PlayerJoin, Time: at, World: w, Player: "ann"},
		{Event: PlayerLeave, Time: at, World: w, Player: "ann"},
		{Event: ScheduledRestart, Time: at, World: w},
		{Event: ScheduledRestart, Time: at, World: w, Skipped: true},
		{Event: LowDisk, Time: at, World: w, FreeBytes: 3_500_000_000},
		{Event: TokenExpiring, Time: at, Token: Token{Name: "Claude", Owner: "ann", Expires: at.Add(24 * time.Hour)}},
	}
}

// The generic body is a documented, versioned format (SPEC 8.11): any
// change to it shows up as a change to this file.
func TestGenericBodyGolden(t *testing.T) {
	var out bytes.Buffer
	for _, m := range sample() {
		r, err := build(Generic, "", i18n.English, m)
		if err != nil {
			t.Fatal(err)
		}
		if r.contentType != "application/json" {
			t.Errorf("content type %q", r.contentType)
		}
		if err := json.Indent(&out, r.body, "", "  "); err != nil {
			t.Fatal(err)
		}
		out.WriteString("\n")
	}
	golden := filepath.Join("testdata", "generic.golden.json")
	if *update {
		if err := os.WriteFile(golden, out.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatalf("%v (run go test -update to write it)", err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Errorf("the generic body changed; if on purpose, raise BodyVersion when needed and run go test -update.\ngot:\n%s", out.Bytes())
	}
}

// Every body starts with version, event, time and world, and the time is
// RFC 3339 in UTC.
func TestGenericBodyFields(t *testing.T) {
	for _, m := range sample() {
		r, _ := build(Generic, "", i18n.English, m)
		var got map[string]any
		if err := json.Unmarshal(r.body, &got); err != nil {
			t.Fatal(err)
		}
		if got["version"] != float64(1) || got["event"] != string(m.Event) || got["time"] != "2026-11-12T23:30:05Z" {
			t.Errorf("%s: %s", m.Event, r.body)
		}
		w, _ := got["world"].(map[string]any)
		wantID, wantName := "main-base", "Main base"
		if m.Event == TokenExpiring {
			wantID, wantName = "", ""
		}
		if w["id"] != wantID || w["name"] != wantName {
			t.Errorf("%s: world %v", m.Event, w)
		}
	}
}

// Every event has a message in every language.
func TestEveryEventHasText(t *testing.T) {
	for _, l := range i18n.Languages {
		for _, m := range sample() {
			text := Text(l.Tag, m, plain)
			if text == "" || strings.Contains(text, "notify.") {
				t.Errorf("%s %s: %q", l.Tag, m.Event, text)
			}
			if m.Event == TokenExpiring {
				for _, want := range []string{"Claude", "ann", "2026-11-13 23:30 UTC"} {
					if !strings.Contains(text, want) {
						t.Errorf("%s: no %q in %q", l.Tag, want, text)
					}
				}
			} else if !strings.Contains(text, "Main base") {
				t.Errorf("%s %s: no world in %q", l.Tag, m.Event, text)
			}
			if m.Player != "" && !strings.Contains(text, m.Player) {
				t.Errorf("%s %s: no player in %q", l.Tag, m.Event, text)
			}
		}
	}
}

func TestDiscordBody(t *testing.T) {
	// A name of nothing but markdown doubles when escaped; the message still
	// fits and no escape is cut in half.
	r, err := build(Discord, "", i18n.English, Message{Event: PlayerJoin, World: World{Name: strings.Repeat("*", 3000)}, Player: "@everyone"})
	if err != nil {
		t.Fatal(err)
	}
	var body discordBody
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(body.Content)); n > discordMaxContent {
		t.Errorf("content is %d characters", n)
	}
	if !strings.Contains(body.Content, `\*…:`) || strings.Contains(body.Content, `**`) {
		t.Errorf("the name is not shortened before escaping: %q", body.Content[len(body.Content)-40:])
	}
	if !bytes.Contains(r.body, []byte(`"allowed_mentions":{"parse":[]}`)) {
		t.Errorf("body %s", r.body)
	}
	if got := discordEscaper.Replace("a*b_c`d@e"); got != `a\*b\_c\`+"`"+`d\@e` {
		t.Errorf("escaped %q", got)
	}
}

func TestNtfyPriority(t *testing.T) {
	for e, want := range map[Event]string{Crash: "high", CrashLoop: "high", LowDisk: "high", Start: "", PlayerJoin: ""} {
		r, _ := build(Ntfy, "", i18n.English, Message{Event: e, World: world})
		if got := r.headers["Priority"]; got != want {
			t.Errorf("%s: priority %q, want %q", e, got, want)
		}
	}
}

// A token reminder names the token, its owner and its expiry, and nothing
// that could be the secret; the generic body has the token object and no
// world (SPEC 8.10, 8.11).
func TestTokenExpiringBody(t *testing.T) {
	at := time.Date(2026, 11, 12, 18, 30, 5, 0, time.UTC)
	m := Message{Event: TokenExpiring, Time: at, Token: Token{Name: "Claude", Owner: "ann", Expires: at.Add(24 * time.Hour)}}
	r, err := build(Generic, "", i18n.English, m)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Token map[string]string `json:"token"`
		World map[string]string `json:"world"`
	}
	if err := json.Unmarshal(r.body, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"name": "Claude", "owner": "ann", "expires": "2026-11-13T18:30:05Z"}
	if len(got.Token) != 3 || got.Token["name"] != want["name"] || got.Token["owner"] != want["owner"] || got.Token["expires"] != want["expires"] {
		t.Errorf("token %v, want %v", got.Token, want)
	}
	if got.World["id"] != "" || got.World["name"] != "" {
		t.Errorf("world %v, want empty", got.World)
	}
	// Other events carry no token field.
	other, _ := build(Generic, "", i18n.English, Message{Event: Start, World: world})
	if bytes.Contains(other.body, []byte(`"token"`)) {
		t.Errorf("a start body has a token: %s", other.body)
	}
}

// A token's name and owner are shortened and escaped for Discord like any
// name, so neither can format the message or ping anyone.
func TestTokenExpiringDiscordEscapes(t *testing.T) {
	m := Message{Event: TokenExpiring, Token: Token{Name: "*bold* @everyone", Owner: strings.Repeat("_", 3000), Expires: time.Unix(0, 0)}}
	r, err := build(Discord, "", i18n.English, m)
	if err != nil {
		t.Fatal(err)
	}
	var body discordBody
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(body.Content)); n > discordMaxContent {
		t.Errorf("content is %d characters", n)
	}
	if !strings.Contains(body.Content, `\*bold\* \@everyone`) || strings.Contains(body.Content, " @everyone") {
		t.Errorf("name not escaped: %q", body.Content[:60])
	}
	if !bytes.Contains(r.body, []byte(`"allowed_mentions":{"parse":[]}`)) {
		t.Errorf("body %s", r.body)
	}
}

// A reminder is off by default, and a stored off-list that names it stays
// off however it is read back and written again (SPEC 8.10).
func TestTokenExpiringOffByDefault(t *testing.T) {
	for _, e := range Events {
		if e.OffByDefault() != (e == TokenExpiring) {
			t.Errorf("%s: OffByDefault is %v", e, e.OffByDefault())
		}
	}
	on := map[Event]bool{}
	for _, e := range Events {
		on[e] = !e.OffByDefault()
	}
	off := parseEventsOff(eventsOff(on))
	if len(off) != 1 || !off[TokenExpiring] {
		t.Errorf("off-list %v", off)
	}
}
