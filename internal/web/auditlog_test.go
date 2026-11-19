package web

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/store"
)

// addAudit records entries one second apart, oldest first, ending now.
func (hn *harness) addAudit(entries ...store.AuditEntry) {
	hn.t.Helper()
	for i, e := range entries {
		e.Time = hn.clock.Now().Add(time.Duration(i-len(entries)) * time.Second)
		if e.Username == "" {
			e.Username = "alice"
		}
		if err := hn.store.AddAudit(context.Background(), e); err != nil {
			hn.t.Fatal(err)
		}
	}
}

func (hn *harness) auditPage(c *http.Cookie, query string) *httptest.ResponseRecorder {
	hn.t.Helper()
	path := auditLogPath()
	if query != "" {
		path += "?" + query
	}
	rec := hn.get(path, withCookie(c))
	if rec.Code != http.StatusOK {
		hn.t.Fatalf("GET %s: %d\n%s", path, rec.Code, rec.Body.String())
	}
	return rec
}

var reAuditIDs = regexp.MustCompile(`<li id="audit-(\d+)"`)

func auditIDs(body string) []string {
	var out []string
	for _, m := range reAuditIDs.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// Only a signed-in user sees the log, and the tab is current on it (SPEC 12.9).
func TestAuditLogNeedsSignInAndShowsItsTab(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	if rec := hn.get(auditLogPath()); rec.Code != http.StatusSeeOther {
		t.Errorf("signed out: %d, want a redirect to sign in", rec.Code)
	}
	body := hn.auditPage(hn.signInAgain("alice"), "").Body.String()
	nav := between(body, `<nav class="subnav"`, "</nav>")
	if !strings.Contains(nav, `href="`+auditLogPath()+`" aria-current="page"`) {
		t.Errorf("the Audit log tab is not current:\n%s", nav)
	}
}

// 50 entries a page, newest first, and the Older link walks to every entry
// once, ending with no link (SPEC 12.9).
func TestAuditLogPagesNewestFirst(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	var entries []store.AuditEntry
	for i := 0; i < 120; i++ {
		entries = append(entries, store.AuditEntry{Action: "start", Detail: "n" + strconv.Itoa(i)})
	}
	hn.addAudit(entries...)

	var seen []string
	query := ""
	for page := 1; ; page++ {
		body := hn.auditPage(c, query).Body.String()
		ids := auditIDs(body)
		seen = append(seen, ids...)
		older := regexp.MustCompile(`<a href="([^"]+)"[^>]*id="act-audit-older"`).FindStringSubmatch(body)
		switch page {
		case 1, 2:
			if len(ids) != 50 || older == nil {
				t.Fatalf("page %d: %d entries, older link %v", page, len(ids), older != nil)
			}
		case 3:
			if len(ids) != 20 || older != nil {
				t.Fatalf("page 3: %d entries, older link %v", len(ids), older != nil)
			}
		}
		if older == nil {
			break
		}
		u, err := url.Parse(strings.ReplaceAll(older[1], "&amp;", "&"))
		if err != nil || u.Path != auditLogPath() {
			t.Fatalf("older link %q: %v", older[1], err)
		}
		query = u.RawQuery
	}
	if len(seen) != 120 {
		t.Fatalf("saw %d entries, want 120", len(seen))
	}
	for i := 1; i < len(seen); i++ {
		a, _ := strconv.Atoi(seen[i-1])
		b, _ := strconv.Atoi(seen[i])
		if a <= b {
			t.Fatalf("entry %s came before %s: not newest first", seen[i-1], seen[i])
		}
	}
}

// Each filter narrows the list, they combine, and the Older link keeps them.
func TestAuditLogFilters(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	var entries []store.AuditEntry
	for i := 0; i < 60; i++ {
		entries = append(entries, store.AuditEntry{Username: "bob", Action: "stop", WorldID: "base"})
	}
	entries = append(entries,
		store.AuditEntry{Username: "alice", Action: "start", WorldID: "base"},
		store.AuditEntry{Username: "alice", Action: "start", WorldID: "other"},
		store.AuditEntry{Username: "alice", Action: "token-create"},
	)
	hn.addAudit(entries...)

	for _, c2 := range []struct {
		query string
		want  int
	}{
		{"user=alice", 3},
		{"action=start", 2},
		{"world=other", 1},
		{"user=alice&action=start&world=base", 1},
		{"user=nobody", 0},
	} {
		body := hn.auditPage(c, c2.query).Body.String()
		if got := len(auditIDs(body)); got != c2.want {
			t.Errorf("%s: %d entries, want %d", c2.query, got, c2.want)
		}
		if c2.want == 0 && !strings.Contains(body, `id="audit-empty"`) {
			t.Errorf("%s: no empty message", c2.query)
		}
	}

	body := hn.auditPage(c, "user=bob&action=stop").Body.String()
	older := regexp.MustCompile(`<a href="([^"]+)"[^>]*id="act-audit-older"`).FindStringSubmatch(body)
	if older == nil {
		t.Fatal("no Older link for 60 matching entries")
	}
	for _, want := range []string{"user=bob", "action=stop", "before="} {
		if !strings.Contains(older[1], want) {
			t.Errorf("Older link %q lacks %s", older[1], want)
		}
	}
	rest := hn.auditPage(c, strings.SplitN(strings.ReplaceAll(older[1], "&amp;", "&"), "?", 2)[1]).Body.String()
	if got := len(auditIDs(rest)); got != 10 {
		t.Errorf("second filtered page has %d entries, want 10", got)
	}
}

// The menus list what the log holds, and a filter that matches nothing the
// log holds still shows as chosen.
func TestAuditLogMenus(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, err := hn.worlds.Create(t.Context(), "Nauvis Prime", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	hn.addAudit(
		store.AuditEntry{Username: "bob", Action: "start", WorldID: id},
		store.AuditEntry{Username: "alice", Action: "mystery-action", WorldID: "gone-1"},
	)
	body := hn.auditPage(c, "").Body.String()
	for _, want := range []string{
		`<option value="alice">alice</option>`,
		`<option value="bob">bob</option>`,
		`<option value="start">Server started</option>`,
		`<option value="mystery-action">mystery-action</option>`,
		`<option value="` + id + `">Nauvis Prime</option>`,
		`<option value="gone-1">gone-1 (deleted)</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menus lack %s", want)
		}
	}
	chosen := hn.auditPage(c, "user=carol").Body.String()
	if !strings.Contains(chosen, `<option value="carol" selected>carol</option>`) {
		t.Error("a filter on a user the log does not hold is not shown as chosen")
	}
	if !strings.Contains(chosen, `id="act-audit-clear"`) {
		t.Error("a filtered view has no way to clear the filters")
	}
}

// What an entry shows: its action in words or as recorded, the token's name,
// the world's current name or its id marked deleted, and the detail escaped.
func TestAuditLogRows(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, err := hn.worlds.Create(t.Context(), "Nauvis Prime", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	hn.addAudit(
		store.AuditEntry{Action: "start", WorldID: id, TokenName: "Claude"},
		store.AuditEntry{Action: "frobnicate", WorldID: "gone-1", Detail: `<script>alert(1)</script> & "quotes"`},
		store.AuditEntry{Action: "console", WorldID: id, Detail: "/c rcon_password ***"},
		store.AuditEntry{Username: "system", Action: "autosave-tidy"},
	)
	// The menus above the list name the same actions; look only in the list.
	body := hn.auditPage(c, "").Body.String()
	body = body[strings.Index(body, `id="audit-list"`):]
	row := func(action string) string {
		i := strings.Index(body, action)
		if i < 0 {
			t.Fatalf("no row for %s", action)
		}
		start := strings.LastIndex(body[:i], `<li id="audit-`)
		return body[start : start+strings.Index(body[start:], "</li>")]
	}

	start := row("Server started")
	for _, want := range []string{
		`<span class="badge ident" translate="no">Claude</span>`,
		`<a href="` + worldPath(id) + `">Nauvis Prime</a>`,
		`<time datetime=`,
	} {
		if !strings.Contains(start, want) {
			t.Errorf("start row lacks %s:\n%s", want, start)
		}
	}
	unknown := row("frobnicate")
	for _, want := range []string{`gone-1 (deleted)`, `&lt;script&gt;alert(1)&lt;/script&gt; &amp; &#34;quotes&#34;`} {
		if !strings.Contains(unknown, want) {
			t.Errorf("unknown-action row lacks %s:\n%s", want, unknown)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("an entry's detail was not escaped")
	}
	if c := row("Console command"); !strings.Contains(c, "/c rcon_password ***") {
		t.Errorf("a masked console argument changed:\n%s", c)
	}
	if tidy := row("Autosaves tidied"); strings.Contains(tidy, "audit.fact.token") || strings.Contains(tidy, "API token") {
		t.Errorf("an entry with no token shows a token:\n%s", tidy)
	}
}

// A cursor that is not one is the first page, not an error.
func TestAuditLogIgnoresABadCursor(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.addAudit(store.AuditEntry{Action: "start"}, store.AuditEntry{Action: "stop"})
	for _, before := range []string{"x", "1", "1.", ".1", "a.b", "-1.-1", "1.0", "99999999999999999999.1"} {
		body := hn.auditPage(c, "before="+before).Body.String()
		if got := len(auditIDs(body)); got != 2 {
			t.Errorf("before=%q: %d entries, want the first page of 2", before, got)
		}
	}
}

// Every action the code records has words in the catalogue, so a page never
// shows a raw action name for one of our own.
func TestEveryAuditActionHasWords(t *testing.T) {
	found := 0
	for _, dir := range []string{".", "../game"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				vs, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Audit") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					action, _ := strconv.Unquote(lit.Value)
					found++
					if _, ok := auditActionWords[action]; !ok {
						t.Errorf("%s: action %q has no words in auditActionWords", f, action)
					}
				}
				return true
			})
		}
	}
	if found < 30 {
		t.Errorf("found only %d audit actions; the scan no longer fits the code", found)
	}
}

// The plan fixes these action names before their code exists.
func TestAuditActionNamesOfLaterFeaturesHaveWords(t *testing.T) {
	for _, a := range []string{
		"world-archive", "world-unarchive", "user-create", "user-delete", "password-change",
		"snapshot-retention-set", "stop-countdown-set", "audit-retention-set",
		"save-upload-limit-set", "mod-upload-limit-set",
	} {
		if _, ok := auditActionWords[a]; !ok {
			t.Errorf("no words for %q", a)
		}
	}
}
