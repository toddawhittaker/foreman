package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Invariant and trust-boundary tests for server control (SPEC 3.8, 4.3 to
// 4.6, 7.3, 7.6, 12.8, 12.9, 14.3, 14.7, 14.8).

// secRig is a gameRig whose controller logs into a buffer and can be
// stopped on its own, as when Foreman stops while the game runs.
type secRig struct {
	*gameRig
	gameLog  *syncBuffer
	stopCtrl func()
}

// startController makes a controller on dockerHost and runs it until the
// returned stop is called (also at test end).
func startController(t *testing.T, dockerHost string, worlds *world.Manager, st *store.Store, logs io.Writer) (*game.Controller, func()) {
	t.Helper()
	dc, err := docker.New(dockerHost)
	if err != nil {
		t.Fatal(err)
	}
	c, err := game.New(game.Config{
		Docker: dc, Worlds: worlds, Audit: st, Switches: st,
		HostDataDir: filepath.Dir(worlds.Dirs().Worlds), GameUID: 845, GameGID: 845,
		Log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return c, stop
}

func newSecRig(t *testing.T) *secRig {
	t.Helper()
	fake := fakegame.New(t, fakegame.Config{Images: []string{image21}})
	sr := &secRig{gameLog: &syncBuffer{}}
	rig := &gameRig{fake: fake}
	hn := newHarnessWith(t, nil, func(worlds *world.Manager, st *store.Store) *game.Controller {
		rig.ctrl, sr.stopCtrl = startController(t, fake.Host(), worlds, st, sr.gameLog)
		return rig.ctrl
	})
	rig.liveServer = newLiveServerFor(t, hn)
	rig.cookie = hn.signIn()
	sr.gameRig = rig
	return sr
}

// mutations lists the Docker calls that change something: every POST and
// DELETE the fake received.
func (g *gameRig) mutations() []string {
	var out []string
	for _, c := range g.fake.State().Calls {
		if strings.HasPrefix(c, "POST ") || strings.HasPrefix(c, "DELETE ") {
			out = append(out, c)
		}
	}
	return out
}

func (g *gameRig) container(id string) docker.ContainerSpec {
	g.t.Helper()
	c, ok := g.fake.Daemon().Container(game.ContainerName(id))
	if !ok {
		g.t.Fatalf("no container %s", game.ContainerName(id))
	}
	return c.Spec
}

func (g *gameRig) containerStatus(id string) string {
	c, _ := g.fake.Daemon().Container(game.ContainerName(id))
	return c.Status
}

// waitResult waits until the world's last action, or an action asked for
// while it ran, is the one named and has ended, and returns its error.
func (g *gameRig) waitResult(id, action string) error {
	g.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		runs := g.h.(*Handler).runs
		if res, ok := runs.result(id); ok && res.Action == action {
			return res.Err
		}
		for _, res := range runs.others(id) {
			if res.Action == action {
				return res.Err
			}
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("the %s never ended", action)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (g *gameRig) startGame(id string) {
	g.t.Helper()
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
}

// collectEvents reads the world's event stream into a buffer until the
// returned function is called or the stream ends; ended is closed then.
func (g *gameRig) collectEvents(id string) (buf *syncBuffer, ended <-chan struct{}, closeIt func()) {
	g.t.Helper()
	return g.collectStream(gamePath(id, "events"))
}

// collectStream is collectEvents for any stream path.
func (g *gameRig) collectStream(path string) (buf *syncBuffer, ended <-chan struct{}, closeIt func()) {
	g.t.Helper()
	req, err := http.NewRequestWithContext(g.t.Context(), http.MethodGet, g.url+path, nil)
	if err != nil {
		g.t.Fatal(err)
	}
	req.AddCookie(g.cookie)
	resp, err := g.client.Do(req) //nolint:bodyclose // closed by the returned function
	if err != nil {
		g.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		g.t.Fatalf("event stream: %d", resp.StatusCode)
	}
	buf = &syncBuffer{}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(buf, resp.Body); close(done) }()
	return buf, done, func() { _ = resp.Body.Close(); <-done }
}

// actionRoute is one server action route, with the method it must have.
type actionRoute struct {
	method string
	path   func(id string) string
}

func serverActionRoutes(snap string) []actionRoute {
	p := func(action string) func(string) string {
		return func(id string) string { return gamePath(id, action) }
	}
	return []actionRoute{
		{http.MethodPost, p("start")},
		{http.MethodPost, p("stop")},
		{http.MethodPost, p("restart")},
		{http.MethodPost, p("force-stop")},
		{http.MethodPost, p("pull-image")},
		{http.MethodPost, p("switch")},
		{http.MethodPost, p("cancel-stop")},
		{http.MethodPost, p("take-snapshot")},
		{http.MethodPost, p("dismiss-result")},
		{http.MethodGet, p("events")},
		{http.MethodPost, snapshotsPath},
		{http.MethodPost, func(id string) string { return restorePath(id, snap) }},
		{http.MethodPost, func(id string) string { return consolePath(id, "") }},
		{http.MethodGet, func(id string) string { return consolePath(id, "/events") }},
	}
}

// actionForm is a form every action route would accept from a signed-in
// admin: no countdown, start anyway.
var actionForm = url.Values{"countdown": {"0"}, "anyway": {"1"}, "label": {"x"}, "command": {"/server-save"}}

// wantUntouched fails when the game was stopped, restarted or recreated,
// or anything was audited or snapshotted, since the baseline.
func (g *gameRig) wantUntouched(id string, what string, mutations, audits, snaps int) {
	g.t.Helper()
	time.Sleep(300 * time.Millisecond) // an accepted action runs in the background
	if got := g.mutations(); len(got) != mutations {
		g.t.Errorf("%s reached Docker: %q", what, got[mutations:])
	}
	if got := g.audit(); len(got) != audits {
		g.t.Errorf("%s was audited: %+v", what, got)
	}
	if list, _ := g.worlds.Snapshots(id); len(list) != snaps {
		g.t.Errorf("%s took a snapshot", what)
	}
	if s := g.containerStatus(id); s != "running" {
		g.t.Errorf("%s left the game %s", what, s)
	}
}

// SPEC 12.8, 14.7: every server action refuses a request with no session,
// a browser request from another origin, the wrong method, and a world
// that does not exist, and reaches neither Docker nor the audit log.
func TestServerActionsRefuseBadRequests(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snap, err := g.worlds.TakeSnapshot(id, "First", "alice")
	if err != nil {
		t.Fatal(err)
	}
	g.startGame(id)
	base, audits := len(g.mutations()), len(g.audit())
	snaps, _ := g.worlds.Snapshots(id)
	cookie := http.Header{"Cookie": {g.cookie.String()}}
	with := func(k, v string) http.Header {
		h := cookie.Clone()
		h.Set(k, v)
		return h
	}

	for _, r := range serverActionRoutes(snap.Name) {
		path := r.path(id)
		var form url.Values
		if r.method == http.MethodPost {
			form = actionForm
		}

		if rep := g.do(r.method, path, form, nil); rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") == PathServer {
			t.Errorf("signed out %s %s: %d to %q", r.method, path, rep.StatusCode, rep.Header.Get("Location"))
		}

		if r.method == http.MethodPost {
			for name, h := range map[string]http.Header{
				"cross-site":     with("Sec-Fetch-Site", "cross-site"),
				"same-site":      with("Sec-Fetch-Site", "same-site"),
				"foreign Origin": with("Origin", "https://evil.example"),
			} {
				if rep := g.do(r.method, path, form, h); rep.StatusCode != http.StatusForbidden {
					t.Errorf("%s %s %s: %d, want 403", name, r.method, path, rep.StatusCode)
				}
			}
			if rep := g.do(http.MethodGet, path, nil, cookie); slices.Contains([]string{"start", "pull-image", "cancel-stop", "take-snapshot"}, strings.TrimPrefix(path, worldPath(id)+"/")) {
				if rep.StatusCode != http.StatusMethodNotAllowed {
					t.Errorf("GET %s: %d, want 405", path, rep.StatusCode)
				}
			}
		} else if rep := g.do(http.MethodPost, path, actionForm, cookie); rep.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: %d, want 405", path, rep.StatusCode)
		}
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			if rep := g.do(m, path, actionForm, cookie); rep.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: %d, want 405", m, path, rep.StatusCode)
			}
		}

		for _, bad := range []string{"nope", "%2e%2e", "..%2f..%2fetc", "base%00", "BASE", "a%2fb"} {
			p := r.path(bad)
			rep := g.do(r.method, p, form, cookie)
			if rep.StatusCode < 300 || rep.Header.Get("Location") == PathServer {
				t.Errorf("unknown world %s %s: %d to %q", r.method, p, rep.StatusCode, rep.Header.Get("Location"))
			}
		}
	}
	for _, bad := range []string{"nope", "..%2F..%2Fworlds", "%2e%2e"} {
		rep := g.do(http.MethodPost, restorePath(id, bad), actionForm, cookie)
		if rep.StatusCode < 300 || rep.Header.Get("Location") == PathServer {
			t.Errorf("unknown snapshot %q: %d", bad, rep.StatusCode)
		}
	}
	g.wantUntouched(id, "a refused request", base, audits, len(snaps))
}

// SPEC 12.8, 16.3: both live streams end when their session ends, by
// sign-out or by idle expiry, checked at each heartbeat.
func TestEventsStreamEndsWithTheSession(t *testing.T) {
	was := heartbeatEvery
	heartbeatEvery = 50 * time.Millisecond
	t.Cleanup(func() { heartbeatEvery = was })

	for _, stream := range []string{"events", "console/events"} {
		for _, how := range []string{"sign-out", "expiry"} {
			t.Run(stream+" "+how, func(t *testing.T) {
				g := newSecRig(t)
				id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
				_, ended, closeIt := g.collectStream(worldPath(id) + "/" + stream)
				defer closeIt()
				time.Sleep(200 * time.Millisecond)
				select {
				case <-ended:
					t.Fatal("the stream ended while the session was live")
				default:
				}
				if how == "sign-out" {
					g.do(http.MethodPost, PathSignOut, url.Values{}, http.Header{"Cookie": {g.cookie.String()}})
				} else {
					g.clock.Advance(g.cfg.SessionIdle + time.Minute)
				}
				select {
				case <-ended:
				case <-time.After(3 * time.Second):
					t.Fatalf("the stream outlived the session (%s)", how)
				}
			})
		}
	}
}

// SPEC 12.8: a heartbeat checks the session without using it, so a page
// left open, or a client holding the stream, cannot keep a session alive
// past its idle limit.
func TestHeartbeatDoesNotKeepTheSessionAlive(t *testing.T) {
	was := heartbeatEvery
	heartbeatEvery = 20 * time.Millisecond
	t.Cleanup(func() { heartbeatEvery = was })

	for _, stream := range []string{"events", "console/events"} {
		t.Run(stream, func(t *testing.T) {
			g := newSecRig(t)
			id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
			_, ended, closeIt := g.collectStream(worldPath(id) + "/" + stream)
			defer closeIt()
			// Each step is well inside the idle limit, and a few heartbeats
			// pass between steps, so a heartbeat that used the session would
			// keep it alive.
			step := g.cfg.SessionIdle / 10
			for range 30 {
				g.clock.Advance(step)
				select {
				case <-ended:
					return
				case <-time.After(5 * heartbeatEvery):
				}
			}
			t.Fatal("the stream kept the session alive past its idle limit")
		})
	}
}

// SPEC 12.5, 16.3: a stream signed in by trusted headers ends after
// proxyStreamLimit, since its own headers never change; the browser then
// reconnects through the proxy, which vouches for the user again.
func TestProxySignedStreamIsCapped(t *testing.T) {
	was := heartbeatEvery
	heartbeatEvery = 20 * time.Millisecond
	t.Cleanup(func() { heartbeatEvery = was })

	fake := fakegame.New(t, fakegame.Config{Images: []string{image21}})
	env := map[string]string{"FOREMAN_AUTH_METHODS": "headers", "FOREMAN_AUTH_REQUIRED_GROUP": testGroup}
	for k, v := range headerEnv {
		env[k] = v
	}
	env["FOREMAN_TRUSTED_PROXIES"] = "127.0.0.1/32" // the test's own client
	hn := newHarnessWith(t, env, func(worlds *world.Manager, st *store.Store) *game.Controller {
		c, _ := startController(t, fake.Host(), worlds, st, io.Discard)
		return c
	})
	ls := newLiveServerFor(t, hn)
	id, err := hn.worlds.Create(context.Background(), "Base", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"events", "console/events"} {
		t.Run(stream, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ls.url+worldPath(id)+"/"+stream, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Authentik-Username", "alice")
			req.Header.Set("X-Authentik-Groups", testGroup)
			resp, err := ls.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("the stream did not open: %d", resp.StatusCode)
			}
			ended := make(chan struct{})
			go func() { _, _ = io.Copy(io.Discard, resp.Body); close(ended) }()

			hn.clock.Advance(proxyStreamLimit - time.Minute)
			select {
			case <-ended:
				t.Fatal("the stream ended before its limit")
			case <-time.After(200 * time.Millisecond):
			}
			hn.clock.Advance(2 * time.Minute)
			select {
			case <-ended:
			case <-time.After(3 * time.Second):
				t.Fatal("the stream outlived its limit")
			}
		})
	}
}

// secrets writes a game password into the world's server settings and
// returns it with the world's RCON password.
func (g *gameRig) secrets(id string) (rconPW, gamePW string) {
	g.t.Helper()
	gamePW = "game-pw-Zq81-unlikely" //nolint:gosec // a test value to look for
	p := filepath.Join(g.worlds.Dirs().Worlds, id, world.ConfigDir, "server-settings.json")
	if err := os.WriteFile(p, []byte(`{"name":"Base","game_password":"`+gamePW+`"}`), 0o600); err != nil {
		g.t.Fatal(err)
	}
	pw, err := g.worlds.RCONPassword(id)
	if err != nil {
		g.t.Fatal(err)
	}
	if pw.Reveal() == "" {
		g.t.Fatal("empty RCON password, so the check proves nothing")
	}
	return pw.Reveal(), gamePW
}

// emitSecrets has the game print both secrets, as the image prints the
// RCON password at startup (SPEC 8.6) and a crash may echo its settings.
func (g *gameRig) emitSecrets(id, rconPW, gamePW string) {
	name := game.ContainerName(id)
	g.fake.Daemon().EmitLog(name, docker.Stdout, "   1.000 Info RCON password is "+rconPW)
	g.fake.Daemon().EmitLog(name, docker.Stderr, "   1.001 Error loading settings: game_password="+gamePW)
}

// SPEC 4.5, 14.8, 15.2: neither the RCON password nor the game password
// appears in a page, a live event, the audit log, an error, Foreman's own
// log, or the container's settings, through start, an RCON refusal, a
// forced stop and a crash loop.
func TestNoSecretLeaksFromServerControl(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	rconPW, gamePW := g.secrets(id)
	name := game.ContainerName(id)
	events, _, closeEvents := g.collectEvents(id)

	var seen strings.Builder
	look := func(paths ...string) {
		for _, p := range paths {
			seen.WriteString(g.get(p))
		}
	}
	pages := []string{PathServer, gamePath(id, "stop"), gamePath(id, "restart"), gamePath(id, "force-stop"),
		snapshotsPath(id), PathWorlds, PathConsole, consolePath(id, "/log")}

	g.startGame(id)
	g.emitSecrets(id, rconPW, gamePW)
	// Console commands that carry each secret (SPEC 8.4, 14.8).
	for _, cmd := range []string{"/config set password " + gamePW, `/c rcon.print("` + rconPW + `")`} {
		if rep := g.post(consolePath(id, ""), url.Values{"command": {cmd}}); rep.StatusCode != http.StatusSeeOther {
			t.Fatalf("console %d: %s", rep.StatusCode, rep.body)
		}
	}
	look(pages...)

	// A stop that cannot reach RCON fails with a message for the admin.
	g.fake.SetRCONRefuse(name, true)
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"0"}}))
	seen.WriteString(g.waitPage(running, "RCON"))
	g.post(snapshotsPath(id), url.Values{"label": {"Live"}}) // refused: the game cannot save
	look(pages...)
	g.wantRedirectHome(g.post(gamePath(id, "force-stop"), url.Values{}))
	g.waitPage(stopped)
	g.fake.SetRCONRefuse(name, false)

	// A crash loop whose log holds the secrets.
	g.startGame(id)
	g.emitSecrets(id, rconPW, gamePW)
	g.fake.SetCrashLoop(name, true)
	// The Server page names only the game's reason (SPEC 8.7); the masked
	// lines themselves are on the Console page.
	seen.WriteString(g.waitPage(`state--crash`))
	if console := g.get(PathConsole); !strings.Contains(console, game.Mask) {
		t.Errorf("the console does not show the masked log lines, so the check proves nothing:\n%s", console)
	}
	look(pages...)
	g.fake.SetCrashLoop(name, false)
	g.wantRedirectHome(g.post(gamePath(id, "force-stop"), url.Values{}))
	g.waitPage(stopped)
	closeEvents()

	seen.WriteString(g.ctrl.Status(id).Reason)
	spec := g.container(id)
	where := map[string]string{
		"pages and errors": seen.String(),
		"live events":      events.String(),
		"web log":          g.logs.String(),
		"controller log":   g.gameLog.String(),
		"container":        strings.Join(slices.Concat(spec.Env, spec.Cmd, spec.Entrypoint, spec.HostConfig.Binds), "\n"),
	}
	var audit strings.Builder
	consoled := 0
	for _, e := range g.audit() {
		audit.WriteString(e.Action + " " + e.Detail + "\n")
		if e.Action == game.AuditConsole {
			consoled++
		}
	}
	if consoled != 2 {
		t.Errorf("%d console commands audited, want 2, so the check proves nothing", consoled)
	}
	where["audit log"] = audit.String()
	for place, text := range where {
		for secretName, s := range map[string]string{"RCON password": rconPW, "game password": gamePW} {
			if strings.Contains(text, s) {
				t.Errorf("the %s is in the %s", secretName, place)
			}
		}
	}
}

// allowedDocker is SPEC 14.3, as "METHOD path".
var allowedDocker = regexp.MustCompile(`^(GET /_ping|GET /version|GET /containers/json|` +
	`GET /containers/[^/]+/(json|logs|stats)|POST /containers/[^/]+/(start|stop|kill)|` +
	`POST /containers/create|DELETE /containers/[^/]+|POST /images/create|GET /images/.+/json|GET /events)$`)

// SPEC 14.3: every flow of server control makes only the listed Docker
// calls, and a graceful stop never kills the container.
func TestOnlyListedDockerCalls(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snap, err := g.worlds.TakeSnapshot(id, "First", "alice")
	if err != nil {
		t.Fatal(err)
	}
	name := game.ContainerName(id)

	g.startGame(id)
	if rep := g.post(snapshotsPath(id), url.Values{"label": {"Live"}}); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("snapshot: %d", rep.StatusCode)
	}
	g.wantRedirectHome(g.post(gamePath(id, "restart"), url.Values{"countdown": {"0"}}))
	if err := g.waitResult(id, game.AuditRestart); err != nil {
		t.Fatalf("restart: %v", err)
	}
	g.wantRedirectHome(g.post(restorePath(id, snap.Name), url.Values{"countdown": {"0"}}))
	g.waitPage(running, "The world is restored.")
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"0"}}))
	g.waitPage(stopped)
	for _, c := range g.fake.State().Calls {
		if strings.HasSuffix(c, "/kill") {
			t.Errorf("a graceful flow killed the container: %s", c)
		}
	}

	g.startGame(id)
	_ = g.fake.Crash(name, 1, false) // the game may already be restarting
	g.waitPage(running)
	g.wantRedirectHome(g.post(gamePath(id, "force-stop"), url.Values{}))
	g.waitPage(stopped)
	g.fake.SetImage(image21, false)
	g.wantRedirectHome(g.post(gamePath(id, "pull-image"), url.Values{}))
	deadline := time.Now().Add(10 * time.Second)
	for !slices.Contains(g.fake.Daemon().Pulls(), image21) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	g.waitPage(stopped)

	for _, c := range g.fake.State().Calls {
		if !allowedDocker.MatchString(c) {
			t.Errorf("Docker call outside SPEC 14.3: %s", c)
		}
	}
	if got := g.fake.Daemon().Pulls(); !slices.Equal(got, []string{image21}) {
		t.Errorf("pulls %q, want only the pinned image once", got)
	}
}

// SPEC 4.3 to 4.6, 14.4: the game container's shape.
func TestGameContainerShape(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	rconPW, _ := g.secrets(id)
	g.startGame(id)
	c, ok := g.fake.Daemon().Container(game.ContainerName(id))
	if !ok || c.Name != "foreman-"+id {
		t.Fatalf("container named %q, want foreman-%s", c.Name, id)
	}
	s := c.Spec
	hostData := filepath.Dir(g.worlds.Dirs().Worlds)

	if s.User != "845:845" {
		t.Errorf("user %q, want the configured non-root 845:845", s.User)
	}
	if !slices.Equal(s.HostConfig.CapDrop, []string{"ALL"}) {
		t.Errorf("CapDrop %q", s.HostConfig.CapDrop)
	}
	if !slices.Contains(s.HostConfig.SecurityOpt, "no-new-privileges") {
		t.Errorf("SecurityOpt %q", s.HostConfig.SecurityOpt)
	}
	want := filepath.Join(hostData, "worlds", id) + ":/factorio"
	if !slices.Equal(s.HostConfig.Binds, []string{want}) {
		t.Errorf("binds %q, want only %q", s.HostConfig.Binds, want)
	}
	if len(c.Mounts) > 1 {
		t.Errorf("mounts %+v", c.Mounts)
	}
	if n := s.HostConfig.NetworkMode; n == "host" || strings.HasPrefix(n, "container:") {
		t.Errorf("network mode %q", n)
	}
	for port := range s.HostConfig.PortBindings {
		if port != "34197/udp" {
			t.Errorf("published port %s; only the game port may be (SPEC 4.4)", port)
		}
	}
	for port := range s.ExposedPorts {
		if strings.HasPrefix(port, game.RCONPort) {
			t.Errorf("RCON port exposed: %s", port)
		}
	}
	for _, e := range s.Env {
		if strings.Contains(strings.ToUpper(e), "PASSWORD") || strings.Contains(e, rconPW) {
			t.Errorf("password in the environment: %s", strings.SplitN(e, "=", 2)[0])
		}
	}
	if strings.Contains(strings.Join(slices.Concat(s.Cmd, s.Entrypoint), " "), rconPW) {
		t.Error("the RCON password is in the command")
	}
	if s.Labels[game.LabelWorld] != id || s.Labels[game.LabelInstall] != hostData {
		t.Errorf("labels %v", s.Labels)
	}
	if s.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("restart policy %q", s.HostConfig.RestartPolicy.Name)
	}
	if s.StopTimeout == nil || *s.StopTimeout != 120 {
		t.Errorf("stop timeout %v, want 120 (SPEC 4.3)", s.StopTimeout)
	}
	if !s.OpenStdin {
		t.Error("stdin is not kept open (SPEC 4.3)")
	}
}

// SPEC 7.6: a stop Foreman asked for is never a crash, graceful or forced,
// even after the monitor has looked again.
func TestAskedForStopIsNeverACrash(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for _, action := range []string{"stop", "force-stop"} {
		g.startGame(id)
		g.wantRedirectHome(g.post(gamePath(id, action), url.Values{"countdown": {"0"}}))
		g.waitPage(stopped)
		time.Sleep(6 * time.Second) // past one monitor poll (SPEC 8.7)
		if st := g.ctrl.Status(id).State; st != game.StateStopped {
			t.Errorf("after %s the state is %s", action, st)
		}
		if body := g.get(PathServer); strings.Contains(strings.ToLower(body), "crash") {
			t.Errorf("after %s the page mentions a crash", action)
		}
	}
}

// SPEC 3.8: Foreman stopping, and Foreman starting again, never stops,
// restarts or recreates the game.
func TestForemanStopAndStartLeaveTheGameRunning(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.startGame(id)
	base := len(g.mutations())
	restarts := func() int { c, _ := g.fake.Daemon().Container(game.ContainerName(id)); return c.RestartCount }
	r0 := restarts()

	g.harness.h.(*Handler).Close()
	g.stopCtrl()
	if s := g.containerStatus(id); s != "running" {
		t.Fatalf("Foreman's stop left the game %s", s)
	}

	c2, _ := startController(t, g.fake.Host(), g.worlds, g.store, io.Discard)
	deadline := time.Now().Add(10 * time.Second)
	for c2.Status(id).State != game.StateRunning {
		if time.Now().After(deadline) {
			t.Fatalf("the new controller sees %s", c2.Status(id).State)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if got := g.mutations(); len(got) != base {
		t.Errorf("Foreman's stop or start changed the game: %q", got[base:])
	}
	if s := g.containerStatus(id); s != "running" || restarts() != r0 {
		t.Errorf("game %s with %d restarts, want running with %d", s, restarts(), r0)
	}
}

// SPEC 7.3: only the active world starts, also when started anyway.
func TestInactiveWorldStartAnywayRefused(t *testing.T) {
	g := newSecRig(t)
	g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	other := g.newWorld("Other", "2.1.24-no-space-age.zip", false)
	rep := g.post(gamePath(other, "start"), url.Values{"anyway": {"1"}})
	if rep.StatusCode != http.StatusConflict {
		t.Errorf("start anyway post of an inactive world: %d", rep.StatusCode)
	}
	if got := g.mutations(); len(got) != 0 {
		t.Errorf("Docker was changed: %q", got)
	}
}

// Concurrent starts on one world do not interleave: one container is made
// and started once.
func TestConcurrentStartsMakeOneContainer(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { g.post(gamePath(id, "start"), url.Values{}) })
	}
	wg.Wait()
	g.waitPage(running)
	time.Sleep(500 * time.Millisecond)
	count := func(call string) int {
		n := 0
		for _, c := range g.mutations() {
			if c == call {
				n++
			}
		}
		return n
	}
	if n := count("POST /containers/create"); n != 1 {
		t.Errorf("%d containers created, want 1", n)
	}
	if n := count("POST /containers/" + game.ContainerName(id) + "/start"); n > 1 {
		t.Errorf("%d container starts by name", n)
	}
	if s := g.containerStatus(id); s != "running" {
		t.Errorf("game %s", s)
	}
}

// SPEC 12.9: each action is audited under the user who asked, and a
// start the pre-start check refuses is still audited.
func TestServerActionsAuditTheUser(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.addUser("bob", testPassword)
	bob := sessionCookie(g.harness.post(PathSignIn, signInForm("bob", testPassword)))
	asBob := func(path string, form url.Values) {
		rep := g.do(http.MethodPost, path, form, http.Header{"Cookie": {bob.String()}})
		g.wantRedirectHome(rep)
	}

	g.startGame(id)
	asBob(gamePath(id, "restart"), url.Values{"countdown": {"0"}})
	if err := g.waitResult(id, game.AuditRestart); err != nil {
		t.Fatalf("restart: %v", err)
	}
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"0"}}))
	g.waitPage(stopped)
	g.startGame(id)
	asBob(gamePath(id, "force-stop"), url.Values{})
	g.waitPage(stopped)
	g.fake.SetImage(image21, false)
	asBob(gamePath(id, "pull-image"), url.Values{})
	if err := g.waitResult(id, game.AuditImageDownload); err != nil {
		t.Fatalf("download: %v", err)
	}

	want := map[string]string{
		game.AuditStart: "alice", game.AuditRestart: "bob", game.AuditStop: "alice",
		game.AuditForceStop: "bob", game.AuditImageDownload: "bob",
	}
	got := map[string][]string{}
	for _, e := range g.audit() {
		if e.WorldID != id {
			t.Errorf("audit %s names world %q", e.Action, e.WorldID)
		}
		if e.UserID == 0 {
			t.Errorf("audit %s has no user id", e.Action)
		}
		got[e.Action] = append(got[e.Action], e.Username)
	}
	for action, user := range want {
		if !slices.Contains(got[action], user) {
			t.Errorf("audit %s: users %q, want %s", action, got[action], user)
		}
	}
}

// SPEC 7.4, 12.9: a start the pre-start check refuses is still audited.
func TestRefusedStartIsAudited(t *testing.T) {
	g := newSecRig(t)
	id, err := g.worlds.Create(context.Background(), "Empty", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	if g.waitResult(id, game.AuditStart) == nil {
		t.Fatal("a world with no save started")
	}
	var found bool
	for _, e := range g.audit() {
		found = found || (e.Action == game.AuditStartRefused && e.Username == "alice" && e.WorldID == id && strings.Contains(e.Detail, "no save"))
	}
	if !found {
		t.Errorf("the refused start is not audited: %+v", g.audit())
	}
	if got := g.mutations(); len(got) != 0 {
		t.Errorf("a refused start changed Docker: %q", got)
	}
}

// A stop asked for while a restart runs either waits its turn and stops
// the game, or is refused in words; it is never silently lost.
func TestStopDuringRestartIsNotLost(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.startGame(id)
	g.wantRedirectHome(g.post(gamePath(id, "restart"), url.Values{"countdown": {"0"}}))
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"0"}}))
	err := g.waitResult(id, game.AuditStop)
	time.Sleep(time.Second)
	st := g.ctrl.Status(id).State
	if err == nil && st != game.StateStopped {
		t.Errorf("the stop reported success but the game is %s", st)
	}
}

// A stop refused while a restart ran is shown on its own and does not
// hide the restart's result.
func TestRefusedStopKeepsTheRestartsResult(t *testing.T) {
	g := newSecRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.startGame(id)
	if err := g.fake.Join(game.ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	// The restart warns bob for 2 seconds, so the stop arrives while it runs
	// and is refused. The image goes away meanwhile, so the restart's start
	// then fails.
	g.wantRedirectHome(g.post(gamePath(id, "restart"), url.Values{"countdown": {"2"}}))
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"0"}}))
	g.fake.SetImage(image21, false)
	if err := g.waitResult(id, game.AuditRestart); !errors.Is(err, game.ErrImageMissing) {
		t.Fatalf("the restart ended with %v, want the missing image", err)
	}
	if err := g.waitResult(id, game.AuditStop); err == nil {
		t.Fatal("the stop was not refused, so the check proves nothing")
	}
	g.waitPage("is not downloaded", "The stop asked for during another action did not finish: ")
}
