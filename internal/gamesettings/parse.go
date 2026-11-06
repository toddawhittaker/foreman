package gamesettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
)

// maxDepth bounds nesting so a hostile file cannot exhaust the stack. Real
// settings files nest three levels deep.
const maxDepth = 32

type kind uint8

const (
	kNull kind = iota
	kBool
	kString
	kNumber
	kArray
	kObject
)

// node is one parsed JSON value. Offsets point into the input so callers
// can edit the text without re-encoding it (which would lose the file's
// own layout).
type node struct {
	kind kind
	line int // line of the value, or of its key when it is an object member
	end  int // offset just after the value
	// start is the offset of the opening brace; set only for objects.
	start   int
	str     string // kString
	num     string // kNumber, as written
	members []member
	elems   []*node
}

type member struct {
	key      string
	node     *node
	lead     int // offset just after the previous token
	keyStart int // offset of the key's opening quote
	valStart int
}

type parser struct {
	b   []byte
	dec *json.Decoder
	nl  []int // offsets of every newline
}

// parse reads b as one JSON object.
func parse(b []byte) (*node, *FieldError) {
	p := &parser{b: b}
	for i, c := range b {
		if c == '\n' {
			p.nl = append(p.nl, i)
		}
	}
	// Decode reports a syntax error with a reliable byte offset; the token
	// reader below then only sees text already known to be valid.
	dec := json.NewDecoder(bytes.NewReader(b))
	var whole json.RawMessage
	if err := dec.Decode(&whole); err != nil {
		return nil, p.syntax(err)
	}
	// Anything after the value means this is not one object.
	if rest := p.skip(int(dec.InputOffset()), 0); rest < len(b) {
		return nil, &FieldError{Line: p.lineAt(rest), Code: CodeSyntax}
	}
	open := p.skip(0, 0)
	if b[open] != '{' {
		return nil, &FieldError{Line: p.lineAt(open), Code: CodeNotObject}
	}
	p.dec = json.NewDecoder(bytes.NewReader(b))
	p.dec.UseNumber()
	if _, err := p.dec.Token(); err != nil {
		return nil, p.syntax(err)
	}
	return p.object(open, 1)
}

// lineAt returns the 1-based line of the byte at off.
func (p *parser) lineAt(off int) int {
	if off < 0 {
		off = 0
	}
	return sort.SearchInts(p.nl, off) + 1
}

func (p *parser) syntax(err error) *FieldError {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return &FieldError{Line: p.lineAt(int(se.Offset) - 1), Code: CodeSyntax}
	}
	// The input ended early: point at its last visible byte.
	return &FieldError{Line: p.lineAt(len(bytes.TrimRight(p.b, " \t\r\n")) - 1), Code: CodeSyntax}
}

func (p *parser) object(start, depth int) (*node, *FieldError) {
	n := &node{kind: kObject, start: start, line: p.lineAt(start)}
	if depth > maxDepth {
		return nil, &FieldError{Line: n.line, Code: CodeSyntax}
	}
	prev := start + 1
	for p.dec.More() {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.syntax(err)
		}
		key, _ := tok.(string)
		keyEnd := int(p.dec.InputOffset())
		keyLine := p.lineAt(keyEnd - 1)
		keyStart := p.skip(prev, ',')
		valStart := p.skip(keyEnd, ':')
		val, fe := p.value(depth)
		if fe != nil {
			return nil, fe
		}
		val.line = keyLine
		n.members = append(n.members, member{key: key, node: val, lead: prev, keyStart: keyStart, valStart: valStart})
		prev = val.end
	}
	if _, err := p.dec.Token(); err != nil {
		return nil, p.syntax(err)
	}
	n.end = int(p.dec.InputOffset())
	return n, nil
}

// skip moves past spaces and then, when sep is not 0, one separator.
func (p *parser) skip(off int, sep byte) int {
	for off < len(p.b) && isSpace(p.b[off]) {
		off++
	}
	if sep != 0 && off < len(p.b) && p.b[off] == sep {
		off++
	}
	for off < len(p.b) && isSpace(p.b[off]) {
		off++
	}
	return off
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// value reads the next value, which the caller knows the decoder is at.
func (p *parser) value(depth int) (*node, *FieldError) {
	tok, err := p.dec.Token()
	if err != nil {
		return nil, p.syntax(err)
	}
	end := int(p.dec.InputOffset())
	n := &node{end: end, line: p.lineAt(end - 1)}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			return p.object(end-1, depth+1)
		}
		return p.array(end-1, depth+1)
	case string:
		n.kind, n.str = kString, t
	case json.Number:
		n.kind, n.num = kNumber, t.String()
	case bool:
		n.kind = kBool
		if t {
			n.str = "true"
		} else {
			n.str = "false"
		}
	default:
		n.kind = kNull
	}
	return n, nil
}

func (p *parser) array(start, depth int) (*node, *FieldError) {
	n := &node{kind: kArray, line: p.lineAt(start)}
	if depth > maxDepth {
		return nil, &FieldError{Line: n.line, Code: CodeSyntax}
	}
	for p.dec.More() {
		el, fe := p.value(depth)
		if fe != nil {
			return nil, fe
		}
		n.elems = append(n.elems, el)
	}
	if _, err := p.dec.Token(); err != nil {
		return nil, p.syntax(err)
	}
	n.end = int(p.dec.InputOffset())
	return n, nil
}
