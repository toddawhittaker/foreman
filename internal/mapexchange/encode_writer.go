package mapexchange

import (
	"encoding/binary"
	"math"
	"sort"
)

// writer builds the bytes reader walks, in the same order and encoding.
type writer struct {
	buf     []byte
	lastPos Position
}

func (w *writer) u8(v uint8) { w.buf = append(w.buf, v) }

func (w *writer) u16(v uint16) { w.buf = binary.LittleEndian.AppendUint16(w.buf, v) }

func (w *writer) i16(v int16) { w.u16(uint16(v)) } //nolint:gosec // stores the two's-complement bits

func (w *writer) u32(v uint32) { w.buf = binary.LittleEndian.AppendUint32(w.buf, v) }

func (w *writer) i32(v int32) { w.u32(uint32(v)) } //nolint:gosec // stores the two's-complement bits

func (w *writer) boolean(v bool) {
	if v {
		w.u8(1)
	} else {
		w.u8(0)
	}
}

func (w *writer) f32(v F32) { w.u32(math.Float32bits(float32(v))) }

func (w *writer) f64(v float64) { w.buf = binary.LittleEndian.AppendUint64(w.buf, math.Float64bits(v)) }

// count writes the game's space-optimised length: one byte below 0xff,
// otherwise 0xff and a uint32.
func (w *writer) count(n int) {
	if n < 0xff {
		w.u8(uint8(n)) //nolint:gosec // n is below 0xff here
		return
	}
	w.u8(0xff)
	w.u32(uint32(n)) //nolint:gosec // lengths are bounded by the settings files
}

func (w *writer) str(s string) {
	w.count(len(s))
	w.buf = append(w.buf, s...)
}

func optF64W(w *writer, v *float64) {
	w.boolean(v != nil)
	if v != nil {
		w.f64(*v)
	}
}

func optU32W(w *writer, v *uint32) {
	w.boolean(v != nil)
	if v != nil {
		w.u32(*v)
	}
}

func optI32W(w *writer, v *int32) {
	w.boolean(v != nil)
	if v != nil {
		w.i32(*v)
	}
}

func optBoolW(w *writer, v *bool) {
	w.boolean(v != nil)
	if v != nil {
		w.boolean(*v)
	}
}

// position writes a delta from the last position when it fits in an int16
// and is not the full-form marker, and the full position otherwise, which is
// the choice the game's own strings show.
func (w *writer) position(p Position) {
	dx := math.Round((p.X - w.lastPos.X) * 256)
	dy := math.Round((p.Y - w.lastPos.Y) * 256)
	if dx >= math.MinInt16 && dx < 0x7fff && dy >= math.MinInt16 && dy <= math.MaxInt16 {
		w.i16(int16(dx))
		w.i16(int16(dy))
	} else {
		w.i16(0x7fff)
		w.i32(int32(math.Round(p.X * 256)))
		w.i32(int32(math.Round(p.Y * 256)))
	}
	w.lastPos = p
}

// sortedKeys gives a map's keys in byte order, the order the game writes.
func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
