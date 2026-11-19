package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
)

// SPEC 5.9, 17.9: when the crash box cannot offer the switch back because
// that world is archived, it says so and links to the world to unarchive it.
func TestCrashBoxSaysWhySwitchBackIsMissing(t *testing.T) {
	crashed := func(archived bool) ServerPage {
		return serverCase(game.StateCrashLooping, func(p *ServerPage) {
			p.WorldNames = hubNames
			if archived {
				p.ArchivedWorlds = map[string]bool{"alpha": true}
			}
			p.Result, p.HaveResult = actionResult{Action: game.AuditSwitch, Target: "base", From: "alpha", At: msgT0}, true
			p.Status.LastCrash = msgT1
		})
	}
	html := controlHTML(t, crashed(true))
	wantAll(t, "archived switch back", html,
		"Alpha is archived, so Foreman cannot switch back to it.",
		`href="/worlds/alpha" id="act-unarchive-switch-back"`)
	wantNone(t, "archived switch back", html, `id="act-switch-back"`)

	wantNone(t, "live switch back", controlHTML(t, crashed(false)), "is archived", "act-unarchive-switch-back")
}
