package gamesettings

import (
	"bytes"
	"encoding/json"
)

// SecretKeys are the top-level keys of server-settings.json that hold a
// secret: the game password and the factorio.com sign-in (SPEC 11.4, 14.8).
// They are write-only: never shown, never in a diff value, never in an error.
var SecretKeys = []string{"game_password", "username", "password", "token"}

func isSecretKey(k string) bool { return contains(SecretKeys, k) }

// isSet reports whether a secret key holds a value. An empty string and null
// both mean "not set".
func isSet(n *node) bool {
	return n.kind != kNull && (n.kind != kString || n.str != "")
}

// lastMember returns the last member called key, which is the one the game
// reads when a key repeats.
func lastMember(n *node, key string) *member {
	for i := len(n.members) - 1; i >= 0; i-- {
		if n.members[i].key == key {
			return &n.members[i]
		}
	}
	return nil
}

// StripSecrets removes every secret key from b so a raw editor never sees
// one. Everything else keeps its text, order and layout. set holds true for
// each secret key that had a value, so a form can say "a password is set".
// Input that is not one JSON object yields nil and an empty set: callers
// validate first, and showing unparsed text could show a secret.
func StripSecrets(b []byte) (stripped []byte, set map[string]bool) {
	set = map[string]bool{}
	if len(b) > MaxBytes {
		return nil, set
	}
	root, fe := parse(b)
	if fe != nil {
		return nil, set
	}
	for _, k := range SecretKeys {
		if m := lastMember(root, k); m != nil && isSet(m.node) {
			set[k] = true
		}
	}
	return rebuild(b, root, func(m member) bool { return !isSecretKey(m.key) }), set
}

// MergeSecrets puts back into edited every secret key that current has a
// value for and edited lacks, so saving what StripSecrets produced keeps the
// secrets. A secret key present in edited wins, which is how an admin sets,
// changes or clears one (an empty string clears it). Restored keys go at the
// end of the object. If edited is returned unchanged it is because nothing
// needed restoring or because either input is not one JSON object; Validate
// the edit first.
func MergeSecrets(edited, current []byte) []byte {
	if len(edited) > MaxBytes || len(current) > MaxBytes {
		return edited
	}
	cur, fe := parse(current)
	if fe != nil {
		return edited
	}
	ed, fe := parse(edited)
	if fe != nil {
		return edited
	}
	var add []keyValue
	for _, k := range SecretKeys {
		m := lastMember(cur, k)
		if m == nil || !isSet(m.node) || lastMember(ed, k) != nil {
			continue
		}
		add = append(add, keyValue{k, compact(current[m.valStart:m.node.end])})
	}
	return appendMembers(edited, ed, add)
}

func compact(raw []byte) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}
