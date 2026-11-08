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
	"time"

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
// 9.13, 9.14, 17.8, 17.9). The portal is the Go fake; nothing
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
	return newModsRigWrapped(t, withAccount, nil)
}

// newModsRigWrapped is newModsRig with the portal client wrapped, for a test
// that needs answers the recorded list does not have.
func newModsRigWrapped(t *testing.T, withAccount bool, wrap func(ModPortal) ModPortal) *modsRig {
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
	var portal ModPortal = modportal.NewForTest(p.URL)
	if wrap != nil {
		portal = wrap(portal)
	}
	h, err := NewHandler(Deps{Config: g.cfg, Store: g.store, Worlds: g.worlds, Game: g.ctrl, Portal: portal,
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
	wantIn(t, "mods tab", body, `>alpha</span>`, `>folder-mod</span>`, "Local folder",
		`href="/worlds/`+id+`/mods/plan?kind=remove&amp;name=alpha"`, `href="/worlds/`+id+`/mods/plan?kind=disable&amp;name=alpha"`,
		`hx-get="/worlds/`+id+`/mods/status"`, "Match mods to the save", `aria-current="page">Mods</a>`,
		// a11y 4, 8, 9: the portal status is a live region; row buttons name their mod.
		`id="mod-status" class="mod-status" role="status"`, `Disable<span class="visually-hidden"> <span translate="no">alpha</span></span>`,
		`Remove<span class="visually-hidden"> <span translate="no">alpha</span></span>`,
		// SPEC 9.7, #372: the built-in line says which are off and why, with the way to turn them on.
		`>base</span>`, "Space Age is off, so", `>space-age</span> are off.`,
		`href="/worlds/`+id+`/settings" id="act-built-in-settings">Turn on Space Age</a>`,
		"at most 200 MB", "At most 4 MB",
		// #360: a portal mod's name is the link to its portal page, in a new tab.
		`<a href="https://mods.factorio.com/mod/alpha" id="`+portalSlot("alpha")+`" class="mod-name" target="_blank" rel="noopener noreferrer"><span class="ident" translate="no">alpha</span><span class="visually-hidden"> on the mod portal, opens in a new tab</span></a>`,
		// #373: the save's mods are read and the differences counted; the save uses neither mod.
		"The mods differ from the save", "2 enabled mods are not in the save", `id="act-sync"`,
		// #371: the rare forms fold away.
		`<details class="disclosure" id="mod-upload">`, `<details class="disclosure" id="mod-settings-replace">`,
		// SPEC 9.6: where the client keeps mod-settings.dat, then the field, then what the upload does.
		`%APPDATA%\Factorio\mods`, "Startup settings apply when the world next starts.")
	if n := strings.Count(body, "Space Age is off"); n != 1 {
		t.Errorf("the built-in line is said %d times", n)
	}
	table := between(body, "<table", "</table>")
	// #360, #343: two columns, and a state word only for a mod that is off.
	for _, bad := range []string{">base</span>", "Built in", "mods.factorio.com/mod/folder-mod", "mods.factorio.com/mod/base", "Enabled", ">State<", "Mod portal"} {
		if strings.Contains(table, bad) {
			t.Errorf("the table has %s", bad)
		}
	}
	for _, bad := range []string{"name=base", "kind=remove&amp;name=folder-mod"} {
		if strings.Contains(body, bad) {
			t.Errorf("the tab offers %s", bad)
		}
	}
	help, field, will := strings.Index(body, `id="settings-file-help"`), strings.Index(body, `id="settings-file"`), strings.LastIndex(body, "This will:")
	if help > field || field > will {
		t.Errorf("the settings form is not help, field, then what it will do: %d %d %d", help, field, will)
	}
	if strings.Contains(body, "Foreman restarts it") {
		t.Error("a stopped world says Foreman restarts it")
	}
	if strings.Contains(body, "Mod downloads are off") {
		t.Error("the tab says downloads are off with an account set")
	}
	// Red only for what destroys something: no danger box on the tab.
	if strings.Contains(body, `class="confirm"`) || strings.Contains(body, "button--danger") {
		t.Error("the Mods tab has a red box or button")
	}
}

// SPEC 9.3, #131, #342: the status is one summary line, and each mod's
// update and flags go into its own row with the fix there.
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
	wantIn(t, "status", body, "2 mods have an update", `kind=update"`, "Update all", "Update to 1.1.0",
		// beta 1.0.0 is gone: an error summary, and the row says so with its fix, the update.
		`message message--error`, "1 mod stops players joining. Its row says how to fix it.",
		`<span id="`+updateSlot("beta")+`" hx-swap-oob="true"><a href="/worlds/`+id+`/mods/plan?kind=update&amp;name=beta" class="button">Update to 2.0.0`,
		`<div id="`+noteSlot("beta")+`" class="mod-flags" hx-swap-oob="true">`,
		"the mod portal no longer has version 1.0.0")
	if strings.Contains(body, "up to date") {
		t.Error("the status says up to date while updates wait")
	}
}

// #365: "up to date" counts only mods the portal has at their installed
// version, and is not said when there are none.
func TestModStatusCurrentCount(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base", "alpha_1.0.0", "zz-not-on-portal_1.0.0")
	_, body := g.getStatus(modsPath(id) + "/status")
	wantIn(t, "status", body, "1 mod from the mod portal is up to date.", "1 mod is not on the mod portal.",
		// The portal's link goes; the name stays.
		`<span id="`+portalSlot("zz-not-on-portal")+`" class="mod-name ident" translate="no" hx-swap-oob="true">zz-not-on-portal</span>`,
		"Not on the mod portal, so players")
	if strings.Contains(body, "message--error") {
		t.Error("a note reads as an error")
	}

	alone := g.world20("Alone", "zz-not-on-portal_1.0.0")
	_, body = g.getStatus(modsPath(alone) + "/status")
	if strings.Contains(body, "up to date") {
		t.Errorf("a world whose only mod the portal lacks says up to date:\n%s", body)
	}
}

func TestCurrentCount(t *testing.T) {
	list := []world.InstalledMod{
		{Name: "base", BuiltIn: true, Enabled: true},
		{Name: "ok", Enabled: true}, {Name: "deprecated", Enabled: true},
		{Name: "gone", Enabled: true}, {Name: "missing", Enabled: true},
		{Name: "off"}, {Name: "folder", Local: true, Enabled: true},
	}
	flags := []mods.Flag{
		{Code: mods.FlagDeprecated, Name: "deprecated"},
		{Code: mods.FlagVersionGone, Name: "gone"},
		{Code: mods.FlagNotOnPortal, Name: "missing"},
	}
	if n := currentCount(list, flags); n != 2 {
		t.Errorf("current = %d, want 2 (ok and deprecated)", n)
	}
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
	wantIn(t, "plan", plain(body), "Install alpha in Base?", "Install 2 mods", "beta 1.0.0 (alpha needs it)", "Before mod change", "Download 2 mods")
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
	// The Done box names what changed (SPEC 17.9).
	wantIn(t, "server page", plain(page), "Installed alpha and beta in Base.")
	wantIn(t, "server page", page, "Undo this mod change", `/worlds/`+id+`/snapshots/`)
	for _, e := range g.audit() {
		if strings.Contains(e.Detail, modportaltest.Token) {
			t.Error("the audit log holds the token")
		}
	}
	if strings.Contains(g.logs.String(), modportaltest.Token) {
		t.Error("the log holds the token")
	}
}

// SPEC 9.14, 17.8: a mods folder that changed after the plan was
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
	wantIn(t, "plan", plain(body), "cannot be applied", "alpha cannot run together with beta")
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
	wantIn(t, "mods tab", tab, "Turned off alpha in Other.", "Undo this mod change", ">Disabled<")
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
	wantIn(t, "plan", plain(body), "The save that loads uses 1 mod this change turns off: foreman-test", `name="confirm-drops"`)
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

// SPEC 9.4: the drops a confirmation is for are part of the plan the page
// showed. If the save that loads now uses a mod the plan turns off, the plan
// changed, and the post applies nothing.
func TestPlanChangesWhenTheDropsChange(t *testing.T) {
	g := newModsRig(t, true)
	id, err := g.worlds.Create(context.Background(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	g.copySave(id, "2.1.24-space-age.zip")
	g.writeMod(id, "foreman-test", "0.1.0")
	_, body := g.getStatus(kindPlanPath(id, mods.KindDisable, "foreman-test"))
	if strings.Contains(body, `name="confirm-drops"`) {
		t.Fatalf("a save without the mod asks about drops:\n%s", body)
	}
	v := planValues(t, body)
	g.copySave(id, "2.1.24-space-age-testmod.zip")
	rep := g.post(modsPath(id)+"/plan", v)
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "The plan changed since you saw it") {
		t.Fatalf("got %d\n%s", rep.StatusCode, rep.body)
	}
	if g.modSnapshots(id) != 0 {
		t.Error("a plan with changed drops was applied")
	}
	wantIn(t, "new plan", plain(rep.body), "turns off: foreman-test", `name="confirm-drops"`)
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
	wantIn(t, "sync", plain(body), "The mod portal does not have foreman-test 0.1.0", "Upload the zip of foreman-test 0.1.0",
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
	rep = g.postMultipart(uploadAction(t, g.get(syncPath(id)), "foreman-test"), nil, "mod", "foreman-test_0.1.0.zip", modZip(t, "foreman-test", "0.1.0"))
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
	if staged := planValues(t, rep.body).Get("staged"); !strings.HasPrefix(staged, world.PrefixModStaging) {
		t.Errorf("the upload was staged as %q, want the prefix %q", staged, world.PrefixModStaging)
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
	id := g.world20("Base", "alpha_1.0.0")
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

// SPEC 9.3, DESIGN 8.11: the Server page loads its notice after the page, and the
// notice's Fix goes straight to the plan that updates the mod.
func TestServerModNotice(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("beta", "2.0.0")
	id := g.world20("Base", "beta_1.0.0")
	page := g.get(PathServer)
	// The notice sits with the status facts and is a live region (DESIGN 7.3).
	wantIn(t, "server page", page, `<div id="mods-notice" class="mods-notice" role="status" hx-get="/worlds/`+id+`/mods/notice"`, `id="act-mods"`)
	if strings.Index(page, `id="mods-notice"`) > strings.Index(page, `id="players-heading"`) {
		t.Error("the notice is not beside the status facts")
	}
	if strings.Contains(page, "can no longer be downloaded") {
		t.Error("the notice is in the first paint")
	}
	_, frag := g.getStatus(modsPath(id) + "/notice")
	wantIn(t, "notice", plain(frag), "beta 1.0.0 is gone from the mod portal, so players cannot join.", "Update beta", `href="/worlds/`+id+`/mods/plan?kind=update&amp;name=beta"`)
	g.portal.FailAPI(http.StatusInternalServerError)
	g.portal.AddZip("other", "1.0.0")
	g.writeMod(id, "other", "0.5.0")
	if _, frag := g.getStatus(modsPath(id) + "/notice"); strings.Contains(frag, "message") && !strings.Contains(frag, "beta") {
		t.Errorf("a failing portal put words on the Server page: %s", frag)
	}
}

// SPEC 17.9: after an import whose save needs a mod the world lacks, the
// result offers the sync; a world that needs none is not offered it.
func TestImportOffersSyncOnlyWhenModsAreMissing(t *testing.T) {
	g := newModsRig(t, true)
	plain := g.world20("Plain")
	id, err := g.worlds.Create(context.Background(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	g.copySave(id, "2.1.24-space-age-testmod.zip")
	wantIn(t, "import result", g.get(worldPath(id)+"?imported=1"), "Match this world&#39;s mods to its save", `href="/worlds/`+id+`/mods/plan?kind=sync"`)
	if strings.Contains(g.get(worldPath(plain)+"?imported=1"), "mods to its save") {
		t.Error("a world with every mod is offered the sync")
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
			CountdownForm: defaultCountdown(),
			Done:          i18n.M("mods.done", "world", w.Name, "label", "x"), Undo: &Offer{Path: "/x", Label: i18n.M("mods.undo")}}},
		{"mods errors", "todd", 200, ModsPage{World: w, Mods: installed[:1], UploadError: i18n.M("mods.upload.not_mod"),
			SettingsError: i18n.M("mods.settings.empty"), Failure: &Failure{Message: i18n.M("mods.error.changed")}}},
		{"mods unreadable", "todd", 200, ModsPage{World: world.Summary{ID: "x", Name: "X", Unreadable: true}}},
		{"mod search", "todd", 200, ModSearchPage{World: w, Query: "alpha", NoAccount: true, Installed: map[string]bool{"alpha": true},
			Picks: []string{"beta", "elsewhere"}, Paged: true, PickError: i18n.M("mods.search.pick_none"),
			Results: pageOfHits([]modportal.Hit{{Name: "alpha", Title: "Alpha", Summary: longName, LatestVersion: "1.0.0", FactorioVersion: "2.0", Downloads: 12345}, {Name: "beta", Title: "beta"}, {Name: longName, Title: longName, Summary: "s"}, {Name: "m4"}, {Name: "m5"}, {Name: "m6"}, {Name: "m7"}, {Name: "m8"}, {Name: "m9"}, {Name: "m10"}, {Name: "m11"}}, 1)}},
		{"mod search empty", "todd", 200, ModSearchPage{World: w, QueryError: i18n.M("mods.search.missing")}},
		{"mod search down", "todd", 200, ModSearchPage{World: w, Query: "x", Unreachable: true}},
		{"mod plan", "todd", 200, ModPlanPage{World: w, Form: form, Plan: plan, Running: true, CountdownForm: defaultCountdown(), NoAccount: true,
			Changed: true, Drops: []string{"gamma"}, DropsError: i18n.M("mods.drops.confirm_missing"),
			ConfirmErrors: map[string]i18n.Msg{"old": i18n.M("mods.unresolved.confirm_missing", "mod", "old")}}},
		{"mod plan ready", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindInstall, Names: []string{"alpha"}}, Plan: mods.Plan{Changes: plan.Changes[:1]}, Running: true, CountdownForm: defaultCountdown()}},
		{"mod plan error", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindUpdate}, Error: i18n.M("mods.error.portal"), Retry: "/x"}},
		{"mod plan nothing", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindUpdate}}},
		{"mod plan zip refused", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindSync, Save: "base.zip"},
			Plan: mods.Plan{Unresolved: []mods.Unresolved{{Name: "old mod", Version: "0.1.0"}}}, UploadFor: "old mod", UploadError: i18n.M("mods.upload.not_mod")}},
		{"mod plan redrawn", "todd", 200, ModPlanPage{World: w, Form: form, Plan: plan, Redraw: true}},
		{"mod plan adding", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindInstall, Names: []string{"alpha", longName}, Query: "a", Added: "alpha"},
			Plan: mods.Plan{Changes: plan.Changes[:2], Optional: plan.Optional}, Redraw: true, Installed: map[string]bool{"gamma": true},
			Results: pageOfHits([]modportal.Hit{{Name: "alpha", Title: "Alpha", LatestVersion: "1.0.0"}, {Name: "gamma"}, {Name: "delta", Title: longName, LatestVersion: "2.0.0", Summary: longName}}, 1)}},
		{"mod plan add search down", "todd", 200, ModPlanPage{World: w, Form: planForm{Kind: mods.KindInstall, Names: []string{"alpha"}, Query: "a"},
			Plan: mods.Plan{Changes: plan.Changes[:1]}, AddUnreachable: true}},
	}
}

// The upload plan's staged name never leaves the server's own staging area:
// another upload's temp file, which starts with the same prefix, is refused
// by a plan page and by a post, and is not deleted.
func TestStagedNameIsOnlyAModUpload(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base")
	other := world.PrefixPartial + "import-upload"
	path := filepath.Join(g.worlds.Dirs().Worlds, other)
	if err := os.WriteFile(path, modZip(t, "delta", "3.2.1"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &server{worlds: g.worlds}
	for _, name := range []string{"", "x.zip", other, "../.partial-mod-x", ".partial-mod-x/../../etc", `.partial-mod-a\b`, world.PrefixModStaging + "gone"} {
		if _, err := s.stagedInfo(name); !errors.Is(err, errStagedGone) {
			t.Errorf("stagedInfo(%q) = %v", name, err)
		}
	}
	// A plan that does not use a staged zip removes it when applied, so a
	// name that is not a staged mod's must never get that far.
	_, body := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	v := planValues(t, body)
	v.Set("staged", other)
	rep := g.post(modsPath(id)+"/plan", v)
	if rep.StatusCode == http.StatusSeeOther {
		t.Error("a post naming another upload was accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("another upload's temp file was removed: %v", err)
	}
}

var reUploadAction = regexp.MustCompile(`<form method="post" action="([^"]+)" enctype="multipart/form-data" id="([^"]+)-form"`)

// uploadAction is the path an unresolved mod's zip form posts to.
func uploadAction(t *testing.T, body, mod string) string {
	t.Helper()
	for _, m := range reUploadAction.FindAllStringSubmatch(body, -1) {
		if m[2] == modID(mod, "zip") {
			return html.UnescapeString(m[1])
		}
	}
	t.Fatalf("no zip form for %s:\n%s", mod, body)
	return ""
}

// Ids follow the mod, not its place in the list, so focus stays on the same
// mod after a redraw; no two names share an id.
func TestModIDFollowsTheName(t *testing.T) {
	if got := modID("foreman-test", "confirm"); got != "mod-foreman-test-confirm" {
		t.Errorf("modID = %q", got)
	}
	seen := map[string]string{}
	for _, name := range []string{"a b", "a_b", "a.20b", "a-b", "Ab", "ab", "a/b", "日本"} {
		id := modID(name, "zip")
		if other, ok := seen[id]; ok {
			t.Errorf("%q and %q share the id %q", name, other, id)
		}
		if strings.ContainsAny(id, " \"'<>/") {
			t.Errorf("modID(%q) = %q", name, id)
		}
		seen[id] = name
	}
}

// a11y 1, 3, 6: a redraw that finds a conflict still answers 200, so htmx
// swaps it in; it is announced, takes no focus, and its ids name the mods.
func TestPlanRedrawShowsAConflict(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("fp-conf", "1.0.0", "! beta")
	g.portal.AddZip("fp-main", "1.0.0", "? fp-conf")
	id := g.world20("Base", "beta_1.0.0")
	path := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-main"}})
	_, body := g.getStatus(path)
	wantIn(t, "plan", body, `id="mod-fp-conf-optional"`, `hx-get="/worlds/`+id+`/mods/plan"`)
	if strings.Contains(body, `hx-swap-oob="innerHTML:#announcer"`) {
		t.Error("a full page load announces a redraw")
	}

	ticked := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-main"}, "optional": {"fp-conf"}})
	if code, _ := g.getStatus(ticked); code != http.StatusConflict {
		t.Errorf("a full page load of a conflicting plan: %d", code)
	}
	rec := g.harness.get(ticked, withCookie(g.cookie), withHeader("HX-Request", "true"))
	body = rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("redraw %d", rec.Code)
	}
	wantIn(t, "redraw", plain(body), "fp-conf cannot run together with beta", `id="plan-conflicts"`,
		`<div hx-swap-oob="innerHTML:#announcer">Plan updated: it has a conflict, so it cannot be applied.</div>`)
	if strings.Contains(between(body, `id="mod-plan"`, `</main>`), "autofocus") {
		t.Error("a redraw moves focus")
	}
}

// a11y 11: a redraw keeps the countdown the admin typed, rather than
// resetting it to the default.
func TestPlanRedrawKeepsTheCountdown(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base")
	g.startGame(id)
	q := url.Values{"kind": {"install"}, "name": {"alpha"}, "countdown": {"7"}}
	rec := g.harness.get(planPath(id, q), withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "redraw", rec.Body.String(), `id="countdown" name="countdown" type="text" inputmode="numeric" value="7"`,
		"Plan updated: 1 mod changes.")
}

// S3: a plan request names at most 50 mods, and another site's page cannot
// make a signed-in browser ask for a plan; the admin's own click can.
func TestPlanRequestIsBounded(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base")
	names := make([]string, maxPlanNames+1)
	for i := range names {
		names[i] = "m" + strings.Repeat("x", i)
	}
	if code, _ := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": names})); code != http.StatusBadRequest {
		t.Errorf("51 names: %d", code)
	}
	if code, _ := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"alpha"}, "optional": names})); code != http.StatusBadRequest {
		t.Errorf("51 optional mods: %d", code)
	}
	path := kindPlanPath(id, mods.KindInstall, "alpha")
	for _, site := range []string{"cross-site", "same-site"} {
		rec := g.harness.get(path, withCookie(g.cookie), withHeader("Sec-Fetch-Site", site))
		body := rec.Body.String()
		if rec.Code != http.StatusForbidden || !strings.Contains(body, "opened from another site") || !strings.Contains(body, `href="`+html.EscapeString(path)+`" class="button">Try again`) {
			t.Errorf("%s: %d\n%s", site, rec.Code, body)
		}
	}
	if n := g.portal.FullCalls("alpha") + g.portal.FullCalls("m"); n != 0 {
		t.Errorf("refused requests asked the portal %d times", n)
	}
	for _, site := range []string{"same-origin", "none", ""} {
		rec := g.harness.get(path, withCookie(g.cookie), withHeader("Sec-Fetch-Site", site))
		if rec.Code != http.StatusOK {
			t.Errorf("%q: %d", site, rec.Code)
		}
	}
}

// a11y 2, 5, 7: a refused zip for an unresolved mod is an error in the
// summary, linked to the zip field in the mod's own group, and every other
// answer on the page is kept.
func TestRefusedUnresolvedZipKeepsTheAnswers(t *testing.T) {
	g := newModsRig(t, true)
	// A 2.0 world with the 2.1 save: foreman-test and the save's built-in
	// recycler are both unresolved.
	id, err := g.worlds.Create(context.Background(), "Modded", v20, true)
	if err != nil {
		t.Fatal(err)
	}
	g.copySave(id, "2.1.24-space-age-testmod.zip")
	q := url.Values{"kind": {"sync"}, "choice-recycler": {"without"}, "confirm-recycler": {"1"}}
	_, body := g.getStatus(planPath(id, q))
	wantIn(t, "sync", body, `id="mod-recycler-confirm" required hx-get="/worlds/`+id+`/mods/plan" checked`, `<label for="mod-foreman-test-zip" class="required">`,
		`form="mod-foreman-test-zip-form"`, `class="hint required-note"`, "at most 200 MB")
	if strings.Contains(body, `id="mod-foreman-test-confirm"`) {
		t.Error("the continue-without box shows for a mod whose choice is upload")
	}
	action := uploadAction(t, body, "foreman-test")
	rep := g.postMultipart(action, nil, "mod", "junk.zip", []byte("not a zip"))
	if rep.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("junk zip: %d", rep.StatusCode)
	}
	wantIn(t, "refused", rep.body, `id="error-summary"`, `<a href="#mod-foreman-test-zip">This file is not a mod zip`,
		`id="mod-foreman-test-zip-error"`, `id="mod-recycler-without" hx-get="/worlds/`+id+`/mods/plan" checked`, `id="mod-recycler-confirm" required hx-get="/worlds/`+id+`/mods/plan" checked`)
}

// plain is a page with its mod names unwrapped, so a test can look for a
// sentence that names a mod (DESIGN 8.3, identifiers never hyphenate).
func plain(body string) string {
	return identSpan.ReplaceAllString(body, "$1")
}

var identSpan = regexp.MustCompile(`<span class="ident" translate="no">([^<]*)</span>`)

// mod-settings.dat arrives in a plain temp file, never a staged mod's name,
// so a mod plan can never read or remove it.
func TestSettingsUploadIsNotStaged(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "alpha_1.0.0")
	_, tab := g.getStatus(modsPath(id))
	fp := regexp.MustCompile(`name="fingerprint" value="([^"]+)"`).FindStringSubmatch(tab)
	if fp == nil {
		t.Fatal("no fingerprint on the tab")
	}
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("fingerprint", fp[1])
	fw, _ := mw.CreateFormFile("settings", "mod-settings.dat")
	_, _ = fw.Write(bytes.Repeat([]byte{2}, 64<<10))
	_ = mw.Close()
	body := b.Bytes()
	// Send all but the end, look for the temp file, then finish.
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, g.url+modsPath(id)+"/mod-settings.dat", pr)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(g.cookie)
	done := make(chan error, 1)
	go func() {
		resp, err := g.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()
	cut := len(body) - 100
	if _, err := pw.Write(body[:cut]); err != nil {
		t.Fatal(err)
	}
	var temp string
	for i := 0; i < 500 && temp == ""; i++ {
		entries, _ := os.ReadDir(g.worlds.Dirs().Worlds)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), world.PrefixPartial) {
				temp = e.Name()
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _ = pw.Write(body[cut:])
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if temp == "" {
		t.Fatal("no temp file seen during the upload")
	}
	if strings.HasPrefix(temp, world.PrefixModStaging) {
		t.Errorf("mod-settings.dat was received as a staged mod: %s", temp)
	}
}
