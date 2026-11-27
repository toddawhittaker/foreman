package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// SPEC 9.16: an admin turns the page on and off at /settings/public-mods;
// it takes effect at once and is audited with "on" or "off".
func TestPublicModsSettings(t *testing.T) {
	p := newPublicRig(t)
	p.setting(false)
	if body := p.get(PathPublicModsSettings); !strings.Contains(body, `name="public_mods" value="on"`) {
		t.Fatalf("the off page offers no Turn on:\n%s", body)
	}
	rep := p.post(PathPublicModsSettings, url.Values{"public_mods": {"on"}})
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != PathPublicModsSettings+"?done=on" {
		t.Fatalf("POST on: %d to %q", rep.StatusCode, rep.Header.Get("Location"))
	}
	if rep := p.anon(PathPublicMods); rep.StatusCode != http.StatusOK {
		t.Errorf("after turning on, the page answered %d", rep.StatusCode)
	}
	body := p.get(PathPublicModsSettings + "?done=on")
	for _, want := range []string{"Done:</strong> <span data-toast-text>", `href="/public/mods"`, `value="off"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the on page lacks %q", want)
		}
	}
	p.post(PathPublicModsSettings, url.Values{"public_mods": {"off"}})
	if rep := p.anon(PathPublicMods); rep.StatusCode != http.StatusNotFound {
		t.Errorf("after turning off, the page answered %d", rep.StatusCode)
	}
	if got := strings.Join(p.audits(AuditPublicModsSet), "|"); got != "alice  on|alice  off" && got != "alice  off|alice  on" {
		t.Errorf("audit %q", got)
	}
	if rep := p.post(PathPublicModsSettings, url.Values{"public_mods": {"yes"}}); rep.StatusCode != http.StatusBadRequest {
		t.Errorf("a bad value: %d, want 400", rep.StatusCode)
	}
	if on := p.server().publicModsOn(context.Background()); on {
		t.Error("a bad value changed the setting")
	}
}

// SPEC 12.1: the settings page needs a signed-in user.
func TestPublicModsSettingsNeedsSignIn(t *testing.T) {
	p := newPublicRig(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rep := p.do(method, PathPublicModsSettings, url.Values{"public_mods": {"off"}}, http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
		if rep.StatusCode == http.StatusOK || rep.StatusCode == http.StatusSeeOther && strings.HasPrefix(rep.Header.Get("Location"), PathPublicModsSettings) {
			t.Errorf("%s signed out: %d to %q", method, rep.StatusCode, rep.Header.Get("Location"))
		}
	}
	if !p.server().publicModsOn(context.Background()) {
		t.Error("a signed-out post turned the page off")
	}
}
