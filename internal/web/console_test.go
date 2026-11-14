package web

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
)

// SPEC 8.4, 14.8: a command that sets a secret is audited and shown with the
// secret replaced; every spelling the game accepts is covered.
func TestAuditText(t *testing.T) {
	cases := []struct{ command, want string }{
		{"/config set password hunter2", "/config set password " + game.Mask},
		{"/config set password   hunter2 two words", "/config set password " + game.Mask},
		{"/CONFIG SET PASSWORD hunter2", "/CONFIG SET PASSWORD " + game.Mask},
		{"  /config\tset  password\thunter2  ", "/config\tset  password " + game.Mask},
		{"config set password hunter2", "config set password " + game.Mask},
		{"/ config set password hunter2", "/ config set password " + game.Mask},
		// Nothing follows, so there is nothing to hide.
		{"/config set password", "/config set password"},
		{"/config set password   ", "/config set password"},
		// Other commands pass through, trimmed.
		{"/config set allow-commands true", "/config set allow-commands true"},
		{"/config set passwordx y", "/config set passwordx y"},
		{"/config get password", "/config get password"},
		{"  /players online ", "/players online"},
		{"/c game.print('config set password x')", "/c game.print('config set password x')"},
	}
	for _, c := range cases {
		if got := auditText(c.command); got != c.want {
			t.Errorf("auditText(%q) = %q, want %q", c.command, got, c.want)
		}
	}
}

func TestSecretListIsUsed(t *testing.T) {
	for _, words := range game.SetsSecret {
		if got := auditText("/" + words + " s3cret"); strings.Contains(got, "s3cret") {
			t.Errorf("%q leaks its argument: %q", words, got)
		}
	}
	for _, words := range showsSecret {
		for _, c := range []string{"/" + words, " /" + strings.ToUpper(words) + "  ", words} {
			if !hidesReply(c) {
				t.Errorf("the reply to %q is not hidden", c)
			}
		}
	}
	for _, c := range []string{"/config get", "/config get password2", "/config set password x", "/players online"} {
		if hidesReply(c) {
			t.Errorf("the reply to %q is hidden", c)
		}
	}
}

func TestCheckCommand(t *testing.T) {
	cases := []struct {
		command string
		bad     bool
	}{
		{"/players online", false},
		{strings.Repeat("x", maxCommandLength), false},
		{strings.Repeat("é", maxCommandLength), false},
		{strings.Repeat("x", maxCommandLength+1), true},
		{"", true},
		{"   \t ", true},
		{"/c a\x00b", true},
		{"/c a\nb", true},
		{"/c a\rb", true},
	}
	for _, c := range cases {
		if got := checkCommand(c.command); !got.IsZero() != c.bad {
			t.Errorf("checkCommand(%.20q): %q", c.command, got)
		}
	}
}

func TestAnnounceReply(t *testing.T) {
	if got := english(announceReply("/players online", "Online players (2):\n  alice (online)\n  bob (online)\n")); got != "Reply to /players online: Online players (2): alice (online) bob (online)" {
		t.Errorf("got %q", got)
	}
	if got := english(announceReply("/c x", "")); got != "The game sent no reply to /c x" {
		t.Errorf("got %q", got)
	}
	long := english(announceReply("/c x", strings.Repeat("word ", 200)))
	if len([]rune(long)) > 300 || !strings.HasSuffix(long, "…") {
		t.Errorf("a long reply is cut with an ellipsis: %q", long)
	}
}

func TestConsoleHistoryKeepsTheNewest(t *testing.T) {
	h := newConsoleHistory()
	for i := range historyEntries + 5 {
		h.add("w", consoleEntry{Command: strings.Repeat("c", i+1)})
	}
	list := h.list("w")
	if len(list) != historyEntries || len(list[0].Command) != 6 || !list[len(list)-1].Last || list[0].Last {
		t.Errorf("history %d long, first %q, last flagged %v", len(list), list[0].Command, list[len(list)-1].Last)
	}
	if len(h.list("other")) != 0 {
		t.Error("a world's history leaked into another's")
	}
	h.add("w", consoleEntry{Reply: strings.Repeat("é", maxReplyBytes)})
	got := h.list("w")[historyEntries-1]
	if !got.Cut || len(got.Reply) > maxReplyBytes || !strings.HasSuffix(got.Reply, "é") {
		t.Errorf("a long reply is cut at a character boundary: cut %v, %d bytes", got.Cut, len(got.Reply))
	}
}

func (g *gameRig) startRunning() string {
	g.t.Helper()
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
	return id
}

func consoleSendPath(id string) string { return worldPath(id) + "/console" }

// SPEC 8.6: Download log sends the lines the Console shows, as a text file
// named for the world, and only to someone signed in. The masking of
// secrets is checked with every other page in TestNoSecretLeaksFromServerControl.
func TestConsoleLogDownload(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	g.fake.Daemon().EmitLog(game.ContainerName(id), docker.Stdout, "  12.345 Error ServerMultiplayerManager.cpp:1: something broke")
	deadline := time.Now().Add(10 * time.Second)
	var rec *httptest.ResponseRecorder
	for {
		rec = g.harness.get(consolePath(id, "/log"), withCookie(g.cookie))
		if strings.Contains(rec.Body.String(), "something broke") || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "  12.345 Error ServerMultiplayerManager.cpp:1: something broke\n") {
		t.Fatalf("log download: %d\n%s", rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if got := h.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}
	if got := h.Get("Content-Disposition"); got != `attachment; filename="foreman-`+id+`-log.txt"` {
		t.Errorf("Content-Disposition %q", got)
	}
	if got := strings.Count(rec.Body.String(), "\n"); got != len(g.ctrl.Lines(id)) {
		t.Errorf("%d lines sent, the Console keeps %d", got, len(g.ctrl.Lines(id)))
	}
	// The Console links to it.
	if page := g.get(PathConsole); !strings.Contains(page, `href="`+consolePath(id, "/log")+`" download`) {
		t.Error("the Console page has no Download log link")
	}
	if rec := g.harness.get(consolePath(id, "/log")); rec.Code == http.StatusOK {
		t.Errorf("a signed-out request got the log: %d", rec.Code)
	}
	if rec := g.harness.get(consolePath("no-such-world", "/log"), withCookie(g.cookie)); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown world: %d", rec.Code)
	}
}

// SPEC 8.6, 17.4: the game's own Error and Warning lines are coloured, and
// keep their word; other lines are plain.
func TestLogLineClass(t *testing.T) {
	var p ConsolePage
	for line, want := range map[string]string{
		"  12.345 Error ServerMultiplayerManager.cpp:1: broke": "log-error",
		"   0.100 Warning Mod.cpp:2: odd":                      "log-warning",
		"   1.000 Info Server started":                         "",
		"2026-11-06 12:00:00 [CHAT] alice: Error here":         "",
		"2026-11-06 12:00:00 [JOIN] alice joined the game":     "log-join",
		"2026-11-06 12:00:00 [LEAVE] alice left the game":      "log-join log-leave",
		"Error without a time":                                 "",
	} {
		if got := p.LineClass(line); got != want {
			t.Errorf("%q: %q, want %q", line, got, want)
		}
	}
}

// DESIGN 8.5: a log line's time is split from its words so it can be set
// quieter; the two halves always join back into the whole line.
func TestLogLineStamp(t *testing.T) {
	var p ConsolePage
	for line, stamp := range map[string]string{
		"   0.807 Info PlayerData.cpp:71: Cloud":  "   0.807 ",
		"2026-11-06 12:00:00 [JOIN] alice joined": "2026-11-06 12:00:00 ",
		"Error without a time":                    "",
		"":                                        "",
	} {
		if got := p.LineStamp(line); got != stamp {
			t.Errorf("%q: stamp %q, want %q", line, got, stamp)
		}
		if got := p.LineStamp(line) + p.LineText(line); got != line {
			t.Errorf("%q: halves join to %q", line, got)
		}
	}
}

// SPEC 8.4, 12.9: a command gets the game's reply, shown in the history,
// and is audited under the signed-in user. Without JavaScript the post
// redirects to the reply.
func TestConsoleSendsAndAudits(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()

	rep := g.post(consoleSendPath(id), url.Values{"command": {"/players online"}})
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != PathConsole+"#last-reply" {
		t.Fatalf("send: %d to %q\n%s", rep.StatusCode, rep.Header.Get("Location"), rep.body)
	}
	body := g.get(PathConsole)
	for _, want := range []string{`<li id="last-reply">`, "You sent </span>/players online", "Online players (0):", `aria-label="Console history"`, `aria-label="Game log lines"`} {
		if !strings.Contains(body, want) {
			t.Errorf("console page lacks %q:\n%s", want, body)
		}
	}
	var found bool
	for _, e := range g.audit() {
		if e.Action == game.AuditConsole {
			found = true
			if e.Username != "alice" || e.WorldID != id || e.Detail != "/players online" {
				t.Errorf("console audit entry %+v", e)
			}
		}
	}
	if !found {
		t.Error("the command was not audited")
	}
}

// SPEC 8.4, 14.8: the game gets the command unchanged; the audit log, the
// history and the page show it masked.
func TestConsoleHidesASecret(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()

	g.post(consoleSendPath(id), url.Values{"command": {"/config set password hunter2"}})
	if cmds := g.commands(id); cmds[len(cmds)-1] != "/config set password hunter2" {
		t.Errorf("the game was sent %q", cmds)
	}
	for _, e := range g.audit() {
		if strings.Contains(e.Detail, "hunter2") {
			t.Errorf("the audit log holds the password: %+v", e)
		}
	}
	body := g.get(PathConsole)
	if strings.Contains(body, "hunter2") || !strings.Contains(body, "/config set password "+game.Mask) {
		t.Errorf("the page shows the password or not the mask:\n%s", body)
	}
	// A refused secret command is not echoed back into the field either.
	rep := g.post(consoleSendPath(id), url.Values{"command": {"/config set password " + strings.Repeat("x", maxCommandLength)}})
	if rep.StatusCode != http.StatusBadRequest || strings.Contains(rep.body, "xxxxxxxx") {
		t.Errorf("a long secret command: %d, echoed %v", rep.StatusCode, strings.Contains(rep.body, "xxxxxxxx"))
	}
}

// SPEC 8.4, DESIGN 7.2: a bad command is refused with the field's error and
// never reaches the game or the audit log.
func TestConsoleRefusesBadCommands(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	before := len(g.audit())
	for _, c := range []string{"", "   ", strings.Repeat("x", maxCommandLength+1), "/c a\x00b", "/c a\nb"} {
		rep := g.post(consoleSendPath(id), url.Values{"command": {c}})
		if rep.StatusCode != http.StatusBadRequest || !strings.Contains(rep.body, `id="command-error"`) || !strings.Contains(rep.body, `aria-invalid="true"`) {
			t.Errorf("command %.20q: %d\n%s", c, rep.StatusCode, rep.body)
		}
	}
	if len(g.audit()) != before {
		t.Error("a refused command was audited")
	}
}

// A script-enhanced post gets the panel alone, with the reply for the
// announcer, and a refusal comes back as a panel the script can swap in.
func TestConsoleFragmentForScript(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	hx := http.Header{"Cookie": {g.cookie.String()}, "HX-Request": {"true"}}

	rep := g.do(http.MethodPost, consoleSendPath(id), url.Values{"command": {"/players online"}}, hx)
	if rep.StatusCode != http.StatusOK || strings.Contains(rep.body, "<html") ||
		!strings.Contains(rep.body, `<div hx-swap-oob="innerHTML:#announcer">Reply to /players online: Online players (0):`) {
		t.Errorf("fragment: %d\n%s", rep.StatusCode, rep.body)
	}
	rep = g.do(http.MethodPost, consoleSendPath(id), url.Values{"command": {""}}, hx)
	if rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, `id="command-error"`) || strings.Contains(rep.body, "<html") {
		t.Errorf("refusal fragment: %d\n%s", rep.StatusCode, rep.body)
	}
}

// SPEC 8.4: with the game stopped a command is refused in plain words and
// nothing is audited.
func TestConsoleNotRunning(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	rep := g.post(consoleSendPath(id), url.Values{"command": {"/players online"}})
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "The game is not running") || !strings.Contains(rep.body, `href="/"`) {
		t.Errorf("not running: %d\n%s", rep.StatusCode, rep.body)
	}
	if len(g.audit()) != 0 {
		t.Errorf("audited: %+v", g.audit())
	}
	if !strings.Contains(g.get(PathConsole), `<span id="server-title">Base <span class="state state--stopped">Stopped</span></span>`) {
		t.Error("the console page does not open while the game is stopped")
	}
}

// SPEC 8.4: when RCON cannot be reached the message points to the Server page.
func TestConsoleRCONUnreachable(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	g.fake.SetRCONRefuse(game.ContainerName(id), true)
	rep := g.post(consoleSendPath(id), url.Values{"command": {"/players online"}})
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "could not reach the game over RCON") || !strings.Contains(rep.body, `href="/"`) {
		t.Errorf("RCON unreachable: %d\n%s", rep.StatusCode, rep.body)
	}
}

func (g *gameRig) openConsoleEvents(id string, cookie bool) *http.Response {
	g.t.Helper()
	req, err := http.NewRequestWithContext(g.t.Context(), http.MethodGet, g.url+consoleSendPath(id)+"/events", nil)
	if err != nil {
		g.t.Fatal(err)
	}
	if cookie {
		req.AddCookie(g.cookie)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	return resp
}

// SPEC 8.6: the log stream starts with the lines so far, then
// sends each new line once; it turns away anyone not signed in.
func TestConsoleLogStream(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()

	anon := g.openConsoleEvents(id, false)
	_ = anon.Body.Close()
	if anon.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous stream: %d", anon.StatusCode)
	}

	resp := g.openConsoleEvents(id, true)
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	name, data, err := readEvent(t, r)
	if err != nil || name != "reset" || !strings.HasPrefix(data, "[") {
		t.Fatalf("first event %q %q, %v", name, data, err)
	}
	if err := g.fake.Join(game.ContainerName(id), "carol"); err != nil {
		t.Fatal(err)
	}
	for {
		name, data, err := readEvent(t, r)
		if err != nil {
			t.Fatalf("stream ended before the new line: %v", err)
		}
		if name == "lines" && strings.Contains(data, "carol") {
			if !strings.HasPrefix(data, `["`) {
				t.Errorf("lines are a JSON array: %s", data)
			}
			break
		}
	}
}

func TestConsoleNeedsSignIn(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for _, path := range []string{PathConsole, consoleSendPath(id) + "/events"} {
		if rep := g.do(http.MethodGet, path, nil, nil); rep.StatusCode != http.StatusSeeOther {
			t.Errorf("GET %s signed out: %d", path, rep.StatusCode)
		}
	}
	if rep := g.do(http.MethodPost, consoleSendPath(id), url.Values{"command": {"/players online"}}, nil); rep.StatusCode != http.StatusSeeOther {
		t.Errorf("POST signed out: %d", rep.StatusCode)
	}
}
