package fakegame_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/fakegame"
)

func hook(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHookProtocol(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	h := r.game.HookHandler()
	id := r.start("foreman-w1", image21, "unless-stopped")

	for _, call := range []struct{ path, body string }{
		{"/join", `{"name":"foreman-w1","player":"alice"}`},
		{"/rcon-refuse", `{"name":"foreman-w1","on":true}`},
		{"/rcon-refuse", `{"name":"foreman-w1","on":false}`},
		{"/save-hangs", `{"name":"foreman-w1","on":true}`},
		{"/image", `{"ref":"example/other:1","on":true}`},
		{"/image", `{"ref":"example/other:1","on":false}`},
		{"/preview-mode", `{"mode":"fail"}`},
		{"/preview-mode", `{"mode":"ok"}`},
	} {
		if rec := hook(t, h, "POST", call.path, call.body); rec.Code != http.StatusNoContent {
			t.Errorf("POST %s %s = %d %s", call.path, call.body, rec.Code, rec.Body)
		}
	}

	var st fakegame.State
	rec := hook(t, h, "GET", "/state", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Containers) != 1 {
		t.Fatalf("state = %+v", st)
	}
	c := st.Containers[0]
	if c.Name != "foreman-w1" || c.ID != id || c.Status != "running" || !strings.HasPrefix(c.IP, "127.") ||
		len(c.Players) != 1 || c.Players[0] != "alice" || !c.SaveHangs || c.RCONRefuse {
		t.Errorf("container state = %+v", c)
	}
	if len(st.Calls) == 0 {
		t.Error("State lists no Docker calls")
	}

	if rec := hook(t, h, "POST", "/crash", `{"name":"foreman-w1","code":139,"oom":true}`); rec.Code != http.StatusNoContent {
		t.Errorf("crash = %d %s", rec.Code, rec.Body)
	}
	if rec := hook(t, h, "POST", "/crash-loop", `{"name":"foreman-w1","on":true}`); rec.Code != http.StatusNoContent {
		t.Errorf("crash-loop = %d", rec.Code)
	}
	if rec := hook(t, h, "POST", "/leave", `{"name":"foreman-w1","player":"alice"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("leave on a dead game = %d", rec.Code)
	}
}

func TestHookErrors(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	h := r.game.HookHandler()
	for _, call := range []struct{ path, body string }{
		{"/join", `{"name":"foreman-nope","player":"alice"}`},
		{"/crash", `{"name":"foreman-nope"}`},
		{"/image", `{"on":true}`},
		{"/preview-mode", `{"mode":"sideways"}`},
		{"/chat", `{"name":"foreman-w1","player":"alice","text":"hi"}`},
		{"/chat", `{"name":"foreman-nope","player":"alice","text":"hi"}`},
		{"/port-in-use", `{"name":"foreman-w1"}`},
		{"/join", `{"name":"x","surprise":1}`},
		{"/join", `not json`},
	} {
		rec := hook(t, h, "POST", call.path, call.body)
		var out map[string]string
		if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["error"] == "" {
			t.Errorf("POST %s %s = %d %s", call.path, call.body, rec.Code, rec.Body)
		}
	}
	if rec := hook(t, h, "GET", "/crash", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /crash = %d", rec.Code)
	}
}

func TestStartServesOnTheGivenFolder(t *testing.T) {
	dir := t.TempDir()
	g, err := fakegame.Start(fakegame.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if g.Socket() != filepath.Join(dir, "docker.sock") || g.Host() != "unix://"+g.Socket() {
		t.Errorf("socket %s, host %s", g.Socket(), g.Host())
	}
	if _, err := os.Stat(g.Socket()); err != nil {
		t.Error(err)
	}
	g.Close()
	if _, err := os.Stat(dir); err != nil {
		t.Error("Close removed a folder it did not make")
	}
}

func TestStartRefusesAMissingFolder(t *testing.T) {
	if _, err := fakegame.Start(fakegame.Config{Dir: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Error("no error for a folder that does not exist")
	}
}

func TestTheFakeCountsDockerCalls(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	r.start("foreman-w1", image21, "unless-stopped")
	var posts int
	for _, c := range r.game.Daemon().Calls() {
		if c.Method == "POST" && c.Path == "/containers/create" {
			posts++
		}
	}
	if posts != 1 {
		t.Errorf("create calls = %d", posts)
	}
	if _, err := r.docker.InspectContainer(context.Background(), "foreman-w1"); err != nil {
		t.Error(err)
	}
}

func TestChatOomAndPortInUseHooks(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	h := r.game.HookHandler()
	id := r.start("foreman-w1", image21, "no")
	if rec := hook(t, h, "POST", "/join", `{"name":"foreman-w1","player":"alice"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("join = %d", rec.Code)
	}
	if rec := hook(t, h, "POST", "/chat", `{"name":"foreman-w1","player":"alice","text":"hello"}`); rec.Code != http.StatusNoContent {
		t.Errorf("chat = %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(r.log(id), " [CHAT] alice: hello\n") {
		t.Errorf("log lacks the chat line:\n%s", r.log(id))
	}
	if rec := hook(t, h, "POST", "/port-in-use", `{"name":"foreman-w2","port":"34198"}`); rec.Code != http.StatusNoContent {
		t.Errorf("port-in-use = %d %s", rec.Code, rec.Body)
	}
	if rec := hook(t, h, "POST", "/oom", `{"name":"foreman-w1","on":true}`); rec.Code != http.StatusNoContent {
		t.Errorf("oom = %d %s", rec.Code, rec.Body)
	}
	eventually(t, "the game to be killed", func() bool { return r.status(id) == "exited" })
	var st fakegame.State
	if err := json.Unmarshal(hook(t, h, "GET", "/state", "").Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	c := st.Containers[0]
	if !c.OOM || c.ExitCode != 137 {
		t.Errorf("state = %+v", c)
	}
}
