package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Tests for the Mods tab, search, the plan page and the mod files (SPEC 9,
// 17.8, 17.9, plan rulings 1, 2, 7, 8). The portal is the Go fake; nothing
// here reaches the real one.

const v20 = "2.0.77"

type modsRig struct {
	*gameRig
	portal *modportaltest.Portal
}

// newModsRig is a game rig whose handler asks the fake portal. withAccount
// sets the fake's factorio.com account in the environment.
func newModsRig(t *testing.T, withAccount bool) *modsRig {
	t.Helper()
	fake := fakegame.New(t, fakegame.Config{Images: []string{image21, game.ImageRef(v20)}})
	g := newGameRigOn(t, fake.Host(), fake)
	p := modportaltest.New(t)
	env := factorioaccount.Account{}
	if withAccount {
		env = factorioaccount.Account{Username: secret.New(modportaltest.Username), Token: secret.New(modportaltest.Token)}
	}
	acct, err := factorioaccount.New(env, secret.Secret{}, g.store)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Deps{Config: g.cfg, Store: g.store, Worlds: g.worlds, Game: g.ctrl, Portal: modportal.NewForTest(p.URL),
		Account: acct, Log: g.slog, Now: g.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	g.h = h
	g.liveServer = newLiveServerFor(t, g.harness)
	return &modsRig{gameRig: g, portal: p}
}

// world20 makes a stopped 2.0 world without Space Age, with a save, and
// the given mod zips ("name_version") in its mods folder.
func (g *modsRig) world20(name string, zips ...string) string {
	g.t.Helper()
	id, err := g.worlds.Create(context.Background(), name, v20, false)
	if err != nil {
		g.t.Fatal(err)
	}
	g.copySave(id, "2.0.77-no-space-age.zip")
	for _, z := range zips {
		n, v, _ := strings.Cut(z, "_")
		g.writeMod(id, n, v)
	}
	return id
}

func (g *modsRig) copySave(id, fixture string) {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.modsDir(id), "..", world.SavesDir, "base.zip"), b, 0o600); err != nil { //nolint:gosec // a test temp folder
		g.t.Fatal(err)
	}
}

func (g *modsRig) modsDir(id string) string {
	return filepath.Join(g.worlds.Dirs().Worlds, id, world.ModsDir)
}

func modZip(t *testing.T, name, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name + "_" + version + "/info.json")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := json.Marshal(map[string]any{"name": name, "version": version, "factorio_version": "2.0"})
	_, _ = w.Write(info)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (g *modsRig) writeMod(id, name, version string) {
	g.t.Helper()
	if err := os.MkdirAll(g.modsDir(id), 0o750); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.modsDir(id), name+"_"+version+".zip"), modZip(g.t, name, version), 0o600); err != nil {
		g.t.Fatal(err)
	}
}

func (g *modsRig) hasFile(id, name string) bool {
	_, err := os.Stat(filepath.Join(g.modsDir(id), name))
	return err == nil
}

func (g *modsRig) modSnapshots(id string) int {
	g.t.Helper()
	list, err := g.worlds.Snapshots(id)
	if err != nil {
		g.t.Fatal(err)
	}
	n := 0
	for _, s := range list {
		if s.Reason == world.ReasonMods {
			n++
		}
	}
	return n
}

var reHidden = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)

// planValues reads the plan form's hidden fields from a page.
func planValues(t *testing.T, body string) url.Values {
	t.Helper()
	form := between(body, `<section class="confirm">`, `</form>`)
	if form == "" {
		t.Fatalf("no plan form:\n%s", body)
	}
	v := url.Values{}
	for _, m := range reHidden.FindAllStringSubmatch(form, -1) {
		v.Add(m[1], html.UnescapeString(m[2]))
	}
	return v
}

// postMultipart sends fields in order, then one file.
func (g *modsRig) postMultipart(path string, fields [][2]string, field, filename string, content []byte) reply {
	g.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			g.t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		g.t.Fatal(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	req, err := http.NewRequest(http.MethodPost, g.url+path, &b)
	if err != nil {
		g.t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(g.cookie)
	resp, err := g.client.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, body: string(body)}
}

func (g *modsRig) getStatus(path string) (int, string) {
	g.t.Helper()
	rec := g.harness.get(path, withCookie(g.cookie))
	return rec.Code, rec.Body.String()
}

// SPEC 9, 9.7: the tab lists built-in, zip and folder mods; built-in ones
// have no Remove and no Disable, a folder mod no Remove.
func TestModsTabListsMods(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "alpha_1.0.0")
	if err := os.MkdirAll(filepath.Join(g.modsDir(id), "folder-mod"), 0o750); err != nil {
		t.Fatal(err)
	}
	code, body := g.getStatus(modsPath(id))
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, g.logs.String())
	}
	wantIn(t, "mods tab", body, `>alpha</span>`, `>folder-mod</span>`, "Built in", "Local folder",
		`href="/worlds/`+id+`/mods/plan?kind=remove&amp;name=alpha"`, `href="/worlds/`+id+`/mods/plan?kind=disable&amp;name=alpha"`,
		`hx-get="/worlds/`+id+`/mods/status"`, "Match mods to the save", `aria-current="page">Mods</a>`)
	for _, bad := range []string{"name=base", "kind=remove&amp;name=folder-mod"} {
		if strings.Contains(body, bad) {
			t.Errorf("the tab offers %s", bad)
		}
	}
	if strings.Contains(body, "Mod downloads are off") {
		t.Error("the tab says downloads are off with an account set")
	}
}

// SPEC 9.3, #131: the status fragment counts updates, fills each row's
// Update, and words each flag with its fix.
func TestModStatusUpdatesAndFlags(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.portal.AddZip("alpha", "1.1.0")
	g.portal.AddZip("beta", "2.0.0")
	id := g.world20("Base", "alpha_1.0.0", "beta_1.0.0")
	code, body := g.getStatus(modsPath(id) + "/status")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	wantIn(t, "status", body, "2 mods have an update", `kind=update"`, "Update all", `hx-swap-oob="true"`, "Update to 1.1.0",
		"the mod portal no longer has that version", "Update beta to 2.0.0")
}

func TestModStatusPortalDownSaysSo(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "alpha_1.0.0")
	g.portal.FailAPI(http.StatusInternalServerError)
	_, body := g.getStatus(modsPath(id) + "/status")
	wantIn(t, "status", body, "could not reach the mod portal", "Check again")
	if strings.Contains(body, "up to date") {
		t.Error("an unreachable portal reads as up to date")
	}
}

// SPEC 9.1, 9.11: search works without an account; the page says why
// downloads are off and links to the account page coming back here.
func TestSearchWithoutAccount(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base")
	code, body := g.getStatus(modsPath(id) + "/search?q=jetpack")
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, body)
	}
	wantIn(t, "search", body, "Jetpack", "Mod downloads are off", `/settings/factorio-account?return=`,
		"kind=install&amp;name=jetpack", "Latest version", "Downloads")
	code, body = g.getStatus(modsPath(id) + "/search?q=+")
	if code != http.StatusBadRequest || !strings.Contains(body, "Enter part of a mod") {
		t.Errorf("empty search: %d", code)
	}
}

// SPEC 9.2, 17.10: install shows the plan with its dependency, applies as
// one snapshotted operation, and the Server page offers the undo.
func TestInstallAppliesThePlanAndOffersUndo(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0", "beta >= 1.0.0")
	g.portal.AddZip("beta", "1.0.0")
	id := g.world20("Base")
	code, body := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	if code != http.StatusOK {
		t.Fatalf("plan %d\n%s", code, body)
	}
	wantIn(t, "plan", body, "Install alpha in Base?", "Install 2 mods", "beta 1.0.0 (alpha needs it)", "Before mod change", "Download 2 mods")
	v := planValues(t, body)
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", v))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "alpha_1.0.0.zip") || !g.hasFile(id, "beta_1.0.0.zip") {
		t.Fatalf("mods not placed: %v", dirNames(t, g.modsDir(id)))
	}
	if g.modSnapshots(id) != 1 {
		t.Error("no Before mod change snapshot")
	}
	page := g.get(PathServer)
	wantIn(t, "server page", page, "The mods of Base are changed", "Undo this mod change", `/worlds/`+id+`/snapshots/`)
	for _, e := range g.audit() {
		if strings.Contains(e.Detail, modportaltest.Token) {
			t.Error("the audit log holds the token")
		}
	}
	if strings.Contains(g.logs.String(), modportaltest.Token) {
		t.Error("the log holds the token")
	}
}

// Plan ruling 7, SPEC 17.8: a mods folder that changed after the plan was
// shown applies nothing and shows the new plan.
func TestPlanChangedAppliesNothing(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base")
	_, body := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	v := planValues(t, body)
	g.writeMod(id, "gamma", "1.0.0")
	rep := g.post(modsPath(id)+"/plan", v)
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "The plan changed since you saw it") {
		t.Fatalf("got %d\n%s", rep.StatusCode, rep.body)
	}
	if g.hasFile(id, "alpha_1.0.0.zip") || g.modSnapshots(id) != 0 {
		t.Error("a changed plan was applied")
	}
	// The new plan can be confirmed.
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", planValues(t, rep.body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
}

// SPEC 9.11, 17.8: without an account a plan that downloads says so before
// the confirmation and has no button; a post is refused before anything.
func TestPlanWithoutAccountRefusesDownloads(t *testing.T) {
	g := newModsRig(t, false)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base")
	_, body := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	wantIn(t, "plan", body, "downloads are off", "/settings/factorio-account?return=")
	if strings.Contains(body, `id="act-apply"`) {
		t.Error("the plan offers to apply without an account")
	}
	rep := g.post(modsPath(id)+"/plan", planValues(t, body))
	if rep.StatusCode != http.StatusConflict || g.modSnapshots(id) != 0 {
		t.Fatalf("got %d with %d snapshots", rep.StatusCode, g.modSnapshots(id))
	}
}

// SPEC 9.2: a conflict is an error message, and the plan has no button.
func TestPlanConflictHasNoButton(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0", "! beta")
	id := g.world20("Base", "beta_1.0.0")
	code, body := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	if code != http.StatusConflict {
		t.Fatalf("status %d", code)
	}
	wantIn(t, "plan", body, "cannot be applied", "alpha cannot run together with beta")
	if strings.Contains(body, `id="act-apply"`) {
		t.Error("a conflicting plan offers to apply")
	}
}

// SPEC 9.4: a world that is not active is changed and the result shows on
// its Mods tab, with the undo.
func TestDisableOnInactiveWorldReportsOnModsTab(t *testing.T) {
	g := newModsRig(t, true)
	g.world20("Active")
	id := g.world20("Other", "alpha_1.0.0")
	_, body := g.getStatus(kindPlanPath(id, mods.KindDisable, "alpha"))
	wantIn(t, "plan", body, "Disable alpha in Other?", "Disable 1 mod")
	g.wantRedirectTo(modsPath(id)+ownResultQuery, g.post(modsPath(id)+"/plan", planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	tab := g.get(modsPath(id) + ownResultQuery)
	wantIn(t, "mods tab", tab, "The mods of Other are changed", "Undo this mod change", ">Disabled<")
}

// Todd's ruling: turning off a mod the loading save uses needs its own
// confirmation, since the game deletes what the mod built.
func TestDroppingASaveModNeedsConfirmation(t *testing.T) {
	g := newModsRig(t, true)
	id, err := g.worlds.Create(context.Background(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	g.copySave(id, "2.1.24-space-age-testmod.zip")
	g.writeMod(id, "foreman-test", "0.1.0")
	_, body := g.getStatus(kindPlanPath(id, mods.KindDisable, "foreman-test"))
	wantIn(t, "plan", body, "The save that loads uses 1 mod this change turns off: foreman-test", `name="confirm-drops"`)
	v := planValues(t, body)
	rep := g.post(modsPath(id)+"/plan", v)
	if rep.StatusCode != http.StatusBadRequest || !strings.Contains(rep.body, "Tick the box to confirm") || g.modSnapshots(id) != 0 {
		t.Fatalf("unconfirmed drop: %d\n%s", rep.StatusCode, rep.body)
	}
	v.Set("confirm-drops", "1")
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", v))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
}

// SPEC 9.8, 9.10: a save's mod the portal lacks blocks the sync, offers an
// upload, and "continue without" needs its own box.
func TestSyncUnresolvedChoices(t *testing.T) {
	g := newModsRig(t, true)
	id, err := g.worlds.Create(context.Background(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	g.copySave(id, "2.1.24-space-age-testmod.zip")
	_, body := g.getStatus(syncPath(id))
	wantIn(t, "sync", body, "The mod portal does not have foreman-test 0.1.0", "Upload the zip of foreman-test 0.1.0",
		"Continue without foreman-test", `name="mod" type="file"`)
	if strings.Contains(body, `id="act-apply"`) {
		t.Error("an unresolved sync offers to apply")
	}
	q := url.Values{"kind": {"sync"}, "choice-foreman-test": {"without"}}
	_, body = g.getStatus(planPath(id, q))
	v := planValues(t, body)
	v.Set("choice-foreman-test", "without")
	rep := g.post(modsPath(id)+"/plan", v)
	if rep.StatusCode != http.StatusBadRequest || !strings.Contains(rep.body, "Tick the box to confirm that the game will delete what foreman-test added") {
		t.Fatalf("got %d\n%s", rep.StatusCode, rep.body)
	}
	// The uploaded zip answers the upload choice and the plan can apply.
	rep = g.postMultipart(modsPath(id)+"/upload", [][2]string{{"kind", "sync"}, {"save", "base.zip"}, {"for", "foreman-test"}},
		"mod", "foreman-test_0.1.0.zip", modZip(t, "foreman-test", "0.1.0"))
	if rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, `id="act-apply"`) || !strings.Contains(rep.body, "from the uploaded zip") {
		t.Fatalf("upload answer: %d\n%s", rep.StatusCode, rep.body)
	}
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", planValues(t, rep.body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "foreman-test_0.1.0.zip") {
		t.Error("the uploaded zip was not placed")
	}
}

// SPEC 9.5: an uploaded zip is named by its info.json and installed
// through its plan; a file that is not a mod is refused at its field.
func TestModUpload(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	rep := g.postMultipart(modsPath(id)+"/upload", [][2]string{{"kind", "upload"}}, "mod", "whatever.zip", modZip(t, "delta", "3.2.1"))
	if rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, "Install the uploaded delta 3.2.1 in Base?") {
		t.Fatalf("got %d\n%s", rep.StatusCode, rep.body)
	}
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", planValues(t, rep.body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "delta_3.2.1.zip") {
		t.Error("the upload was not placed")
	}
	rep = g.postMultipart(modsPath(id)+"/upload", [][2]string{{"kind", "upload"}}, "mod", "x.zip", []byte("not a zip"))
	if rep.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(rep.body, "not a mod zip") {
		t.Errorf("bad zip: %d", rep.StatusCode)
	}
	// A crafted staged name reads nothing outside the staging area.
	v := url.Values{"kind": {"upload"}, "staged": {"../" + id + "/world.json"}}
	if code, body := g.getStatus(planPath(id, v)); code != http.StatusBadGateway || !strings.Contains(body, "no longer on the server") {
		t.Errorf("crafted staged name: %d", code)
	}
}

// SPEC 9.6: mod-settings.dat goes in through the snapshotted change and
// comes back out as a download.
func TestModSettingsUploadAndDownload(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	_, tab := g.getStatus(modsPath(id))
	fp := regexp.MustCompile(`name="fingerprint" value="([^"]+)"`).FindStringSubmatch(tab)
	if fp == nil {
		t.Fatal("no fingerprint on the tab")
	}
	data := []byte("\x02\x00settings")
	rep := g.postMultipart(modsPath(id)+"/mod-settings.dat", [][2]string{{"fingerprint", fp[1]}}, "settings", "mod-settings.dat", data)
	g.wantRedirectHome(rep)
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	dl := g.do(http.MethodGet, modsPath(id)+"/mod-settings.dat", nil, http.Header{"Cookie": {g.cookie.String()}})
	if dl.StatusCode != http.StatusOK || dl.body != string(data) || !strings.Contains(dl.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("download: %d %q", dl.StatusCode, dl.body)
	}
	if g.modSnapshots(id) != 1 {
		t.Error("no snapshot before the settings change")
	}
	// A stale page changes nothing.
	rep = g.postMultipart(modsPath(id)+"/mod-settings.dat", [][2]string{{"fingerprint", fp[1]}}, "settings", "mod-settings.dat", []byte("new"))
	if rep.StatusCode != http.StatusConflict {
		t.Errorf("stale settings upload: %d", rep.StatusCode)
	}
}

// Plan ruling 2: the Server page loads its notice after the page, and the
// notice's Fix goes straight to the plan that updates the mod.
func TestServerModNotice(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("beta", "2.0.0")
	id := g.world20("Base", "beta_1.0.0")
	page := g.get(PathServer)
	wantIn(t, "server page", page, `hx-get="/worlds/`+id+`/mods/notice"`, `id="act-mods"`)
	if strings.Contains(page, "can no longer be downloaded") {
		t.Error("the notice is in the first paint")
	}
	_, frag := g.getStatus(modsPath(id) + "/notice")
	wantIn(t, "notice", frag, "1 mod of Base can no longer be downloaded", `href="/worlds/`+id+`/mods/plan?kind=update&amp;name=beta"`)
	g.portal.FailAPI(http.StatusInternalServerError)
	g.portal.AddZip("other", "1.0.0")
	g.writeMod(id, "other", "0.5.0")
	if _, frag := g.getStatus(modsPath(id) + "/notice"); strings.Contains(frag, "message") && !strings.Contains(frag, "beta") {
		t.Errorf("a failing portal put words on the Server page: %s", frag)
	}
}

// Every mods route needs a signed-in admin.
func TestModRoutesNeedSignIn(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	for _, p := range []string{modsPath(id), modsPath(id) + "/status", modsPath(id) + "/search?q=a", syncPath(id), modsPath(id) + "/mod-settings.dat"} {
		rec := g.harness.get(p)
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusUnauthorized {
			t.Errorf("%s answered %d to a stranger", p, rec.Code)
		}
	}
}

// The hash ignores the "continue without" box, so ticking it needs no
// second confirmation.
func TestPlanHashIgnoresConfirmation(t *testing.T) {
	p := mods.Plan{Unresolved: []mods.Unresolved{{Name: "a", Choice: &mods.Choice{Kind: mods.ChoiceWithout}}}}
	q := mods.Plan{Unresolved: []mods.Unresolved{{Name: "a", Choice: &mods.Choice{Kind: mods.ChoiceWithout, Confirmed: true}}}}
	if planHash(p) != planHash(q) {
		t.Error("ticking the box changed the hash")
	}
	q.Unresolved[0].Choice.Kind = mods.ChoiceNearest
	if planHash(p) == planHash(q) {
		t.Error("another choice kept the hash")
	}
}

func TestGameLine(t *testing.T) {
	for in, want := range map[string]string{"2.0.77": "2.0", "1.1.110": "1.1", "2": ""} {
		if got := gameLine(in); got != want {
			t.Errorf("gameLine(%q) = %q", in, got)
		}
	}
}

// modPageCases render the mods pages in every language (TestEveryPageRenders).
func modPageCases() []pageCase {
	w := world.Summary{ID: "base", Name: longName, GameVersion: "2.0.77", Active: true}
	installed := []world.InstalledMod{
		{Name: "base", Version: "2.0.77", Enabled: true, BuiltIn: true},
		{Name: "alpha", Version: "1.0.0", File: "alpha_1.0.0.zip", Enabled: true},
		{Name: "folder", Version: "0.1.0", Enabled: false, Local: true},
	}
	plan := mods.Plan{
		Changes: []mods.Change{
			{Name: "alpha", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}, Enabled: true, Release: &modportal.Release{}},
			{Name: "beta", From: "1.0.0", To: "1.1.0", Action: mods.ActUpgrade, Why: mods.Reason{Code: mods.WhyRequiredBy, Of: "alpha"}, Enabled: true},
			{Name: "gamma", From: "1.0.0", To: "1.0.0", Action: mods.ActKeep},
		},
		Conflicts:  []mods.Conflict{{Code: mods.ConflictIncompatible, Mod: "alpha", Other: "delta"}},
		Unresolved: []mods.Unresolved{{Name: "old", Version: "0.1.0", Nearest: "0.2.0"}},
		Optional:   []mods.Offer{{Name: "extra", Of: "alpha"}},
	}
	form := planForm{Kind: mods.KindSync, Save: "base.zip", Choices: map[string]mods.ChoiceKind{"old": mods.ChoiceWithout}, Confirmed: map[string]bool{}}
	return []pageCase{
		{"mods", "todd", 200, ModsPage{World: w, Mods: installed, NoAccount: true, LoadSave: "base.zip", HasSettings: true, Running: true,
			CountdownForm: defaultCountdown(), UploadLimit: MaxModUpload, SettingsLimit: MaxSettingsUpload,
			Done: i18n.M("mods.done", "world", w.Name, "label", "x"), Undo: &Offer{Path: "/x", Label: i18n.M("mods.undo")}}},
		{"mods errors", "todd", 200, ModsPage{World: w, Mods: installed[:1], UploadError: i18n.M("mods.upload.not_mod"),
			SettingsError: i18n.M("mods.settings.empty"), Failure: &Failure{Message: i18n.M("mods.error.changed")}}},
		{"mods unreadable", "todd", 200, ModsPage{World: world.Summary{ID: "x", Name: "X", Unreadable: true}}},
		{"mod search", "todd", 200, ModSearchPage{World: w, Query: "alpha", NoAccount: true, Installed: map[string]bool{"alpha": true},
			Hits: []modportal.Hit{{Name: "alpha", Title: "Alpha", Summary: longName, LatestVersion: "1.0.0", FactorioVersion: "2.0", Downloads: 12345}, {Name: "beta", Title: "beta"}}}},
		{"mod search empty", "todd", 200, ModSearchPage{World: w, QueryError: i18n.M("mods.search.missing")}},
		{"mod search down", "todd", 200, ModSearchPage{World: w, Query: "x", Unreachable: true}},
		{"mod plan", "todd", 200, ModPlanPage{World: w, Form: form, Plan: plan, Running: true, CountdownForm: defaultCountdown(), NoAccount: true,
			Changed: true, Drops: []string{"gamma"}, DropsError: i18n.M("mods.drops.confirm_missing"),
			ConfirmErrors: map[string]i18n.Msg{"old": i18n.M("mods.unresolved.confirm_missing", "mod", "old")}}},
		{"mod plan ready", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindInstall, Names: []string{"alpha"}}, Plan: mods.Plan{Changes: plan.Changes[:1]}, Running: true, CountdownForm: defaultCountdown()}},
		{"mod plan error", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindUpdate}, Error: i18n.M("mods.error.portal"), Retry: "/x"}},
		{"mod plan nothing", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindUpdate}}},
	}
}

// The upload plan's staged name never leaves the server's own staging area.
func TestStagedInfoRefusesOtherNames(t *testing.T) {
	g := newModsRig(t, true)
	s := &server{worlds: g.worlds}
	for _, name := range []string{"", "x.zip", "../.partial-x", ".partial-x/../../etc", `.partial-a\b`} {
		if _, err := s.stagedInfo(name); !errors.Is(err, errStagedGone) {
			t.Errorf("stagedInfo(%q) = %v", name, err)
		}
	}
}
