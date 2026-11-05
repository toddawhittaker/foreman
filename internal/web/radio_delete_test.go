package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

var reRadio = regexp.MustCompile(`<input\b[^>]*type="radio"[^>]*>`)

// DESIGN 7.2: when a radio group has an error, each radio points at the
// error text, so a screen reader announces it on whichever radio has focus.
func TestRadioGroupsPointEveryRadioAtTheError(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		name   string
		page   Page
		errID  string
		radios int
	}{
		{"saves", SavesPage{WorldName: "Base", WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves",
			LoadHref: "/worlds/base/load-save", UploadLimit: MaxSaveUpload, NewestSelected: true,
			LoadError: "Choose which save loads.",
			Rows:      []SaveRow{{Name: "a.zip", Href: "/x"}, {Name: "b.zip", Href: "/y"}}}, "load-save-error", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, body := render(t, r, pageCase{status: 422, page: c.page})
			radios := reRadio.FindAllString(body, -1)
			if len(radios) != c.radios {
				t.Fatalf("%d radios, want %d:\n%s", len(radios), c.radios, body)
			}
			for _, in := range radios {
				if !strings.Contains(in, `aria-describedby="`+c.errID+`"`) || !strings.Contains(in, `aria-invalid="true"`) {
					t.Errorf("radio lacks describedby %s or aria-invalid: %s", c.errID, in)
				}
			}
		})
	}
}

// Without an error the radios carry no description.
func TestRadiosHaveNoDescriptionWithoutAnError(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, page: SavesPage{WorldName: "Base", WorldHref: "/worlds/base",
		SavesHref: "/worlds/base/saves", LoadHref: "/worlds/base/load-save", NewestSelected: true,
		Rows: []SaveRow{{Name: "a.zip", Href: "/x"}}}})
	for _, in := range reRadio.FindAllString(body, -1) {
		if strings.Contains(in, "aria-describedby") {
			t.Errorf("radio has a description with no error: %s", in)
		}
	}
}

const (
	reasonActive     = "This is the active world. Switch to another world before deleting it."
	reasonNewer      = "This world is from a newer Foreman, so this version will not delete it."
	reasonUnreadable = "Because its world.json cannot be read, Foreman will not delete this world."
)

// DESIGN 7.1: say why, instead of offering an action that will be refused.
func TestWorldPageDeletePanel(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		name   string
		sum    world.Summary
		reason string
	}{
		{"active", world.Summary{ID: "base", Name: "Base", Active: true}, reasonActive},
		{"newer", world.Summary{ID: "base", Name: "Base", Newer: true}, reasonNewer},
		{"unreadable", world.Summary{ID: "base", Name: "base", Unreadable: true}, reasonUnreadable},
		{"deletable", world.Summary{ID: "base", Name: "Base"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, body := render(t, r, pageCase{status: 200, page: WorldPage{World: c.sum, Name: c.sum.Name}})
			hasLink := strings.Contains(body, `href="/worlds/base/delete"`)
			if c.reason == "" {
				if !hasLink {
					t.Fatalf("no Delete world button:\n%s", body)
				}
				// The button only leads to the confirmation: a grey one.
				if strings.Contains(body, "button--danger") {
					t.Errorf("the world page's Delete button is red:\n%s", body)
				}
				return
			}
			if hasLink || strings.Contains(body, "Delete world") {
				t.Errorf("a Delete button is offered:\n%s", body)
			}
			if !strings.Contains(strings.ReplaceAll(body, "&#39;", "'"), c.reason) {
				t.Errorf("reason %q missing:\n%s", c.reason, body)
			}
		})
	}
}

// The confirmation page shows the reason instead of the form for a world
// that cannot be deleted; the confirmation keeps its red button otherwise.
func TestDeleteConfirmationShowsTheReasonInsteadOfTheForm(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	activeID, otherID := hn.seedTwo()
	future, err := hn.worlds.Create(t.Context(), "Future", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	broken, err := hn.worlds.Create(t.Context(), "Broken", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := hn.worlds.Dirs().Worlds
	if err := os.WriteFile(filepath.Join(dir, future, "world.json"), []byte(`{"format_version": 9, "name": "Future"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, broken, "world.json"), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	for id, reason := range map[string]string{activeID: reasonActive, future: reasonNewer, broken: reasonUnreadable} {
		rec := hn.get(deletePath(id), withCookie(c))
		body := strings.ReplaceAll(rec.Body.String(), "&#39;", "'")
		if rec.Code != http.StatusOK || !strings.Contains(body, reason) {
			t.Errorf("%s: %d, reason missing:\n%s", id, rec.Code, body)
		}
		if strings.Contains(body, `name="confirm_name"`) || strings.Contains(body, "button--danger") {
			t.Errorf("%s: the form is offered:\n%s", id, body)
		}
	}
	body := hn.get(deletePath(otherID), withCookie(c)).Body.String()
	if !strings.Contains(body, `class="button button--danger"`) || !strings.Contains(body, `name="confirm_name"`) {
		t.Errorf("a deletable world lost its confirmation form:\n%s", body)
	}
}

// A snapshot's reason is shown above its label only when it adds words the
// label lacks.
func TestSnapshotReasonShownOnlyWhenItAddsWords(t *testing.T) {
	cases := []struct{ reason, label, want string }{
		{world.ReasonManual, "Manual snapshot", ""},
		{world.ReasonManual, "manual", ""},
		{world.ReasonBeforeDelete, world.LabelBeforeDelete, ""},
		{world.ReasonBeforeRestore, world.LabelBeforeRestore, ""},
		{world.ReasonManual, "Before the rocket launch", "Manual"},
		{world.ReasonBeforeDelete, "Final copy", "Before delete"},
		{world.ReasonManual, "Manually tuned", "Manual"},
	}
	for _, c := range cases {
		if got := showReason(c.reason, c.label); got != c.want {
			t.Errorf("showReason(%q, %q) = %q, want %q", c.reason, c.label, got, c.want)
		}
	}

	r := newTestRenderer(t)
	for _, p := range []Page{snapshotsCase(), DeletedPage{Worlds: []world.DeletedWorld{{ID: "base", Name: "Base", Snapshots: snapshotsCase().Snapshots}}}} {
		_, body := render(t, r, pageCase{status: 200, page: p})
		// The case has a "Manual" snapshot labelled with a long name, and
		// a "Before restore" snapshot labelled "Before restore".
		if !strings.Contains(body, "<td>Manual<br>") {
			t.Errorf("a reason that adds words is hidden:\n%s", body)
		}
		if strings.Contains(body, "<td>Before restore<br>Before restore<br>") || !strings.Contains(body, "<td>Before restore<br><a") {
			t.Errorf("a repeated reason is shown:\n%s", body)
		}
	}
}

// The page offers no delete for a world whose world.json cannot be read;
// a request sent anyway is refused in words and removes nothing.
func TestDeletePostRefusedForUnreadableWorld(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedTwo()
	broken, err := hn.worlds.Create(t.Context(), "Broken", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(hn.worlds.Dirs().Worlds, broken)
	if err := os.WriteFile(filepath.Join(dir, "world.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := hn.post(deletePath(broken), url.Values{"confirm_name": {broken}}, withCookie(c))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "world.json cannot be read") {
		t.Errorf("delete: %d\n%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the refused delete changed the world: %v", err)
	}
}
