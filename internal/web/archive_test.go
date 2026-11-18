package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

func (r *worldsRig) archived(id string) bool {
	r.t.Helper()
	sum, err := r.worlds.Get(context.Background(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	return sum.Archived
}

// SPEC 5.9, 12.9: archiving from the world page hides the world in a closed
// foldout, says so on its page, offers Unarchive, and is audited; unarchiving
// puts it back in the main list.
func TestArchiveAndUnarchiveFromThePages(t *testing.T) {
	r := newWorldsRig(t)
	_, id := r.seedTwo()

	page := r.get(worldPath(id), withCookie(r.cookie)).Body.String()
	if !strings.Contains(page, `id="act-archive"`) || strings.Contains(page, `id="act-unarchive"`) {
		t.Errorf("world page before:\n%s", page)
	}

	rec := r.post(worldPath(id)+"/archive", url.Values{}, withCookie(r.cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != worldPath(id)+"?archived=on" {
		t.Fatalf("archive: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if !r.archived(id) {
		t.Fatal("the world is not archived")
	}
	page = r.get(rec.Header().Get("Location"), withCookie(r.cookie)).Body.String()
	for _, want := range []string{"Second is archived.", `id="act-unarchive"`, "Archived world", `<span class="badge">Archived</span>`} {
		if !strings.Contains(page, want) {
			t.Errorf("archived world page lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, `id="act-archive"`) || strings.Contains(page, `id="act-switch"`) {
		t.Errorf("an archived world still offers Archive or Switch:\n%s", page)
	}

	// The list keeps it out of the main list and shows it in a closed foldout.
	list := r.get(PathWorlds, withCookie(r.cookie)).Body.String()
	if !strings.Contains(list, `<summary>Archived (1)</summary>`) || strings.Contains(list, `<details class="disclosure" id="archived-worlds" open`) {
		t.Errorf("worlds list:\n%s", list)
	}
	mainList, foldout, _ := strings.Cut(list, `id="archived-worlds"`)
	if strings.Contains(mainList, `href="`+worldPath(id)+`"`) || !strings.Contains(foldout, `href="`+worldPath(id)+`"`) {
		t.Errorf("the archived world is not only in the foldout:\n%s", list)
	}

	if got := r.auditOf(AuditWorldArchive); len(got) != 1 || got[0].WorldID != id || got[0].Username != "alice" || got[0].TokenName != "" {
		t.Errorf("audit: %+v", got)
	}

	rec = r.post(worldPath(id)+"/unarchive", url.Values{}, withCookie(r.cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != worldPath(id)+"?archived=off" {
		t.Fatalf("unarchive: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if r.archived(id) {
		t.Fatal("the world is still archived")
	}
	if got := r.auditOf(AuditWorldUnarchive); len(got) != 1 || got[0].WorldID != id {
		t.Errorf("audit: %+v", got)
	}
	list = r.get(PathWorlds, withCookie(r.cookie)).Body.String()
	if strings.Contains(list, "archived-worlds") {
		t.Errorf("an empty archive still shows a foldout:\n%s", list)
	}
}

// SPEC 5.9: the active world cannot be archived: the page says why, even to
// a hand-made request, and nothing is audited.
func TestArchiveRefusesTheActiveWorld(t *testing.T) {
	r := newWorldsRig(t)
	active, _ := r.seedTwo()

	page := r.get(worldPath(active), withCookie(r.cookie)).Body.String()
	if strings.Contains(page, `id="act-archive"`) || !strings.Contains(page, "the active world cannot be archived") {
		t.Errorf("active world page:\n%s", page)
	}
	rec := r.post(worldPath(active)+"/archive", url.Values{}, withCookie(r.cookie))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `id="archive-error"`) {
		t.Errorf("forced archive: %d\n%s", rec.Code, rec.Body)
	}
	if r.archived(active) || len(r.auditOf(AuditWorldArchive)) != 0 {
		t.Error("the active world was archived or audited")
	}
}

// A crafted link cannot put the done message on a world in the other state.
func TestArchiveDoneMessageNeedsTheState(t *testing.T) {
	sum := world.Summary{Name: "Second", Archived: true}
	if archiveDone(archiveOn, sum).IsZero() || !archiveDone(archiveOff, sum).IsZero() || !archiveDone("", sum).IsZero() {
		t.Error("an archived world shows the wrong message")
	}
	sum.Archived = false
	if !archiveDone(archiveOn, sum).IsZero() || archiveDone(archiveOff, sum).IsZero() {
		t.Error("an unarchived world shows the wrong message")
	}
}

// SPEC 5.9: a refusal to start or switch an archived world has its own
// words, on every page that shows start errors.
func TestArchivedStartErrorHasWords(t *testing.T) {
	status, msg := gameError(&game.StartError{Kind: game.StartArchived})
	if status != http.StatusConflict || !strings.Contains(inEnglish(msg), "archived") {
		t.Errorf("gameError = %d %q", status, inEnglish(msg))
	}
}

// SPEC 5.9, 21.7: archive_world and unarchive_world do what the page does, in
// one call each, audited with the token's name; list_worlds says which worlds
// are archived.
func TestMCPArchiveAndUnarchive(t *testing.T) {
	r := newWorldsRig(t)
	active, id := r.seedTwo()

	res, out := call(t, r.cs, "archive_world", map[string]any{"world_id": id})
	if res.IsError || out["archived"] != true {
		t.Fatalf("archive: %s %s", resultText(res), asJSON(out))
	}
	if !r.archived(id) {
		t.Fatal("the world is not archived")
	}
	if got := r.auditOf(AuditWorldArchive); len(got) != 1 || got[0].TokenName != "Claude" || got[0].Username != "alice" || got[0].WorldID != id {
		t.Errorf("audit: %+v", got)
	}
	_, list := call(t, r.cs, "list_worlds", nil)
	worlds, _ := list["worlds"].([]any)
	seen := map[string]bool{}
	for _, w := range worlds {
		m, _ := w.(map[string]any)
		seen[m["id"].(string)], _ = m["archived"].(bool)
	}
	if len(seen) != 2 || !seen[id] || seen[active] {
		t.Errorf("list_worlds archived flags: %v", seen)
	}

	// The active world is refused with the page's words, and nothing is audited.
	res, _ = call(t, r.cs, "archive_world", map[string]any{"world_id": active})
	if !res.IsError || !strings.Contains(resultText(res), "active world cannot be archived") {
		t.Errorf("active: %s", resultText(res))
	}
	if r.archived(active) || len(r.auditOf(AuditWorldArchive)) != 1 {
		t.Error("the active world was archived or audited")
	}
	if res, _ = call(t, r.cs, "archive_world", map[string]any{"world_id": "nope"}); !res.IsError {
		t.Error("an unknown world was archived")
	}
	if res, _ = call(t, r.cs, "archive_world", map[string]any{}); !res.IsError {
		t.Error("an archive with no world_id was accepted")
	}

	res, out = call(t, r.cs, "unarchive_world", map[string]any{"world_id": id})
	if res.IsError || out["archived"] != false || r.archived(id) {
		t.Fatalf("unarchive: %s %s", resultText(res), asJSON(out))
	}
	if got := r.auditOf(AuditWorldUnarchive); len(got) != 1 || got[0].TokenName != "Claude" || got[0].WorldID != id {
		t.Errorf("audit: %+v", got)
	}
}
