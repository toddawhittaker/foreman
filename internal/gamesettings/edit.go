package gamesettings

import (
	"bytes"
	"encoding/json"
)

// These helpers edit the text of a settings file in place, so a change keeps
// the file's own key order, unknown keys and layout.

type keyValue struct {
	key string
	raw []byte // a JSON value
}

// leadSpace is the whitespace between the separator before a member and its key.
func leadSpace(b []byte, m member) []byte {
	seg := b[m.lead:m.keyStart]
	if i := bytes.IndexByte(seg, ','); i >= 0 {
		seg = seg[i+1:]
	}
	return seg
}

// rebuild returns b with the members keep rejects removed.
func rebuild(b []byte, root *node, keep func(member) bool) []byte {
	var out bytes.Buffer
	out.Write(b[:root.start+1])
	// tail is the whitespace between the last value and the closing brace.
	tail := b[root.start+1 : root.end-1]
	if n := len(root.members); n > 0 {
		tail = b[root.members[n-1].node.end : root.end-1]
	}
	first := true
	for _, m := range root.members {
		if !keep(m) {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		out.Write(leadSpace(b, m))
		out.Write(b[m.keyStart:m.node.end])
	}
	out.Write(tail)
	out.Write(b[root.end-1:])
	return out.Bytes()
}

// appendMembers adds the pairs at the end of the object root parsed from b.
func appendMembers(b []byte, root *node, add []keyValue) []byte {
	if len(add) == 0 {
		return b
	}
	space := []byte("\n  ")
	at := root.start + 1
	if n := len(root.members); n > 0 {
		last := root.members[n-1]
		space = leadSpace(b, last)
		at = last.node.end
	}
	var ins bytes.Buffer
	for i, kv := range add {
		if i > 0 || len(root.members) > 0 {
			ins.WriteByte(',')
		}
		ins.Write(space)
		k, _ := json.Marshal(kv.key)
		ins.Write(k)
		ins.WriteString(": ")
		ins.Write(kv.raw)
	}
	var out bytes.Buffer
	out.Write(b[:at])
	out.Write(ins.Bytes())
	if len(root.members) == 0 {
		out.WriteByte('\n')
		out.Write(b[root.end-1:])
	} else {
		out.Write(b[at:])
	}
	return out.Bytes()
}

// setMember replaces the value of key, or appends it when absent.
func setMember(b []byte, root *node, key string, raw []byte) []byte {
	m := lastMember(root, key)
	if m == nil {
		return appendMembers(b, root, []keyValue{{key, raw}})
	}
	var out bytes.Buffer
	out.Write(b[:m.valStart])
	out.Write(raw)
	out.Write(b[m.node.end:])
	return out.Bytes()
}
