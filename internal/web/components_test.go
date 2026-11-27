package web

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// renderComponent renders one shared partial for one input.
func renderComponent(t *testing.T, name string, data any) (string, error) {
	t.Helper()
	var b bytes.Buffer
	err := newTestRenderer(t).pages[i18n.English]["server"].ExecuteTemplate(&b, name, data)
	return b.String(), err
}

// The working bar is decorative and its words say what is happening, so a
// screen reader hears the words once and never a progress bar (DESIGN 7.6).
// The end-to-end checks draw this same markup in every browser.
func TestWorkingBarMarkup(t *testing.T) {
	want, err := os.ReadFile("../../e2e/testdata/working-bar.html")
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderComponent(t, "working", "Counting resources…")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("working bar:\n got %s\nwant %s", got, want)
	}
	// The bar alone says nothing, so it is refused.
	for _, words := range []any{"", nil, template.HTML("")} {
		if bare, err := renderComponent(t, "working", words); err == nil {
			t.Errorf("a working bar with words %#v renders: %s", words, bare)
		}
	}
	if strings.Contains(got, "progress") {
		t.Errorf("the working bar is not a progress element: %s", got)
	}
}

func TestEmptyStateMarkup(t *testing.T) {
	// The end-to-end checks centre this same markup in a plate.
	wantDeleted, err := os.ReadFile("../../e2e/testdata/empty-state.html")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := emptyState("deleted", "Nothing has been deleted. A world you delete waits here for 30 days.")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := renderComponent(t, "empty", deleted); got != string(wantDeleted) {
		t.Errorf("empty state:\n got %s\nwant %s", got, wantDeleted)
	}

	e, err := emptyState("worlds", template.HTML(`No worlds yet. <a href="/worlds/import">Import one</a>.`), "worlds-empty")
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderComponent(t, "empty", e)
	if err != nil {
		t.Fatal(err)
	}
	want := `<p class="empty empty--art empty--worlds" id="worlds-empty"><span class="empty__text">No worlds yet. <a href="/worlds/import">Import one</a>.</span></p>`
	if got != want {
		t.Errorf("empty state:\n got %s\nwant %s", got, want)
	}
	plain, _ := emptyState("chat", "<b>none</b>")
	if got, _ := renderComponent(t, "empty", plain); !strings.Contains(got, "&lt;b&gt;none&lt;/b&gt;") || strings.Contains(got, " id=") {
		t.Errorf("plain text is escaped and no id is written: %s", got)
	}
	if _, err := emptyState("bananas", "x"); err == nil {
		t.Error("an empty state with no drawing is refused")
	}
	if _, err := emptyState("chat", "x", "a", "b"); err == nil {
		t.Error("two ids are refused")
	}
}

// One footer (DESIGN 7.1): the note leads, then Cancel as a ghost button,
// then the primary action, in screen and keyboard order alike.
func TestFormFootMarkup(t *testing.T) {
	f, err := formFoot("note", "Values come from the config file.", "noteID", "general-source",
		"cancel", "/server", "cancelText", "Cancel", "submit", "Save", "submitID", "act-save")
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderComponent(t, "form-foot", f)
	if err != nil {
		t.Fatal(err)
	}
	want := `<div class="actions form-foot">
  <p class="hint form-foot__note" id="general-source">Values come from the config file.</p>
  <a href="/server" class="button button--ghost">Cancel</a>
  <button type="submit" class="button button--primary" id="act-save">Save</button>
</div>`
	if got != want {
		t.Errorf("form foot:\n got %s\nwant %s", got, want)
	}

	live, err := formFoot("noteID", "settings-unsaved", "status", true, "submit", "Preview")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = renderComponent(t, "form-foot", live)
	if !strings.Contains(got, `<p class="hint form-foot__note" id="settings-unsaved" role="status"></p>`) || strings.Contains(got, "<a ") {
		t.Errorf("a live note may start empty, and no Cancel is drawn without a path: %s", got)
	}

	for name, pairs := range map[string][]any{
		"no submit":           {"cancel", "/x", "cancelText", "Cancel"},
		"unknown field":       {"submit", "Save", "colour", "red"},
		"odd count":           {"submit"},
		"wrong type":          {"submit", 3},
		"cancel without text": {"submit", "Save", "cancel", "/x"},
		// A status note is filled by a script, which finds it by its id.
		"status without noteID": {"submit", "Save", "status", true, "note", "Unsaved"},
	} {
		if _, err := formFoot(pairs...); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
