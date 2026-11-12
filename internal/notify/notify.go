// Package notify sends Foreman's events to webhooks. It is a leaf package:
// it does not import the game controller, which is handed a Notifier.
package notify

// Notifier delivers events to the configured webhooks.
type Notifier struct{}

// New returns a Notifier. It sends nothing yet.
func New() *Notifier { return &Notifier{} }
