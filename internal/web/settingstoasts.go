package web

import "github.com/toddawhittaker/foreman/internal/i18n"

// The Settings area's result messages are toasts, so a page that comes back
// after a save shows its plates and fields where they were (DESIGN 6,
// "Toasts"). Field errors stay at their fields.

// doneParams are the address parameters a Settings page's success comes
// from; toast.js drops them, so a reload shows the toast no more (DESIGN 6,
// "Toasts").
var doneParams = []string{"done"}

// doneToast is a one-toast list for a success, or nothing. drop names the
// address parameters it came from, "done" when none are given.
func doneToast(text i18n.Msg, drop ...string) []Toast {
	if text.IsZero() {
		return nil
	}
	if len(drop) == 0 {
		drop = doneParams
	}
	return []Toast{{Text: text, Drop: drop}}
}

// withErrorToast adds an error toast that sums up a refused action.
func withErrorToast(toasts []Toast, text i18n.Msg) []Toast {
	if text.IsZero() {
		return toasts
	}
	return append(toasts, Toast{Error: true, Text: text})
}

func (p GeneralPage) toasts() []Toast {
	if !p.Saved {
		return nil
	}
	// The trim counts stay in the page, in plain text, after the toast closes.
	return doneToast(i18n.M("general.done"))
}

func (p SchedulePage) toasts() []Toast {
	switch {
	case !p.Saved:
		return nil
	case p.On:
		return doneToast(i18n.M("schedule.done.next", "next", momentUTC(p.Next)))
	default:
		return doneToast(i18n.M("schedule.done.off"))
	}
}

func (p NotificationsPage) toasts() []Toast { return doneToast(p.Done, "done", "preset", "host") }

func (p PublicModsSettingsPage) toasts() []Toast {
	switch p.Done {
	case "on":
		return doneToast(i18n.M("publicmods.settings.done.on"))
	case "off":
		return doneToast(i18n.M("publicmods.settings.done.off"))
	}
	return nil
}

func (p APITokensPage) toasts() []Toast {
	drop := []string{"done", "name"}
	switch p.Done {
	case "":
		return nil
	case "revoked":
		return doneToast(i18n.M("apitokens.done.revoked", "name", p.DoneName), drop...)
	case "mcp-on":
		return doneToast(i18n.M("apitokens.done.mcp_on"), drop...)
	case "mcp-off":
		return doneToast(i18n.M("apitokens.done.mcp_off"), drop...)
	case "api-on":
		return doneToast(i18n.M("apitokens.done.api_on"), drop...)
	default:
		return doneToast(i18n.M("apitokens.done.api_off"), drop...)
	}
}

func (p UsersPage) toasts() []Toast {
	var out []Toast
	switch p.Done {
	case "deleted":
		out = doneToast(i18n.M("users.done.deleted"))
	case "":
	default:
		out = doneToast(i18n.M("users.done.password"))
	}
	return withErrorToast(out, p.Error)
}

func (p AccountPage) toasts() []Toast { return withErrorToast(doneToast(p.Done), p.Error) }
