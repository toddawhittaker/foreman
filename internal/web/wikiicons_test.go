package web

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// cspOf is the Content-Security-Policy of a signed-in page.
func cspOf(hn *harness, path string, c *http.Cookie) string {
	return hn.get(path, withCookie(c)).Header().Get("Content-Security-Policy")
}

// SPEC 13.1, 14.10: the switch is off by default; turning it on and off is
// audited "from off to on" and decides whether a page may load images from
// the wiki.
func TestWikiIconsSwitchAndPolicy(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	const off = "frame-ancestors 'none'; img-src 'self' data:"
	if body := hn.get(generalPath(), withCookie(c)).Body.String(); !strings.Contains(body, `name="wiki_icons"`) || strings.Contains(body, `name="wiki_icons" checked`) {
		t.Fatal("the switch is missing or on by default")
	}
	if got := cspOf(hn, generalPath(), c); got != off {
		t.Errorf("policy with the switch off: %q, want %q", got, off)
	}
	if rec := hn.post(generalPath(), generalForm(map[string]string{"wiki_icons": "on"}), withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d", rec.Code)
	}
	if got := strings.Join(hn.audits(AuditWikiIconsSet), "|"); got != "alice  from off to on" {
		t.Errorf("audit %q", got)
	}
	if got, want := cspOf(hn, generalPath(), c), off+" https://wiki.factorio.com"; got != want {
		t.Errorf("policy with the switch on: %q, want %q", got, want)
	}
	if body := hn.get(generalPath(), withCookie(c)).Body.String(); !strings.Contains(body, `name="wiki_icons" checked`) {
		t.Error("the page does not show the switch on")
	}
	// An unchanged save audits nothing more; unticking it turns it off.
	hn.post(generalPath(), generalForm(map[string]string{"wiki_icons": "on"}), withCookie(c))
	if n := len(hn.audits(AuditWikiIconsSet)); n != 1 {
		t.Errorf("%d audit entries after an unchanged save, want 1", n)
	}
	hn.post(generalPath(), generalForm(nil), withCookie(c))
	if got := cspOf(hn, generalPath(), c); got != off {
		t.Errorf("policy after turning it off: %q", got)
	}
}

// SPEC 14.10: pages that are not signed-in pages, and the error page, keep
// the policy too; the sign-in page never allows the wiki.
func TestWikiIconsPolicyOnEveryPage(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	if rec := hn.post(generalPath(), generalForm(map[string]string{"wiki_icons": "on"}), withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d", rec.Code)
	}
	rec := hn.get("/no-such-page", withCookie(c))
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "img-src 'self' data:") || !strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("error page policy %q", got)
	}
}

// SPEC 14.10: Foreman itself never contacts the wiki, with the switch on.
func TestWikiIconsServerMakesNoWikiRequest(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		seen = append(seen, r.URL.String())
		mu.Unlock()
		return nil, http.ErrNotSupported
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.post(generalPath(), generalForm(map[string]string{"wiki_icons": "on"}), withCookie(c))
	for _, p := range []string{generalPath(), PathAbout, "/no-such-page"} {
		hn.get(p, withCookie(c))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, u := range seen {
		if strings.Contains(u, "wiki.factorio.com") {
			t.Errorf("the server requested %s", u)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// SPEC 14.10: an unreadable switch counts as off, so a fault never lets a
// page contact the wiki.
func TestWikiIconsUnreadableCountsAsOff(t *testing.T) {
	hn := newHarness(t, nil)
	s := hn.generalServer()
	if err := hn.store.SetAppSetting(t.Context(), settingWikiIcons.Name, "maybe", "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if s.wikiIconsOn(t.Context()) {
		t.Error("a value that is not on or off counted as on")
	}
}

// SPEC 14.10: a fragment swapped in by htmx gets images only with the
// switch on for the request.
func TestShowWikiIconsFragment(t *testing.T) {
	frag := []byte(`<p><span class="rt-obj" data-wiki="Iron_ore">Iron ore</span></p>`)
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	if got := showWikiIcons(r, frag); string(got) != string(frag) {
		t.Errorf("changed with the switch off: %s", got)
	}
	on := withWikiIcons(r, true)
	if got := string(showWikiIcons(on, frag)); !strings.Contains(got, `src="https://wiki.factorio.com/images/Iron_ore.png"`) {
		t.Errorf("no image with the switch on: %s", got)
	}
}

// SPEC 13.1, 21.8: foreman_settings shows the switch (off by default) and
// set_foreman_settings changes it and audits it like any General setting.
func TestMCPWikiIcons(t *testing.T) {
	r := newForemanRig(t, nil)
	_, out := r.run(t, "foreman_settings", map[string]any{})
	if !strings.Contains(asJSON(out), `"wiki_icons":false`) {
		t.Fatalf("settings: %s", asJSON(out))
	}
	args := map[string]any{"wiki_icons": true}
	code := askCode(t, r.cs, "set_foreman_settings", args)
	res, out := r.run(t, "set_foreman_settings", withCode(args, code))
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if text := asJSON(out); !strings.Contains(text, `{"from":false,"setting":"wiki_icons","to":true}`) {
		t.Errorf("changes: %s", text)
	}
	if rows := r.auditRows(AuditWikiIconsSet); len(rows) != 1 || rows[0].Detail != "from off to on" {
		t.Errorf("audit rows %+v", rows)
	}
}
