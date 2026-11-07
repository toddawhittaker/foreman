package web

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// tipsPage renders the map settings of a version, in English or another language.
func tipsPage(t *testing.T, loc i18n.Locale, version, line string, spaceAge bool) string {
	t.Helper()
	ms, _, _ := readMapSettings(version, "default", nil)
	if ms.Untested || len(ms.Groups) == 0 {
		t.Fatalf("%s: no map settings", version)
	}
	r := newTestRenderer(t)
	var b bytes.Buffer
	f := GenerateForm{MapGen: NewMapGenFormForPreset(line, spaceAge, "default"), MapSettings: ms, Preset: "default", Presets: []string{"default"}}
	if err := r.pages[loc]["generate"].ExecuteTemplate(&b, "map-settings", f); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

var tipVersions = []struct {
	version, line string
}{{"1.1.110", "1.1"}, {"2.0.77", "2.0"}, {"2.1.24", "2.1"}}

// SPEC 5.6, 17: every setting of the Enemy and Advanced tabs explains itself
// with a toggletip, in the game's words where it has them.
func TestEveryEnemyAndAdvancedSettingHasATip(t *testing.T) {
	for _, v := range tipVersions {
		ms, _, _ := readMapSettings(v.version, "default", nil)
		for _, g := range ms.Groups {
			for _, fl := range g.Fields {
				if fl.Tip.IsZero() {
					t.Errorf("%s: %s has no tip", v.version, fl.Name)
				}
			}
		}
		f := NewMapGenFormForPreset(v.line, true, "default")
		for _, g := range f.Enemy {
			for _, s := range g.Selects {
				if s.Tip.IsZero() {
					t.Errorf("%s: %s has no tip", v.line, s.Name)
				}
			}
		}
		for _, c := range []MapGenCheck{f.Peaceful, f.NoEnemies} {
			if c.Name != "" && c.Tip.IsZero() {
				t.Errorf("%s: %s has no tip", v.line, c.Name)
			}
		}
		if f.StartingArea.Tip.IsZero() {
			t.Errorf("%s: starting area has no tip", v.line)
		}
	}
}

// A toggletip's button names the setting, controls its text, and the field
// it explains is described by that text (DESIGN 7.7).
func TestToggletipMarkup(t *testing.T) {
	page := tipsPage(t, i18n.Locale("en"), "2.1.24", "2.1", true)
	for _, want := range []string{
		`<button type="button" class="tip" aria-expanded="false" aria-controls="ms-enemy-evolution-time-factor-tip" aria-describedby="ms-enemy-evolution-time-factor-tip">`,
		`<span class="visually-hidden">About Time factor</span>`,
		`<p class="tip__text" id="ms-enemy-evolution-time-factor-tip">Controls how fast evolution increases over time. Each second, evolution rises by the number times 0.0000001: 40 means 0.000004.</p>`,
		// A group's Enabled box is about the group.
		`<span class="visually-hidden">About Evolution</span>`,
		`<span class="visually-hidden">About Peaceful mode</span>`,
		`<span class="visually-hidden">About Starting area size</span>`,
		`<p class="tip__text" id="mg-nauvis-enemy-base-frequency-tip">The number of enemy bases.</p>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	// Every tip's text is named by the aria-describedby of its field and its button.
	for _, m := range regexp.MustCompile(`<p class="tip__text" id="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		id := m[1]
		field := strings.TrimSuffix(id, "-tip")
		re := regexp.MustCompile(`id="` + regexp.QuoteMeta(field) + `"[^>]*aria-describedby="[^"]*\b` + regexp.QuoteMeta(id) + `\b`)
		if !re.MatchString(page) {
			t.Errorf("%s does not describe its field", id)
		}
		if strings.Count(page, `aria-controls="`+id+`"`) != 1 {
			t.Errorf("%s has no button", id)
		}
	}
	// No achievement note or game placeholder leaks into a tip.
	if strings.Contains(page, "__1__") || strings.Contains(page, `\n`) {
		t.Error("a tip holds the game's note")
	}
}

// The game shows the evolution factors as plain numbers, so Foreman shows
// no "× 10⁻ⁿ" unit after them; the tip gives the scale.
func TestNoPowerOfTenUnit(t *testing.T) {
	for _, v := range tipVersions {
		for _, loc := range []i18n.Locale{"en", "de", "ru", "zh-Hans"} {
			page := tipsPage(t, loc, v.version, v.line, true)
			for _, bad := range []string{"× 10", "10⁻", "ms-enemy-evolution-time-factor-unit"} {
				if strings.Contains(page, bad) {
					t.Errorf("%s %s: page holds %q", v.version, loc, bad)
				}
			}
		}
	}
}

// The other languages show the game's own translated tip.
func TestTipsUseTheGamesTranslation(t *testing.T) {
	page := tipsPage(t, i18n.Locale("de"), "2.1.24", "2.1", true)
	if !strings.Contains(page, `>Die Anzahl an Feindbasen.</p>`) {
		t.Error("German enemy frequency tip is not the game's")
	}
}
