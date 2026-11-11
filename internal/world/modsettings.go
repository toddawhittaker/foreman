package world

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// maxModFolderDepth bounds how deep a folder mod is copied, so a crafted
// tree cannot run the copy away.
const maxModFolderDepth = 32

// Most files and bytes one LinkModsInto call places. Hard links add no
// bytes; copies do. A real mod pack is far below both.
const (
	maxLinkFiles = 100_000
	maxLinkBytes = 4 << 30
)

// linkLimitFiles and linkLimitBytes are the limits in force; tests lower them.
var (
	linkLimitFiles = maxLinkFiles
	linkLimitBytes = int64(maxLinkBytes)
)

// ErrModsTooBig is returned by LinkModsInto for a mods folder with more
// files, or more bytes to copy, than a definitions run takes.
var ErrModsTooBig = errors.New("the world's mods folder is too big to read its settings")

// linkBudget counts what one LinkModsInto call has placed.
type linkBudget struct {
	files int
	bytes int64
}

// take counts one more file of size bytes that must be copied (0 for a link).
func (b *linkBudget) take(size int64) error {
	b.files++
	b.bytes += size
	if b.files > linkLimitFiles || b.bytes > linkLimitBytes {
		return ErrModsTooBig
	}
	return nil
}

// isFactorioLocale reports whether code is one of the locale folders
// Foreman reads. Any other code is never used to build a path.
func isFactorioLocale(code string) bool {
	for _, c := range modsettings.FactorioLocales {
		if c == code {
			return true
		}
	}
	return false
}

// linkInto hard-links a file between worlds' roots. Tests replace it to fake
// a link that fails, as it does across filesystems.
var linkInto = linkFile

// ModSettingsKey returns the cache key for a world's mod setting
// definitions: a SHA-256 of the game version plus the mods fingerprint
// without mod-settings.dat, since setting values cannot change definitions
// (SPEC 9.6). Installing, removing, enabling or disabling a mod, or moving
// the game version, changes it.
func (m *Manager) ModSettingsKey(id string) (string, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return "", err
	}
	fp, err := fingerprintMods(root, true)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	// The Space Age flag picks the default mod list of a world with none.
	_, _ = fmt.Fprintf(h, "%q %t %s\n", meta.GameVersion, meta.SpaceAge, fp)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// LinkModsInto fills dst, a definitions run's work folder, with a mods/
// folder made from the world's: zips by hard link (copy when linking fails),
// folder mods and mod-list.json by copy, DefaultModList when the world has
// none. It never copies mod-settings.dat. A symlink or anything but a
// regular file or folder in the world's mods folder is refused, and every
// path written stays inside dst.
//
// Only new files are created in dst, never opened for writing again, so a
// write into dst cannot reach through a hard link to the world's file. The
// caller must keep it that way: the run reads dst/mods and writes elsewhere.
// Like Mods, it does not take the world's lock; the caller holds Lock(id)
// when it needs the folder to hold still.
func (m *Manager) LinkModsInto(id, dst string) error {
	src, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	meta, err := ReadMeta(src)
	if err != nil {
		return err
	}
	out, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if err := Mkdir(out, ModsDir); err != nil {
		return err
	}

	// The game can swap mods for a link to the world's own folder, which
	// would carry config/ into the work folder.
	entries, err := readModsDir(src)
	if err != nil {
		return err
	}
	var budget linkBudget
	haveList := false
	for _, e := range entries {
		name := e.Name()
		if name == ModSettingsFile {
			continue
		}
		p := path.Join(ModsDir, name)
		info, err := src.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if strings.HasPrefix(name, ".") {
				continue
			}
			if err := copyModFolder(src, out, p, 0, &budget); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			switch {
			case name == ModListFile:
				haveList = true
				b, err := ReadSmallFile(src, p, maxModList)
				if err != nil {
					return err
				}
				if err := budget.take(int64(len(b))); err != nil {
					return err
				}
				if err := WriteFileAtomic(out, p, b); err != nil {
					return err
				}
			case strings.HasSuffix(name, ".zip") && !strings.HasPrefix(name, "."):
				if err := linkOrCopy(src, out, p, &budget); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("%s is not a regular file or folder, so it is not used", p)
		}
	}
	if haveList {
		return nil
	}
	if !SupportedGameVersion(meta.GameVersion) {
		return fmt.Errorf("no mod list for game version %q", meta.GameVersion)
	}
	return WriteFileAtomic(out, path.Join(ModsDir, ModListFile), DefaultModList(meta.GameVersion, meta.SpaceAge))
}

// readModsDir lists the world's mods folder, which is empty when there is
// none. A link or file in its place is refused: os.Root follows a link that
// stays inside the world, and mods pointing at "." would show config/.
func readModsDir(root *os.Root) ([]fs.DirEntry, error) {
	info, err := root.Lstat(ModsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", ModsDir)
	}
	return fs.ReadDir(root.FS(), ModsDir)
}

// linkOrCopy puts the regular file name of src at the same place in out, as
// a hard link, or as a copy when the link fails (another filesystem, or one
// that allows no links).
func linkOrCopy(src, out *os.Root, name string, budget *linkBudget) error {
	if err := budget.take(0); err != nil {
		return err
	}
	if linkInto(src, out, name) == nil {
		// linkat does not follow a symlink swapped in after the check;
		// confirm what landed in dst is a regular file.
		if got, err := out.Lstat(name); err == nil && got.Mode().IsRegular() {
			return nil
		}
		_ = out.Remove(name)
		return fmt.Errorf("%s changed while it was being linked", name)
	}
	info, err := src.Lstat(name)
	if err != nil {
		return err
	}
	if err := budget.take(info.Size()); err != nil {
		return err
	}
	return copyFile(src, name, out, name)
}

// copyModFolder copies the folder dir of src to the same place in out. Only
// regular files and folders are copied; anything else is refused.
func copyModFolder(src, out *os.Root, dir string, depth int, budget *linkBudget) error {
	if depth > maxModFolderDepth {
		return fmt.Errorf("%s is nested too deep to be a mod", dir)
	}
	if err := mkdirAll(out, dir); err != nil {
		return err
	}
	entries, err := fs.ReadDir(src.FS(), dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := path.Join(dir, e.Name())
		info, err := src.Lstat(p)
		if err != nil {
			return err
		}
		size := int64(0)
		if info.Mode().IsRegular() {
			size = info.Size()
		}
		if err := budget.take(size); err != nil {
			return err
		}
		switch {
		case info.IsDir():
			err = copyModFolder(src, out, p, depth+1, budget)
		case info.Mode().IsRegular():
			err = copyFile(src, p, out, p)
		default:
			err = fmt.Errorf("%s is not a regular file or folder, so it is not used", p)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ModLocaleFiles returns the raw locale .cfg files of each enabled mod,
// keyed by mod name, then Factorio locale code, for the codes given. It
// enforces the per-file and per-language limits in package modsettings.
// Built-in mods have no zip and are skipped. A mod whose zip or folder
// cannot be read is left out; it never fails the call.
func (m *Manager) ModLocaleFiles(id string, codes []string) (map[string]map[string][][]byte, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return nil, err
	}
	list, err := readModList(root)
	if err != nil {
		return nil, err
	}
	var want []string
	for _, c := range codes {
		if isFactorioLocale(c) && !slices.Contains(want, c) {
			want = append(want, c)
		}
	}

	entries, err := readModsDir(root)
	if err != nil {
		return nil, err
	}

	// One source per mod name, a folder before a zip. The game loads a mod
	// that mod-list.json does not mention, so only a listed-off mod is left.
	builtin := BuiltinMods(meta.GameVersion)
	sources := map[string]string{} // mod name to its path in the world
	for name, file := range newestZips(root) {
		sources[name] = path.Join(ModsDir, file)
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			sources[modFromFileName(e.Name()).Name] = path.Join(ModsDir, e.Name())
		}
	}
	names := make([]string, 0, len(sources))
	for name := range sources {
		if !list.disabled[name] && !slices.Contains(builtin, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	lim := &localeLimit{used: map[string]int{}, full: map[string]bool{}}
	out := map[string]map[string][][]byte{}
	for _, name := range names {
		var files map[string][][]byte
		if strings.HasSuffix(sources[name], ".zip") {
			files = zipLocale(root, sources[name], want, lim)
		} else {
			files = folderLocale(root, sources[name], want, lim)
		}
		if len(files) > 0 {
			out[name] = files
		}
	}
	return out, nil
}

// localeLimit counts the .cfg bytes kept for each language across mods.
type localeLimit struct {
	used map[string]int
	full map[string]bool
}

// fits reports whether a file of n bytes may be added for code's language.
// A file over the per-file limit is skipped alone; once a file would pass
// the language's limit, that language takes nothing more.
func (l *localeLimit) fits(code string, n int) bool {
	switch {
	case l.full[code], n > modsettings.MaxCfgFileBytes:
		return false
	case l.used[code]+n > modsettings.MaxCfgLanguageBytes:
		l.full[code] = true
		return false
	}
	return true
}

// zipLocale reads the wanted locale files from a mod zip. The zip's one top
// folder is name_version or name; whichever it is, locale/<code>/ sits in
// it. A zip it cannot read gives nothing.
func zipLocale(root *os.Root, name string, codes []string, lim *localeLimit) map[string][][]byte {
	f, st, err := openRegular(root, name)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	if st.Size() > maxModZip || zipEntriesOver(f, st.Size(), maxModZipEntries) {
		return nil
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil || len(zr.File) > maxModZipEntries {
		return nil
	}
	files := slices.Clone(zr.File)
	slices.SortStableFunc(files, func(a, b *zip.File) int { return strings.Compare(a.Name, b.Name) })
	out := map[string][][]byte{}
	for _, zf := range files {
		parts := strings.Split(zf.Name, "/")
		if len(parts) != 4 || parts[0] == "" || parts[1] != "locale" || !slices.Contains(codes, parts[2]) ||
			!strings.HasSuffix(parts[3], ".cfg") || zf.FileInfo().IsDir() {
			continue
		}
		code := parts[2]
		if zf.UncompressedSize64 > uint64(modsettings.MaxCfgFileBytes) || !lim.fits(code, int(zf.UncompressedSize64)) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		// The declared size can lie; read no more than it, and treat more as
		// a broken entry.
		b, err := io.ReadAll(io.LimitReader(rc, int64(zf.UncompressedSize64)+1))
		_ = rc.Close()
		if err != nil || uint64(len(b)) != zf.UncompressedSize64 {
			continue
		}
		lim.used[code] += len(b)
		out[code] = append(out[code], b)
	}
	return out
}

// folderLocale reads the wanted locale files from a folder mod. It follows
// no link: each folder and file on the way is checked as it is reached.
func folderLocale(root *os.Root, dir string, codes []string, lim *localeLimit) map[string][][]byte {
	out := map[string][][]byte{}
	localeDir := path.Join(dir, "locale")
	if !isRealDir(root, localeDir) {
		return nil
	}
	for _, code := range codes {
		codeDir := path.Join(localeDir, code)
		if !isRealDir(root, codeDir) {
			continue
		}
		entries, err := fs.ReadDir(root.FS(), codeDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".cfg") {
				continue
			}
			p := path.Join(codeDir, e.Name())
			b, err := ReadSmallFile(root, p, modsettings.MaxCfgFileBytes)
			if err != nil || !lim.fits(code, len(b)) {
				continue
			}
			lim.used[code] += len(b)
			out[code] = append(out[code], b)
		}
	}
	return out
}

// isRealDir reports whether p inside root is a folder and not a link.
func isRealDir(root *os.Root, p string) bool {
	st, err := root.Lstat(p)
	return err == nil && st.IsDir()
}
