package web

import (
	"errors"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/mods"
)

func change(act mods.Action, name, to string) mods.Change {
	return mods.Change{Action: act, Name: name, To: to}
}

// SPEC 17.9: a finished mod change says which mods it installed, updated
// or turned off, in which world, then the snapshot it took first.
func TestModsDoneNamesTheChange(t *testing.T) {
	snap := ` Foreman took the snapshot "Before mod change" first.`
	cases := []struct {
		name    string
		changes []mods.Change
		want    string
	}{
		{"one install", []mods.Change{change(mods.ActInstall, "fp-lamp", "1.0.0")},
			"Installed fp-lamp in Base."},
		{"three installs", []mods.Change{change(mods.ActInstall, "fp-lamp", "1"), change(mods.ActInstall, "fp-lamp-colours", "1"),
			change(mods.ActInstall, "fp-lamp-lib", "1"), change(mods.ActKeep, "base", "2.1")},
			"Installed fp-lamp, fp-lamp-colours and fp-lamp-lib in Base."},
		{"one update", []mods.Change{change(mods.ActUpgrade, "visible-planets", "1.7.2")},
			"Updated visible-planets to 1.7.2 in Base."},
		{"two downgrades", []mods.Change{change(mods.ActDowngrade, "a", "1.0.0"), change(mods.ActDowngrade, "b", "2.0.0")},
			"Downgraded a to 1.0.0 and b to 2.0.0 in Base."},
		{"turned off", []mods.Change{change(mods.ActDisable, "foreman-test", "1")},
			"Disabled foreman-test in Base."},
		{"turned on", []mods.Change{change(mods.ActEnable, "x", "1")}, "Enabled x in Base."},
		{"removed", []mods.Change{change(mods.ActRemove, "x", "")}, "Removed x from Base."},
		{"five updates", []mods.Change{change(mods.ActUpgrade, "a", "1"), change(mods.ActUpgrade, "b", "2"), change(mods.ActUpgrade, "c", "3"),
			change(mods.ActUpgrade, "d", "4"), change(mods.ActUpgrade, "e", "5")},
			"Updated a to 1, b to 2, c to 3 and 2 more in Base."},
		{"mixed", []mods.Change{change(mods.ActUpgrade, "a", "1"), change(mods.ActInstall, "b", "1"), change(mods.ActUpgrade, "c", "1"),
			change(mods.ActDisable, "d", "1")},
			"Changed 4 mods in Base: 1 installed, 2 updated, 1 disabled."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := english(modsDone(modResult{Changes: c.changes}, "Base")); got != c.want+snap {
				t.Errorf("got  %q\nwant %q", got, c.want+snap)
			}
		})
	}
	// A settings upload changes no mod, so the plain sentence stays.
	if got := english(modsDone(modResult{}, "Base")); got != `The mods of Base are changed. Foreman took the snapshot "Before mod change" first.` {
		t.Errorf("no changes: %q", got)
	}
}

// SPEC 9.12, 17.9: a failed download names the mod, says nothing changed
// and whether the game kept running, and offers the next step as a control.
func TestModsFailureNamesTheModAndOffersTheNextStep(t *testing.T) {
	retry := "/worlds/base/mods/plan?kind=install&name=fp-bad"
	res := modResult{Retry: retry}
	dl := func(cause error) error {
		return &game.ModDownloadError{Mod: "fp-bad", Version: "1.0.0", Err: cause}
	}
	cases := []struct {
		name      string
		err       error
		res       modResult
		state     game.State
		want      string
		fix, path string
	}{
		{"checksum, running", dl(modportal.ErrChecksum), res, game.StateRunning,
			"fp-bad 1.0.0 did not match the checksum the mod portal publishes, so Foreman did not use it. Nothing was changed, and Base kept running.",
			"Try again", retry},
		{"unreachable, stopped", dl(modportal.ErrUnreachable), res, game.StateStopped,
			"fp-bad 1.0.0 could not be downloaded from the mod portal. Nothing was changed, and Base stayed stopped.",
			"Try again", retry},
		{"gone, state unknown", dl(modportal.ErrNotOnPortal), res, game.StateUnknown,
			"The mod portal no longer has fp-bad 1.0.0. Nothing was changed.", "Try again", retry},
		{"login refused goes to the account, then back to the plan", dl(modportal.ErrUnauthorized), res, game.StateRunning,
			"factorio.com refused the username or token, so fp-bad 1.0.0 was not downloaded. Nothing was changed, and Base kept running.",
			"Check the factorio.com account", accountLink(retry)},
		{"login refused with no plan returns here", dl(modportal.ErrUnauthorized), modResult{}, game.StateRunning,
			"factorio.com refused the username or token, so fp-bad 1.0.0 was not downloaded. Nothing was changed, and Base kept running.",
			"Check the factorio.com account", accountLink(PathServer)},
		{"no plan to open again", dl(modportal.ErrChecksum), modResult{}, game.StateRunning,
			"fp-bad 1.0.0 did not match the checksum the mod portal publishes, so Foreman did not use it. Nothing was changed, and Base kept running.",
			"", ""},
		{"other failure keeps its words", errors.New("disk on fire"), res, game.StateRunning,
			english(failureText(errors.New("disk on fire"))), "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, fix := modsFailure(c.err, c.res, "Base", c.state, PathServer)
			if got := english(msg); got != c.want {
				t.Errorf("message %q\nwant    %q", got, c.want)
			}
			switch {
			case c.fix == "" && fix != nil:
				t.Errorf("fix %+v, want none", fix)
			case c.fix != "" && (fix == nil || english(fix.Label) != c.fix || fix.Path != c.path):
				t.Errorf("fix %+v, want %q at %q", fix, c.fix, c.path)
			}
		})
	}
}

// SPEC 17.9, 17.10: on the Server page the failed mod change's box holds
// the named error and Try again, which opens the same plan (2 clicks to
// retry), and the steps that never ran fold into one line.
func TestFailedModChangeOnTheServerPage(t *testing.T) {
	err := &game.ModDownloadError{Mod: "fp-bad", Version: "1.0.0", Err: modportal.ErrChecksum}
	steps := []game.Step{{Name: game.StepDownloadMods, State: game.StepFailed}}
	for _, n := range []string{game.StepWarn, game.StepSave, game.StepSnapshotMods, game.StepApplyMods} {
		steps = append(steps, game.Step{Name: n, State: game.StepSkipped})
	}
	retry := "/worlds/base/mods/plan?kind=install&name=fp-bad"
	p := serverCase(game.StateRunning, func(p *ServerPage) {
		p.World.Name = "Base"
		p.Status.Operation = &game.Operation{Action: game.AuditMods, Done: true, Error: err.Error(), Err: err, Steps: steps}
		p.Result, p.HaveResult = actionResult{Action: game.AuditMods, Err: err, Mods: modResult{Retry: retry}}, true
	})
	html := controlHTML(t, p)
	for _, w := range []string{
		`<span class="ident" translate="no">fp-bad</span> 1.0.0 did not match`,
		"Nothing was changed, and Base kept running.",
		`<a href="/worlds/base/mods/plan?kind=install&amp;name=fp-bad" class="button" id="act-fix">Try again</a>`,
		`<summary id="act-later-operation">4 later steps were not run.</summary>`,
	} {
		if !strings.Contains(html, w) {
			t.Errorf("lacks %q:\n%s", w, html)
		}
	}
	// The failed step and its box come before the folded steps.
	if i, j := strings.Index(html, "act-fix"), strings.Index(html, "<details"); i < 0 || j < i {
		t.Errorf("Try again at %d, folded steps at %d", i, j)
	}
	if strings.Count(html, "Skipped") != 4 || strings.Index(html, "Skipped") < strings.Index(html, "<details") {
		t.Errorf("each skipped step belongs inside the fold:\n%s", html)
	}
}

// DESIGN 7.6: steps fold only after a failure, when two or more never ran;
// a running operation and a refusal show their list as before.
func TestStepsFoldOnlyAfterAFailure(t *testing.T) {
	failed := []game.Step{{Name: game.StepSave, State: game.StepDone}, {Name: game.StepStopContainer, State: game.StepFailed},
		{Name: game.StepCreate, State: game.StepSkipped}, {Name: game.StepLoad, State: game.StepSkipped}}
	running := []game.Step{{Name: game.StepSave, State: game.StepProgress}, {Name: game.StepCreate, State: game.StepWaiting},
		{Name: game.StepLoad, State: game.StepWaiting}}
	cases := []struct {
		name        string
		op          game.Operation
		shown, fold int
	}{
		{"failed", game.Operation{Done: true, Error: "x", Steps: failed}, 2, 2},
		{"running", game.Operation{Steps: running}, 3, 0},
		{"one never ran", game.Operation{Done: true, Error: "x", Steps: failed[:3]}, 3, 0},
		{"cancelled without a failed step", game.Operation{Done: true, Error: "x", Steps: []game.Step{
			{Name: game.StepSave, State: game.StepSkipped}, {Name: game.StepLoad, State: game.StepSkipped}}}, 2, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := stepList{Op: &c.op, In: "operation"}
			if got, fold := len(l.Steps()), len(l.Later()); got != c.shown || fold != c.fold {
				t.Errorf("shown %d, folded %d; want %d, %d", got, fold, c.shown, c.fold)
			}
		})
	}
}

// SPEC 9.12, 17.9, 17.10: a failed download on the active world leaves a
// Server page box that names the mod and offers Try again, which opens the
// same plan, so retrying is 2 clicks (Try again, Install N mods).
func TestFailedInstallOffersTryAgain(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.SetMode(g.portal.AddZip("alpha", "1.0.0"), modportaltest.WrongSHA1)
	id := g.world20("Base")
	_, body := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); !errors.Is(err, modportal.ErrChecksum) {
		t.Fatalf("result %v, want a checksum failure", err)
	}
	page := g.get(PathServer)
	wantIn(t, "server page", plain(page), "alpha 1.0.0 did not match the checksum", "Nothing was changed, and Base stayed stopped.")
	fix := between(page, `<a href="`, `" class="button" id="act-fix">Try again</a>`)
	if fix == "" || !strings.Contains(page, `id="act-fix">Try again</a>`) {
		t.Fatalf("no Try again:\n%s", page)
	}
	fix = fix[strings.LastIndex(fix, `"`)+1:]
	if code, again := g.getStatus(strings.ReplaceAll(fix, "&amp;", "&")); code != 200 || !strings.Contains(again, `id="act-apply"`) {
		t.Errorf("Try again at %q: %d, want the plan with its button", fix, code)
	}
}
