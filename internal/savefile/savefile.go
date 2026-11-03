// Package savefile reads a Factorio save's game version and mod list from
// the header of level.dat0 inside the save zip, without running the game
// (SPEC 9.9). The format is undocumented; the layout here was worked out
// from real saves made by Factorio 1.1, 2.0 and 2.1 (see testdata/README).
// A save that does not match it is refused, never guessed at.
package savefile

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Reasons a save cannot be read. Callers test them with errors.Is.
var (
	ErrNotZip      = errors.New("the save is not a zip file, or the file is cut short")
	ErrNoLevelData = errors.New("the save has no level.dat0 or level.dat inside it")
	ErrFormat      = errors.New("the save's header is in a format Foreman does not understand")
	ErrTruncated   = errors.New("the save's level data is cut short")
)

// UnsupportedVersionError reports a header from a game version whose layout
// Foreman has not been tested against: anything but 1.1, 2.0 and 2.1.
// Version is filled in so the caller can say which version made the save.
type UnsupportedVersionError struct {
	Version Version
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("the save was made by Factorio %s, which Foreman cannot read", e.Version)
}

// Version is a game or mod version. Build is zero for mods.
type Version struct {
	Major, Minor, Patch, Build uint16
}

// String gives major.minor.patch; the build number is left out because
// players and the mod portal never show it.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Mod is one mod the save was last played with, including base and the
// built-in DLC mods.
type Mod struct {
	Name    string
	Version Version
}

// Save is what the header says.
type Save struct {
	Version Version
	Mods    []Mod
}

// Limits on hostile input. Real headers are a few hundred bytes plus about
// 30 bytes a mod, so these are far above any real save.
const (
	maxHeader    = 1 << 20
	maxString    = 1024
	maxMods      = 10000
	extraBytesV2 = 6 // fields added after "allowed commands" in 2.x, not needed here
)

// Read reads the save zip in r, which is size bytes long. It takes an
// io.ReaderAt rather than a path so the caller decides how the file is
// opened and locked, and so tests and the fuzzer can pass bytes.
func Read(r io.ReaderAt, size int64) (Save, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return Save{}, ErrNotZip
	}
	f := levelFile(zr)
	if f == nil {
		return Save{}, ErrNoLevelData
	}
	rc, err := f.Open()
	if err != nil {
		return Save{}, ErrFormat
	}
	defer func() { _ = rc.Close() }()

	data, err := headerBytes(rc)
	if err != nil {
		return Save{}, err
	}
	return parse(data)
}

// levelFile finds level.dat0, or level.dat for saves that predate it, in
// the save's top folder.
func levelFile(zr *zip.Reader) *zip.File {
	var dat *zip.File
	for _, f := range zr.File {
		if strings.Count(f.Name, "/") > 1 {
			continue
		}
		switch path.Base(f.Name) {
		case "level.dat0":
			return f
		case "level.dat":
			if dat == nil {
				dat = f
			}
		}
	}
	return dat
}

// headerBytes returns at most maxHeader bytes of the level data, inflating
// it first when the game stored it zlib-compressed.
func headerBytes(rc io.Reader) ([]byte, error) {
	br := bufio.NewReader(rc)
	head, err := br.Peek(2)
	if err != nil {
		return nil, ErrTruncated
	}
	var src io.Reader = br
	// A zlib header: CM=8 and a valid check value. An uncompressed header
	// starts with the game's major version, which is never 0x78.
	if head[0] == 0x78 && (uint16(head[0])<<8|uint16(head[1]))%31 == 0 {
		zr, err := zlib.NewReader(br)
		if err != nil {
			return nil, ErrFormat
		}
		defer func() { _ = zr.Close() }()
		src = zr
	}
	data, err := io.ReadAll(io.LimitReader(src, maxHeader))
	if err != nil && len(data) == 0 {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrTruncated
		}
		return nil, ErrFormat
	}
	// Bytes read before a later error are still usable: the header is at
	// the start, and parse reports ErrTruncated if it runs out.
	return data, nil
}

// supported reports whether Foreman knows the header layout for v.
func supported(v Version) bool {
	return (v.Major == 1 && v.Minor == 1) || (v.Major == 2 && (v.Minor == 0 || v.Minor == 1))
}

func parse(data []byte) (Save, error) {
	p := &parser{buf: data, capped: len(data) >= maxHeader}
	var s Save
	s.Version = Version{p.u16(), p.u16(), p.u16(), p.u16()}
	if p.err != nil {
		return Save{}, p.err
	}
	if !supported(s.Version) {
		return Save{}, &UnsupportedVersionError{Version: s.Version}
	}
	p.skip(1)  // unknown flag
	p.str()    // campaign
	p.str()    // scenario name
	p.str()    // mod that holds the scenario
	p.skip(1)  // difficulty
	p.skip(2)  // finished, player won
	p.str()    // next level
	p.skip(4)  // can continue, finished but continuing, saving replay, allow non-admin debug options
	p.modVer() // version the save was loaded from
	p.skip(2)  // its build
	p.skip(1)  // allowed commands
	if s.Version.Major >= 2 {
		p.skip(extraBytesV2)
	}
	n := p.count()
	if p.err == nil && n > maxMods {
		return Save{}, ErrFormat
	}
	for i := uint32(0); i < n && p.err == nil; i++ {
		m := Mod{Name: p.str(), Version: p.modVer()}
		p.skip(4) // CRC
		if p.err == nil && !validName(m.Name) {
			return Save{}, ErrFormat
		}
		s.Mods = append(s.Mods, m)
	}
	if p.err != nil {
		return Save{}, p.err
	}
	// Every save has base first. If not, the layout above did not fit.
	if len(s.Mods) == 0 || s.Mods[0].Name != "base" {
		return Save{}, ErrFormat
	}
	return s, nil
}

// validName accepts the characters the mod portal allows in a mod name.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == ' ' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

// parser reads little-endian fields; after the first error every read
// returns zero and the error is kept.
type parser struct {
	buf    []byte
	capped bool
	err    error
}

func (p *parser) take(n int) []byte {
	if p.err != nil {
		return nil
	}
	if n > len(p.buf) {
		p.err = ErrTruncated
		if p.capped {
			// The data ran out only because headerBytes stopped reading.
			p.err = ErrFormat
		}
		return nil
	}
	b := p.buf[:n]
	p.buf = p.buf[n:]
	return b
}

func (p *parser) skip(n int) { p.take(n) }

func (p *parser) u8() uint8 {
	b := p.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (p *parser) u16() uint16 {
	b := p.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (p *parser) u32() uint32 {
	b := p.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

// count reads Factorio's space-optimised count: one byte, or 0xFF then a
// four-byte value.
func (p *parser) count() uint32 {
	n := uint32(p.u8())
	if n == 0xFF {
		n = p.u32()
	}
	return n
}

// short is the two-byte form of the same encoding, used in mod versions.
func (p *parser) short() uint16 {
	n := uint16(p.u8())
	if n == 0xFF {
		n = p.u16()
	}
	return n
}

func (p *parser) modVer() Version {
	return Version{Major: p.short(), Minor: p.short(), Patch: p.short()}
}

func (p *parser) str() string {
	n := p.count()
	if p.err != nil {
		return ""
	}
	if n > maxString {
		p.err = ErrFormat
		return ""
	}
	return string(bytes.Clone(p.take(int(n))))
}
