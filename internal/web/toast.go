package web

import "github.com/toddawhittaker/foreman/internal/i18n"

// Toast is a result message that floats over the page and takes no room
// (DESIGN 6, "Toasts"). A success closes itself; an error stays until closed.
// Field errors stay inline at their field instead.
type Toast struct {
	// Error marks a failed action; otherwise the toast says it was done.
	Error bool
	Text  i18n.Msg
	// Key names the result the toast is about, so a page shows it once
	// (DESIGN 6, "Toasts"); "" for a toast drawn with its answer only.
	Key string
}

// Toasts are the page's result messages, from pages that have any. A page
// whose htmx answers carry its toasts too has them as an exported method.
func (v view) Toasts() []Toast {
	switch p := v.Page.(type) {
	case interface{ toasts() []Toast }:
		return p.toasts()
	case interface{ Toasts() []Toast }:
		return p.Toasts()
	}
	return nil
}

// toasts: a refused sign-in is summed up over the form, which stays as it was
// (SPEC 12.4).
func (p SignInPage) toasts() []Toast {
	switch {
	case p.RateLimited:
		return []Toast{{Error: true, Text: p.RateLimitMessage()}}
	case p.Busy:
		return []Toast{{Error: true, Text: i18n.M("accounts.signin.busy")}}
	}
	return nil
}
