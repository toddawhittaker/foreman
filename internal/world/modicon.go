package world

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"io/fs"
	"path"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// Limits for reading one icon out of a mod zip (SPEC 14.6).
const (
	// maxModIconBytes is the most bytes read of one icon entry, compressed
	// and declared and actually read.
	maxModIconBytes = 2 << 20
	// maxModIconSide is the largest width or height of a PNG that is decoded:
	// at 4096 by 4096 a 16-bit picture decodes to 128 MiB at worst. Planet
	// starmap art can be 2048 wide, so the side stays; the icon endpoint
	// decodes one picture at a time to bound the total.
	maxModIconSide = 4096
	// maxModIconOut is the largest picture ModIcon makes.
	maxModIconOut = 1024
)

var (
	// ErrModIconNotFound means the world, the mod at that version, or the
	// file in its zip is not there. A caller answers "not found".
	ErrModIconNotFound = errors.New("the mod icon is not there")
	// ErrModIconRefused means the request or the file broke a rule: a path
	// that is not allowed, a folder mod, a file over a limit, or bytes that
	// are not a usable PNG. A caller answers "refused" and shows the name.
	ErrModIconRefused = errors.New("the mod icon is refused")
)

// refuseIcon builds an error that is ErrModIconRefused. The reason is one
// of Foreman's own words, never text from a mod.
func refuseIcon(reason string) error {
	return fmt.Errorf("%w: %s", ErrModIconRefused, reason)
}

// ModIcon reads one icon from an installed mod zip without unpacking it.
// inZipPath is the Factorio path, "__mod__/dir/file.png". The zip is the
// one the world's mods folder holds for exactly this mod name and version.
// The first picture of a mipmap strip is the top-left size by size square;
// it is scaled to out by out pixels and written as a fresh PNG, so no byte of
// the zip reaches the caller (SPEC 14.5, 14.6). A folder mod is refused.
// Errors are ErrModIconNotFound or ErrModIconRefused, or an unexpected
// failure of the disk.
func (m *Manager) ModIcon(id, mod, version, inZipPath string, size, out int) ([]byte, error) {
	if size < 1 || size > maxModIconSide || out < 1 || out > maxModIconOut {
		return nil, refuseIcon("the size is not allowed")
	}
	rest, ok := modIconPath(mod, inZipPath)
	if !ok {
		return nil, refuseIcon("the path is not allowed")
	}
	mods, err := m.Mods(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrModIconNotFound
		}
		return nil, err
	}
	var found *InstalledMod
	for i := range mods {
		if mods[i].BuiltIn || mods[i].Name != mod || mods[i].Version != version {
			continue
		}
		if mods[i].Local || mods[i].File == "" {
			return nil, refuseIcon("a mod folder is not read")
		}
		found = &mods[i]
		break
	}
	if found == nil {
		return nil, ErrModIconNotFound
	}
	raw, err := m.readZipIcon(id, path.Join(ModsDir, found.File), rest)
	if err != nil {
		return nil, err
	}
	return cropScalePNG(raw, size, out)
}

// modIconPath checks an icon path again, as the layer that made it did:
// "__<mod>__/", then a relative path of plain names ending in ".png". It
// returns the part after the prefix.
func modIconPath(mod, p string) (string, bool) {
	if !ValidModName(mod) {
		return "", false
	}
	rest, ok := strings.CutPrefix(p, "__"+mod+"__/")
	if !ok || rest == "" || !strings.HasSuffix(rest, ".png") {
		return "", false
	}
	if strings.Contains(rest, `\`) || strings.HasPrefix(rest, "/") {
		return "", false
	}
	for _, r := range rest {
		if r < ' ' || r == 0x7f {
			return "", false
		}
	}
	for part := range strings.SplitSeq(rest, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return rest, true
}

// readZipIcon reads the entry rest, under the zip's one top folder, from the
// zip at name inside the world. The zip's size and entry count are checked
// before its directory is built, and no more than maxModIconBytes is read.
func (m *Manager) readZipIcon(id, name, rest string) ([]byte, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrModIconNotFound
		}
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, st, err := openRegular(root, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrModIconNotFound
		}
		return nil, refuseIcon("the mod zip is not a regular file")
	}
	defer func() { _ = f.Close() }()
	if st.Size() > MaxModZip || zipEntriesOver(f, st.Size(), maxModZipEntries) {
		return nil, refuseIcon("the mod zip is too big")
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil || len(zr.File) > maxModZipEntries {
		return nil, refuseIcon("the mod zip cannot be read")
	}
	// The mod's one top folder holds info.json, as in zipLocale and
	// readZipInfo; its name is name_version or name.
	top := ""
	for _, zf := range zr.File {
		if dir, file, ok := strings.Cut(zf.Name, "/"); ok && dir != "" && file == "info.json" {
			top = dir
			break
		}
	}
	if top == "" {
		return nil, refuseIcon("the mod zip has no top folder")
	}
	want := top + "/" + rest
	for _, zf := range zr.File {
		if zf.Name != want {
			continue
		}
		return readIconEntry(zf)
	}
	return nil, ErrModIconNotFound
}

// readIconEntry reads one zip entry. The sizes the zip declares can lie, so
// both are checked first and the read is capped at the declared size too.
func readIconEntry(zf *zip.File) ([]byte, error) {
	if zf.FileInfo().IsDir() {
		return nil, ErrModIconNotFound
	}
	if zf.CompressedSize64 > maxModIconBytes || zf.UncompressedSize64 > maxModIconBytes {
		return nil, refuseIcon("the file is too big")
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, refuseIcon("the file cannot be read")
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, int64(zf.UncompressedSize64)+1)) //nolint:gosec // checked against maxModIconBytes above
	if err != nil || uint64(len(b)) != zf.UncompressedSize64 {
		return nil, refuseIcon("the file is not the size the zip says")
	}
	return b, nil
}

// cropScalePNG decodes a PNG, takes its top-left size by size square (the
// first mipmap), scales it to out by out and encodes a new PNG. The header
// is read first, so a small file that claims a huge picture is refused
// before anything is allocated for it.
func cropScalePNG(raw []byte, size, out int) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, refuseIcon("the file is not a PNG")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxModIconSide || cfg.Height > maxModIconSide {
		return nil, refuseIcon("the picture is too big")
	}
	if cfg.Width < size || cfg.Height < size {
		return nil, refuseIcon("the picture is smaller than the icon size")
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, refuseIcon("the file is not a usable PNG")
	}
	b := img.Bounds()
	if b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		return nil, refuseIcon("the file is not a usable PNG")
	}
	dst := image.NewNRGBA(image.Rect(0, 0, out, out))
	src := image.Rect(b.Min.X, b.Min.Y, b.Min.X+size, b.Min.Y+size)
	if size == out {
		draw.Draw(dst, dst.Bounds(), img, src.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, src, xdraw.Src, nil)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
