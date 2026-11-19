package web

import "testing"

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
