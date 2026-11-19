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
	"slices"
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

var reAuditIDs = regexp.MustCompile(`<tr role="row" id="audit-(\d+)"`)

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
	nav := between(body, `<nav class="subnav subnav--settings"`, "</nav>")
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
		start := strings.LastIndex(body[:i], `<tr role="row" id="audit-`)
		return body[start : start+strings.Index(body[start:], "</tr>")]
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
	// The token cell is there, empty, so the columns never move.
	tidy := row("Autosaves tidied")
	if strings.Contains(tidy, `class="badge`) || !strings.Contains(tidy, `class="audit-token" data-label="API token"></td>`) {
		t.Errorf("an entry with no token has no empty token cell:\n%s", tidy)
	}
	for _, r := range []string{start, unknown, tidy} {
		if got := strings.Count(r, "<td "); got != 6 {
			t.Errorf("a row has %d cells, want the 6 fixed columns:\n%s", got, r)
		}
	}
}

var (
	reAuditNewer = regexp.MustCompile(`<a href="([^"]+)"[^>]*id="act-audit-newer"`)
	reAuditOlder = regexp.MustCompile(`<a href="([^"]+)"[^>]*id="act-audit-older"`)
)

// auditLinkQuery returns the query of a pager link, or "" and false when there is none.
func auditLinkQuery(t *testing.T, re *regexp.Regexp, body string) (string, bool) {
	t.Helper()
	m := re.FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	u, err := url.Parse(strings.ReplaceAll(m[1], "&amp;", "&"))
	if err != nil || u.Path != auditLogPath() {
		t.Fatalf("pager link %q: %v", m[1], err)
	}
	return u.RawQuery, true
}

func addAuditN(hn *harness, n int) {
	var entries []store.AuditEntry
	for i := 0; i < n; i++ {
		entries = append(entries, store.AuditEntry{Username: "bob", Action: "stop", Detail: "n" + strconv.Itoa(i)})
	}
	hn.addAudit(entries...)
}

// A page size from the menu sets the page, any other value is 50, and the
// choice is in the menu and in the links (SPEC 12.9).
func TestAuditLogPageSize(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	addAuditN(hn, 230)
	for _, tc := range []struct {
		size string
		want int
	}{
		{"", 50}, {"25", 25}, {"50", 50}, {"100", 100}, {"200", 200},
		{"30", 50}, {"0", 50}, {"-25", 50}, {"1000", 50}, {"abc", 50}, {"25abc", 50},
	} {
		query := ""
		if tc.size != "" {
			query = "size=" + url.QueryEscape(tc.size)
		}
		body := hn.auditPage(c, query).Body.String()
		if got := len(auditIDs(body)); got != tc.want {
			t.Errorf("size=%q: %d entries, want %d", tc.size, got, tc.want)
		}
		for _, n := range []string{"25", "50", "100", "200"} {
			sel := `<option value="` + n + `">`
			if n == strconv.Itoa(tc.want) {
				sel = `<option value="` + n + `" selected>`
			}
			if !strings.Contains(body, sel) {
				t.Errorf("size=%q: menu lacks %s", tc.size, sel)
			}
		}
	}
	// A size other than the default rides in the links, the default does not.
	older, _ := auditLinkQuery(t, reAuditOlder, hn.auditPage(c, "size=25").Body.String())
	if !strings.Contains(older, "size=25") {
		t.Errorf("Older link %q drops the page size", older)
	}
	older, _ = auditLinkQuery(t, reAuditOlder, hn.auditPage(c, "").Body.String())
	if strings.Contains(older, "size=") {
		t.Errorf("Older link %q names the default size", older)
	}
}

// Newer and Older walk the whole log both ways, once each, at any size, and
// the ends have no link past them (SPEC 12.9).
func TestAuditLogNewerAndOlder(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	addAuditN(hn, 60)
	for _, size := range []int{25, 50} {
		sz := "size=" + strconv.Itoa(size)
		first := hn.auditPage(c, sz).Body.String()
		if _, ok := auditLinkQuery(t, reAuditNewer, first); ok {
			t.Errorf("size %d: the newest page has a Newer link", size)
		}
		var down [][]string
		body := first
		for {
			down = append(down, auditIDs(body))
			q, ok := auditLinkQuery(t, reAuditOlder, body)
			if !ok {
				break
			}
			body = hn.auditPage(c, q).Body.String()
			if _, ok := auditLinkQuery(t, reAuditNewer, body); !ok {
				t.Fatalf("size %d: a later page has no Newer link", size)
			}
		}
		total := 0
		for _, ids := range down {
			total += len(ids)
		}
		if total != 60 {
			t.Fatalf("size %d: walked %d entries, want 60", size, total)
		}
		// Now back up: each Newer page is exactly the page above it.
		for i := len(down) - 2; i >= 0; i-- {
			q, ok := auditLinkQuery(t, reAuditNewer, body)
			if !ok {
				t.Fatalf("size %d: no Newer link at page %d", size, i+2)
			}
			body = hn.auditPage(c, q).Body.String()
			if got := auditIDs(body); !slices.Equal(got, down[i]) {
				t.Errorf("size %d: Newer from page %d gave %v, want %v", size, i+2, got, down[i])
			}
			if size != auditPageSize && !strings.Contains(q, sz) {
				t.Errorf("size %d: Newer link %q drops the size", size, q)
			}
		}
		if _, ok := auditLinkQuery(t, reAuditNewer, body); ok {
			t.Errorf("size %d: back at the top there is still a Newer link", size)
		}
	}
}

// A last page that holds exactly one page of entries still has no Older link,
// and the filters ride in Newer as well as Older.
func TestAuditLogPagerEndsAndFilters(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	addAuditN(hn, 50)
	hn.addAudit(store.AuditEntry{Username: "alice", Action: "start"})
	// 51 entries: one on the first page's successor.
	older, ok := auditLinkQuery(t, reAuditOlder, hn.auditPage(c, "").Body.String())
	if !ok {
		t.Fatal("51 entries and no Older link")
	}
	last := hn.auditPage(c, older).Body.String()
	if _, ok := auditLinkQuery(t, reAuditOlder, last); ok {
		t.Error("the last page has an Older link")
	}
	// Exactly 50 matching entries: one page, no links at all.
	only := hn.auditPage(c, "user=bob").Body.String()
	if _, ok := auditLinkQuery(t, reAuditOlder, only); ok {
		t.Error("50 matching entries have an Older link")
	}
	if _, ok := auditLinkQuery(t, reAuditNewer, only); ok {
		t.Error("the newest page has a Newer link")
	}
	// With filters and a size, both links keep them.
	q := "user=bob&action=stop&size=25"
	older, _ = auditLinkQuery(t, reAuditOlder, hn.auditPage(c, q).Body.String())
	mid := hn.auditPage(c, older).Body.String()
	newer, ok := auditLinkQuery(t, reAuditNewer, mid)
	if !ok {
		t.Fatal("no Newer link on the second page")
	}
	for _, want := range []string{"user=bob", "action=stop", "size=25", "after="} {
		if !strings.Contains(newer, want) {
			t.Errorf("Newer link %q lacks %s", newer, want)
		}
	}
	if !strings.Contains(mid, `id="act-audit-clear"`) || !strings.Contains(mid, `href="`+auditLogPath()+`?size=25"`) {
		t.Error("Clear filters does not keep the page size")
	}
}

// A cursor past either end of the log, or after wins over before, shows a
// real page, not an empty one.
func TestAuditLogCursorPastTheEnds(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	addAuditN(hn, 3)
	for _, q := range []string{"before=1.1", "after=99999999999.1", "after=99999999999.1&before=1.1"} {
		rec := hn.get(auditLogPath()+"?"+q, withCookie(c))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != auditLogPath() {
			t.Errorf("%s: %d to %q, want a redirect to the first page", q, rec.Code, rec.Header().Get("Location"))
		}
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
