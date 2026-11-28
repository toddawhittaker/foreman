package web

import (
	"strings"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

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
	// Drop names the address parameters that produced the toast, such as
	// "renamed" after a redirect. toast.js takes them out of the address
	// once the toast is shown, so a reload or Back does not show it again.
	Drop []string
}

// DropParams is Drop as the toast's data-drop attribute.
func (t Toast) DropParams() string { return strings.Join(t.Drop, " ") }

// Toasts are the page's result messages, from pages that have any, after
// a banner Renew's, which any page can show. A page whose htmx answers carry
// its toasts too has them as an exported method.
func (v view) Toasts() []Toast {
	var out []Toast
	// Renewed names only a working token of the user's own
	// (dropSpoofedRenewed), so a crafted link shows no made-up text.
	if v.Renewed != "" {
		out = append(out, Toast{Text: i18n.M("apitokens.done.renewed", "name", v.Renewed), Drop: []string{"done", "name"}})
	}
	switch p := v.Page.(type) {
	case interface{ toasts() []Toast }:
		out = append(out, p.toasts()...)
	case interface{ Toasts() []Toast }:
		out = append(out, p.Toasts()...)
	}
	return out
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
