package modsettings

import (
	"encoding/binary"
	"errors"
	"math"
)

// Property tree node types in mod-settings.dat.
const (
	nodeNone   = 0
	nodeBool   = 1
	nodeDouble = 2
	nodeString = 3
	nodeList   = 4
	nodeDict   = 5
	nodeInt64  = 6
	nodeUint64 = 7
)

const (
	// headerBytes is four little-endian uint16 version numbers.
	headerBytes = 8
	// maxDepth caps nesting so a hostile file cannot exhaust the stack.
	maxDepth = 32
	// minPairBytes is the smallest list or dictionary entry: a key (just the
	// empty flag) and a node (type, any-type flag).
	minPairBytes = 3
	// maxExactFloat is the largest whole number a double holds exactly.
	maxExactFloat = 1 << 53
)

var (
	errTruncated = errors.New("modsettings: mod-settings.dat is cut short")
	errMalformed = errors.New("modsettings: mod-settings.dat is not a valid settings file")
	errTooDeep   = errors.New("modsettings: mod-settings.dat nests too deeply")
	errTrailing  = errors.New("modsettings: mod-settings.dat has data after the settings")
	errMismatch  = errors.New("modsettings: the value does not match the setting's type")
	errNoValue   = errors.New("modsettings: the setting has no value node")
	errBadSet    = errors.New("modsettings: not a setting type or name Foreman can set")
	errNotFinite = errors.New("modsettings: a number is not finite")
)

// pstr is a property tree string with the exact choices the file made, so it
// encodes to the bytes it was read from.
type pstr struct {
	// flag is the "empty" byte; any non-zero value means empty.
	flag byte
	// long records the 0xFF length form used for a length under 255.
	long bool
	data []byte
}

func (s pstr) text() string {
	if s.flag != 0 {
		return ""
	}
	return string(s.data)
}

func newPstr(text string) pstr {
	if text == "" {
		return pstr{flag: 1}
	}
	return pstr{data: []byte(text)}
}

// node is one property tree node. scalar holds the raw bool byte, the bits
// of a double, or an int64 or uint64.
type node struct {
	typ     byte
	anyType byte
	scalar  uint64
	str     pstr
	keys    []pstr
	vals    []*node
}

// child returns the first child of a list or dictionary under key.
func (n *node) child(key string) *node {
	if n == nil || (n.typ != nodeDict && n.typ != nodeList) {
		return nil
	}
	for i, k := range n.keys {
		if k.text() == key {
			return n.vals[i]
		}
	}
	return nil
}

func (n *node) clone() *node {
	c := *n
	c.str.data = append([]byte(nil), n.str.data...)
	c.keys = make([]pstr, len(n.keys))
	c.vals = make([]*node, len(n.vals))
	for i := range n.keys {
		c.keys[i] = n.keys[i]
		c.keys[i].data = append([]byte(nil), n.keys[i].data...)
		c.vals[i] = n.vals[i].clone()
	}
	return &c
}

// File is a decoded mod-settings.dat. It keeps the header, flag byte and the
// bytes of every entry it does not change, so Encode of an unchanged File
// returns the input byte for byte.
type File struct {
	header [headerBytes]byte
	flag   byte
	root   *node
}

type reader struct {
	b   []byte
	p   int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.b)-r.p {
		r.err = errTruncated
		return nil
	}
	s := r.b[r.p : r.p+n]
	r.p += n
	return s
}

func (r *reader) u8() byte {
	if s := r.take(1); s != nil {
		return s[0]
	}
	return 0
}

func (r *reader) u32() uint32 {
	if s := r.take(4); s != nil {
		return binary.LittleEndian.Uint32(s)
	}
	return 0
}

func (r *reader) u64() uint64 {
	if s := r.take(8); s != nil {
		return binary.LittleEndian.Uint64(s)
	}
	return 0
}

func (r *reader) str() pstr {
	if flag := r.u8(); flag != 0 {
		return pstr{flag: flag}
	}
	s := pstr{}
	n := uint32(r.u8())
	if n == 255 {
		n = r.u32()
		s.long = n < 255
	}
	if n > math.MaxInt32 {
		r.err = errTruncated
		return s
	}
	s.data = r.take(int(n))
	return s
}

func (r *reader) node(depth int) *node {
	if depth > maxDepth {
		r.err = errTooDeep
		return nil
	}
	n := &node{typ: r.u8(), anyType: r.u8()}
	if r.err != nil {
		return nil
	}
	switch n.typ {
	case nodeNone:
	case nodeBool:
		n.scalar = uint64(r.u8())
	case nodeDouble, nodeInt64, nodeUint64:
		n.scalar = r.u64()
	case nodeString:
		n.str = r.str()
	case nodeList, nodeDict:
		count := uint64(r.u32())
		if r.err == nil && count > uint64(len(r.b)-r.p)/minPairBytes { //nolint:gosec // p never passes len(b)
			r.err = errTruncated
		}
		if r.err != nil {
			return nil
		}
		n.keys = make([]pstr, 0, count)
		n.vals = make([]*node, 0, count)
		for i := uint64(0); i < count; i++ {
			k := r.str()
			v := r.node(depth + 1)
			if r.err != nil {
				return nil
			}
			n.keys = append(n.keys, k)
			n.vals = append(n.vals, v)
		}
	default:
		r.err = errMalformed
	}
	if r.err != nil {
		return nil
	}
	return n
}

// Decode parses mod-settings.dat bytes. It never panics and allocates no
// more than a small multiple of the input's size.
func Decode(data []byte) (*File, error) {
	if len(data) < headerBytes+1 {
		return nil, errTruncated
	}
	// Copy once so the file does not alias the caller's buffer.
	r := &reader{b: append([]byte(nil), data...)}
	f := &File{}
	copy(f.header[:], r.take(headerBytes))
	f.flag = r.u8()
	f.root = r.node(0)
	if r.err != nil {
		return nil, r.err
	}
	if f.root.typ != nodeDict {
		return nil, errMalformed
	}
	if r.p != len(r.b) {
		return nil, errTrailing
	}
	return f, nil
}

func appendStr(b []byte, s pstr) []byte {
	if s.flag != 0 {
		return append(b, s.flag)
	}
	b = append(b, 0)
	if len(s.data) < 255 && !s.long {
		b = append(b, byte(len(s.data))) //nolint:gosec // under 255
	} else {
		b = append(b, 255)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s.data))) //nolint:gosec // length checked in Set and Decode
	}
	return append(b, s.data...)
}

func appendNode(b []byte, n *node) []byte {
	b = append(b, n.typ, n.anyType)
	switch n.typ {
	case nodeBool:
		b = append(b, byte(n.scalar)) //nolint:gosec // the raw byte read
	case nodeDouble, nodeInt64, nodeUint64:
		b = binary.LittleEndian.AppendUint64(b, n.scalar)
	case nodeString:
		b = appendStr(b, n.str)
	case nodeList, nodeDict:
		b = binary.LittleEndian.AppendUint32(b, uint32(len(n.keys))) //nolint:gosec // bounded by the input size
		for i, k := range n.keys {
			b = appendStr(b, k)
			b = appendNode(b, n.vals[i])
		}
	}
	return b
}

// Encode writes the file back out, unchanged entries byte for byte.
func (f *File) Encode() ([]byte, error) {
	if f == nil || f.root == nil {
		return nil, errMalformed
	}
	b := make([]byte, 0, 1024)
	b = append(b, f.header[:]...)
	b = append(b, f.flag)
	return appendNode(b, f.root), nil
}

func validSetting(s SettingType) bool {
	return s == Startup || s == RuntimeGlobal || s == RuntimePerUser
}

// valueNode finds the node holding a setting's value; ok is false when the
// file lacks the setting. A setting without a value node is an error.
func (f *File) valueNode(setting SettingType, name string) (n *node, found bool, err error) {
	if f == nil || f.root == nil || !validSetting(setting) || name == "" {
		return nil, false, nil
	}
	entry := f.root.child(string(setting)).child(name)
	if entry == nil {
		return nil, false, nil
	}
	if entry.typ != nodeDict {
		return nil, true, errMalformed
	}
	v := entry.child("value")
	if v == nil {
		return nil, true, errNoValue
	}
	return v, true, nil
}

func nodeDoubleValue(n *node) (float64, bool) {
	if n == nil || n.typ != nodeDouble {
		return 0, false
	}
	return math.Float64frombits(n.scalar), true
}

// Get returns the value of one setting and whether the file holds it. A
// game before 2.0 stores whole numbers as doubles, so an int setting reads as
// TypeDouble there.
func (f *File) Get(setting SettingType, name string) (Value, bool, error) {
	n, found, err := f.valueNode(setting, name)
	if !found || err != nil {
		return Value{}, found, err
	}
	switch n.typ {
	case nodeBool:
		return Value{Type: TypeBool, Bool: n.scalar != 0}, true, nil
	case nodeDouble:
		return Value{Type: TypeDouble, Double: math.Float64frombits(n.scalar)}, true, nil
	case nodeInt64:
		return Value{Type: TypeInt, Int: int64(n.scalar)}, true, nil //nolint:gosec // same 64 bits
	case nodeString:
		return Value{Type: TypeString, String: n.str.text()}, true, nil
	case nodeDict:
		var c [4]float64
		for i, k := range [4]string{"r", "g", "b", "a"} {
			d, ok := nodeDoubleValue(n.child(k))
			if !ok {
				return Value{}, true, errMismatch
			}
			c[i] = d
		}
		return Value{Type: TypeColor, Color: Color{R: c[0], G: c[1], B: c[2], A: c[3]}}, true, nil
	}
	return Value{}, true, errMismatch
}

// emptyValueNode builds a value node of the right shape for v.
func emptyValueNode(t ValueType) (*node, error) {
	switch t {
	case TypeBool:
		return &node{typ: nodeBool}, nil
	case TypeInt:
		return &node{typ: nodeInt64}, nil
	case TypeDouble:
		return &node{typ: nodeDouble}, nil
	case TypeString:
		return &node{typ: nodeString, str: pstr{flag: 1}}, nil
	case TypeColor:
		n := &node{typ: nodeDict}
		for _, k := range [4]string{"r", "g", "b", "a"} {
			n.keys = append(n.keys, newPstr(k))
			n.vals = append(n.vals, &node{typ: nodeDouble})
		}
		return n, nil
	}
	return nil, errMismatch
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// setNode changes only the value inside n, keeping its any-type flag. It
// leaves n untouched when it returns an error.
func setNode(n *node, v Value) error {
	switch v.Type {
	case TypeBool:
		if n.typ != nodeBool {
			return errMismatch
		}
		n.scalar = 0
		if v.Bool {
			n.scalar = 1
		}
	case TypeInt:
		switch n.typ {
		case nodeInt64:
			n.scalar = uint64(v.Int) //nolint:gosec // same 64 bits
		case nodeDouble:
			// Games before 2.0 store whole numbers as doubles.
			if v.Int > maxExactFloat || v.Int < -maxExactFloat {
				return errMismatch
			}
			n.scalar = math.Float64bits(float64(v.Int))
		default:
			return errMismatch
		}
	case TypeDouble:
		if n.typ != nodeDouble {
			return errMismatch
		}
		if !finite(v.Double) {
			return errNotFinite
		}
		n.scalar = math.Float64bits(v.Double)
	case TypeString:
		if n.typ != nodeString {
			return errMismatch
		}
		if uint64(len(v.String)) > math.MaxInt32 {
			return errMismatch
		}
		n.str = newPstr(v.String)
	case TypeColor:
		if n.typ != nodeDict {
			return errMismatch
		}
		comps := [4]float64{v.Color.R, v.Color.G, v.Color.B, v.Color.A}
		var targets [4]*node
		for i, k := range [4]string{"r", "g", "b", "a"} {
			t := n.child(k)
			if t == nil || t.typ != nodeDouble {
				return errMismatch
			}
			if !finite(comps[i]) {
				return errNotFinite
			}
			targets[i] = t
		}
		for i, t := range targets {
			t.scalar = math.Float64bits(comps[i])
		}
	default:
		return errMismatch
	}
	return nil
}

// Set changes one setting's value. When the file lacks the setting, its node
// is copied from defaults (the file a definitions run wrote) and then set.
// Every other byte of the file stays as it was.
func (f *File) Set(setting SettingType, name string, v Value, defaults *File) error {
	if f == nil || f.root == nil || !validSetting(setting) || name == "" {
		return errBadSet
	}
	group := f.root.child(string(setting))
	if group != nil && group.typ != nodeDict {
		return errMalformed
	}
	if group != nil {
		if entry := group.child(name); entry != nil {
			if entry.typ != nodeDict {
				return errMalformed
			}
			val := entry.child("value")
			if val == nil {
				return errNoValue
			}
			return setNode(val, v)
		}
	}
	entry, err := newEntry(setting, name, v, defaults)
	if err != nil {
		return err
	}
	if group == nil {
		group = &node{typ: nodeDict}
		if dg := defaults.group(setting); dg != nil {
			group.anyType = dg.anyType
		}
		f.root.keys = append(f.root.keys, newPstr(string(setting)))
		f.root.vals = append(f.root.vals, group)
	}
	group.keys = append(group.keys, newPstr(name))
	group.vals = append(group.vals, entry)
	return nil
}

func (f *File) group(setting SettingType) *node {
	if f == nil || f.root == nil {
		return nil
	}
	g := f.root.child(string(setting))
	if g == nil || g.typ != nodeDict {
		return nil
	}
	return g
}

// newEntry builds the node for a setting the file lacks: a copy of the
// defaults file's node, else a plain {value: X}.
func newEntry(setting SettingType, name string, v Value, defaults *File) (*node, error) {
	var entry *node
	if d := defaults.group(setting).child(name); d != nil && d.typ == nodeDict && d.child("value") != nil {
		entry = d.clone()
	} else {
		val, err := emptyValueNode(v.Type)
		if err != nil {
			return nil, err
		}
		entry = &node{typ: nodeDict, keys: []pstr{newPstr("value")}, vals: []*node{val}}
	}
	if err := setNode(entry.child("value"), v); err != nil {
		return nil, err
	}
	return entry, nil
}
