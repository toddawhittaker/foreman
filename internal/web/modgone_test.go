package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 7.4: mods the portal no longer offers read as a reason players
// cannot join, and say going ahead deletes nothing.
func TestGoneModWords(t *testing.T) {
	one := world.Problem{Kind: world.ProblemModGone, Gone: []world.GoneMod{{Name: "visible-planets", Version: "1.5.5", Update: "1.7.2"}}}
	if got, want := english(problemMsg(one)), "The mod portal no longer has visible-planets 1.5.5, so players' games cannot download it and they cannot join. Going ahead anyway deletes nothing from the save."; got != want {
		t.Errorf("one:\n got %q\nwant %q", got, want)
	}
	two := world.Problem{Kind: world.ProblemModGone, Gone: []world.GoneMod{{Name: "a", Version: "1.0.0"}, {Name: "b", Version: "2.0.0"}}}
	if got, want := english(problemMsg(two)), "The mod portal no longer has a 1.0.0, b 2.0.0, so players' games cannot download them and they cannot join. Going ahead anyway deletes nothing from the save."; got != want {
		t.Errorf("two:\n got %q\nwant %q", got, want)
	}
	only := world.PreCheck{Problems: []world.Problem{one}}
	if got, want := english(precheckLeadFor(only, false, "Base")), "Foreman did not start the game, because players could not join it:"; got != want {
		t.Errorf("start lead %q", got)
	}
	if got, want := english(precheckLeadFor(only, true, "Base")), "Foreman did not switch, because players could not join the new world:"; got != want {
		t.Errorf("switch lead %q", got)
	}
	if got, want := english(switchProblemsLead(only, "Beta")), "Players could not join Beta:"; got != want {
		t.Errorf("switch page lead %q", got)
	}
	mixed := world.PreCheck{Problems: []world.Problem{one, {Kind: world.ProblemModMissing}}}
	if got := english(precheckLeadFor(mixed, false, "Base")); !strings.Contains(got, "does not match the world") {
		t.Errorf("mixed lead %q", got)
	}
}

// SPEC 17.9: the refusal's first fix is the same plan the Server page's
// notice opens; matching the mods to the save comes first when the game
// would otherwise delete content.
func TestPrecheckFix(t *testing.T) {
	gone := func(g ...world.GoneMod) world.Problem { return world.Problem{Kind: world.ProblemModGone, Gone: g} }
	cases := []struct {
		name        string
		problems    []world.Problem
		path, label string
	}{
		{"one with an update", []world.Problem{gone(world.GoneMod{Name: "vp", Version: "1.5.5", Update: "1.7.2"})},
			"/worlds/w1/mods/plan?kind=update&name=vp", "Update vp"},
		{"one with none left", []world.Problem{gone(world.GoneMod{Name: "vp", Version: "1.5.5"})},
			"/worlds/w1/mods/plan?kind=remove&name=vp", "Remove vp"},
		{"two with updates", []world.Problem{gone(world.GoneMod{Name: "a", Version: "1", Update: "2"}, world.GoneMod{Name: "b", Version: "1", Update: "2"})},
			"/worlds/w1/mods/plan?kind=update&name=a&name=b", "Update 2 mods"},
		{"two, one with none left", []world.Problem{gone(world.GoneMod{Name: "a", Version: "1", Update: "2"}, world.GoneMod{Name: "b", Version: "1"})},
			"/worlds/w1/mods", "Review 2 mods"},
		{"a missing mod comes first", []world.Problem{gone(world.GoneMod{Name: "vp", Version: "1", Update: "2"}), {Kind: world.ProblemModMissing, Mods: []string{"x"}}},
			"/worlds/w1/mods/plan?kind=sync", "Match the mods to the save"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := precheckFix(world.PreCheck{Problems: c.problems}, "w1")
			if o == nil {
				t.Fatal("no fix offered")
			}
			if o.Path != c.path || (c.label != "" && english(o.Label) != c.label) {
				t.Errorf("got %s %q, want %s %q", o.Path, english(o.Label), c.path, c.label)
			}
		})
	}
	if o := precheckFix(world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemSaveNewer}}}, "w1"); o != nil {
		t.Errorf("a fix for a problem with none: %+v", o)
	}
}

// SPEC 9.3: whenever an operation the page has not seen ends, the stream
// tells the Server page to load its mods notice again, once. It need not
// have seen the operation running: a short one can start and end within
// one send.
func TestOperationEndReloadsTheModsNotice(t *testing.T) {
	r := newTestRenderer(t)
	op := func(id uint64, done bool) ServerPage {
		return serverCase(game.StateRunning, func(p *ServerPage) {
			p.Status.Operation = &game.Operation{Action: game.AuditMods, Done: done, ID: id}
		})
	}
	event := "event: " + modsRecheckEvent + "\n"
	sends := func(l *liveStream, steps []ServerPage, want []int) {
		t.Helper()
		rec := httptest.NewRecorder()
		l.w, l.rc = rec, http.NewResponseController(rec)
		for i, page := range steps {
			rec.Body.Reset()
			if err := l.send(page); err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(rec.Body.String(), event); got != want[i] {
				t.Errorf("send %d: %d mods events, want %d:\n%s", i, got, want[i], rec.Body.String())
			}
		}
	}
	// Seen running, then ended; then a second operation seen only ended.
	sends(&liveStream{render: r, sent: map[string]string{}},
		[]ServerPage{op(1, false), op(1, false), op(1, true), op(1, true), op(2, true), op(2, true)},
		[]int{0, 0, 1, 0, 1, 0})

	// A page drawn with operation 1 ended already loaded the notice after it.
	page := op(1, true)
	if got := page.liveURL(); got != "/worlds/base/events?op=1" {
		t.Fatalf("live URL after operation 1: %q", got)
	}
	sends(&liveStream{render: r, sent: map[string]string{}, doneOp: 1}, []ServerPage{op(1, true)}, []int{0})

	// A page drawn while an operation ran has no ended one to name, so the
	// operation ending before the stream opens still reloads the notice.
	if got := op(1, false).liveURL(); got != "/worlds/base/events" {
		t.Errorf("live URL while an operation runs: %q", got)
	}
	sends(&liveStream{render: r, sent: map[string]string{}}, []ServerPage{op(1, true)}, []int{1})
}

// SPEC 7.4: a start that could not ask the portal says so in one line.
func TestPortalUncheckedNote(t *testing.T) {
	p := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Status.Operation = &game.Operation{Action: game.AuditStart, Done: true, PortalUnchecked: true}
	})
	if !p.PortalUnchecked() {
		t.Fatal("no note")
	}
	var b strings.Builder
	if err := newTestRenderer(t).Fragment(&b, "en", p, "server-control"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Foreman could not reach the mod portal in time, so it did not check that players can download this world") {
		t.Errorf("the note is not shown:\n%s", b.String())
	}
}
