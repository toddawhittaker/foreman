package web

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

const image20 = "factoriotools/factorio:2.0.77"

// oldWorld makes a 2.0 world, whose image the rig's fake does not hold
// unless a test adds it.
func (g *gameRig) oldWorld(name string) string {
	g.t.Helper()
	id, err := g.worlds.Create(context.Background(), name, "2.0.77", false)
	if err != nil {
		g.t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", "2.0.77-no-space-age.zip")) //nolint:gosec // a fixed test fixture
	if err != nil {
		g.t.Fatal(err)
	}
	p := filepath.Join(g.worlds.Dirs().Worlds, id, world.SavesDir, "base.zip")
	if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // a test temp folder
		g.t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, at, at); err != nil {
		g.t.Fatal(err)
	}
	return id
}

// SPEC 3.7, 7.9, 17.10: a switch to a world whose image is missing is one
// confirmation and one run. The page lists the download as its first step,
// naming the image; confirming downloads it, switches and starts. The
// active world's download is never offered (the bug in #129).
func TestSwitchDownloadsTheNewWorldsImage(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.oldWorld("Old")
	g.startGame(a)

	form := g.get(switchPath(b))
	g.wantContains("confirmation", form, "Download the game image <code>"+image20+"</code>",
		"If the download fails, nothing else happens and Alpha keeps running.", "Start the game on Old.")
	if d, stop := strings.Index(form, "Download the game image"), strings.Index(form, "save the game and stop Alpha"); d > stop {
		t.Error("the download is not listed before the stop")
	}

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}}))
	body := g.waitPage(`id="server-title">Old <span`, running, "The worlds are switched")
	g.wantMissing("server page", body, gamePath(a, "pull-image"), "Download game image")
	if !slices.Contains(g.fake.Daemon().Pulls(), image20) {
		t.Errorf("pulls %v, want %s", g.fake.Daemon().Pulls(), image20)
	}
}

// The confirmation page asks Docker Hub for the image's size; the POST that
// confirms does not, because it shows nothing of it.
func TestSwitchPostAsksNoSize(t *testing.T) {
	rel := &fakeReleases{size: 345678901}
	g := newUpdateRig(t, rel)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.oldWorld("Old")
	g.startGame(a)

	g.wantContains("confirmation", g.get(switchPath(b)), "about 345.7 MB")
	if n := rel.sizeAsked(); n != 1 {
		t.Fatalf("the page asked for the size %d times, want 1", n)
	}
	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}}))
	g.waitPage(`id="server-title">Old <span`, "The worlds are switched")
	if n := rel.sizeAsked(); n != 1 {
		t.Errorf("the POST asked for the size: %d calls in all, want 1", n)
	}
}

// SPEC 7.9 (fail safe), regression for #129: a download that fails leaves
// the old world active and running, says so, and offers no download of the
// old world's image.
func TestSwitchFailedDownloadOffersNoWrongDownload(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.oldWorld("Old")
	g.startGame(a)
	g.fake.Daemon().FailPull(image20, "manifest unknown")

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}}))
	body := g.waitPage("The switch from Alpha to Old failed", "Alpha is still the active world")
	g.wantContains("server page", body, `id="server-title">Alpha <span`, running,
		"Foreman could not download "+image20+". The details are in Foreman&#39;s log. Try the switch again.")
	g.wantMissing("server page", body, gamePath(a, "pull-image"), `id="act-pull"`, "manifest unknown", "could not finish this")
}

// SPEC 17.9: a download Docker says it has no image for names the image and
// the next step, and the raw text of the daemon stays off the page. The
// cause of any other failure is logged.
func TestDownloadFailureWords(t *testing.T) {
	notFound := &game.ImageDownloadError{Image: image20, NotFound: true, Err: &docker.APIError{Status: 404, Message: "manifest unknown"}}
	other := &game.ImageDownloadError{Image: image20, Err: errors.New("toomanyrequests: rate limit")}
	for name, c := range map[string]struct {
		err    error
		action string
		want   string
	}{
		"not found": {notFound, game.AuditSwitch,
			"Foreman could not download " + image20 + ": Docker Hub has no image with that version. <a href=\"/worlds/base\" id=\"act-failure-next\">Check the world&#39;s game version on its page.</a>"},
		"other, a switch": {other, game.AuditSwitch,
			"Foreman could not download " + image20 + ". The details are in Foreman&#39;s log. Try the switch again."},
		"other, a download": {other, game.AuditImageDownload,
			"Foreman could not download " + image20 + ". The details are in Foreman&#39;s log. Try again."},
	} {
		p := serverCase(game.StateStopped, func(p *ServerPage) {
			p.Result, p.HaveResult = actionResult{Action: c.action, Err: c.err}, true
		})
		html := fragmentHTML(t, p, "server-control")
		if !strings.Contains(html, c.want) {
			t.Errorf("%s: page lacks %q:\n%s", name, c.want, html)
		}
		for _, raw := range []string{"manifest unknown", "toomanyrequests", "could not finish this"} {
			if strings.Contains(html, raw) {
				t.Errorf("%s: page shows %q", name, raw)
			}
		}
	}

	var logs bytes.Buffer
	s := &server{log: slog.New(slog.NewTextHandler(&logs, nil))}
	s.finish("base", game.AuditSwitch, other)
	if !strings.Contains(logs.String(), "toomanyrequests") {
		t.Errorf("the cause of a download failure is not logged:\n%s", logs.String())
	}
	logs.Reset()
	s.finish("base", game.AuditSwitch, notFound)
	if logs.Len() != 0 {
		t.Errorf("a plain no-such-image was logged:\n%s", logs.String())
	}
}

// Regression for #129: a switch result that names a missing image never
// offers the page's own world's download, which is the wrong image.
func TestSwitchImageRefusalNeverOffersTheActiveWorldsDownload(t *testing.T) {
	p := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.AuditSwitch, Err: game.ErrImageMissing, Target: "second"}, true
	})
	html := controlHTML(t, p)
	if strings.Contains(html, "pull-image") || strings.Contains(html, `id="act-pull"`) {
		t.Errorf("the active world's download is offered:\n%s", html)
	}
}

// SPEC 3.7, #125: the Server page says the image is missing before Start,
// names it, and offers the download in place of Start; once downloaded it
// says the world is ready, and Start is back.
func TestServerPageSaysTheImageIsMissingBeforeStart(t *testing.T) {
	g := newGameRig(t)
	id := g.newWorld("Base", fixtureNoSA, false)

	body := g.get(PathServer)
	g.wantContains("server page", body, "runs on the game image "+image21+", which is not on this host",
		`action="`+gamePath(id, "pull-image")+`"`, "Download game image")
	g.wantMissing("server page", body, `id="act-start"`)

	g.wantRedirectHome(g.post(gamePath(id, "pull-image"), url.Values{}))
	body = g.waitPage("Downloaded "+image21+"; Base is ready to start.", `id="act-start"`)
	g.wantMissing("server page", body, "which is not on this host", `id="act-pull"`)
}

// #136: after a switch the checks refused, the Start button names the old
// world it starts, and the page says that world is still active.
func TestRefusedSwitchNamesTheWorldOnStart(t *testing.T) {
	p := serverCase(game.StateStopped, func(p *ServerPage) {
		p.World.Name = "Alpha"
		p.Result, p.HaveResult = actionResult{Action: game.AuditSwitch, Err: game.ErrSwitchTargetRunning, Target: "second"}, true
	})
	html := controlHTML(t, p)
	for _, want := range []string{">Start Alpha</button>", "Alpha is still the active world, and nothing changed."} {
		if !strings.Contains(html, want) {
			t.Errorf("lacks %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "Start server") {
		t.Errorf("a plain Start server follows the refused switch:\n%s", html)
	}
}

// #142: a start the pre-start check refused is titled "Start refused" and
// lists only the step that ran, not the ones skipped.
func TestStartRefusedListsNoSkippedSteps(t *testing.T) {
	pe := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Text: "The save is from a newer game."}}}}
	p := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Status.Operation = &game.Operation{Action: game.AuditStart, Done: true, Error: pe.Error(), Steps: []game.Step{
			{Name: game.StepCheck, State: game.StepFailed}, {Name: game.StepImage, State: game.StepSkipped}, {Name: game.StepLoad, State: game.StepSkipped},
		}}
		p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: pe}, true
	})
	html := controlHTML(t, p)
	if !strings.Contains(html, "<h3>Start refused</h3>") || strings.Contains(html, "Skipped") || strings.Contains(html, game.StepLoad) {
		t.Errorf("refused start:\n%s", html)
	}
}

// #145: the advice for missing mods names the world's own mods folder on
// the host, from the host data folder Foreman was given.
func TestHostModsFolder(t *testing.T) {
	if got, want := hostModsFolder("/srv/foreman", "base"), "/srv/foreman/worlds/base/mods/"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := hostModsFolder("", "base"); got != "" {
		t.Errorf("an unknown host folder gave %q", got)
	}
}

// SPEC 7.4, 7.9: when problems appear between the confirmation and the
// switch, the Server page's box goes ahead as "Switch and start anyway"
// does, so the switch handler starts the new world without asking again.
func TestSwitchAnywayBoxPostsTheChoice(t *testing.T) {
	pe := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemModMissing, Text: "Mod a is missing.", StartAnyway: true}}}}
	p := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.AuditSwitch, Err: pe, Target: "second"}, true
	})
	html := controlHTML(t, p)
	for _, want := range []string{`action="/worlds/second/switch"`, `name="choice" value="anyway"`, "Switch anyway", "Start the game on second."} {
		if !strings.Contains(html, want) {
			t.Errorf("lacks %q:\n%s", want, html)
		}
	}
}
