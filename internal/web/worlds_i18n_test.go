package web

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

var (
	reJSONBlock   = regexp.MustCompile(`(?s)<script type="application/json".*?</script>`)
	reUnfilled    = regexp.MustCompile(`\{[a-z_]+\}`)
	reMissingKeys = regexp.MustCompile(`\b(worlds|common)\.[a-z_]+\.[a-z_.]+`)
)

// No page of the worlds area shows a catalogue key or an unfilled
// placeholder in any language (SPEC 22.3, 22.4).
func TestWorldsPagesShowNoKeysOrPlaceholders(t *testing.T) {
	r := newTestRenderer(t)
	for _, l := range i18n.Languages {
		for _, c := range pageCases() {
			_, body := renderIn(t, r, c, l.Tag)
			body = reJSONBlock.ReplaceAllString(body, "")
			if m := reUnfilled.FindString(body); m != "" {
				t.Errorf("%s/%s: unfilled placeholder %s", l.Tag, c.name, m)
			}
			if m := reMissingKeys.FindString(body); m != "" {
				t.Errorf("%s/%s: key %s shown as text", l.Tag, c.name, m)
			}
		}
	}
}

// The worlds pages speak the chosen language and format their figures for
// it, while a name a person typed stays as written (SPEC 22.3, 22.4).
func TestWorldsPageInGerman(t *testing.T) {
	r := newTestRenderer(t)
	p := WorldsPage{
		Worlds: []world.Summary{{ID: "base", Name: "Base", GameVersion: "2.0.72", SpaceAge: true, DiskBytes: 2_100_000_000,
			LastPlayed: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)}},
		SnapshotAdds: map[string]uint64{"base": 1_500_000},
	}
	_, body := renderIn(t, r, pageCase{status: 200, user: "todd", page: p}, "de")
	for _, want := range []string{"<h1>Welten</h1>", "Spielversion", "2,1 GB", "Snapshots belegen zusätzlich 1,5 MB", ">Base</a>"} {
		if !strings.Contains(body, want) {
			t.Errorf("German worlds page lacks %q:\n%s", want, body)
		}
	}
}

// Snapshots Foreman named itself show in the page's language; a label a
// person typed shows as written, even when it matches an English label
// word for word under another reason (SPEC 22.3).
func TestSnapshotLabelsAreTranslatedOnlyWhenForemanNamedThem(t *testing.T) {
	snap := func(reason, label string) world.Snapshot {
		return world.Snapshot{Manifest: world.Manifest{Reason: reason, Label: label}}
	}
	for _, c := range []struct {
		name string
		s    world.Snapshot
		loc  i18n.Locale
		want string
	}{
		{"automatic, German", snap(world.ReasonBeforeRestore, world.LabelBeforeRestore), "de", "Vor der Wiederherstellung"},
		{"automatic, English", snap(world.ReasonSwitch, world.LabelBeforeSwitch), "en", "Before switch"},
		{"default manual, French", snap(world.ReasonManual, defaultLabel), "fr", "Instantané manuel"},
		{"typed label", snap(world.ReasonManual, "Before restore"), "de", "Before restore"},
		{"typed label on an automatic reason", snap(world.ReasonSwitch, "My own words"), "de", "My own words"},
		{"unknown reason", snap("something-new", "Something new"), "de", "Something new"},
	} {
		if got := snapshotLabel(c.loc, c.s); got != c.want {
			t.Errorf("%s: label %q, want %q", c.name, got, c.want)
		}
	}
	// The reason line above a typed label is translated too.
	if got := snapshotReason("de", snap(world.ReasonManual, "Before the rocket launch")); got != "Manuell" {
		t.Errorf("reason %q, want Manuell", got)
	}
	if got := snapshotReason("de", snap(world.ReasonBeforeRestore, world.LabelBeforeRestore)); got != "" {
		t.Errorf("reason %q repeats the label", got)
	}
}

// A size in a message is shown in the language's units.
func TestSizeArgumentFollowsTheLanguage(t *testing.T) {
	m := i18n.M("worlds.upload.too_large", "limit", i18n.Size(1_500_000_000))
	if got := i18n.English.Format(m); got != "The file is larger than the limit of 1.5 GB." {
		t.Errorf("en: %q", got)
	}
	if got := i18n.Locale("fr").Format(m); !strings.Contains(got, "1,5") {
		t.Errorf("fr: %q", got)
	}
}
