package web

import (
	"strings"
	"testing"
)

// The page names are fixed by the plan; links and bookmarks depend on them.
func TestSettingsPageNames(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{generalPath(), "/settings/general"},
		{usersPath(), "/settings/users"},
		{auditLogPath(), "/settings/audit-log"},
		{userDeletePath(7), "/settings/users/7/delete"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// A tab never leads to a page that does not exist yet.
func TestSettingsTabsHideUnbuiltPages(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	body := hn.get(PathAccount, withCookie(c)).Body.String()
	nav := between(body, `<nav class="subnav" aria-label="Foreman settings">`, "</nav>")
	for _, p := range []struct {
		on   bool
		path string
	}{
		{generalTabOn, generalPath()},
		{usersTabOn, usersPath()},
		{auditTabOn, auditLogPath()},
	} {
		if strings.Contains(nav, `href="`+p.path+`"`) != p.on {
			t.Errorf("tab %s shown %v, flag %v\n%s", p.path, !p.on, p.on, nav)
		}
		if !p.on {
			if code := hn.get(p.path, withCookie(c)).Code; code != 404 {
				t.Errorf("%s answers %d, want 404", p.path, code)
			}
		}
	}
}
