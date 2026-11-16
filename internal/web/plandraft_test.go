package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// A draft is the user's own, for one world, and lasts draftTTL (SPEC 9.14).
func TestDraftIsScopedAndExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	d := newDraftStore(func() time.Time { return now })
	f := planForm{Kind: mods.KindInstall, Names: []string{"a"}, Removed: "x", Fixed: "y"}
	tok := d.keep("", 1, "w1", f)
	if len(tok) < 22 {
		t.Errorf("token %q is too short to be unguessable", tok)
	}
	got, ok := d.get(tok, 1, "w1")
	if !ok || got.Names[0] != "a" || got.Removed != "" || got.Fixed != "" {
		t.Fatalf("get = %+v, %v; the one-time notes must not be kept", got, ok)
	}
	if _, ok := d.get(tok, 2, "w1"); ok {
		t.Error("another user read the draft")
	}
	if _, ok := d.get(tok, 1, "w2"); ok {
		t.Error("another world read the draft")
	}
	// Another user's token is never updated in place: they get a new one.
	if other := d.keep(tok, 2, "w1", f); other == tok {
		t.Error("another user wrote into the draft")
	}
	got.Names[0] = "changed"
	if again, _ := d.get(tok, 1, "w1"); again.Names[0] != "a" {
		t.Error("a page's form shares its lists with the kept draft")
	}
	if d.keep(tok, 1, "w1", f) != tok {
		t.Error("keeping the user's own draft changed its token")
	}
	now = now.Add(draftTTL)
	if _, ok := d.get(tok, 1, "w1"); ok {
		t.Error("an expired draft is still served")
	}
}

// The drafts are bounded per user and in all; the oldest go first.
func TestDraftsAreBounded(t *testing.T) {
	d := newDraftStore(time.Now)
	first := d.keep("", 1, "w", planForm{Kind: mods.KindInstall})
	for range maxDraftsPerUser {
		d.keep("", 1, "w", planForm{Kind: mods.KindInstall})
	}
	if _, ok := d.get(first, 1, "w"); ok {
		t.Error("a user's oldest draft outlived the per-user bound")
	}
	if n := d.countLocked(1); n != maxDraftsPerUser {
		t.Errorf("%d drafts for one user, want %d", n, maxDraftsPerUser)
	}
	for u := range int64(maxDrafts) {
		d.keep("", 100+u, "w", planForm{Kind: mods.KindInstall})
	}
	if len(d.byKey) != maxDrafts || len(d.order) != maxDrafts {
		t.Errorf("%d drafts kept, want %d", len(d.byKey), maxDrafts)
	}
}

var reDraft = regexp.MustCompile(`(?:draft=|name="draft" value=")([A-Za-z0-9_-]+)`)

// draftOf is the draft token a review page carries.
func draftOf(t *testing.T, body string) string {
	t.Helper()
	m := reDraft.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the page carries no draft")
	}
	return m[1]
}

// reAddress finds every address a page offers: links, forms, buttons and
// htmx requests.
var reAddress = regexp.MustCompile(`(?:href|action|formaction|hx-get|hx-post|hx-push-url)="([^"]*)"`)

func wantShortAddresses(t *testing.T, what, body string) {
	t.Helper()
	n := 0
	for _, m := range reAddress.FindAllStringSubmatch(body, -1) {
		n++
		if a := html.UnescapeString(m[1]); len(a) > maxAddress {
			t.Errorf("%s: address of %d bytes: %.120s…", what, len(a), a)
		}
	}
	if n == 0 {
		t.Errorf("%s: no addresses found", what)
	}
}

// bigFavorite makes a world with n portal mods of long names, and a
// favorite of it.
func (g *modsRig) bigFavorite(n int) (string, []string) {
	g.t.Helper()
	src := g.world20("Source")
	var names []string
	for i := range n {
		name := fmt.Sprintf("mod-%03d-%s", i, strings.Repeat("x", 60))
		names = append(names, name)
		g.writeMod(src, name, "1.0.0")
		g.portal.AddZip(name, "1.0.0")
	}
	favID, err := g.favs.Make(context.Background(), src, "Big")
	if err != nil {
		g.t.Fatal(err)
	}
	return favID, names
}

// A 120-mod favorite reaches one review and installs in one change, and
// every address the review and the mods pages build stays short (SPEC
// 5.12, 9.14, 17).
func TestBigFavoriteInstallsInOneReview(t *testing.T) {
	g := newModsRig(t, true)
	favID, names := g.bigFavorite(120)
	id := g.world20("Big world")
	g.pick(id, names...)
	code, body := g.getStatus(planPath(id, url.Values{favoriteQuery: {favID}, generatedQuery: {"1"}}))
	if code != http.StatusOK {
		t.Fatalf("review %d\n%.2000s", code, body)
	}
	wantIn(t, "review", plain(body), "Install 120 mods")
	wantShortAddresses(t, "review", body)
	_, tab := g.getStatus(modsPath(id))
	wantShortAddresses(t, "mods tab", tab)
	if rep := g.post(modsPath(id)+"/plan", planValues(t, body)); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("apply %d\n%.2000s", rep.StatusCode, rep.body)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if !g.hasFile(id, n+"_1.0.0.zip") {
			t.Fatalf("%s not installed", n)
		}
	}
	if left := g.picks(id); len(left) != 0 {
		t.Errorf("%d picks left after the install", len(left))
	}
}

// The review page's links, redraws and upload carry the draft's token,
// and a redraw without script comes back to the draft's address.
func TestReviewAnswersLiveInTheDraft(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.portal.AddZip("beta", "1.0.0")
	id := g.world20("Base")
	g.pick(id, "alpha", "beta")
	code, body := g.getStatus(kindPlanPath(id, mods.KindInstall, ""))
	if code != http.StatusOK {
		t.Fatalf("review %d\n%s", code, body)
	}
	v := planValues(t, body)
	tok := v.Get(draftField)
	if tok == "" {
		t.Fatal("the form carries no draft")
	}
	wantShortAddresses(t, "review", body)
	// Taking beta out keeps the draft's other answers.
	_, body = g.getStatus(draftPath(id, tok, "removed", "beta"))
	wantIn(t, "after removal", plain(body), "Install alpha in Base?")
	// A redraw without script lands on the draft's address.
	rec := g.harness.post(modsPath(id)+"/plan/redraw", planValues(t, body), withCookie(g.cookie))
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != draftPath(id, tok) {
		t.Errorf("redraw: %d to %q", rec.Code, loc)
	}
	// Another signed-in user, or a lost draft, starts again from the picks
	// and says so.
	_, body = g.getStatus(draftPath(id, "not-a-token"))
	wantIn(t, "lost draft", body, `id="plan-draft-lost"`)
	wantIn(t, "lost draft", plain(body), "Install 2 mods")
}

// A favorite saved on a game version this Foreman does not support is a
// conflict on the review, not a quiet fall back (SPEC 5.12).
func TestUnsupportedFavoriteVersionIsAConflict(t *testing.T) {
	g := newModsRig(t, true)
	favID, _ := g.bigFavorite(1)
	file := filepath.Join(g.favsDir, favID, world.FavoriteFile)
	raw, err := os.ReadFile(file) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["game_version"] = "0.17.79"
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	id := g.world20("Old fav")
	code, body := g.getStatus(planPath(id, url.Values{favoriteQuery: {favID}}))
	if code != http.StatusConflict {
		t.Fatalf("review %d", code)
	}
	wantIn(t, "review", plain(body), "0.17.79")
	wantIn(t, "review", body, `id="plan-conflicts"`)
	if strings.Contains(body, `id="act-apply"`) {
		t.Error("an unsupported favorite can be applied")
	}
	// Posting the form anyway applies nothing.
	if rep := g.post(modsPath(id)+"/plan", planValues(t, body)); rep.StatusCode != http.StatusConflict {
		t.Errorf("post: %d", rep.StatusCode)
	}
}

// The gone-mods fix posts; the server finds the gone mods again and plans
// only those, from a draft with a short address, however many there are
// (SPEC 9.3, 17).
func TestGoneFixPlansOnlyTheGoneMods(t *testing.T) {
	g := newModsRig(t, true)
	var zips, gone []string
	for i := range 30 {
		name := fmt.Sprintf("gone-%02d-%s", i, strings.Repeat("y", 60))
		gone = append(gone, name)
		zips = append(zips, name+"_1.0.0")
		g.portal.AddZip(name, "2.0.0")
	}
	// delta has an update but is not gone, so the fix leaves it alone.
	g.portal.AddZip("delta", "1.0.0")
	g.portal.AddZip("delta", "1.1.0")
	id := g.world20("Base", append(zips, "delta_1.0.0")...)
	_, frag := g.getStatus(modsPath(id) + "/notice")
	wantIn(t, "notice", frag, `action="`+goneUpdatePath(id)+`"`, ">Update 30 mods</button>")
	rec := g.harness.post(goneUpdatePath(id), url.Values{}, withCookie(g.cookie))
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, modsPath(id)+"/plan?draft=") || len(loc) > maxAddress {
		t.Fatalf("fix: %d to %q", rec.Code, loc)
	}
	code, body := g.getStatus(loc)
	if code != http.StatusOK {
		t.Fatalf("plan %d", code)
	}
	wantIn(t, "plan", plain(body), "Update 30 mods")
	for _, n := range gone {
		wantIn(t, "plan", body, n)
	}
	if strings.Contains(planRows(body), "delta") {
		t.Error("the fix plans a mod that is not gone")
	}
	wantShortAddresses(t, "plan", body)
	// Another site cannot post it.
	if rec := g.harness.post(goneUpdatePath(id), url.Values{}, withCookie(g.cookie), withHeader("Origin", "https://evil.example")); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
}

// A refused cross-site request makes no draft, so another site's page
// cannot push out the admin's own draft by asking many times.
func TestCrossSiteReviewMakesNoDraft(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base")
	_, body := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	tok := draftOf(t, body)
	for range maxDraftsPerUser + 5 {
		rec := g.harness.get(kindPlanPath(id, mods.KindInstall, "alpha"), withCookie(g.cookie), withHeader("Sec-Fetch-Site", "cross-site"))
		if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "draft=") {
			t.Fatalf("cross-site: %d, or it names a draft", rec.Code)
		}
	}
	_, body = g.getStatus(draftPath(id, tok))
	if strings.Contains(body, `id="plan-draft-lost"`) {
		t.Error("cross-site requests pushed out the admin's draft")
	}
}
