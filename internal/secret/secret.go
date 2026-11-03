// Package secret holds a value that must never be printed, logged or
// serialised (SPEC 14.8). Every formatting path yields Redacted; the only
// way to read the value is Reveal.
package secret

import (
	"fmt"
	"log/slog"
)

// Redacted is what a Secret prints as, in every format.
const Redacted = "[redacted]"

// Secret wraps a sensitive string.
type Secret struct {
	v string
}

// New wraps v.
func New(v string) Secret { return Secret{v: v} }

// Reveal returns the wrapped value. Call it only where the value is used.
func (s Secret) Reveal() string { return s.v }

// String implements fmt.Stringer.
func (Secret) String() string { return Redacted }

// GoString implements fmt.GoStringer, used by %#v.
func (Secret) GoString() string { return Redacted }

// Format implements fmt.Formatter so that verbs such as %d and %x cannot
// reach the wrapped string either.
func (Secret) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(Redacted))
}

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalJSON implements json.Marshaler.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + Redacted + `"`), nil }

// MarshalText implements encoding.TextMarshaler.
func (Secret) MarshalText() ([]byte, error) { return []byte(Redacted), nil }
