//go:build game

package web

// A world with no server-settings.json, saved once through the settings page,
// must start with the game's auto-pause on (SPEC 11.1). The real game images
// show it: with no player connected, an auto-paused game's tick does not
// move. Run with `go test -tags game -run TestRealGameUIBAutoPause
// ./internal/web/` while no other game test runs (they share the game's UDP
// port); `make test-game` runs only ./internal/game/ and `make verify` runs
// neither.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

var autoPauseVersions = []string{"1.1.110", "2.0.77", "2.1.24"}

func TestRealGameUIBAutoPause(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range autoPauseVersions {
		if only != "" && !strings.HasPrefix(v, only) {
			continue
		}
		if _, err := dc.InspectImage(context.Background(), game.ImageRef(v)); err != nil {
			t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", v, err)
		}
		t.Run("v"+v, func(t *testing.T) {
			// The form as the page posts it, with auto-pause left as shown.
			t.Run("left on", func(t *testing.T) {
				runAutoPause(t, dc, v, true)
			})
			// A control: an explicit off must be kept, and the game must then run on.
			t.Run("turned off", func(t *testing.T) {
				runAutoPause(t, dc, v, false)
			})
		})
	}
}

// consoleNumber runs a Lua expression that prints a number.
func consoleNumber(t *testing.T, c *game.Controller, id, expr string) float64 {
	t.Helper()
	cmd := "/silent-command rcon.print(" + expr + ")"
	// The first Lua command of a 1.1 game only warns about achievements.
	for range 2 {
		_, reply, err := c.Console(context.Background(), id, game.Actor{UserID: 1, Username: "alice"}, cmd, cmd)
		if err != nil {
			t.Fatalf("RCON: %v", err)
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(reply), 64); err == nil {
			return n
		}
	}
	t.Fatalf("no number from %s", expr)
	return 0
}

func runAutoPause(t *testing.T, dc *docker.Client, version string, leaveOn bool) {
	var ctrl *game.Controller
	makeGame := func(worlds *world.Manager, st *store.Store) *game.Controller {
		c, err := game.New(game.Config{
			Docker: dc, Worlds: worlds, Audit: st, Switches: st,
			HostDataDir: filepath.Dir(worlds.Dirs().Worlds), GameUID: os.Getuid(), GameGID: os.Getgid(),
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
		ctrl = c
		return c
	}
	hn := newHarnessWith(t, nil, makeGame)
	ls := newLiveServerFor(t, hn)
	rig := &settingsRig{liveServer: ls, cookie: hn.signIn()}
	id, err := hn.worlds.Generate(context.Background(), world.NewWorld{Name: "uib-realgame1 autopause", GameVersion: version})
	if err != nil {
		t.Fatal(err)
	}
	rig.id, rig.dir = id, filepath.Join(hn.worlds.Dirs().Worlds, id)
	t.Cleanup(func() {
		_ = ctrl.Stop(context.Background(), id, game.Actor{UserID: 1, Username: "alice"}, game.StopOptions{})
		if err := dc.RemoveContainer(context.Background(), game.ContainerName(id), true); err != nil && !strings.Contains(err.Error(), "not found") {
			t.Errorf("could not remove test container: %v", err)
		}
	})
	if !strings.HasPrefix(game.ContainerName(id), "foreman-uib-realgame1-") {
		t.Fatalf("container name %s is not recognisable as this test's", game.ContainerName(id))
	}
	if cfg, err := hn.worlds.ReadConfig(id, world.ServerSettingsFile); err != nil || cfg.Found {
		t.Fatalf("a generated world has a server-settings.json: %v %v", cfg.Found, err)
	}

	// The page shows auto-pause on for a world with no file.
	body := rig.getAs(rig.cookie, rig.path("")).Body.String()
	if !strings.Contains(body, `name="auto_pause"`) || !checkedBox(body, "auto_pause") {
		t.Fatalf("the first form does not show auto_pause on:\n%s", body)
	}
	form := url.Values{"name": {"uib autopause"}, "description": {""}, "visibility": {"hidden"}, "game_password": {"newpass-typed"},
		"max_players": {""}, "autosave_interval": {""}, "autosave_slots": {""}, "admins": {""}}
	if leaveOn {
		form.Set("auto_pause", "on")
	}
	rec := rig.postAs(rig.cookie, rig.path(""), form)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status %d:\n%s", rec.Code, rec.Body.String())
	}
	conf := rig.postAs(rig.cookie, rig.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}})
	if conf.Code != http.StatusSeeOther {
		t.Fatalf("confirm status %d:\n%s", conf.Code, conf.Body.String())
	}
	got := rig.read(world.ServerSettingsFile)
	requiredKeys(t, got)
	want := `"auto_pause": true`
	if !leaveOn {
		want = `"auto_pause": false`
	}
	if !strings.Contains(got, want) {
		t.Fatalf("the saved file lacks %s:\n%s", want, got)
	}

	// The game starts with that file (the first start creates the map).
	if err := ctrl.Start(context.Background(), id, game.Actor{UserID: 1, Username: "alice"}, game.StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if st := ctrl.Status(id); st.State != game.StateRunning {
		t.Fatalf("state %s; want running", st.State)
	}
	time.Sleep(2 * time.Second)
	t1 := consoleNumber(t, ctrl, id, "game.tick")
	time.Sleep(3 * time.Second)
	t2 := consoleNumber(t, ctrl, id, "game.tick")
	t.Logf("ticks %.0f then %.0f, three seconds later", t1, t2)
	if leaveOn {
		// With no player connected the game pauses; each RCON command still
		// costs it a tick or two. A running game does 180 ticks in 3 s.
		if t2 > t1+20 {
			t.Errorf("auto_pause is true in the file, but the game ran %.0f ticks in 3 s", t2-t1)
		}
	} else if t2 < t1+100 {
		t.Errorf("auto_pause is false, but the game ran only %.0f ticks in 3 s", t2-t1)
	}
}

// checkedBox reports whether the checkbox named name is checked in html.
func checkedBox(html, name string) bool {
	i := strings.Index(html, `name="`+name+`"`)
	if i < 0 {
		return false
	}
	start := strings.LastIndex(html[:i], "<input")
	end := i + strings.Index(html[i:], ">")
	return start >= 0 && strings.Contains(html[start:end], "checked")
}
