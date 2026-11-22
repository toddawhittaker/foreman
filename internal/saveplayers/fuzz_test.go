package saveplayers

import "testing"

// Both parsers read bytes a container wrote: they must never panic, and what
// Parse accepts must marshal and parse again.
func FuzzParse(f *testing.F) {
	f.Add([]byte(goodSeed))
	f.Add([]byte(`{"format_version":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseSeed(b)
		r, err := Parse(b)
		if err != nil {
			return
		}
		out, err := r.Marshal()
		if err != nil {
			t.Fatalf("accepted a record it cannot write: %v", err)
		}
		if _, err := Parse(out); err != nil {
			t.Fatalf("wrote a record it cannot read: %v", err)
		}
	})
}
