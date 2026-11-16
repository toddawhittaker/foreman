package savefile

import (
	"archive/zip"
	"bufio"
	"compress/zlib"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/toddawhittaker/foreman/internal/mapexchange"
)

// levelInitName is the file holding the map as the game created it.
const levelInitName = "level-init.dat"

// maxLevelInit bounds how much of level-init.dat is read. The test saves'
// are under 600 KiB; the map settings sit near the start and nauvis's
// map-gen settings follow the prototype data.
const maxLevelInit = 32 << 20

// Reasons ExchangeString gives no string, beside Read's. Callers test them
// with errors.Is.
var (
	ErrNoLevelInit   = errors.New("the save has no level-init.dat inside it")
	ErrTooLarge      = errors.New("the save's level-init.dat is larger than Foreman reads")
	ErrNoMapSettings = mapexchange.ErrNoSettings
)

// ExchangeString returns the map exchange string the game gives for the
// save in r (size bytes long), read from its level-init.dat without running
// the game (SPEC 5.6). The version in the string is the save's own, so it
// names the game that made the map. Errors are ErrNotZip, ErrNoLevelInit,
// ErrTooLarge, ErrFormat, ErrTruncated, ErrNoMapSettings (which is
// mapexchange.ErrNoSettings) or an *UnsupportedVersionError. Each call may
// hold up to maxLevelInit bytes, so callers should not run many at once.
func ExchangeString(r io.ReaderAt, size int64) (string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", ErrNotZip
	}
	f := topFile(zr, levelInitName)
	if f == nil {
		return "", ErrNoLevelInit
	}
	data, err := readLevelInit(f)
	if err != nil {
		return "", err
	}
	_, end, err := parseHeader(data, false)
	if err != nil {
		return "", err
	}
	return mapexchange.FromLevel(data, end)
}

// topFile finds a file by name in the save's top folder.
func topFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if strings.Count(f.Name, "/") <= 1 && path.Base(f.Name) == name {
			return f
		}
	}
	return nil
}

// readLevelInit reads the whole file, inflating it when it is stored
// zlib-compressed, and refuses one over maxLevelInit.
func readLevelInit(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, ErrFormat
	}
	defer func() { _ = rc.Close() }()
	br := bufio.NewReader(rc)
	head, err := br.Peek(2)
	if err != nil {
		return nil, ErrTruncated
	}
	var src io.Reader = br
	if isZlib(head) {
		z, err := zlib.NewReader(br)
		if err != nil {
			return nil, ErrFormat
		}
		defer func() { _ = z.Close() }()
		src = z
	}
	data, err := io.ReadAll(io.LimitReader(src, maxLevelInit+1))
	switch {
	case err != nil:
		return nil, ErrFormat
	case len(data) > maxLevelInit:
		return nil, ErrTooLarge
	}
	return data, nil
}
