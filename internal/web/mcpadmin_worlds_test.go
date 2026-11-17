package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// worldsRig is a harness with favorites, served live, alice signed in, MCP
// on and an admin token named Claude connected.
type worldsRig struct {
	*harness
	favs   *world.Favorites
	cookie *http.Cookie
	cs     *mcp.ClientSession
}

func newWorldsRig(t *testing.T) *worldsRig {
	t.Helper()
	hn, favs := newFavoritesHarness(t)
	ls := newLiveServerFor(t, hn)
	c := hn.signIn()
	u, _, err := hn.store.UserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	turnMCPOn(t, hn)
	secret, _ := apiToken(t, hn, u, "Claude", auth.ScopeAdmin)
	return &worldsRig{harness: hn, favs: favs, cookie: c, cs: mcpConnect(t, ls.url, secret)}
}

func (r *worldsRig) audits() []store.AuditEntry {
	r.t.Helper()
	list, err := r.store.ListAudit(context.Background(), 100)
	if err != nil {
		r.t.Fatal(err)
	}
	return list
}

func (r *worldsRig) auditOf(action string) []store.AuditEntry {
	var out []store.AuditEntry
	for _, e := range r.audits() {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// dataDigest sums every file under the data folder: path, mode and bytes.
func (r *worldsRig) dataDigest() string {
	r.t.Helper()
	root := filepath.Dir(r.worlds.Dirs().Worlds)
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		_, _ = fmt.Fprintf(h, "%s %v\n", rel, d.Type())
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p) //nolint:gosec // a test temp folder
			if err != nil {
				return err
			}
			h.Write(b)
		}
		return nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

var tagRE = regexp.MustCompile(`<[^>]*>`)

// pageText renders a page's content template in English as plain text.
func pageText(t *testing.T, name string, p Page) string {
	t.Helper()
	var b bytes.Buffer
	if err := newTestRenderer(t).pages[i18n.English][name].ExecuteTemplate(&b, "content", view{Page: p}); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return strings.Join(strings.Fields(html.UnescapeString(tagRE.ReplaceAllString(b.String(), ""))), " ")
}

// wantParity checks each will sentence appears on the UI confirm page.
func wantParity(t *testing.T, page string, will []string) {
	t.Helper()
	for _, w := range will {
		if !strings.Contains(page, strings.Join(strings.Fields(w), " ")) {
			t.Errorf("the confirm page lacks %q:\n%s", w, page)
		}
	}
}

func strs(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// SPEC 5.10, 21.8, 12.9: delete_world's first call changes nothing and
// shows the Delete page's sentences; the second deletes after the final
// snapshot, audited as world-delete with the token's name before the delete.
func TestMCPDeleteWorld(t *testing.T) {
	r := newWorldsRig(t)
	_, id := r.seedTwo()
	args := map[string]any{"world_id": id}

	before, audits := r.dataDigest(), len(r.audits())
	res, preview := call(t, r.cs, "delete_world", args)
	if res.IsError || preview["done"] != false || preview["snapshot"] != "Before delete" || preview[confirmCodeKey] == "" {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(preview))
	}
	if r.dataDigest() != before || len(r.audits()) != audits {
		t.Fatal("the preview changed the data folder or the audit log")
	}
	sum, err := r.worlds.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	wantParity(t, pageText(t, "delete", DeletePage{World: sum, FinalSnapshot: true}), strs(preview["will"]))

	res, out := call(t, r.cs, "delete_world", withCode(args, preview[confirmCodeKey].(string)))
	if res.IsError || out["done"] != true || out["finished"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if _, err := os.Stat(filepath.Join(r.worlds.Dirs().Worlds, id)); !os.IsNotExist(err) {
		t.Errorf("the world folder remains: %v", err)
	}
	snaps, _ := r.worlds.Snapshots(id)
	if len(snaps) != 1 || snaps[0].Label != "Before delete" {
		t.Errorf("final snapshot: %+v", snaps)
	}
	if got := r.auditOf(AuditWorldDelete); len(got) != 1 || got[0].TokenName != "Claude" || got[0].Username != "alice" ||
		got[0].Detail != deleteDetail("Second", true) || got[0].WorldID != id {
		t.Errorf("audit: %+v", got)
	}

	_, del := call(t, r.cs, "list_deleted_worlds", nil)
	if !strings.Contains(asJSON(del), `"id":"`+id+`"`) || !strings.Contains(asJSON(del), `"untrusted_label":"Before delete"`) {
		t.Errorf("list_deleted_worlds: %s", asJSON(del))
	}
}

// SPEC 5.10: without a final snapshot none is taken, and the preview says so.
func TestMCPDeleteWorldNoFinalSnapshot(t *testing.T) {
	r := newWorldsRig(t)
	_, id := r.seedTwo()
	preview, res, out := confirmCall(t, r.cs, "delete_world", map[string]any{"world_id": id, "final_snapshot": false})
	if preview["snapshot"] != nil || !slices.Contains(strs(preview["will"]), "No final snapshot will be taken.") {
		t.Errorf("preview: %s", asJSON(preview))
	}
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s", resultText(res))
	}
	if snaps, _ := r.worlds.Snapshots(id); len(snaps) != 0 {
		t.Errorf("a snapshot was taken: %+v", snaps)
	}
	if got := r.auditOf(AuditWorldDelete); len(got) != 1 || got[0].Detail != deleteDetail("Second", false) {
		t.Errorf("audit: %+v", got)
	}
}

// SPEC 5.10, 21.8: delete_world is refused as the page refuses: the active
// world, a missing world_id; and a world renamed since the preview is
// refused with a fresh preview.
func TestMCPDeleteWorldRefusals(t *testing.T) {
	r := newWorldsRig(t)
	active, id := r.seedTwo()
	if res, _ := call(t, r.cs, "delete_world", map[string]any{"world_id": active}); !res.IsError || !strings.Contains(resultText(res), "active world") {
		t.Errorf("active: %s", resultText(res))
	}
	if res, _ := call(t, r.cs, "delete_world", map[string]any{}); !res.IsError {
		t.Error("a delete without world_id was accepted")
	}

	args := map[string]any{"world_id": id}
	code := askCode(t, r.cs, "delete_world", args)
	if err := r.worlds.Rename(context.Background(), id, "Renamed"); err != nil {
		t.Fatal(err)
	}
	before := r.dataDigest()
	res, out := call(t, r.cs, "delete_world", withCode(args, code))
	fresh, _ := out[confirmCodeKey].(string)
	if !res.IsError || out["done"] != false || fresh == "" || fresh == code || !strings.Contains(resultText(res), "changed since the preview") {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if r.dataDigest() != before || len(r.auditOf(AuditWorldDelete)) != 0 {
		t.Error("a moved world was acted on")
	}
}

// breakAuditLog drops the audit table from the test's database, through a
// second connection, so every audit write fails.
func breakAuditLog(t *testing.T) {
	t.Helper()
	found, _ := filepath.Glob(filepath.Join(filepath.Dir(t.TempDir()), "*", "foreman.db"))
	if len(found) != 1 {
		t.Fatalf("databases: %v", found)
	}
	db, err := sql.Open("sqlite", found[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), "DROP TABLE audit_log"); err != nil {
		t.Fatal(err)
	}
}

// SPEC 12.9: world-delete is written before the delete; when it cannot be
// written, the delete is refused in the UI and through MCP.
func TestDeleteRefusedWithoutAuditRecord(t *testing.T) {
	r := newWorldsRig(t)
	_, id := r.seedTwo()

	rec := r.post(deletePath(id), url.Values{"confirm_name": {"Second"}}, withCookie(r.cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("UI delete: %d\n%s", rec.Code, rec.Body)
	}
	if got := r.auditOf(AuditWorldDelete); len(got) != 1 || got[0].TokenName != "" || got[0].Username != "alice" || got[0].Detail != deleteDetail("Second", false) {
		t.Errorf("UI audit: %+v", got)
	}

	other, err := r.worlds.Create(context.Background(), "Third", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"world_id": other}
	code := askCode(t, r.cs, "delete_world", args)
	breakAuditLog(t)
	rec = r.post(deletePath(other), url.Values{"confirm_name": {"Third"}, "final_snapshot": {"on"}}, withCookie(r.cookie))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("UI delete without the audit log: %d", rec.Code)
	}
	if res, out := call(t, r.cs, "delete_world", withCode(args, code)); !res.IsError || out["done"] == true {
		t.Errorf("delete_world went ahead without its audit record: %s %s", resultText(res), asJSON(out))
	}
	if _, err := os.Stat(filepath.Join(r.worlds.Dirs().Worlds, other)); err != nil {
		t.Errorf("the world was deleted without its audit record: %v", err)
	}
	if snaps, _ := r.worlds.Snapshots(other); len(snaps) != 0 {
		t.Errorf("a snapshot was taken: %+v", snaps)
	}
}

// SPEC 5.6, 12.9: generate_world makes a world as Generate does, audited
// with the token's name, and is refused for the form's reasons.
func TestMCPGenerateWorld(t *testing.T) {
	r := newWorldsRig(t)
	res, out := call(t, r.cs, "generate_world", map[string]any{"name": "Fresh", "game_version": "2.0.77", "preset": "rich-resources", "seed": "1234"})
	if res.IsError {
		t.Fatalf("generate: %s", resultText(res))
	}
	w, _ := out["world"].(map[string]any)
	id, _ := w["id"].(string)
	sum, err := r.worlds.Get(context.Background(), id)
	if err != nil || sum.Name != "Fresh" || sum.GameVersion != "2.0.77" || !sum.SpaceAge {
		t.Fatalf("world: %+v %v", sum, err)
	}
	if got := r.auditOf(AuditGenerate); len(got) != 1 || got[0].TokenName != "Claude" || !strings.Contains(got[0].Detail, "preset rich-resources") {
		t.Errorf("audit: %+v", got)
	}

	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "", "game_version": "2.0.77"}, "name"},
		{map[string]any{"name": "Fresh", "game_version": "2.0.77"}, "already"},
		{map[string]any{"name": "X", "game_version": "2.0.77", "preset": "nope"}, "preset"},
		{map[string]any{"name": "X", "game_version": "2.0.77", "map_exchange_string": "garbage"}, "exchange string"},
		{map[string]any{"name": "X", "game_version": "1.1.110", "space_age": true}, "Space Age"},
	} {
		res, _ := call(t, r.cs, "generate_world", c.args)
		if !res.IsError || !strings.Contains(resultText(res), c.want) {
			t.Errorf("%v: %s", c.args, resultText(res))
		}
	}
}

// SPEC 5.12: generate_world from a favorite takes its version and map
// files, names its mods for change_mods, and saves them as picks.
func TestMCPGenerateFromFavorite(t *testing.T) {
	r := newWorldsRig(t)
	src := favSource(t, r.harness)
	favID, err := r.favs.Make(context.Background(), src, "Club")
	if err != nil {
		t.Fatal(err)
	}
	res, out := call(t, r.cs, "generate_world", map[string]any{"name": "From fav", "favorite_id": favID})
	if res.IsError {
		t.Fatalf("generate: %s", resultText(res))
	}
	if len(strs(out["untrusted_favorite_mods"])) == 0 {
		t.Errorf("favorite mods: %s", asJSON(out))
	}
	id := out["world"].(map[string]any)["id"].(string)
	ms, err := os.ReadFile(filepath.Join(r.worlds.Dirs().Worlds, id, world.ConfigDir, world.MapSettingsFile)) //nolint:gosec // a test temp folder
	if err != nil || !regexp.MustCompile(`"pollution": \{[^}]*"enabled": false`).Match(ms) {
		t.Errorf("map settings: %s %v", ms, err)
	}
	if got := r.auditOf(AuditGenerate); len(got) != 1 || !strings.Contains(got[0].Detail, "favorite Club") {
		t.Errorf("audit: %+v", got)
	}
	if res, _ := call(t, r.cs, "generate_world", map[string]any{"name": "Y", "favorite_id": "nope"}); !res.IsError {
		t.Error("a missing favorite was accepted")
	}
}

// SPEC 5.7, 12.9: clone_world copies a world as Clone does, audited with
// the token's name, and is refused for the form's reasons.
func TestMCPCloneWorld(t *testing.T) {
	r := newWorldsRig(t)
	_, id := r.seedTwo()
	res, out := call(t, r.cs, "clone_world", map[string]any{"world_id": id, "name": "Copy", "map": "keep"})
	if res.IsError || out["finished"] != true {
		t.Fatalf("clone: %s %s", resultText(res), asJSON(out))
	}
	nid := out["world"].(map[string]any)["id"].(string)
	if sum, err := r.worlds.Get(context.Background(), nid); err != nil || sum.Name != "Copy" {
		t.Errorf("clone: %+v %v", sum, err)
	}
	if got := r.auditOf(AuditClone); len(got) != 1 || got[0].TokenName != "Claude" || got[0].Detail != "from "+id+", keep" {
		t.Errorf("audit: %+v", got)
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"world_id": id, "name": " ", "map": "keep"}, "name"},
		{map[string]any{"world_id": id, "name": "Other", "map": "maybe"}, "Choose"},
		{map[string]any{"world_id": id, "name": "Copy", "map": "fresh"}, "already"},
		{map[string]any{"world_id": "nope", "name": "Z", "map": "fresh"}, "no"},
	} {
		if res, _ := call(t, r.cs, "clone_world", c.args); !res.IsError || !strings.Contains(resultText(res), c.want) {
			t.Errorf("%v: %s", c.args, resultText(res))
		}
	}
}

// SPEC 5.8, 5.11: rename_world, list_saves and choose_save do what their
// pages do.
func TestMCPRenameAndSaves(t *testing.T) {
	r := newWorldsRig(t)
	_, id := r.seedTwo()
	if res, out := call(t, r.cs, "rename_world", map[string]any{"world_id": id, "name": "Renamed"}); res.IsError || out["name"] != "Renamed" {
		t.Errorf("rename: %s %s", resultText(res), asJSON(out))
	}
	if res, _ := call(t, r.cs, "rename_world", map[string]any{"world_id": id, "name": " "}); !res.IsError {
		t.Error("an empty name was accepted")
	}
	p := filepath.Join(r.worlds.Dirs().Worlds, id, "saves", "older.zip")
	if err := os.WriteFile(p, []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := r.clock.Now().Add(-48 * 3600 * 1e9)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	res, out := call(t, r.cs, "list_saves", map[string]any{"world_id": id})
	if res.IsError || !strings.Contains(asJSON(out), `"name":"older.zip"`) {
		t.Fatalf("list_saves: %s %s", resultText(res), asJSON(out))
	}
	if res, out := call(t, r.cs, "choose_save", map[string]any{"world_id": id, "save": "older.zip"}); res.IsError || out["chosen"] != "older.zip" {
		t.Errorf("choose_save: %s %s", resultText(res), asJSON(out))
	}
	if r.worlds.LoadChoice(id) != "older.zip" {
		t.Errorf("load choice: %q", r.worlds.LoadChoice(id))
	}
	if res, _ := call(t, r.cs, "choose_save", map[string]any{"world_id": id, "save": "gone.zip"}); !res.IsError || !strings.Contains(resultText(res), "no save") {
		t.Errorf("a missing save: %s", resultText(res))
	}
	if res, out := call(t, r.cs, "choose_save", map[string]any{"world_id": id, "save": ""}); res.IsError || out["chosen"] != nil {
		t.Errorf("newest: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 5.12, 21.8, 14.8: the favorites tools work as the pages do, never
// return a favorite's secrets, and delete_favorite takes two steps.
func TestMCPFavorites(t *testing.T) {
	r := newWorldsRig(t)
	src := favSource(t, r.harness)
	res, made := call(t, r.cs, "make_favorite", map[string]any{"world_id": src, "name": "Club"})
	if res.IsError {
		t.Fatalf("make: %s", resultText(res))
	}
	favID, _ := made["id"].(string)
	if res, _ := call(t, r.cs, "make_favorite", map[string]any{"world_id": src, "name": "Club"}); !res.IsError || !strings.Contains(resultText(res), "already") {
		t.Errorf("a taken name: %s", resultText(res))
	}
	if res, out := call(t, r.cs, "rename_favorite", map[string]any{"favorite_id": favID, "name": "Club two"}); res.IsError || out["untrusted_name"] != "Club two" {
		t.Errorf("rename: %s %s", resultText(res), asJSON(out))
	}
	res, list := call(t, r.cs, "list_favorites", nil)
	if res.IsError || !strings.Contains(asJSON(list), `"untrusted_name":"Club two"`) || !strings.Contains(asJSON(list), "untrusted_mods") {
		t.Errorf("list: %s %s", resultText(res), asJSON(list))
	}
	for _, secret := range []string{"hunter2-game", "tok-abc123"} {
		if strings.Contains(asJSON(list)+resultText(res), secret) {
			t.Errorf("list_favorites returned a secret: %s", secret)
		}
	}

	args := map[string]any{"favorite_id": favID}
	before, audits := r.dataDigest(), len(r.audits())
	res, preview := call(t, r.cs, "delete_favorite", args)
	if res.IsError || preview["done"] != false || preview["snapshot"] != nil {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(preview))
	}
	if r.dataDigest() != before || len(r.audits()) != audits {
		t.Fatal("the preview changed the data folder or the audit log")
	}
	fav, _, err := r.favs.Get(favID)
	if err != nil {
		t.Fatal(err)
	}
	wantParity(t, pageText(t, "favoritedelete", FavoriteDeletePage{Favorite: FavoriteRow{fav}}), strs(preview["will"]))

	// A rename since the preview moves the seen state.
	code := preview[confirmCodeKey].(string)
	if err := r.favs.Rename(context.Background(), favID, "Club three"); err != nil {
		t.Fatal(err)
	}
	res, out := call(t, r.cs, "delete_favorite", withCode(args, code))
	if fresh, _ := out[confirmCodeKey].(string); !res.IsError || fresh == "" || fresh == code {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if _, _, err := r.favs.Get(favID); err != nil {
		t.Fatal("a moved favorite was deleted")
	}
	res, out = call(t, r.cs, "delete_favorite", withCode(args, out[confirmCodeKey].(string)))
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if _, _, err := r.favs.Get(favID); err == nil {
		t.Error("the favorite remains")
	}
	if res, _ := call(t, r.cs, "rename_favorite", map[string]any{"favorite_id": favID, "name": "X"}); !res.IsError || !strings.Contains(resultText(res), "no such favorite") {
		t.Errorf("a gone favorite: %s", resultText(res))
	}
}

// SPEC 21.8: exactly delete_world and delete_favorite are two-step here.
func TestMCPWorldToolsHints(t *testing.T) {
	r := newWorldsRig(t)
	res, err := r.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mine := []string{"generate_world", "clone_world", "rename_world", "delete_world", "list_deleted_worlds", "list_favorites",
		"make_favorite", "rename_favorite", "delete_favorite", "list_saves", "choose_save"}
	seen := 0
	for _, tool := range res.Tools {
		if !slices.Contains(mine, tool.Name) {
			continue
		}
		seen++
		two := tool.Name == "delete_world" || tool.Name == "delete_favorite"
		if strings.HasPrefix(tool.Name, "list_") {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Errorf("%s is not read-only", tool.Name)
			}
			continue
		}
		if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != two {
			t.Errorf("%s's destructive hint", tool.Name)
		}
	}
	if seen != len(mine) {
		t.Errorf("found %d of %d tools", seen, len(mine))
	}
}

// SPEC 12.9: a refused delete writes no world-delete entry: a locked
// world, the active world, a wrong typed name.
func TestRefusedDeleteWritesNoAudit(t *testing.T) {
	r := newWorldsRig(t)
	active, id := r.seedTwo()
	unlock, err := r.worlds.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	rec := r.post(deletePath(id), url.Values{"confirm_name": {"Second"}}, withCookie(r.cookie))
	unlock()
	if rec.Code != http.StatusConflict {
		t.Errorf("busy: %d", rec.Code)
	}
	if rec := r.post(deletePath(active), url.Values{"confirm_name": {"Base"}}, withCookie(r.cookie)); rec.Code != http.StatusConflict {
		t.Errorf("active: %d", rec.Code)
	}
	if rec := r.post(deletePath(id), url.Values{"confirm_name": {"second"}}, withCookie(r.cookie)); rec.Code != http.StatusBadRequest {
		t.Errorf("wrong name: %d", rec.Code)
	}
	if res, _ := call(t, r.cs, "delete_world", map[string]any{"world_id": active}); !res.IsError {
		t.Error("MCP deleted the active world")
	}
	if got := r.auditOf(AuditWorldDelete); len(got) != 0 {
		t.Errorf("refused deletes were audited: %+v", got)
	}
}
