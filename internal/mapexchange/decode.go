// Package mapexchange decodes a Factorio map exchange string into the
// map-gen-settings.json and map-settings.json files the game reads when it
// creates a map (SPEC 5.6).
//
// The binary layout is ported from rfvgyhn/factorio-exchange-string-parser
// (exchangeString.mjs, MIT licence, copyright 2024 rfvgyhn); see NOTICE.
package mapexchange

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"slices"
	"strings"
	"unicode"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// Size limits from SPEC 14.6: the pasted text, and the bytes it inflates to.
const (
	MaxInputBytes        = 64 << 10
	MaxDecompressedBytes = 1 << 20
)

// Result is a decoded string: the game version that wrote it and the two
// settings files, as JSON.
type Result struct {
	Version        [4]uint16
	MapGenSettings []byte
	MapSettings    []byte
}

// UnsupportedError means the string is well formed but from a game line
// this decoder does not know.
type UnsupportedError struct {
	Version [4]uint16
	Reason  string
	// From and Until bound the world game versions that could use the
	// string: From is the string's own version, and Until is the first
	// version with a different map-settings layout, or "" when none is
	// known. Both are empty for a string that did not decode.
	From, Until string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("map exchange string from game %d.%d.%d is not supported: %s",
		e.Version[0], e.Version[1], e.Version[2], e.Reason)
}

var (
	errTooLong   = errors.New("map exchange string is too long")
	errEncoding  = errors.New("map exchange string is not valid base64 or zlib data")
	errTooBig    = errors.New("map exchange string inflates past the size limit")
	errTrailing  = errors.New("map exchange string has unexpected bytes after the settings")
	errChecksum  = errors.New("map exchange string checksum does not match")
	errDuplicate = errors.New("map exchange string repeats a name")
	errMarkers   = errors.New("map exchange string has only one of its >>> and <<< markers")
)

// Decode parses a map exchange string. It takes both its >>> and <<< markers
// or neither, and refuses exactly one, which is a cut-off paste. It refuses anything it cannot read exactly: a newer patch on a
// known line is accepted only because every byte, the checksum and every
// range check still pass. Error text never quotes the input.
func Decode(s string) (Result, error) {
	if len(s) > MaxInputBytes {
		return Result{}, errTooLong
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	start, end := strings.HasPrefix(s, ">>>"), strings.HasSuffix(s, "<<<")
	if start != end {
		return Result{}, errMarkers
	}
	if start {
		s = strings.TrimSuffix(strings.TrimPrefix(s, ">>>"), "<<<")
	}

	compressed, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(compressed) == 0 {
		return Result{}, errEncoding
	}
	raw, err := inflate(compressed)
	if err != nil {
		return Result{}, err
	}
	return decodeRaw(raw)
}

// decodeRaw reads the inflated bytes: the settings, then a CRC-32 of them.
func decodeRaw(raw []byte) (Result, error) {
	if len(raw) < 4 {
		return Result{}, errShort
	}

	body := raw[:len(raw)-4]
	if crc32.ChecksumIEEE(body) != binary.LittleEndian.Uint32(raw[len(body):]) {
		return Result{}, errChecksum
	}
	r := &reader{buf: body}
	v := [4]uint16{r.u16(), r.u16(), r.u16(), r.u16()}
	if r.err != nil {
		return Result{}, r.err
	}
	line, ok := lineOf(v)
	if !ok {
		return Result{}, &UnsupportedError{Version: v, Reason: linesReason()}
	}
	r.u8() // a byte the game writes after the version; its meaning is unknown
	mgs := readMapGenSettings(r, line)
	ms := readMapSettings(r, line, v)
	if r.err != nil {
		return Result{}, r.err
	}
	if r.pos != len(r.buf) {
		return Result{}, errTrailing
	}
	res := Result{Version: v}
	var err error
	if res.MapGenSettings, err = json.MarshalIndent(mgs, "", "  "); err != nil {
		return Result{}, err
	}
	if res.MapSettings, err = json.MarshalIndent(ms, "", "  "); err != nil {
		return Result{}, err
	}
	return res, nil
}

func inflate(compressed []byte) ([]byte, error) {
	br := bytes.NewReader(compressed)
	zr, err := zlib.NewReader(br)
	if err != nil {
		return nil, errEncoding
	}
	defer func() { _ = zr.Close() }()
	raw, err := io.ReadAll(io.LimitReader(zr, MaxDecompressedBytes+1))
	if err != nil {
		return nil, errEncoding
	}
	if len(raw) > MaxDecompressedBytes {
		return nil, errTooBig
	}
	// Anything after the zlib stream is input this decoder did not read.
	if br.Len() != 0 {
		return nil, errTrailing
	}
	return raw, nil
}

// gameLine is a supported major.minor line of the game: its position in
// mapgen.Lines, the one list of supported lines. The decoder still needs code
// for each line's layout, so a new line is not read until it has a constant
// here and a case in the field readers.
type gameLine int

const (
	line11 gameLine = iota
	line20
	line21
)

// lineOf returns the supported line of a game version.
func lineOf(v [4]uint16) (gameLine, bool) {
	l, ok := mapgen.LineOf(fmt.Sprintf("%d.%d", v[0], v[1]))
	if !ok {
		return 0, false
	}
	return gameLine(slices.Index(mapgen.Lines, l.Name)), true
}

// linesReason says which game lines the decoder accepts.
func linesReason() string {
	n := len(mapgen.Lines)
	return "only game lines " + strings.Join(mapgen.Lines[:n-1], ", ") + " and " + mapgen.Lines[n-1] + " are supported"
}
