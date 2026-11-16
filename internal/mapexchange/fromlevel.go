package mapexchange

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"maps"
	"slices"
)

// The save format is undocumented. In the level-init.dat of every save tested
// (1.1, 2.0 and 2.1; see internal/savefile/testdata), the map settings follow
// a copy of the map-gen settings soon after the header, in the exchange
// string's own binary form, and the first surface, nauvis, stores the
// map-gen settings the game puts in its string just after its name. Each
// candidate is read with the decoder's own field readers, and the result
// must decode in full, so a save that does not fit is refused, not guessed.
const (
	// headerWindow is how far past the header the settings may start.
	headerWindow = 4096
	// maxBlock bounds one settings block; real ones are a few kilobytes.
	maxBlock = 64 << 10
	// maxSurfaceTries bounds how many "nauvis" names are tried.
	maxSurfaceTries = 64
	// fsrMinBytes is the least one autoplace control can take: a one-byte
	// name length and three floats.
	fsrMinBytes = 13
	// settingMinBytes is the least one autoplace setting can take: a name
	// length, its flag and an empty dictionary.
	settingMinBytes = 3
)

// ErrNoSettings means the level data does not hold settings Foreman can read.
var ErrNoSettings = errors.New("the save's map settings could not be found")

// surfaceName is nauvis's name as the game stores it. On 2.x a zero byte
// sits between the name and the settings; on 1.1 none does.
var surfaceName = []byte("\x06nauvis")

// FromLevel makes the map exchange string for a save from its level-init.dat
// bytes (SPEC 5.6). from is where the save header ends, so the search starts
// past the mod list. Errors are *UnsupportedError or ErrNoSettings.
func FromLevel(level []byte, from int) (string, error) {
	r := &reader{buf: level}
	v := [4]uint16{r.u16(), r.u16(), r.u16(), r.u16()}
	if r.err != nil || from < r.pos || from > len(level) {
		return "", ErrNoSettings
	}
	line, ok := lineOf(v)
	if !ok {
		return "", &UnsupportedError{Version: v, Reason: linesReason()}
	}
	head, ms, ok := findMapSettings(level, from, line, v)
	if !ok {
		return "", ErrNoSettings
	}
	mgs, ok := findSurface(level, line, head)
	if !ok {
		return "", ErrNoSettings
	}
	raw := make([]byte, 0, 8+1+len(mgs)+len(ms)+4)
	raw = append(raw, level[:8]...)
	raw = append(raw, 0) // the byte after the version, as the game writes it
	raw = append(raw, mgs...)
	raw = append(raw, ms...)
	raw = binary.LittleEndian.AppendUint32(raw, crc32.ChecksumIEEE(raw))
	if _, err := decodeRaw(raw); err != nil {
		return "", ErrNoSettings
	}
	return wrap(raw), nil
}

// block returns level from pos, cut to maxBlock.
func block(level []byte, pos int) *reader {
	return &reader{buf: level[:min(len(level), pos+maxBlock)], pos: pos}
}

// plausible cheaply refuses a start whose first two counts (autoplace
// controls, then autoplace settings) cannot fit in the bytes left, so a
// hostile save cannot make many tries each read a long block.
func plausible(r *reader, line gameLine) bool {
	p := &reader{buf: r.buf, pos: r.pos}
	if line == line11 {
		p.take(8) // terrain_segmentation and water
	}
	n := p.count()
	if p.err != nil || n*fsrMinBytes > len(p.buf)-p.pos {
		return false
	}
	for range n {
		p.take(p.count()) // the control's name
		p.take(12)        // its frequency, size and richness
	}
	m := p.count()
	return p.err == nil && m*settingMinBytes <= len(p.buf)-p.pos
}

// findMapSettings finds the first map-gen block after the header that a map
// settings block follows, and returns both.
func findMapSettings(level []byte, from int, line gameLine, v [4]uint16) (MapGenSettings, []byte, bool) {
	end := min(len(level), from+headerWindow)
	for k := max(from, 1); k < end; k++ {
		if level[k-1] != 0 {
			continue
		}
		r := block(level, k)
		if !plausible(r, line) {
			continue
		}
		mgs := readMapGenSettings(r, line)
		msStart := r.pos
		readMapSettings(r, line, v)
		if r.err == nil {
			return mgs, level[msStart:r.pos], true
		}
	}
	return MapGenSettings{}, nil, false
}

// findSurface finds nauvis's map-gen block: one that reads in full right
// after the name and names the same seed and size as the header's copy.
func findSurface(level []byte, line gameLine, head MapGenSettings) ([]byte, bool) {
	pos := 0
	for range maxSurfaceTries {
		i := bytes.Index(level[pos:], surfaceName)
		if i < 0 {
			return nil, false
		}
		start := pos + i + len(surfaceName)
		pos = start
		if line != line11 {
			if start >= len(level) || level[start] != 0 {
				continue
			}
			start++
		}
		r := block(level, start)
		if !plausible(r, line) {
			continue
		}
		m := readMapGenSettings(r, line)
		if r.err == nil && sameMap(m, head, line) {
			return level[start:r.pos], true
		}
	}
	return nil, false
}

// sameMap reports whether a surface's map-gen settings describe the map the
// header's copy does. On 1.1 the header copy is complete, so the autoplace
// controls must match too; on 2.x the header copy has none, the surface's
// own being what the game's string carries.
func sameMap(s, head MapGenSettings, line gameLine) bool {
	if s.Seed != head.Seed || s.Width != head.Width || s.Height != head.Height ||
		s.StartingArea != head.StartingArea || s.PeacefulMode != head.PeacefulMode ||
		!slices.Equal(s.StartingPoints, head.StartingPoints) {
		return false
	}
	return line != line11 || maps.Equal(s.AutoplaceControls, head.AutoplaceControls)
}
