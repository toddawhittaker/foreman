package gamesettings

import (
	"strconv"
)

// Kind says how one key differs between two versions of a file.
type Kind string

const (
	KindAdded   Kind = "added"
	KindRemoved Kind = "removed"
	KindChanged Kind = "changed"
)

// Change is one difference. Old and New are the values as compact JSON, so a
// string keeps its quotes and a number stays a number. For a secret key they
// are always empty: the web layer says "will be set", "will be changed" or
// "will be removed" from Kind alone (SPEC 11.3, 11.4).
type Change struct {
	Key    string // dotted path, as in FieldError
	Kind   Kind
	Old    string
	New    string
	Secret bool
}

// leaf is one comparable value: a scalar, an array, or an empty object.
type leaf struct {
	n   *node
	raw string
}

type leafSet struct {
	order  []string
	leaves map[string]leaf
}

// flatten lists the leaves of root under dotted keys. A non-empty object is
// walked into, except under a secret key; it is not a leaf. Where a key repeats the last one wins.
func flatten(b []byte, root *node) leafSet {
	ls := leafSet{leaves: map[string]leaf{}}
	var walk func(n *node, start int, path string, whole bool)
	walk = func(n *node, start int, path string, whole bool) {
		if n.kind == kObject && len(n.members) > 0 && !whole {
			for _, m := range n.members {
				walk(m.node, m.valStart, joinPath(path, m.key), false)
			}
			return
		}
		if _, seen := ls.leaves[path]; !seen {
			ls.order = append(ls.order, path)
		}
		ls.leaves[path] = leaf{n: n, raw: string(compact(b[start:n.end]))}
	}
	for _, m := range root.members {
		// A secret is one value, whatever it holds, so no part of it is listed.
		walk(m.node, m.valStart, m.key, isSecretKey(m.key))
	}
	return ls
}

// sameValue reports whether two leaves mean the same, so 1 and 1.0, or two
// spellings of one string, do not show as a change.
func sameValue(a, b leaf) bool {
	if a.raw == b.raw {
		return true
	}
	if a.n.kind != b.n.kind {
		return false
	}
	switch a.n.kind {
	case kString:
		return a.n.str == b.n.str
	case kNumber:
		x, errX := strconv.ParseFloat(a.n.num, 64)
		y, errY := strconv.ParseFloat(b.n.num, 64)
		return errX == nil && errY == nil && x == y
	}
	return false
}

// Diff lists what differs between two versions of one settings file, in the
// order of new and then the keys only old had. Unknown keys are included. A
// secret key (SecretKeys, top level only) counts as present only when it has a
// value, and its Change carries no value. Text that is not one JSON object
// counts as an empty file; Validate it first.
func Diff(oldB, newB []byte) []Change {
	oldSet, newSet := leaves(oldB), leaves(newB)
	var out []Change
	for _, k := range newSet.order {
		nl := newSet.leaves[k]
		ol, had := oldSet.leaves[k]
		secret := isSecretKey(k)
		switch {
		case secret:
			if c, ok := secretChange(k, ol, had, nl); ok {
				out = append(out, c)
			}
		case !had:
			out = append(out, Change{Key: k, Kind: KindAdded, New: nl.raw})
		case !sameValue(ol, nl):
			out = append(out, Change{Key: k, Kind: KindChanged, Old: ol.raw, New: nl.raw})
		}
	}
	for _, k := range oldSet.order {
		if _, still := newSet.leaves[k]; still {
			continue
		}
		ol := oldSet.leaves[k]
		switch {
		case isSecretKey(k):
			if isSet(ol.n) {
				out = append(out, Change{Key: k, Kind: KindRemoved, Secret: true})
			}
		default:
			out = append(out, Change{Key: k, Kind: KindRemoved, Old: ol.raw})
		}
	}
	return out
}

func secretChange(key string, ol leaf, had bool, nl leaf) (Change, bool) {
	oldSet := had && isSet(ol.n)
	newSet := isSet(nl.n)
	c := Change{Key: key, Secret: true}
	switch {
	case !oldSet && newSet:
		c.Kind = KindAdded
	case oldSet && !newSet:
		c.Kind = KindRemoved
	case oldSet && newSet && !sameValue(ol, nl):
		c.Kind = KindChanged
	default:
		return Change{}, false
	}
	return c, true
}

// leaves parses b into leaves; unusable input is an empty file.
func leaves(b []byte) leafSet {
	if len(b) > MaxBytes {
		return leafSet{leaves: map[string]leaf{}}
	}
	root, fe := parse(b)
	if fe != nil {
		return leafSet{leaves: map[string]leaf{}}
	}
	return flatten(b, root)
}
