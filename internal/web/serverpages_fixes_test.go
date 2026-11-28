package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// DESIGN 7.4: Stop and Restart destroy nothing ("Nothing is lost"), so their
// confirmation is a plain box, not the danger box, and the box carries no
// landmark name of its own, as on every other confirmation page.
func TestStopAndRestartConfirmationsArePlain(t *testing.T) {
	r := mustRenderer(t)
	for _, restart := range []bool{false, true} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: StopPage{World: world.Summary{ID: "base", Name: "Base"}, Restart: restart, CountdownForm: CountdownForm{Countdown: "60"}}})
		box := regexp.MustCompile(`<section class="confirm[^"]*"[^>]*>`).FindString(body)
		if box == "" {
			t.Fatalf("restart %v: no confirmation box", restart)
		}
		if !strings.Contains(box, `class="confirm confirm--plain"`) {
			t.Errorf("restart %v: %s is the danger box", restart, box)
		}
		if strings.Contains(box, "aria-label") {
			t.Errorf("restart %v: %s names the box as a landmark", restart, box)
		}
	}
}

// An error page for an address in an area keeps that area: its highlight in
// the top bar and its tabs (DESIGN 6, "Sub-navigation").
func TestErrorPageKeepsItsArea(t *testing.T) {
	hn := newNotifyHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := hn.signIn()
	settingsCurrent := `href="` + generalPath() + `" aria-current="true"`
	for _, tc := range []struct {
		path, current string
		want          []string
	}{
		{"/settings/notifications/999", settingsCurrent, []string{`class="subnav subnav--settings"`, `href="` + PathNotifications + `" aria-current="true"`}},
		{"/settings/users/999/delete", settingsCurrent, []string{`class="subnav subnav--settings"`, `href="` + usersPath() + `" aria-current="true"`}},
		{"/settings/api-tokens/999/revoke", settingsCurrent, []string{`class="subnav subnav--settings"`}},
		{"/worlds/no-such-world", `href="` + PathWorlds + `" aria-current="true"`, []string{`class="subnav subnav--worlds"`}},
		{"/favorites/999/delete", `href="` + PathWorlds + `" aria-current="true"`, []string{`class="subnav subnav--worlds"`}},
	} {
		rec := hn.get(tc.path, withCookie(c))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", tc.path, rec.Code)
			continue
		}
		body := rec.Body.String()
		for _, w := range append([]string{tc.current}, tc.want...) {
			if !strings.Contains(body, w) {
				t.Errorf("%s: the error page lacks %s", tc.path, w)
			}
		}
		// The area's link is current, never the page: the page shown is the error.
		if strings.Contains(body, `aria-current="page"`) {
			t.Errorf("%s: a link is marked as the page shown", tc.path)
		}
	}
	// An address in no area has neither.
	body := hn.get("/no-such-page", withCookie(c)).Body.String()
	if strings.Contains(body, `class="subnav`) || regexp.MustCompile(`href="[^"]*" aria-current="true">`+"[A-Z]").MatchString(body) {
		t.Error("/no-such-page: the error page shows an area")
	}
}

// DESIGN 6, "The page header": the About page's title is the header's h1
// alone, like its siblings', and its facts sit in a plate.
func TestAboutPageHasTheSharedHeaderAndAPlate(t *testing.T) {
	r := mustRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: AboutPage{Info: aboutOf("")}})
	h1 := regexp.MustCompile(`(?s)<h1>(.*?)</h1>`).FindStringSubmatch(body)
	if h1 == nil {
		t.Fatal("no h1")
	}
	if strings.Contains(h1[1], "<img") {
		t.Errorf("the h1 holds the wordmark: %s", h1[1])
	}
	if !regexp.MustCompile(`(?s)</header>\s*<div class="panel">.*id="about-facts".*class="about__source"[^\n]*\n</div>`).MatchString(body) {
		t.Error("the facts and the project's line are not in a plate after the page header")
	}
}

// DESIGN 6, "Controls stay put": Take snapshot leads the Server plate's
// actions in every state, the state's own buttons follow it, and during a
// start, stop or restart the buttons stay where they were, disabled, so
// nothing moves.
func TestServerActionsKeepTheirPlaceAcrossStates(t *testing.T) {
	r := mustRenderer(t)
	busy := func(action string) func(*ServerPage) {
		return func(p *ServerPage) {
			p.Status.Operation = &game.Operation{Action: action, Steps: []game.Step{{Name: game.StepSave, State: game.StepProgress}}}
		}
	}
	none := func(*ServerPage) {}
	for _, tc := range []struct {
		name  string
		state game.State
		edit  func(*ServerPage)
		want  []string
	}{
		{"stopped", game.StateStopped, none, []string{"act-snapshot", "act-start"}},
		{"starting", game.StateStarting, busy(game.AuditStart), []string{"act-snapshot disabled", "act-start disabled"}},
		{"running", game.StateRunning, none, []string{"act-snapshot", "act-restart", "act-stop"}},
		{"stopping", game.StateStopping, busy(game.AuditStop), []string{"act-snapshot disabled", "act-restart disabled", "act-stop disabled"}},
		{"restart warning players", game.StateRunning, busy(game.AuditRestart), []string{"act-snapshot disabled", "act-restart disabled", "act-stop disabled"}},
		{"restart starting again", game.StateStarting, busy(game.AuditRestart), []string{"act-snapshot disabled", "act-restart disabled", "act-stop disabled"}},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: serverCase(tc.state, tc.edit)})
		row := regexp.MustCompile(`(?s)<div class="actions server-actions">(.*?)\n</div>`).FindStringSubmatch(body)
		if row == nil {
			t.Fatalf("%s: no actions row", tc.name)
		}
		var got []string
		for _, m := range regexp.MustCompile(`<(?:a|button)\b[^>]*>`).FindAllString(row[1], -1) {
			id := regexp.MustCompile(`id="([^"]*)"`).FindStringSubmatch(m)
			if id == nil {
				continue
			}
			if strings.Contains(m, " disabled") {
				got = append(got, id[1]+" disabled")
			} else {
				got = append(got, id[1])
			}
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: actions %v, want %v", tc.name, got, tc.want)
		}
	}
}
