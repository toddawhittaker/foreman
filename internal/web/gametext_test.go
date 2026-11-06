package web

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

// sentence is an error's log text as a sentence, which the English page
// text must match.
func sentence(err error) string {
	msg := err.Error()
	r, n := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[n:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

// english is m as an English page shows it.
func english(m i18n.Msg) string { return i18n.English.Format(m) }

// gameErrorText is gameError with its words in English.
func gameErrorText(err error) (int, string) {
	status, msg := gameError(err)
	return status, english(msg)
}

// typedFailures are the game's failures that carry facts for the page, each
// as server control returns it.
var typedFailures = []error{
	&game.StartError{Kind: game.StartLoadFailed},
	&game.StartError{Kind: game.StartLoadFailed, Detail: "Map version 2.1.30 cannot be loaded"},
	&game.StartError{Kind: game.StartStopped},
	&game.StartError{Kind: game.StartStopped, Reason: game.LoadReason{Mods: 1, Required: "2.0", Current: "2.1", Shown: []string{"lonely"}}},
	&game.StartError{Kind: game.StartStopped, Reason: game.LoadReason{Mods: 7, Required: "2.0", Current: "2.1", Shown: []string{"a", "b", "c", "d", "e"}, More: 2}},
	&game.StartError{Kind: game.StartStopped, Reason: game.LoadReason{Mods: 2, Required: "2.0", Current: "2.1"}},
	&game.StartError{Kind: game.StartStopped, Reason: game.LoadReason{Said: "Opening zip failed"}},
	&game.StartError{Kind: game.StartLoadTimeout, Timeout: 5 * time.Minute},
	&game.StartError{Kind: game.StartRCONTimeout, Timeout: 90 * time.Second},
	&game.StartError{Kind: game.StartRCONRefused},
	&game.ForcedStopError{World: "Alpha", Stopped: true},
	&game.ForcedStopError{World: "Alpha"},
	game.NotActiveError{Name: "Bravo"},
	&game.NetworkError{Kind: game.NetworkUnset},
	&game.NetworkError{Kind: game.NetworkNotJoined, Network: "games"},
	&game.NetworkError{Kind: game.NetworkNoRange, Network: "games"},
	&game.NetworkError{Kind: game.NetworkProxyOverlap, Network: "games", Subnet: "172.20.0.0/16", Proxies: "172.20.0.0/24"},
	&game.NetworkError{Kind: game.NetworkShared, Network: "games", Members: []game.NetworkMember{
		{Name: "old", State: "stopped"}, {Name: "proxy", State: "running"}, {Name: "web", State: "paused", Install: true}}},
	&game.NetworkError{Kind: game.NetworkShared, Network: "games", Members: []game.NetworkMember{{Name: "old", State: "stopped", Install: true}}},
	&game.NetworkError{Kind: game.NetworkHostLookup, Host: "proxy", Network: "games"},
	&game.NetworkError{Kind: game.NetworkHostAddress, Host: "proxy", Address: "x"},
	&game.NetworkError{Kind: game.NetworkHostOnGame, Address: "172.20.0.5", Network: "games", Subnet: "172.20.0.0/16"},
	&game.NetworkError{Kind: game.NetworkHostOutside, Address: "10.0.0.1", Network: "games"},
	&game.NetworkError{Kind: game.NetworkSelfFile, File: "/.dockerenv"},
	&game.NetworkError{Kind: game.NetworkSelfTCP},
}

// The page's English for every failure says what the log says, word for
// word, so converting the words changed nothing an admin reads (SPEC 22.3).
func TestTypedFailuresReadAsTheLogInEnglish(t *testing.T) {
	plain := []error{game.ErrAlreadyRunning, game.ErrNotRunning, game.ErrNotReady, game.ErrImageMissing,
		game.ErrRCONUnreachable, game.ErrSaveTimeout, game.ErrStartFailed, game.ErrCancelled, game.ErrStopCancelled,
		game.ErrNoCountdown, game.ErrDownloading, game.ErrForeign, game.ErrPaused, world.ErrSaveInFuture,
		game.ErrSwitchSame, game.ErrSwitchNotActive, game.ErrSwitchNoTarget, game.ErrSwitchTargetRunning,
		game.ErrSwitchNoActive, game.ErrSwitchUnreadable, game.ErrSwitchNewer, game.ErrSwitchBusy,
		game.ErrSwitchUnsettled, game.ErrSwitchUnavailable, game.ErrWorldNotActive, world.ErrVersionNotNewer,
		world.ErrVersionUnsupported, world.ErrNoRollback, game.ErrCountdown}
	for _, err := range append(plain, typedFailures...) {
		if got, want := english(failureText(err)), sentence(err); got != want {
			t.Errorf("%T %v:\n got %q\nwant %q", err, err, got, want)
		}
		// Wrapped as the controller wraps them, the words stay the same.
		if got, want := english(failureText(fmt.Errorf("start: %w", err))), sentence(err); got != want {
			t.Errorf("wrapped %v: got %q", err, got)
		}
	}
}

// Each failure names its step, world, image or network in every language,
// and Factorio's own words stay English (SPEC 22.3).
func TestTypedFailuresNameTheirFactsInGerman(t *testing.T) {
	de := i18n.Locale("de")
	cases := []struct {
		err   error
		names []string
	}{
		{&game.StartError{Kind: game.StartStopped, Reason: game.LoadReason{Mods: 2, Required: "2.0", Current: "2.1", Shown: []string{"alpha-mod"}, More: 1}}, []string{"2.0", "2.1", "alpha-mod"}},
		{&game.StartError{Kind: game.StartStopped, Reason: game.LoadReason{Said: "Opening zip failed"}}, []string{"Opening zip failed"}},
		{&game.StartError{Kind: game.StartLoadFailed, Detail: "Map version 2.1.30"}, []string{"Map version 2.1.30"}},
		{&game.ForcedStopError{World: "Alpha", Stopped: true}, []string{"Alpha"}},
		{game.NotActiveError{Name: "Bravo"}, []string{"Bravo"}},
		{&game.NetworkError{Kind: game.NetworkNotJoined, Network: "games"}, []string{"games", "FOREMAN_GAME_NETWORK"}},
		{&game.ImageDownloadError{Image: "factoriotools/factorio:2.0.76", Err: errors.New("x")}, []string{"factoriotools/factorio:2.0.76"}},
	}
	for _, c := range cases {
		_, msg := gameError(c.err)
		got := de.Format(msg)
		if got == english(msg) {
			t.Errorf("%v is not translated: %q", c.err, got)
		}
		for _, n := range c.names {
			if !strings.Contains(got, n) {
				t.Errorf("%v in German lacks %q: %q", c.err, n, got)
			}
		}
	}
	html := string(de.FormatHTML(startErrorMsg(&game.StartError{Kind: game.StartStopped, Reason: game.LoadReason{Said: "bad <thing>"}})))
	if !strings.Contains(html, `<span lang="en">bad &lt;thing&gt;</span>`) {
		t.Errorf("the game's words are not marked English: %s", html)
	}
}

// Every step the game can show has words, in English the game's own name
// (DESIGN 7.6), and another language says it differently.
func TestStepNamesAreTranslated(t *testing.T) {
	for _, name := range []string{game.StepWarn, game.StepSave, game.StepStopContainer, game.StepCheck,
		game.StepSnapshot, game.StepImage, game.StepCreate, game.StepStartContainer, game.StepLoad, game.StepRCON,
		game.StepRestore, game.StepDownload, game.StepSnapshotUpdate, game.StepSetVersion, game.StepSnapshotBoth,
		game.StepPresent} {
		m := stepName(name)
		if m.Key == "server.step.other" {
			t.Errorf("%q has no words of its own", name)
		}
		if got := english(m); got != name {
			t.Errorf("%q reads %q in English", name, got)
		}
		if got := i18n.Locale("fr").Format(m); got == name {
			t.Errorf("%q is not translated to French", name)
		}
	}
}

// SPEC 7.10: the notice of an unfinished switch says where it stopped and
// names both worlds, from the step the store recorded.
func TestSwitchNoticeNamesTheStepAndWorlds(t *testing.T) {
	names := map[string]string{"alpha": "Alpha", "bravo": "Bravo"}
	cases := map[string]string{
		"begun":       "Switch from Alpha to Bravo stopped at stopping Alpha. Alpha is still active and was not started again.",
		"stopped":     "Switch from Alpha to Bravo stopped at the snapshots. Alpha is still active and was not started again.",
		"snapshotted": "Switch from Alpha to Bravo stopped at the check of Bravo. Alpha is still active and was not started again.",
		"checked":     "Switch from Alpha to Bravo stopped at making Bravo active. Alpha is still active and was not started again.",
		"odd":         "Switch from Alpha to Bravo stopped at step odd. Alpha is still active and was not started again.",
	}
	for step, want := range cases {
		n := &game.SwitchNotice{SwitchInfo: game.SwitchInfo{From: "alpha", To: "bravo"}, Step: step}
		p := ServerPage{Status: game.Status{Notice: n}, WorldNames: names}
		if got := english(p.Notice()); got != want {
			t.Errorf("%s: got %q\nwant %q", step, got, want)
		}
		if got := i18n.Locale("pl").Format(p.Notice()); !strings.Contains(got, "Alpha") || !strings.Contains(got, "Bravo") || got == want {
			t.Errorf("%s in Polish: %q", step, got)
		}
	}
	n := &game.SwitchNotice{SwitchInfo: game.SwitchInfo{From: "alpha", To: "bravo", Presented: true}, Step: "presented"}
	if got := english((ServerPage{Status: game.Status{Notice: n}, WorldNames: names}).Notice()); got != "Bravo is active; the switch did not confirm it running." {
		t.Errorf("presented: %q", got)
	}
}

// SPEC 7.4: the pre-start problems read as the world package writes them.
func TestProblemWords(t *testing.T) {
	cases := []struct {
		p    world.Problem
		want string
	}{
		{world.Problem{Kind: world.ProblemGameVersion, GameVersion: "3.0.1"}, "The world is set to Factorio 3.0.1. Foreman runs only Factorio 1.1, 2.0 and 2.1."},
		{world.Problem{Kind: world.ProblemNoSave}, "The world has no save. Upload a save first; without one the game would make a new map."},
		{world.Problem{Kind: world.ProblemNoSave, Save: "a.zip"}, "The save chosen to load, a.zip, is no longer in the world. Choose another save."},
		{world.Problem{Kind: world.ProblemSaveUnsupported, Save: "a.zip", SaveVersion: "0.17.79"}, "The save a.zip was made by Factorio 0.17.79. Foreman runs only saves from Factorio 1.1, 2.0 and 2.1."},
		{world.Problem{Kind: world.ProblemSaveUnreadable, Save: "a.zip", Detail: "not a zip"}, "Foreman could not read the save a.zip (not a zip), so its mods could not be checked."},
		{world.Problem{Kind: world.ProblemSaveNewer, Save: "a.zip", SaveVersion: "2.1.30", GameVersion: "2.1.24"}, "The save a.zip was made by Factorio 2.1.30, newer than this world's Factorio 2.1.24. An older game cannot load it."},
		{world.Problem{Kind: world.ProblemSpaceAgeOff, Save: "a.zip", Names: []string{"space-age", "quality"}}, "a.zip uses Space Age (space-age, quality), but Space Age is off for this world."},
		{world.Problem{Kind: world.ProblemModMissing, Save: "a.zip", Mods: []string{"x"}}, "a.zip uses x, but this world has no file for it."},
		{world.Problem{Kind: world.ProblemModMissing, Save: "a.zip", Mods: []string{"x", "y"}}, "a.zip uses x, y, but this world has no files for them."},
		{world.Problem{Kind: world.ProblemModDisabled, Save: "a.zip", Mods: []string{"x", "y"}}, "a.zip uses x, y, but they are turned off in this world."},
		{world.Problem{Kind: world.ProblemModListSpaceAge, SpaceAge: true, Names: []string{"quality"}}, "Space Age is on for this world, but mod-list.json has quality turned off. Fix the mod list before starting."},
		{world.Problem{Kind: world.ProblemModGameVersion, World: "Check", GameVersion: "2.0.77", UpdateTo: "2.1",
			Groups: []world.ModGroup{{Version: "2.1", Count: 2, Shown: []string{"b", "c"}}, {Version: "1.1", Count: 1, Shown: []string{"a"}}}},
			"Check cannot run on Factorio 2.0.77: 2 of its mods were made for Factorio 2.1, and the game refuses to load them: b, c. " +
				"1 of its mods was made for Factorio 1.1, and the game refuses to load it: a. Update the world's game version to a 2.1 release, or update its mods."},
	}
	for _, c := range cases {
		if got := english(problemMsg(c.p)); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.p.Kind, got, c.want)
		}
	}
}

// SPEC 8.7, 8.9: the crash box names the game's reason and when, in each
// of its forms.
func TestCrashWords(t *testing.T) {
	at := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	mods := game.LoadReason{Mods: 1, Required: "2.0", Current: "2.1", Shown: []string{"m"}}
	said := game.LoadReason{Said: "Opening zip failed."}
	cases := []struct {
		looping, earlier bool
		at               time.Time
		r                game.LoadReason
		want             string
	}{
		{true, false, at, game.LoadReason{}, "The game crashed again and again, so Foreman stopped it."},
		{true, false, at, mods, "The game crashed again and again, so Foreman stopped it: 1 mod was made for Factorio 2.0, and this game runs 2.1: m."},
		{true, true, at, said, "Earlier, at 2026-11-04 10:00 UTC: the game crashed again and again, so Foreman stopped it. Factorio said: Opening zip failed."},
		{true, true, time.Time{}, said, "Earlier: the game crashed again and again, so Foreman stopped it. Factorio said: Opening zip failed."},
		{false, false, at, game.LoadReason{}, "The game crashed at 2026-11-04 10:00 UTC. Docker is starting it again."},
		{false, false, time.Time{}, mods, "The game crashed: 1 mod was made for Factorio 2.0, and this game runs 2.1: m. Docker is starting it again."},
		{false, true, at, said, "Earlier, at 2026-11-04 10:00 UTC: the game crashed. Factorio said: Opening zip failed. Docker is starting it again."},
		{false, true, time.Time{}, game.LoadReason{}, "Earlier: the game crashed. Docker is starting it again."},
	}
	for _, c := range cases {
		if got := english(crashMsg(c.looping, c.earlier, c.at, c.r)); got != c.want {
			t.Errorf("got  %q\nwant %q", got, c.want)
		}
	}
}

// SPEC 7.1: an unknown state says why, in the page's language.
func TestUnknownReasonWords(t *testing.T) {
	for _, r := range []game.UnknownReason{{Kind: game.UnknownDocker}, {Kind: game.UnknownInspect}, {Kind: game.UnknownAsking}, {Kind: game.UnknownPaused}} {
		if got := english(unknownMsg(r)); got != r.Text() {
			t.Errorf("%s: %q, want %q", r.Kind, got, r.Text())
		}
	}
	if !unknownMsg(game.UnknownReason{}).IsZero() {
		t.Error("a known state has a reason")
	}
}

// An English technical detail inside a translated sentence is marked
// lang="en", so a screen reader reads it with English rules (SPEC 22.3).
func TestEnglishDetailsAreMarkedEnglish(t *testing.T) {
	de := i18n.Locale("de")
	for name, m := range map[string]i18n.Msg{
		"save unreadable": problemMsg(world.Problem{Kind: world.ProblemSaveUnreadable, Save: "a.zip", Detail: "zip: not a valid zip file"}),
		"password":        unknownMsg(game.UnknownReason{Kind: game.UnknownPassword, Detail: "zip: not a valid zip file"}),
	} {
		if got := string(de.FormatHTML(m)); !strings.Contains(got, `<span lang="en">zip: not a valid zip file</span>`) {
			t.Errorf("%s: detail not marked English: %s", name, got)
		}
	}
}

// A save-read reason savefile names is said in the page's language, even
// when wrapped; one it does not name stays English, marked (SPEC 22.3).
func TestSaveReadDetailIsTranslated(t *testing.T) {
	de := i18n.Locale("de")
	for _, detail := range []string{savefile.ErrNotZip.Error(), "open a.zip: " + savefile.ErrTruncated.Error()} {
		got := string(de.FormatHTML(problemMsg(world.Problem{Kind: world.ProblemSaveUnreadable, Save: "a.zip", Detail: detail})))
		if strings.Contains(got, "the save") || strings.Contains(got, `lang="en"`) {
			t.Errorf("%q not translated: %s", detail, got)
		}
	}
}
