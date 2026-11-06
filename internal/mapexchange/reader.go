package mapexchange

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

// reader walks the decompressed bytes. The first failure sticks, so the
// decode functions can read straight through and check err once.
type reader struct {
	buf     []byte
	pos     int
	err     error
	lastPos Position
}

var errShort = errors.New("map exchange string ends early")

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.buf)-r.pos {
		r.fail(errShort)
		return nil
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *reader) u8() uint8 {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *reader) u16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (r *reader) i16() int16 { return int16(r.u16()) } //nolint:gosec // reinterprets the stored two's-complement bits

func (r *reader) u32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *reader) i32() int32 { return int32(r.u32()) } //nolint:gosec // reinterprets the stored two's-complement bits

// boolean accepts only 0 and 1; anything else means the layout is wrong.
func (r *reader) boolean(field string) bool {
	v := r.u8()
	if v > 1 {
		r.fail(rangeError(field))
	}
	return v == 1
}

func (r *reader) f32(field string) F32 {
	v := math.Float32frombits(r.u32())
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		r.fail(rangeError(field))
	}
	return F32(v)
}

func (r *reader) f64(field string) float64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(b))
	if math.IsNaN(v) || math.IsInf(v, 0) {
		r.fail(rangeError(field))
	}
	return v
}

// count reads the game's space-optimised length: one byte, or 0xff and a
// uint32. A count larger than the bytes left cannot be honest.
func (r *reader) count() int {
	n := uint32(r.u8())
	if n == 0xff {
		n = r.u32()
	}
	if r.err != nil {
		return 0
	}
	if int64(n) > int64(len(r.buf)-r.pos) {
		r.fail(errShort)
		return 0
	}
	return int(n)
}

func (r *reader) str(field string) string {
	b := r.take(r.count())
	if !utf8.Valid(b) {
		r.fail(rangeError(field))
	}
	return string(b)
}

// present reads the flag in front of an optional value.
func (r *reader) present(field string) bool { return r.boolean(field) }

func optF64(r *reader, field string) *float64 {
	if !r.present(field) {
		return nil
	}
	v := r.f64(field)
	return &v
}

func optU32(r *reader, field string) *uint32 {
	if !r.present(field) {
		return nil
	}
	v := r.u32()
	return &v
}

func optI32(r *reader, field string) *int32 {
	if !r.present(field) {
		return nil
	}
	v := r.i32()
	return &v
}

func optBool(r *reader, field string) *bool {
	if !r.present(field) {
		return nil
	}
	v := r.boolean(field)
	return &v
}

// position reads a map position, stored either as a delta from the last
// one or, after the 0x7fff marker, in full. Units are 1/256 of a tile.
func (r *reader) position() Position {
	dx := r.i16()
	var p Position
	if dx == 0x7fff {
		p.X = float64(r.i32()) / 256
		p.Y = float64(r.i32()) / 256
	} else {
		dy := r.i16()
		p.X = r.lastPos.X + float64(dx)/256
		p.Y = r.lastPos.Y + float64(dy)/256
	}
	r.lastPos = p
	return p
}

func rangeError(field string) error {
	return fmt.Errorf("map exchange string has a value out of range in %s", field)
}

// F32 is a game float. It is written with float32 precision so the JSON
// holds the value the game stored rather than its widened float64 form.
type F32 float32

func (f F32) MarshalJSON() ([]byte, error) {
	return strconv.AppendFloat(nil, float64(f), 'g', -1, 32), nil
}
