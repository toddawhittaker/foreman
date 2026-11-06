// Package gamesettings holds the rules for the three settings files a world
// owns and for its admin list (SPEC 11): checking a raw edit, keeping secrets
// out of the editor, diffing a change, and working out who to promote or
// demote. It does no I/O and has no user-facing text; the web layer
// translates the codes below.
package gamesettings

import (
	"strconv"
	"strings"
)

// MaxBytes is the largest settings file Validate accepts (SPEC 14.6).
const MaxBytes = 1 << 20

// maxErrors stops a file full of wrong values from producing a page of errors.
const maxErrors = 50

// The closed set of FieldError codes.
//
//	syntax       The text is not JSON, or nests too deeply. Line is set. Key and Want are empty.
//	not_object   Valid JSON, but the top level is not an object. Line is set.
//	too_large    The input is over MaxBytes. Line is 0.
//	wrong_type   Key holds the wrong kind of value. Want names the allowed kinds
//	             ("boolean", "string", "integer", "number", "array", "object", "null"),
//	             joined by " or ".
//	out_of_range Key holds a right-typed but unacceptable value. Want is either a
//	             range written "lo..hi" (an empty side is unbounded) or the allowed
//	             values joined by "|".
const (
	CodeSyntax     = "syntax"
	CodeNotObject  = "not_object"
	CodeTooLarge   = "too_large"
	CodeWrongType  = "wrong_type"
	CodeOutOfRange = "out_of_range"
)

// FieldError is one problem in a settings file. It never holds a value from
// the file, so it is safe to show and to log.
type FieldError struct {
	Line int    // 1-based line of the problem; 0 when it has no place
	Key  string // dotted path such as "visibility.lan" or "tags[1]"
	Code string
	Want string
}

// Validate checks b, the text of file (one of the File constants) for a world
// on a game line (Line11, Line20 or Line21). It refuses input over MaxBytes,
// text that is not one JSON object, and known keys of the wrong type or
// outside their range. Unknown keys are allowed. For a file or line without a
// table only the first three checks apply. It returns at most 50 errors.
func Validate(file, line string, b []byte) []FieldError {
	if len(b) > MaxBytes {
		return []FieldError{{Code: CodeTooLarge}}
	}
	root, fe := parse(b)
	if fe != nil {
		return []FieldError{*fe}
	}
	sp, ok := schemaFor(file, line)
	if !ok {
		return nil
	}
	var out []FieldError
	check(root, sp, "", &out)
	return out
}

func check(n *node, sp *spec, path string, out *[]FieldError) {
	if len(*out) >= maxErrors {
		return
	}
	fail := func(code, want string) {
		*out = append(*out, FieldError{Line: n.line, Key: path, Code: code, Want: want})
	}
	if sp.types&typeBit(n.kind) == 0 {
		fail(CodeWrongType, sp.typeNames())
		return
	}
	switch n.kind {
	case kNumber:
		if sp.integer && strings.ContainsAny(n.num, ".eE") {
			fail(CodeWrongType, "integer")
			return
		}
		// A number that overflows float64 parses to ±Inf, which is out of range too.
		f, _ := strconv.ParseFloat(n.num, 64)
		if sp.ranged && (f < sp.lo || f > sp.hi) {
			fail(CodeOutOfRange, sp.rangeText())
		}
	case kString, kBool:
		if sp.enum != nil && !contains(sp.enum, n.str) {
			fail(CodeOutOfRange, strings.Join(sp.enum, "|"))
		}
	case kArray:
		if sp.elem == nil {
			return
		}
		for i, e := range n.elems {
			check(e, sp.elem, path+"["+strconv.Itoa(i)+"]", out)
		}
	case kObject:
		for _, m := range n.members {
			child := sp.fields[m.key]
			if child == nil {
				child = sp.elemFor()
			}
			if child != nil {
				check(m.node, child, joinPath(path, m.key), out)
			}
		}
	}
}

// elemFor returns the spec for a key the table does not list: only an object
// of uniform members has one. A table with fields leaves unknown keys alone.
func (s *spec) elemFor() *spec {
	if s.fields == nil {
		return s.elem
	}
	return nil
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (s *spec) typeNames() string {
	var names []string
	for _, k := range []kind{kBool, kString, kNumber, kArray, kObject, kNull} {
		if s.types&typeBit(k) == 0 {
			continue
		}
		switch {
		case k == kNumber && s.integer:
			names = append(names, "integer")
		default:
			names = append(names, kindNames[k])
		}
	}
	return strings.Join(names, " or ")
}

var kindNames = map[kind]string{
	kNull: "null", kBool: "boolean", kString: "string", kNumber: "number", kArray: "array", kObject: "object",
}

func (s *spec) rangeText() string {
	lo := strconv.FormatFloat(s.lo, 'f', -1, 64)
	hi := ""
	if s.hi < 1e300 {
		hi = strconv.FormatFloat(s.hi, 'f', -1, 64)
	}
	return lo + ".." + hi
}
