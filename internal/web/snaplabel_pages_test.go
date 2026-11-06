package web

import (
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Wherever a page names one snapshot, an automatic one shows its label from
// the reason in the page's language, never the stored English, and a
// manual one keeps the label the person typed (SPEC 22.3).
func TestNamedSnapshotLabelsFollowTheLanguage(t *testing.T) {
	at := time.Date(2026, 11, 4, 9, 59, 0, 0, time.UTC)
	anyway := world.Snapshot{Name: "20261104T095900Z", Manifest: world.Manifest{Label: game.LabelStartAnyway, Reason: game.ReasonStartAnyway, TakenAt: at}}
	update := world.Snapshot{Name: "20261104T095900Z", Manifest: world.Manifest{Label: world.LabelBeforeUpdate, Reason: world.ReasonGameUpdate, TakenAt: at}}
	typed := world.Snapshot{Name: "20261104T095900Z", Manifest: world.Manifest{Label: "Before update", Reason: world.ReasonManual, TakenAt: at}}
	de := i18n.Locale("de")

	recover := serverCase(game.StateCrashLooping, func(p *ServerPage) { p.StartAnywaySnap = &anyway })
	if got := de.Format(recover.RecoverRestore().Label); !strings.Contains(got, de.T("worlds.snapshot.label.before_start_anyway")) || strings.Contains(got, "Before") {
		t.Errorf("recover offer: %q", got)
	}
	done := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: actionSnapshot, At: at,
			Taken: snapshotLabelArg(world.Snapshot{Manifest: world.Manifest{Label: defaultLabel, Reason: world.ReasonManual}})}, true
	})
	if got := de.Format(done.Done()); !strings.Contains(got, de.T("worlds.snapshot.label.manual")) {
		t.Errorf("snapshot done: %q", got)
	}

	r := newTestRenderer(t)
	for name, c := range map[string]pageCase{
		"restore":  {status: 200, user: "todd", page: RestorePage{WorldID: "base", WorldName: "Base", Snapshot: update}},
		"rollback": {status: 200, user: "todd", page: RollbackPage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.73"}, Snapshot: update, Version: "2.0.72"}},
	} {
		_, body := renderIn(t, r, c, "de")
		if !strings.Contains(body, de.T("worlds.snapshot.label.before_update")) || strings.Contains(body, "Before update") {
			t.Errorf("%s page does not show the German label", name)
		}
	}
	_, body := renderIn(t, r, pageCase{status: 200, user: "todd", page: RestorePage{WorldID: "base", WorldName: "Base", Snapshot: typed}}, "de")
	if !strings.Contains(body, "Before update") {
		t.Error("a typed label was not shown as written")
	}
}
