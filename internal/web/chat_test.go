package web

import (
	"bufio"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
)

func chatSendPath(id string) string { return worldPath(id) + "/chat" }

func (g *gameRig) chatAudit() []string {
	var out []string
	for _, e := range g.audit() {
		if e.Action == game.AuditChat {
			out = append(out, e.Username+": "+e.Detail)
		}
	}
	return out
}

func (g *gameRig) say(id, player, text string) {
	g.t.Helper()
	if err := g.fake.Chat(game.ContainerName(id), player, text); err != nil {
		g.t.Fatal(err)
	}
}

// waitChatPage reads the Chat tab until it holds every text.
func (g *gameRig) waitChatPage(texts ...string) string {
	g.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		body := g.get(PathChat)
		if !slices.ContainsFunc(texts, func(s string) bool { return !strings.Contains(body, s) }) || time.Now().After(deadline) {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// SPEC 8.5: the tab shows the chat the game logged, oldest first, with a
// player's words as text and the server named "Server".
func TestChatTabShowsHistory(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	g.join(id, "alice")
	g.say(id, "alice", `<b>hi</b> & "bye"`)
	if rep := g.post(chatSendPath(id), url.Values{"message": {"on my way"}}); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("send: %d\n%s", rep.StatusCode, rep.body)
	}
	body := g.waitChatPage("on my way", "&lt;b&gt;hi&lt;/b&gt;")
	for _, want := range []string{
		`role="log"`, `aria-label="Chat messages"`, `<strong class="chat-name" translate="no">alice</strong>`,
		`<span class="chat-text" dir="auto">&lt;b&gt;hi&lt;/b&gt; &amp; &#34;bye&#34;</span>`,
		`<li class="chat-line--server">`, `>Server</strong>`, `on my way`,
		`sse-connect="` + chatPath(id, "/events") + `"`, `<span id="server-title">Base <span class="state state--running">Running</span></span>`,
		`maxlength="500"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chat page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<b>hi</b>") {
		t.Error("a player's markup was not escaped")
	}
	if strings.Index(body, "bye") > strings.Index(body, "on my way") {
		t.Error("the history is not oldest first")
	}
	if strings.Contains(body, "chat-not-running") {
		t.Error("a running server shows the not-running note")
	}
}

func TestChatTabEmptyAndStopped(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	if body := g.get(PathChat); !strings.Contains(body, `id="chat-empty"`) || !strings.Contains(body, "No chat since the server started.") {
		t.Errorf("a quiet running server:\n%s", body)
	}
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"0"}}))
	g.waitPage("state--stopped")
	body := g.get(PathChat)
	if !strings.Contains(body, `id="chat-not-running"`) || !strings.Contains(body, `href="/">Go to the Server page</a>`) {
		t.Errorf("a stopped server:\n%s", body)
	}
}

func TestChatTabWithoutAWorld(t *testing.T) {
	g := newGameRig(t, image21)
	body := g.get(PathChat)
	if !strings.Contains(body, "<h1>Chat</h1>") || !strings.Contains(body, "No world is active yet") || strings.Contains(body, `id="chat-message"`) {
		t.Errorf("no world:\n%s", body)
	}
}

// SPEC 8.5, 12.9: a message is audited under the signed-in user and said in
// the game as written.
func TestChatSendsAndAudits(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	rep := g.post(chatSendPath(id), url.Values{"message": {"restart in ten minutes"}})
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != PathChat {
		t.Fatalf("send: %d to %q", rep.StatusCode, rep.Header.Get("Location"))
	}
	if got := g.chatAudit(); !slices.Equal(got, []string{"alice: restart in ten minutes"}) {
		t.Errorf("audit = %q", got)
	}
	if cmds := g.commands(id); cmds[len(cmds)-1] != "restart in ten minutes" {
		t.Errorf("the game was sent %q", cmds)
	}
}

// The plan's invariants: a message that starts with "/" after any blank is
// refused with a link to the Console and never reaches the game or the audit
// log; so are an empty, long and multi-line message.
func TestChatRefusesWhatItMust(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	sent := len(g.commands(id))
	cases := map[string]string{
		"/c game.print(1)":        `chat-error`,
		" /c rcon.print('ran')":   `href="/console">Go to the Console</a>`,
		"   /c rcon.print('ran')": `href="/console">Go to the Console</a>`,
		"\t/c rcon.print('ran')":  `href="/console">Go to the Console</a>`,
		"":                        "Enter a message to send",
		"   ":                     "Enter a message to send",
		strings.Repeat("x", 501):  "at most 500 characters",
		"a\nb":                    "one line",
		"a\rb":                    "one line",
		"a\x00b":                  "NUL",
	}
	for msg, want := range cases {
		rep := g.post(chatSendPath(id), url.Values{"message": {msg}})
		if rep.StatusCode != http.StatusBadRequest || !strings.Contains(rep.body, want) || !strings.Contains(rep.body, `aria-invalid="true"`) {
			t.Errorf("message %.20q: %d, want %q\n%s", msg, rep.StatusCode, want, rep.body)
		}
	}
	if !strings.Contains(g.post(chatSendPath(id), url.Values{"message": {" /c x"}}).body, "A chat message cannot start with /. To run a command, use the Console.") {
		t.Error("the refusal does not say what to do")
	}
	if len(g.commands(id)) != sent || len(g.chatAudit()) != 0 {
		t.Errorf("a refused message was sent %q or audited %q", g.commands(id)[sent:], g.chatAudit())
	}
}

// A refusal keeps the text in the field, so a long message is not lost.
func TestChatRefusalKeepsTheText(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	rep := g.post(chatSendPath(id), url.Values{"message": {"/oops"}})
	if !strings.Contains(rep.body, `value="/oops"`) {
		t.Errorf("the field was emptied:\n%s", rep.body)
	}
}

func TestChatNotRunningAndUnreachable(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	rep := g.post(chatSendPath(id), url.Values{"message": {"hello"}})
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "The game is not running, so the message was not sent.") || !strings.Contains(rep.body, `href="/">Go to the Server page</a>`) || !strings.Contains(rep.body, `value="hello"`) {
		t.Errorf("not running: %d\n%s", rep.StatusCode, rep.body)
	}
	if len(g.audit()) != 0 {
		t.Errorf("audited: %+v", g.audit())
	}

	g = newGameRig(t, image21)
	id = g.startRunning()
	g.fake.SetRCONRefuse(game.ContainerName(id), true)
	rep = g.post(chatSendPath(id), url.Values{"message": {"hello"}})
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "did not answer over RCON, so the message was not sent.") || !strings.Contains(rep.body, `value="hello"`) {
		t.Errorf("RCON unreachable: %d\n%s", rep.StatusCode, rep.body)
	}
}

// A script-enhanced post gets the panel alone, with "Message sent" for the
// announcer and an empty field; a refusal comes back as a panel too.
func TestChatFragmentForScript(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	hx := http.Header{"Cookie": {g.cookie.String()}, "HX-Request": {"true"}}

	rep := g.do(http.MethodPost, chatSendPath(id), url.Values{"message": {"hello"}}, hx)
	if rep.StatusCode != http.StatusOK || strings.Contains(rep.body, "<html") || strings.Contains(rep.body, `value="hello"`) ||
		!strings.Contains(rep.body, `<div hx-swap-oob="innerHTML:#announcer">Message sent</div>`) || !strings.Contains(rep.body, `id="chat-panel"`) {
		t.Errorf("fragment: %d\n%s", rep.StatusCode, rep.body)
	}
	rep = g.do(http.MethodPost, chatSendPath(id), url.Values{"message": {"/c 1"}}, hx)
	if rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, `id="chat-error"`) || strings.Contains(rep.body, "<html") || strings.Contains(rep.body, "Message sent") {
		t.Errorf("refusal fragment: %d\n%s", rep.StatusCode, rep.body)
	}
}

func (g *gameRig) openChatEvents(id string, cookie bool) *http.Response {
	g.t.Helper()
	req, err := http.NewRequestWithContext(g.t.Context(), http.MethodGet, g.url+chatPath(id, "/events"), nil)
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

// SPEC 8.5: the stream starts with the chat so far, then adds each new chat
// line once and only chat lines; player text arrives escaped.
func TestChatStream(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	g.join(id, "alice")
	g.say(id, "alice", "before")

	anon := g.openChatEvents(id, false)
	_ = anon.Body.Close()
	if anon.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous stream: %d", anon.StatusCode)
	}

	deadline := time.Now().Add(10 * time.Second)
	for !slices.ContainsFunc(g.ctrl.ChatLines(id), func(c game.ChatLine) bool { return c.Text == "before" }) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	resp := g.openChatEvents(id, true)
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	name, data, err := readEvent(t, r)
	if err != nil || name != "chat" || !strings.Contains(data, `hx-swap-oob="innerHTML:#chat-lines"`) || !strings.Contains(data, ">before</span>") || !strings.Contains(data, `hx-swap-oob="delete:#chat-empty"`) {
		t.Fatalf("first event %q %q, %v", name, data, err)
	}
	if err := g.fake.Join(game.ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	g.say(id, "alice", "<script>x</script>")
	for {
		name, data, err := readEvent(t, r)
		if err != nil {
			t.Fatalf("stream ended before the new line: %v", err)
		}
		if name != "chat" {
			t.Fatalf("event %q", name)
		}
		if strings.Contains(data, "bob") {
			t.Fatalf("a join was sent as chat: %s", data)
		}
		if strings.Contains(data, "script") {
			if !strings.Contains(data, `hx-swap-oob="beforeend:#chat-lines"`) || strings.Contains(data, "<script>") || !strings.Contains(data, "&lt;script&gt;x&lt;/script&gt;") {
				t.Errorf("new line: %s", data)
			}
			if strings.Contains(data, ">before<") {
				t.Errorf("a line was sent twice: %s", data)
			}
			break
		}
	}
}

func TestChatNeedsSignIn(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for _, path := range []string{PathChat, chatPath(id, "/events")} {
		if rep := g.do(http.MethodGet, path, nil, nil); rep.StatusCode != http.StatusSeeOther {
			t.Errorf("GET %s signed out: %d", path, rep.StatusCode)
		}
	}
	if rep := g.do(http.MethodPost, chatSendPath(id), url.Values{"message": {"hi"}}, nil); rep.StatusCode != http.StatusSeeOther {
		t.Errorf("POST signed out: %d", rep.StatusCode)
	}
	if len(g.audit()) != 0 {
		t.Errorf("audited: %+v", g.audit())
	}
}

// A mode without server control has no chat: the tab goes to the Worlds page.
func TestChatWithoutServerControl(t *testing.T) {
	hn := newHarness(t, nil)
	rec := hn.get(PathChat)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != PathWorlds {
		t.Errorf("GET %s: %d to %q", PathChat, rec.Code, rec.Header().Get("Location"))
	}
}

// SPEC 14.8: a secret in a chat line, in a message an admin types, or in
// either one's audit entry reaches neither the tab, its stream nor the audit log.
func TestChatNeverShowsASecret(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	rconPW, gamePW := g.secrets(id)
	g.startGame(id)
	events, _, closeEvents := g.collectStream(chatPath(id, "/events"))
	g.join(id, "alice")
	g.say(id, "alice", "the game password is "+gamePW)
	g.post(chatSendPath(id), url.Values{"message": {"the RCON password is " + rconPW}})
	g.say(id, "alice", "marker")
	body := g.waitChatPage("marker", game.Mask)
	closeEvents()
	var audit strings.Builder
	for _, a := range g.chatAudit() {
		audit.WriteString(a)
	}
	for what, text := range map[string]string{"page": body, "stream": events.String(), "audit": audit.String()} {
		for _, secret := range []string{rconPW, gamePW} {
			if strings.Contains(text, secret) {
				t.Errorf("the %s holds a secret:\n%s", what, text)
			}
		}
	}
}
