package web

import (
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/version"
)

// setBuild pretends the linker set the build variables, as the Makefile and
// the Dockerfile do.
func setBuild(t *testing.T, ver, commit string) {
	t.Helper()
	oldV, oldC := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = oldV, oldC })
	version.Version, version.Commit = ver, commit
}

// DESIGN 6 (User menu): the About page shows the
// product, the build's version and commit, the Go version, the licence and
// the project's address, and nothing about game versions.
func TestAboutPageShowsTheBuild(t *testing.T) {
	setBuild(t, "v1.4.0-3-gabcdef1", "0123456789abcdef0123456789abcdef01234567-dirty")
	hn := newHarness(t, nil)
	c := hn.signIn()
	rec := hn.get(PathAbout, withCookie(c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<h1 class="about__heading">`, "About Foreman",
		"<dt>Version</dt><dd>v1.4.0-3-gabcdef1</dd>",
		"<dt>Commit</dt><dd>0123456789ab (modified)</dd>",
		"<dt>Go version</dt><dd>" + runtime.Version() + "</dd>",
		"<dt>Licence</dt><dd>MIT</dd>",
		`<a href="https://github.com/toddawhittaker/foreman" target="_blank" rel="noopener noreferrer">GitHub`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q\n%s", want, body)
		}
	}
	main := body[strings.Index(body, "<main"):strings.Index(body, "</main>")]
	if strings.Contains(main, "development build") || strings.Contains(strings.ToLower(main), "game") {
		t.Errorf("page says development build or lists game versions\n%s", main)
	}
	// The About page already holds the content, so it carries no dialog,
	// and its ids are not repeated.
	if strings.Contains(body, `<dialog id="about-dialog"`) || strings.Count(body, `id="about-title"`) != 0 {
		t.Error("the About page carries the dialog too")
	}
}

// A build from an untagged commit has no release version: the page shows the
// commit alone, clean or modified, and does not call it a development build.
func TestAboutPageOfAnUntaggedBuildShowsOnlyTheCommit(t *testing.T) {
	setBuild(t, "", "0123456789abcdef0123456789abcdef01234567")
	hn := newHarness(t, nil)
	c := hn.signIn()
	body := hn.get(PathAbout, withCookie(c)).Body.String()
	main := body[strings.Index(body, "<main"):strings.Index(body, "</main>")]
	if !strings.Contains(main, "<dt>Commit</dt><dd>0123456789ab</dd>") {
		t.Errorf("no clean commit row\n%s", main)
	}
	if strings.Contains(main, "<dt>Version</dt>") || strings.Contains(main, "development build") || strings.Contains(main, "modified") {
		t.Errorf("a version row, a development build or a modified mark\n%s", main)
	}
}

// A build that recorded neither a version nor a commit says so, in words.
func TestAboutPageOfADevelopmentBuild(t *testing.T) {
	setBuild(t, "", "")
	hn := newHarness(t, nil)
	c := hn.signIn()
	body := hn.get(PathAbout, withCookie(c)).Body.String()
	if !strings.Contains(body, "<dt>Version</dt><dd>development build</dd>") {
		t.Errorf("no development build\n%s", body)
	}
	// With the build variables empty, Go's build info may still have a commit.
	if version.Get().Commit == "" && strings.Contains(body, "<dt>Commit</dt>") {
		t.Error("a Commit row with no commit")
	}
}

// The words follow the page's language, and the names stay as they are.
func TestAboutPageSpeaksTheLanguage(t *testing.T) {
	setBuild(t, "v1.0.0", "")
	hn := newHarness(t, nil)
	c := hn.signIn()
	body := hn.get(PathAbout, withCookie(c), withHeader("Accept-Language", "de")).Body.String()
	for _, want := range []string{"Über Foreman", "<dt>Lizenz</dt><dd>MIT</dd>", "<dt>Go-Version</dt>"} {
		if !strings.Contains(body, want) {
			t.Errorf("German page lacks %q", want)
		}
	}
}

// Signed-out visitors get the sign-in redirect, not the build's details.
func TestAboutPageNeedsSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	rec := hn.get(PathAbout)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), version.Get().Go) {
		t.Error("a signed-out visitor sees the build")
	}
}

// DESIGN 6: every signed-in page has the About item between the theme row
// and Sign out, a link to the page (it works without script), and the dialog
// with the same content; the sign-in page has neither.
func TestUserMenuHasAboutAndItsDialog(t *testing.T) {
	setBuild(t, "v2.0.0", "")
	hn := newHarness(t, nil)
	c := hn.signIn()
	body := hn.get(PathWorlds, withCookie(c)).Body.String()
	link := `<a href="/about" class="menu__item" data-about>About</a>`
	theme, about, signout := strings.Index(body, `action="/theme"`), strings.Index(body, link), strings.Index(body, `action="/signout"`)
	if theme < 0 || theme >= about || about >= signout {
		t.Errorf("order: theme %d, About %d, sign out %d", theme, about, signout)
	}
	dlg := regexp.MustCompile(`(?s)<dialog id="about-dialog" class="about-dialog" aria-labelledby="about-title" aria-describedby="about-facts">.*?</dialog>`).FindString(body)
	for _, want := range []string{`id="about-title"`, `id="about-facts"`, "<dd>v2.0.0</dd>", "<dd>MIT</dd>", `id="about-close"`, "github.com/toddawhittaker/foreman"} {
		if !strings.Contains(dlg, want) {
			t.Errorf("dialog lacks %q: %s", want, dlg)
		}
	}
	// Every page carries the dialog, so its list must not be a `dl.facts`: a
	// page's own facts list has to be the only one.
	if strings.Contains(dlg, `class="facts`) {
		t.Error("the About dialog's list uses the page facts class")
	}
	if !strings.Contains(body, "/static/about.js") {
		t.Error("signed-in pages do not load about.js")
	}
	out := hn.get(PathSignIn).Body.String()
	if strings.Contains(out, "data-about") || strings.Contains(out, "about-dialog") || strings.Contains(out, "about.js") {
		t.Error("the sign-in page has an About item")
	}
}
