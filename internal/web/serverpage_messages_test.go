package web

import (
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

var (
	msgT0 = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	msgT1 = msgT0.Add(5 * time.Minute)
)

func refusal(anyway bool) error {
	return &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{
		{Kind: world.ProblemModMissing, Text: "A mod is missing.", Mods: []string{"m"}, StartAnyway: anyway}}}}
}

// errSaveNewer is a plain refusal, with no "Start anyway": the pre-start
// check's answer to a save newer than the world's game (SPEC 7.4).
var errSaveNewer = &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemSaveNewer,
	Text: "The save base.zip was made by a newer Factorio than this world's Factorio 2.1.20. An older game cannot load it."}}}}

// SPEC 8.7: the crash message names the game's reason and links to the full
// log on the Console page, and shows no raw log lines.
func TestCrashMessageNamesReasonAndLinksConsole(t *testing.T) {
	html := controlHTML(t, serverCase(game.StateCrashLooping, func(p *ServerPage) {
		p.Status.CrashReason = ". Factorio said: Opening zip failed"
	}))
	for _, want := range []string{"so Foreman stopped it. Factorio said: Opening zip failed.", `href="/worlds/base/console"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
	if strings.Contains(html, "<pre>") {
		t.Error("the crash message still pastes log lines")
	}
	unknown := controlHTML(t, serverCase(game.StateCrashed, func(*ServerPage) {}))
	if !strings.Contains(unknown, "Foreman could not tell why from the log.") {
		t.Errorf("an unknown reason is not said:\n%s", unknown)
	}
}

// A crash loop, or a start the check refused, shows why a start will not run
// and offers only a plain retry, not the main Start button.
func TestStartBlocked(t *testing.T) {
	cases := []struct {
		name    string
		page    ServerPage
		blocked bool
	}{
		{"stopped", serverCase(game.StateStopped, func(*ServerPage) {}), false},
		{"crash loop", serverCase(game.StateCrashLooping, func(*ServerPage) {}), true},
		{"refused start", serverCase(game.StateStopped, func(p *ServerPage) {
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: refusal(false)}, true
		}), true},
		{"newer save", serverCase(game.StateStopped, func(p *ServerPage) {
			p.Result, p.HaveResult = actionResult{Action: game.AuditRestart, Err: errSaveNewer}, true
		}), true},
		{"image missing", serverCase(game.StateStopped, func(p *ServerPage) {
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: game.ErrImageMissing}, true
		}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := controlHTML(t, c.page)
			if got := c.page.StartBlocked() != ""; got != c.blocked {
				t.Fatalf("blocked = %v, want %v", got, c.blocked)
			}
			if c.blocked != strings.Contains(html, ">Try to start again<") || c.blocked == strings.Contains(html, ">Start server<") {
				t.Errorf("wrong start button in\n%s", html)
			}
			// A crash loop says what to do in its own box, with no second
			// note repeating it.
			why := "<strong>Note:</strong> "
			if c.page.CrashLooping() {
				why = "then try to start it again."
				if strings.Contains(html, "<strong>Note:</strong>") {
					t.Errorf("a crash loop still adds a note:\n%s", html)
				}
			}
			if c.blocked && !strings.Contains(html, why) {
				t.Errorf("no reason given in\n%s", html)
			}
		})
	}
}

// DESIGN 7.3: a refusal that offers a choice is a warning; a plain one is an
// error, on the page and when read out.
func TestRefusalLabelByKind(t *testing.T) {
	for _, anyway := range []bool{true, false} {
		p := serverCase(game.StateStopped, func(p *ServerPage) {
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: refusal(anyway)}, true
		})
		word, class := "Error:", "message--error"
		if anyway {
			word, class = "Warning:", "message--warning"
		}
		html := controlHTML(t, p)
		if !strings.Contains(html, `class="message `+class+`"`) || !strings.Contains(html, "<strong>"+word+"</strong>") {
			t.Errorf("anyway %v: want %s in\n%s", anyway, word, html)
		}
		if got := resultAnnouncement(p); !strings.HasPrefix(got, word+" ") {
			t.Errorf("anyway %v: announced %q", anyway, got)
		}
	}
}

// Only the latest message leads: a crash after a refusal comes first and the
// refusal is marked earlier with its time; a crash before it is marked
// earlier; a failed switch a later refusal supersedes is dropped.
func TestLatestMessageLeads(t *testing.T) {
	refused := func(crash time.Time) ServerPage {
		return serverCase(game.StateCrashLooping, func(p *ServerPage) {
			p.Status.LastCrash = crash
			p.Result, p.HaveResult = actionResult{Action: game.AuditRestart, Err: errSaveNewer, At: msgT0}, true
		})
	}
	html := controlHTML(t, refused(msgT1))
	crash, ref := strings.Index(html, "game crashed"), strings.Index(html, "newer Factorio")
	if crash < 0 || ref < 0 || crash > ref {
		t.Errorf("the newer crash does not lead:\n%s", html)
	}
	if !strings.Contains(html, "Earlier, at <time datetime=\"2026-11-04T10:00:00Z\">") {
		t.Errorf("the older refusal is not marked earlier:\n%s", html)
	}
	html = controlHTML(t, refused(msgT0.Add(-time.Minute)))
	crash, ref = strings.Index(html, "game crashed"), strings.Index(html, "newer Factorio")
	if crash < ref || !strings.Contains(html, "Earlier, at <time datetime=\"2026-11-04T09:59:00Z\">") {
		t.Errorf("the older crash is not after the refusal and marked earlier:\n%s", html)
	}

	stale := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Status.Operation = &game.Operation{Action: game.AuditSwitch, Done: true, Error: "the game did not start",
			Switch: &game.SwitchInfo{From: "base", To: "second"}, Steps: []game.Step{{Name: game.StepLoad, State: game.StepFailed}}}
		p.Result, p.HaveResult = actionResult{Action: game.AuditSwitch, Err: game.ErrSwitchTargetRunning, Target: "second", At: msgT1}, true
	})
	html = controlHTML(t, stale)
	if strings.Contains(html, "The switch") || strings.Contains(html, "Nothing was switched") || strings.Contains(html, `class="steps"`) {
		t.Errorf("the superseded failed switch is still shown:\n%s", html)
	}
	if !strings.Contains(html, "already has a game running") {
		t.Errorf("the new refusal is not shown:\n%s", html)
	}
}
