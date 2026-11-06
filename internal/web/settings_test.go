package web

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The secrets in the world's files. None may ever reach a response
// (SPEC 11.4, 14.8).
var settingsSecrets = []string{"hunter2-secret", "tok-secret-123", "factorio-user", "newpass-typed"}

const settingsFile = `{
  "name": "Family base",
  "description": "Come play",
  "visibility": {"public": false, "lan": true},
  "game_password": "hunter2-secret",
  "username": "factorio-user",
  "token": "tok-secret-123",
  "max_players": 0,
  "autosave_interval": 10,
  "autosave_slots": 5,
  "auto_pause": true,
  "require_user_verification": true,
  "_comment_x": "keep me"
}
`

func settingsFormCase(errors bool) SettingsPage {
	p := SettingsPage{WorldID: "base", WorldName: longName, Name: longName, Description: "d\nline two", Visibility: "lan",
		PasswordSet: true, MaxPlayers: "0", Interval: "10", Slots: "5", Verify: true, AutoPause: true, Admins: "alice\nbob"}
	if errors {
		p.Admins, p.Running = "alice\n\u0001", true
		p.formErrors = formErrors{NameError: i18n.M("settings.name.empty"), PasswordError: i18n.M("settings.password.both"),
			MaxPlayersError: wholeError(), IntervalError: wholeError(), SlotsError: wholeError(),
			AdminsError: i18n.M("settings.admins.bad_name", "line", 2, "max", 64)}
		p.Error = i18n.M("settings.error.failed")
	}
	return p
}

func settingsRawCase(errors bool) SettingsRawPage {
	p := SettingsRawPage{WorldID: "base", WorldName: longName, Text: "{\n  \"name\": \"x\"\n}\n"}
	if errors {
		p.Running = true
		p.Errors = rawProblems([]gamesettings.FieldError{
			{Line: 2, Key: "max_players", Code: gamesettings.CodeWrongType, Want: "integer"},
			{Line: 2, Code: gamesettings.CodeSyntax},
		}, p.Text)
	}
	return p
}

func settingsPreviewCase() SettingsPreviewPage {
	return SettingsPreviewPage{WorldID: "base", WorldName: longName, Token: "tok", Running: true, FileChanged: true,
		Settings: changeRows("2.1", gamesettings.Diff(
			[]byte(`{"name": "a", "game_password": "x"}`),
			[]byte(`{"name": "b", "mystery": [1, 2], "game_password": "y"}`))),
		Promote: []string{"alice"}, Demote: []string{"<b>bob</b>"}}
}

// page is a response read in full.
type page struct {
	Code int
	Body *bytes.Buffer
	hdr  http.Header
}

func (p page) Header() http.Header { return p.hdr }

// settingsRig serves the handler over a real connection, because a confirm
// lifts the server's deadlines.
type settingsRig struct {
	*liveServer
	cookie *http.Cookie
	id     string
	dir    string
}

// newSettingsRig makes a signed-in harness with one 2.1.24 world whose
// server-settings.json is settingsFile.
func newSettingsRig(t *testing.T) *settingsRig {
	t.Helper()
	ls := newLiveServer(t, nil)
	hn := ls.harness
	r := &settingsRig{liveServer: ls, cookie: hn.signIn()}
	id, err := hn.worlds.Create(context.Background(), "Base", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	r.id, r.dir = id, filepath.Join(hn.worlds.Dirs().Worlds, id)
	r.write(world.ServerSettingsFile, settingsFile)
	return r
}

func (r *settingsRig) write(file, text string) {
	r.t.Helper()
	dir := filepath.Join(r.dir, world.ConfigDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *settingsRig) read(file string) string {
	r.t.Helper()
	cfg, err := r.worlds.ReadConfig(r.id, file)
	if err != nil || !cfg.Found {
		r.t.Fatalf("read %s: found %v, err %v", file, cfg.Found, err)
	}
	return string(cfg.Data)
}

func (r *settingsRig) path(suffix string) string { return settingsPath(r.id) + suffix }

func (r *settingsRig) request(c *http.Cookie, method, path string, form url.Values) page {
	r.t.Helper()
	rep := r.do(method, path, form, http.Header{"Cookie": {c.String()}})
	rec := page{Code: rep.StatusCode, Body: bytes.NewBufferString(rep.body), hdr: rep.Header}
	r.noSecrets(rec)
	return rec
}

func (r *settingsRig) getAs(c *http.Cookie, path string) page {
	r.t.Helper()
	return r.request(c, http.MethodGet, path, nil)
}

func (r *settingsRig) postAs(c *http.Cookie, path string, form url.Values) page {
	r.t.Helper()
	return r.request(c, http.MethodPost, path, form)
}

// noSecrets fails the test if a response holds any secret of the world or
// any password the test typed (SPEC 11.4).
func (r *settingsRig) noSecrets(rec page) {
	r.t.Helper()
	for _, s := range settingsSecrets {
		if strings.Contains(rec.Body.String(), s) || strings.Contains(rec.Header().Get("Location"), s) {
			r.t.Fatalf("a response holds the secret %q:\n%s", s, rec.Body.String())
		}
	}
}

// settingsFormValues is the form as the page shows it for settingsFile.
func settingsFormValues() url.Values {
	return url.Values{
		"name": {"Family base"}, "description": {"Come play"}, "visibility": {"lan"},
		"max_players": {"0"}, "autosave_interval": {"10"}, "autosave_slots": {"5"},
		"require_user_verification": {"on"}, "auto_pause": {"on"}, "admins": {""},
	}
}

var reToken = regexp.MustCompile(`name="token" value="([^"]+)"`)

func tokenOf(t *testing.T, body string) string {
	t.Helper()
	m := reToken.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no confirmation token in:\n%s", body)
	}
	return m[1]
}

func (r *settingsRig) snapshots() []world.SnapshotUse {
	r.t.Helper()
	c, err := r.worlds.CountWorldSnapshots(r.id)
	if err != nil {
		r.t.Fatal(err)
	}
	return c.Snapshots()
}

func (r *settingsRig) auditActions() []string {
	r.t.Helper()
	list, err := r.store.ListAudit(context.Background(), 50)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		if e.Action == game.AuditSettingsSave {
			out = append(out, e.Detail+"|"+e.Username+"|"+e.WorldID)
		}
	}
	return out
}

// SPEC 11.1, 11.4: the form shows the file's values and whether a password
// is set, never the password or any other secret.
func TestSettingsFormShowsValuesNotSecrets(t *testing.T) {
	r := newSettingsRig(t)
	rec := r.getAs(r.cookie, r.path(""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`value="Family base"`, ">Come play</textarea>", `id="visibility-lan" name="visibility" value="lan" checked`,
		`value="10"`, `value="5"`, `name="auto_pause" checked`, `name="require_user_verification" checked`,
		"A password is set.", `name="game_password_remove"`, `type="password"`, `autocomplete="new-password"`,
		"Edit server-settings.json as JSON"} {
		if !strings.Contains(body, want) {
			t.Errorf("form lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `name="game_password" value`) {
		t.Error("the password field has a value")
	}

	// With no password the remove box is gone and the text says so.
	r.write(world.ServerSettingsFile, `{"name": "n", "game_password": ""}`)
	body = r.getAs(r.cookie, r.path("")).Body.String()
	if !strings.Contains(body, "No password is set. Anyone who can reach the server can join.") || strings.Contains(body, "game_password_remove") {
		t.Errorf("no-password form:\n%s", body)
	}
}

// SPEC 11.3, 11.4, 6.2: edit, preview with the diff, confirm; the snapshot is
// taken first and the secrets are never shown, only that they will be set.
func TestSettingsFormPreviewAndConfirm(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("name", "Renamed <b>")
	form.Set("max_players", "12")
	form.Set("visibility", "hidden")
	form.Set("game_password", "newpass-typed")
	form.Set("auto_pause", "")
	delete(form, "auto_pause")
	form.Set("admins", "Alice\nbob\n\n  alice  ")

	rec := r.postAs(r.cookie, r.path(""), form)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Save these changes to Base?", "Changed</strong> <code>name</code>", "Renamed &lt;b&gt;",
		"<del>0</del>", "<ins>12</ins>", "<code>visibility.lan</code>", "<code>auto_pause</code>",
		"<code>game_password</code>", "The value is not shown. It will be changed.",
		"Added admin Alice", "Added admin bob", "Save settings", "Go back and edit",
		"Take an automatic snapshot of Base", "Write <code>server-settings.json</code>", "Write <code>server-adminlist.json</code>"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "The server is running") {
		t.Error("a stopped world's preview has the restart note")
	}
	if strings.Contains(body, "<code>description</code>") || strings.Contains(body, "<code>autosave_slots</code>") {
		t.Error("an unchanged field is listed")
	}
	if got := r.read(world.ServerSettingsFile); got != settingsFile {
		t.Fatal("the preview wrote the file")
	}
	token := tokenOf(t, body)

	conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}})
	if conf.Code != http.StatusSeeOther {
		t.Fatalf("confirm status %d:\n%s\nlogs: %s", conf.Code, conf.Body.String(), r.logs.String())
	}
	to := conf.Header().Get("Location")
	if !strings.HasPrefix(to, r.path("")+"?saved=1&snap=") || strings.Contains(to, "restart") {
		t.Fatalf("redirect to %q", to)
	}
	got := r.read(world.ServerSettingsFile)
	for _, want := range []string{`"name": "Renamed <b>"`, `"max_players": 12`, `"visibility": {"public": false, "lan": false}`,
		`"game_password": "newpass-typed"`, `"username": "factorio-user"`, `"token": "tok-secret-123"`, `"_comment_x": "keep me"`} {
		if !strings.Contains(got, want) {
			t.Errorf("file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "auto_pause\": true") {
		t.Errorf("auto_pause is still true:\n%s", got)
	}
	if errs := gamesettings.Validate(gamesettings.FileServerSettings, gamesettings.Line21, []byte(got)); len(errs) != 0 {
		t.Errorf("the saved file is invalid: %+v", errs)
	}
	if got := r.read(world.ServerAdminListFile); !strings.Contains(got, `"Alice"`) || !strings.Contains(got, `"bob"`) || strings.Count(got, "lice") != 1 {
		t.Errorf("admin list %q", got)
	}

	snaps := r.snapshots()
	if len(snaps) != 1 || snaps[0].Reason != world.ReasonSettings || snaps[0].Label != world.LabelBeforeSettings {
		t.Fatalf("snapshots %+v", snaps)
	}
	if a := r.auditActions(); len(a) != 1 || a[0] != "server-settings.json, server-adminlist.json|alice|"+r.id {
		t.Errorf("audit %q", a)
	}

	// The Done message names the snapshot, linked, and shows no secret.
	done := r.getAs(r.cookie, to).Body.String()
	for _, want := range []string{"Done:", "Settings saved. Foreman took the snapshot &#34;Before settings change&#34; first.",
		snapshotsPath(r.id) + "#restore-"} {
		if !strings.Contains(done, want) {
			t.Errorf("done page lacks %q:\n%s", want, done)
		}
	}
	// One confirmation saves once.
	again := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}})
	if again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), "This preview has expired, or its changes were already saved.") {
		t.Errorf("second confirm: %d\n%s", again.Code, again.Body.String())
	}
	if len(r.snapshots()) != 1 {
		t.Error("the second confirm took a snapshot")
	}
}

// SPEC 11.4: a typed or kept password never comes back; going back to edit
// keeps the edits, not the password.
func TestSettingsPasswordIsWriteOnly(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("game_password", "newpass-typed")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())

	back := r.getAs(r.cookie, r.path("?edit="+token))
	if back.Code != http.StatusOK || !strings.Contains(back.Body.String(), `name="edit" value="`+token+`"`) {
		t.Fatalf("go back: %d\n%s", back.Code, back.Body.String())
	}
	// Submitting the refilled form without retyping keeps the typed password.
	form.Del("game_password")
	form.Set("edit", token)
	again := r.postAs(r.cookie, r.path(""), form)
	body := again.Body.String()
	if !strings.Contains(body, "<code>game_password</code>") || !strings.Contains(body, "It will be changed.") {
		t.Errorf("the carried password is not in the second preview:\n%s", body)
	}
	r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, body)}})
	if !strings.Contains(r.read(world.ServerSettingsFile), `"game_password": "newpass-typed"`) {
		t.Error("the carried password was not saved")
	}

	// Remove and a new password together is a field error.
	form = settingsFormValues()
	form.Set("game_password", "x")
	form.Set("game_password_remove", "on")
	rec := r.postAs(r.cookie, r.path(""), form)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Enter a new password or remove it, not both.") ||
		!strings.Contains(rec.Body.String(), "Fix this problem to continue") {
		t.Errorf("both: %d\n%s", rec.Code, rec.Body.String())
	}

	// Remove clears it and the diff says "removed", without a value.
	form = settingsFormValues()
	form.Set("game_password_remove", "on")
	body = r.postAs(r.cookie, r.path(""), form).Body.String()
	if !strings.Contains(body, "The value is not shown. It will be removed.") {
		t.Errorf("remove preview:\n%s", body)
	}
}

// SPEC 11.3: a form that changes nothing says so and offers no save.
func TestSettingsNothingChanged(t *testing.T) {
	r := newSettingsRig(t)
	rec := r.postAs(r.cookie, r.path(""), settingsFormValues())
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Nothing changed. The settings already hold these values.") ||
		strings.Contains(body, "Save settings") || strings.Contains(body, `name="token"`) {
		t.Errorf("%d\n%s", rec.Code, body)
	}
	if len(r.snapshots()) != 0 {
		t.Error("a snapshot was taken")
	}
}

// SPEC 11.1, DESIGN 7.2: wrong input shows every error at once with the
// text as typed.
func TestSettingsFormErrors(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("name", "  ")
	form.Set("max_players", "ten")
	form.Set("autosave_interval", "-1")
	form.Set("autosave_slots", "4294967296")
	form.Set("admins", "alice\nbad\u0001name")
	rec := r.postAs(r.cookie, r.path(""), form)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{"Fix 5 problems to continue", "Enter a server name.", "Enter a whole number from 0 to 4,294,967,295.",
		"Line 2 is not a usable player name.", `value="ten"`, `aria-invalid="true"`, `href="#max-players"`, `href="#admins"`,
		"<title>Error: Settings of Base"} {
		if !strings.Contains(body, want) {
			t.Errorf("errors lack %q:\n%s", want, body)
		}
	}
	if got := r.read(world.ServerSettingsFile); got != settingsFile {
		t.Error("the file changed")
	}
}

// SPEC 11.3: a token belongs to its user, its world and its 15 minutes.
func TestSettingsTokenOwnershipAndExpiry(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "9")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())

	r.addUser("bob", testPassword)
	bob := sessionCookie(r.post(PathSignIn, signInForm("bob", testPassword)))
	if bob == nil {
		t.Fatal("bob did not sign in")
	}
	rec := r.postAs(bob, r.path("/confirm"), url.Values{"token": {token}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "This preview has expired") {
		t.Errorf("another user's token: %d\n%s", rec.Code, rec.Body.String())
	}
	if r.getAs(bob, r.path("?edit="+token)).Code != http.StatusOK || strings.Contains(r.getAs(bob, r.path("?edit="+token)).Body.String(), `name="edit"`) {
		t.Error("another user's token refilled the form")
	}
	if r.read(world.ServerSettingsFile) != settingsFile {
		t.Fatal("another user's token saved a change")
	}

	// Bob taking the token did not use it up: it is still alice's.
	other, err := r.worlds.Create(context.Background(), "Other", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	rec = r.postAs(r.cookie, settingsPath(other)+"/confirm", url.Values{"token": {token}})
	if rec.Code != http.StatusConflict {
		t.Errorf("another world's confirm: %d", rec.Code)
	}

	r.clock.Advance(pendingTTL - time.Second)
	// One second before the end it still refills the form.
	if body := r.getAs(r.cookie, r.path("?edit="+token)).Body.String(); !strings.Contains(body, `name="edit"`) {
		t.Error("a token just inside its 15 minutes was refused")
	}
	r.clock.Advance(2 * time.Second)
	rec = r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "This preview has expired") {
		t.Errorf("an expired token: %d\n%s", rec.Code, rec.Body.String())
	}
	if r.read(world.ServerSettingsFile) != settingsFile || len(r.snapshots()) != 0 {
		t.Error("an expired token changed the world")
	}
}

// SPEC 11.3: a file that changed after the preview is not overwritten
// unseen: the browser gets the new diff with a warning, and no snapshot.
func TestSettingsFileChangedSincePreview(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "9")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())

	changed := strings.Replace(settingsFile, `"autosave_slots": 5`, `"autosave_slots": 7`, 1)
	r.write(world.ServerSettingsFile, changed)

	rec := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}})
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{"Warning:", "server-settings.json changed since you previewed. These are the changes against the file as it is now.",
		"<ins>9</ins>", "Save settings"} {
		if !strings.Contains(body, want) {
			t.Errorf("new preview lacks %q:\n%s", want, body)
		}
	}
	if r.read(world.ServerSettingsFile) != changed || len(r.snapshots()) != 0 {
		t.Fatal("the stale confirmation wrote or took a snapshot")
	}
	// The new preview's own token saves, against the file as it now is.
	newToken := tokenOf(t, body)
	if newToken == token {
		t.Error("the token was reused")
	}
	if rec := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {newToken}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("second confirm %d:\n%s", rec.Code, rec.Body.String())
	}
	got := r.read(world.ServerSettingsFile)
	if !strings.Contains(got, `"max_players": 9`) || !strings.Contains(got, `"autosave_slots": 7`) {
		t.Errorf("saved file:\n%s", got)
	}
}

// A change that the other edit already made leaves nothing to save.
func TestSettingsChangeAlreadyMade(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "9")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())
	r.write(world.ServerSettingsFile, strings.Replace(settingsFile, `"max_players": 0`, `"max_players": 9`, 1))
	body := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}}).Body.String()
	if !strings.Contains(body, "There is nothing left to save.") || strings.Contains(body, "Save settings") {
		t.Errorf("%s", body)
	}
}

// SPEC 11.2: the raw editor leaves secrets out, puts them back on save,
// and refuses a bad edit with the line and the reason.
func TestSettingsRawEditor(t *testing.T) {
	r := newSettingsRig(t)
	rec := r.getAs(r.cookie, r.path("/raw/server-settings.json"))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`<textarea id="content" name="content" class="code" rows="24" wrap="off" spellcheck="false"`, `&#34;name&#34;: &#34;Family base&#34;`,
		`&#34;_comment_x&#34;: &#34;keep me&#34;`, "are not shown here"} {
		if !strings.Contains(body, want) {
			t.Errorf("editor lacks %q:\n%s", want, body)
		}
	}
	for _, key := range []string{"game_password", "&#34;username&#34;", "&#34;token&#34;"} {
		if strings.Contains(body, key) {
			t.Errorf("editor shows the secret key %q", key)
		}
	}

	// A wrong type: every error at once, with line, reason and the line.
	bad := "{\n  \"name\": \"x\",\n  \"max_players\": \"ten\",\n  \"auto_pause\": 3,\n  \"description\": \"\",\n  \"visibility\": {\"public\": false, \"lan\": true}\n}\n"
	rec = r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {bad}})
	body = rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad edit status %d", rec.Code)
	}
	for _, want := range []string{"Fix 2 problems to continue", "Line 3: max_players must be a whole number. The line reads: &#34;max_players&#34;: &#34;ten&#34;,",
		"Line 4: auto_pause must be true or false.", `aria-invalid="true"`, `href="#content"`} {
		if !strings.Contains(body, want) {
			t.Errorf("errors lack %q:\n%s", want, body)
		}
	}
	// The text stays exactly as typed.
	if !strings.Contains(body, "&#34;max_players&#34;: &#34;ten&#34;,") {
		t.Error("the typed text was not kept")
	}

	// Broken JSON names the line.
	rec = r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {"{\n  \"name\": \"x\"\n  \"b\": 1\n}"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "this is not valid JSON") {
		t.Errorf("syntax: %d\n%s", rec.Code, rec.Body.String())
	}

	// A valid edit without the secret keys keeps them; unknown keys stay.
	edit := "{\n  \"name\": \"Raw name\",\n  \"description\": \"Raw description\",\n  \"visibility\": {\"public\": false, \"lan\": true},\n  \"max_players\": 3,\n  \"mystery\": [1, 2]\n}\n"
	rec = r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {edit}})
	body = rec.Body.String()
	for _, want := range []string{"Changed</strong> <code>name</code>", "<code>mystery</code>", "Foreman does not check this key. It is saved as written.",
		"Changed</strong> <code>description</code>"} {
		if !strings.Contains(body, want) {
			t.Errorf("raw preview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<code>game_password</code>") || strings.Contains(body, "<code>token</code>") {
		t.Error("the preview lists a secret that is only kept")
	}
	r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, body)}})
	got := r.read(world.ServerSettingsFile)
	for _, want := range []string{`"name": "Raw name"`, `"mystery": [1, 2]`, `"game_password": "hunter2-secret"`, `"token": "tok-secret-123"`, `"username": "factorio-user"`} {
		if !strings.Contains(got, want) {
			t.Errorf("saved file lacks %q:\n%s", want, got)
		}
	}

	// Setting the secret in the raw text changes it; the diff says only so.
	edit = "{\n  \"name\": \"Raw name\",\n  \"description\": \"Come play\",\n  \"visibility\": {\"public\": false, \"lan\": true},\n  \"game_password\": \"newpass-typed\"\n}\n"
	body = r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {edit}}).Body.String()
	if !strings.Contains(body, "<code>game_password</code>") || !strings.Contains(body, "It will be changed.") {
		t.Errorf("secret change preview:\n%s", body)
	}

	// Only server-settings.json has an editor.
	if rec := r.getAs(r.cookie, r.path("/raw/map-settings.json")); rec.Code != http.StatusNotFound {
		t.Errorf("map-settings editor: %d", rec.Code)
	}
	if rec := r.postAs(r.cookie, r.path("/raw/server-adminlist.json"), url.Values{"content": {"[]"}}); rec.Code != http.StatusNotFound {
		t.Errorf("adminlist editor: %d", rec.Code)
	}
}

// The raw editor takes the 1 MB the spec allows and refuses more.
func TestSettingsRawEditorSize(t *testing.T) {
	r := newSettingsRig(t)
	pad := strings.Repeat("x", 1<<19)
	big := `{"name": "n", "visibility": {"public": false, "lan": false}, "description": "` + pad + `"}`
	rec := r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {big}})
	if rec.Code != http.StatusOK {
		t.Fatalf("half a megabyte: %d", rec.Code)
	}
	huge := `{"name": "n", "description": "` + strings.Repeat("x", 1<<20) + `"}`
	rec = r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {huge}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "The text is over 1.0 MB") && !strings.Contains(rec.Body.String(), "The text is over") {
		t.Errorf("over a megabyte: %d", rec.Code)
	}
}

// A file that is not JSON is not shown (it could hold a password), and
// saving the editor replaces it.
func TestSettingsRawEditorUnreadableFile(t *testing.T) {
	r := newSettingsRig(t)
	r.write(world.ServerSettingsFile, `{"game_password": "hunter2-secret",`)
	rec := r.getAs(r.cookie, r.path("/raw/server-settings.json"))
	body := rec.Body.String()
	if !strings.Contains(body, "so its text is not shown") || !strings.Contains(body, "></textarea>") {
		t.Errorf("unreadable file:\n%s", body)
	}
	form := r.getAs(r.cookie, r.path("")).Body.String()
	if !strings.Contains(form, "cannot read server-settings.json as a JSON object") || strings.Contains(form, "Preview changes") {
		t.Errorf("unreadable form:\n%s", form)
	}
}

// SPEC 11.1, 14.9: admins come from server-adminlist.json and are written
// there while the game is stopped.
func TestSettingsAdmins(t *testing.T) {
	r := newSettingsRig(t)
	r.write(world.ServerAdminListFile, `["Alice", "bob"]`)
	body := r.getAs(r.cookie, r.path("")).Body.String()
	if !strings.Contains(body, ">Alice\nbob</textarea>") {
		t.Errorf("admins not shown:\n%s", body)
	}
	form := settingsFormValues()
	form.Set("admins", "bob\ncarol\"quote")
	body = r.postAs(r.cookie, r.path(""), form).Body.String()
	for _, want := range []string{"Added admin carol&#34;quote", "Removed admin Alice"} {
		if !strings.Contains(body, want) {
			t.Errorf("admin preview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "server-settings.json</code>") {
		t.Error("an admin-only change writes server-settings.json")
	}
	r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, body)}})
	names, err := gamesettings.ParseAdminList([]byte(r.read(world.ServerAdminListFile)))
	if err != nil || len(names) != 2 || names[0] != "bob" || names[1] != `carol"quote` {
		t.Errorf("admin list %q %v", names, err)
	}

	// A list that is not names is left alone.
	r.write(world.ServerAdminListFile, `{"not": "a list"}`)
	body = r.getAs(r.cookie, r.path("")).Body.String()
	if !strings.Contains(body, "server-adminlist.json is not a list of player names") || strings.Contains(body, `name="admins"`) {
		t.Errorf("broken admin list:\n%s", body)
	}
}

// SPEC 5.1, 11.3: a world a newer Foreman wrote cannot be changed, and
// says why.
func TestSettingsNewerWorldIsReadOnly(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "9")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())

	// Enough of world.json for a newer Foreman's world.
	if err := os.WriteFile(filepath.Join(r.dir, "world.json"), []byte(`{"format_version": 99, "name": "Base", "game_version": "2.1.24"}`), 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	body := r.getAs(r.cookie, r.path("")).Body.String()
	if !strings.Contains(body, "A newer version of Foreman wrote this world, so this version cannot change it.") ||
		strings.Contains(body, "Preview changes") || !strings.Contains(body, "<fieldset disabled>") {
		t.Errorf("read-only form:\n%s", body)
	}
	raw := r.getAs(r.cookie, r.path("/raw/server-settings.json")).Body.String()
	if !strings.Contains(raw, "readonly") || strings.Contains(raw, "Preview changes") {
		t.Errorf("read-only editor:\n%s", raw)
	}
	for _, rec := range []page{
		r.postAs(r.cookie, r.path(""), form),
		r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {"{}"}}),
		r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}}),
	} {
		if rec.Code != http.StatusConflict {
			t.Errorf("a write to a read-only world: %d\n%s", rec.Code, rec.Body.String())
		}
	}
	if r.read(world.ServerSettingsFile) != settingsFile {
		t.Error("a newer world's settings changed")
	}
}

func TestSettingsNeedsSignInAndAWorld(t *testing.T) {
	r := newSettingsRig(t)
	for _, p := range []string{r.path(""), r.path("/raw/server-settings.json")} {
		if rec := r.get(p); rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s signed out: %d", p, rec.Code)
		}
	}
	for _, p := range []string{r.path(""), r.path("/raw/server-settings.json"), r.path("/confirm")} {
		if rec := r.post(p, url.Values{}); rec.Code != http.StatusSeeOther {
			t.Errorf("POST %s signed out: %d", p, rec.Code)
		}
	}
	for _, p := range []string{settingsPath("nope"), settingsRawPath("nope", "server-settings.json")} {
		if rec := r.getAs(r.cookie, p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	if rec := r.postAs(r.cookie, settingsConfirmPath("nope"), url.Values{"token": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("confirm on no world: %d", rec.Code)
	}
}

// A page made from a malicious crafted link cannot claim a save.
func TestSettingsDoneNeedsASettingsSnapshot(t *testing.T) {
	r := newSettingsRig(t)
	for _, q := range []string{"?saved=1&snap=20260101T000000.000Z", "?saved=1&snap=../x", "?saved=0&snap=x", "?snap=x"} {
		if body := r.getAs(r.cookie, r.path(q)).Body.String(); strings.Contains(body, "Settings saved") {
			t.Errorf("%s: a crafted link made a Done message", q)
		}
	}
	body := r.getAs(r.cookie, r.path("?saved=1")).Body.String()
	if !strings.Contains(body, "Settings saved.") {
		t.Errorf("no-snapshot Done missing:\n%s", body)
	}
}

// writeConfig puts a settings file into a world made by the game rig.
func (g *gameRig) writeConfig(id, file, text string) {
	g.t.Helper()
	dir := filepath.Join(g.worlds.Dirs().Worlds, id, world.ConfigDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o600); err != nil { //nolint:gosec // a test temp folder
		g.t.Fatal(err)
	}
}

func (g *gameRig) settingsSnapshots(id string) int {
	g.t.Helper()
	c, err := g.worlds.CountWorldSnapshots(id)
	if err != nil {
		g.t.Fatal(err)
	}
	n := 0
	for _, u := range c.Snapshots() {
		if u.Reason == world.ReasonSettings {
			n++
		}
	}
	return n
}

func settingsSavedAudits(g *gameRig) int {
	n := 0
	for _, e := range g.audit() {
		if e.Action == game.AuditSettingsSave {
			n++
		}
	}
	return n
}

// SPEC 11.3, 6.10, 11.1: a running world's change says it needs a restart,
// in the preview and after saving, with a Restart link. The game layer saves
// the game, takes the snapshot and sends the admin change over RCON.
func TestSettingsRunningWorld(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	path := settingsPath(id)
	g.writeConfig(id, world.ServerSettingsFile, `{"name": "Old"}`)

	if body := g.get(path); !strings.Contains(body, "The server is running. Changes to these settings take effect after a restart.") {
		t.Errorf("form lacks the running note:\n%s", body)
	}
	rep := g.post(path, url.Values{"name": {"Live"}, "description": {""}, "max_players": {""}, "autosave_interval": {""}, "autosave_slots": {""}, "admins": {"carol"}})
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("preview %d:\n%s", rep.StatusCode, rep.body)
	}
	for _, want := range []string{"Note:", "The server is running. These changes take effect after a restart.",
		"Changes to admins apply at once.", "Save the running game.", "Promote and demote these players in the running game"} {
		if !strings.Contains(rep.body, want) {
			t.Errorf("running preview lacks %q:\n%s", want, rep.body)
		}
	}
	conf := g.post(path+"/confirm", url.Values{"token": {tokenOf(t, rep.body)}})
	loc := conf.Header.Get("Location")
	if conf.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "&snap=") || !strings.Contains(loc, "&restart=1&live=1") {
		t.Fatalf("confirm %d to %q\n%s", conf.StatusCode, loc, conf.body)
	}
	if g.settingsSnapshots(id) != 1 {
		t.Errorf("want one snapshot for the whole change, got %d", g.settingsSnapshots(id))
	}
	if cmds := g.commands(id); !slices.Contains(cmds, "/server-save") || !slices.Contains(cmds, "/promote carol") {
		t.Errorf("RCON commands %q: want a save and a promote", cmds)
	}
	if cfg, err := g.worlds.ReadConfig(id, world.ServerSettingsFile); err != nil || !strings.Contains(string(cfg.Data), `"name": "Live"`) {
		t.Errorf("server-settings.json %q %v", cfg.Data, err)
	}
	if settingsSavedAudits(g) != 1 {
		t.Errorf("audit entries: %d, want one for the whole change", settingsSavedAudits(g))
	}
	// The game writes the admin list, not Foreman.
	if cfg, err := g.worlds.ReadConfig(id, world.ServerAdminListFile); err != nil || cfg.Found {
		t.Errorf("Foreman wrote the admin list of a running game: %v %v", cfg.Found, err)
	}
	// The page the redirect names, built from the known path rather than the header.
	done := g.get(path + "?saved=1&restart=1&live=1")
	for _, want := range []string{"Done:", "Settings saved.", "take effect after a restart", `href="/worlds/` + id + `/restart"`, "Restart server"} {
		if !strings.Contains(done, want) {
			t.Errorf("done page lacks %q:\n%s", want, done)
		}
	}
}

// A name with a space is refused before the preview, whether the game runs
// or not (SPEC 14.9).
func TestSettingsAdminNameWithSpaceIsRefused(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	g2 := newGameRig(t, image21)
	id2 := g2.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for name, rep := range map[string]reply{
		"running": g.post(settingsPath(id), url.Values{"name": {"Live"}, "admins": {"Mary Ann"}}),
		"stopped": g2.post(settingsPath(id2), url.Values{"name": {"Live"}, "admins": {"Mary Ann"}}),
	} {
		if rep.StatusCode != http.StatusBadRequest || !strings.Contains(rep.body, "Line 1 is not a usable player name.") {
			t.Errorf("%s: %d\n%s", name, rep.StatusCode, rep.body)
		}
	}
}

// Two saves to one world each name their own snapshot in the Done link, even
// when they come one right after the other.
func TestSettingsTwoSavesEachLinkTheirOwnSnapshot(t *testing.T) {
	r := newSettingsRig(t)
	save := func(maxPlayers string) string {
		form := settingsFormValues()
		form.Set("max_players", maxPlayers)
		token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())
		r.clock.Advance(time.Minute)
		conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}})
		if conf.Code != http.StatusSeeOther {
			t.Fatalf("confirm %d:\n%s", conf.Code, conf.Body.String())
		}
		return conf.Header().Get("Location")
	}
	first, second := save("5"), save("6")
	snap := func(loc string) string {
		u, err := url.Parse(loc)
		if err != nil {
			t.Fatal(err)
		}
		return u.Query().Get("snap")
	}
	if snap(first) == "" || snap(second) == "" || snap(first) == snap(second) {
		t.Errorf("links %q and %q", first, second)
	}
	if n := len(r.snapshots()); n != 2 {
		t.Errorf("%d snapshots, want 2", n)
	}
	for _, loc := range []string{first, second} {
		if !strings.Contains(r.getAs(r.cookie, loc).Body.String(), "#restore-"+snap(loc)) {
			t.Errorf("the Done page of %q does not link its own snapshot", loc)
		}
	}
}

// With a game controller and a stopped game the controller saves: snapshot,
// audit entry and write, and the Done link names the snapshot.
func TestSettingsStoppedWorldSavesThroughController(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.writeConfig(id, world.ServerSettingsFile, `{"name": "Old", "game_password": "hunter2-secret"}`)
	rep := g.post(settingsPath(id), url.Values{"name": {"New"}, "max_players": {"4"}})
	conf := g.post(settingsPath(id)+"/confirm", url.Values{"token": {tokenOf(t, rep.body)}})
	loc := conf.Header.Get("Location")
	if conf.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "&snap=") || strings.Contains(loc, "restart") {
		t.Fatalf("confirm %d to %q\n%s", conf.StatusCode, loc, conf.body)
	}
	cfg, err := g.worlds.ReadConfig(id, world.ServerSettingsFile)
	if err != nil || !strings.Contains(string(cfg.Data), `"name": "New"`) || !strings.Contains(string(cfg.Data), `"max_players": 4`) ||
		!strings.Contains(string(cfg.Data), "hunter2-secret") {
		t.Errorf("file %q %v", cfg.Data, err)
	}
	if g.settingsSnapshots(id) != 1 || settingsSavedAudits(g) != 1 {
		t.Errorf("snapshots %d, audits %d", g.settingsSnapshots(id), settingsSavedAudits(g))
	}
}

// A file the game layer refuses is not written, and the page says what to do.
func TestSettingsInvalidFileIsRefused(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.writeConfig(id, world.ServerSettingsFile, `{"name": "Old", "max_players": "ten"}`)
	rep := g.post(settingsPath(id), url.Values{"name": {"New"}})
	conf := g.post(settingsPath(id)+"/confirm", url.Values{"token": {tokenOf(t, rep.body)}})
	if conf.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(conf.body, "Foreman checked the new settings again and refused them") ||
		!strings.Contains(conf.body, "Edit server-settings.json as JSON") {
		t.Errorf("confirm %d\n%s", conf.StatusCode, conf.body)
	}
	if cfg, err := g.worlds.ReadConfig(id, world.ServerSettingsFile); err != nil || strings.Contains(string(cfg.Data), "New") {
		t.Errorf("an invalid file was written: %q %v", cfg.Data, err)
	}
}

// The Done message shows the snapshot's label in the page's language
// (SPEC 22.3), as the snapshot list does.
func TestSettingsDoneShowsTranslatedLabel(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "9")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())
	if conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}}); conf.Code != http.StatusSeeOther {
		t.Fatalf("confirm %d", conf.Code)
	}
	snap := r.snapshots()[0].Name
	rep := r.do(http.MethodGet, r.path("?saved=1&snap="+snap), nil, http.Header{"Cookie": {r.cookie.String()}, "Accept-Language": {"de"}})
	if !strings.Contains(rep.body, "Vor der Einstellungsänderung") || strings.Contains(rep.body, "Before settings change") {
		t.Errorf("German Done message:\n%s", rep.body)
	}
}

func TestPendingStoreLimits(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	p := newPendingStore(clock.Now)
	first := p.add(pendingChange{UserID: 1, WorldID: "w"})
	for i := 0; i < maxPendingPerUser; i++ {
		clock.Advance(time.Second)
		p.add(pendingChange{UserID: 1, WorldID: "w"})
	}
	if _, ok := p.get(first, 1, "w"); ok {
		t.Error("the oldest change was kept past the per-user limit")
	}
	other := p.add(pendingChange{UserID: 2, WorldID: "w"})
	if _, ok := p.get(other, 1, "w"); ok {
		t.Error("another user's token was found")
	}
	if _, ok := p.get(other, 2, "x"); ok {
		t.Error("another world's token was found")
	}
	if _, ok := p.take(other, 2, "w"); !ok {
		t.Error("the owner could not take it")
	}
	if _, ok := p.take(other, 2, "w"); ok {
		t.Error("a change was taken twice")
	}
	if a, b := newToken(), newToken(); a == b || len(a) < 20 {
		t.Errorf("tokens %q %q", a, b)
	}
}
