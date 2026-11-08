package world

import (
	"archive/zip"
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxInfoJSON caps what is read from a mod's info.json.
	maxInfoJSON = 64 << 10
	// maxModZip is the largest mod zip opened (SPEC 14.6).
	maxModZip = 200 << 20
	// maxModZipEntries is the most files a mod zip may list before it is
	// left unread.
	maxModZipEntries = 10000
	// listedMods is how many mod names the mod version problem names.
	listedMods = 5
	// maxShownModName is the longest mod name shown, in characters.
	maxShownModName = 100
)

// modGameVersions returns, for each mod the game would load that has a zip
// in the world's mods folder, the Factorio version its info.json declares,
// as major.minor. The game loads a zip that mod-list.json does not mention
// and turns it on, so only a mod listed as off is left out. A mod whose zip
// or info.json cannot be read, or that names no version, is left out too:
// no information is not a problem.
func modGameVersions(root *os.Root, mods modList, running string) map[string]string {
	builtin := BuiltinMods(running)
	out := map[string]string{}
	for name, file := range newestZips(root) {
		if mods.disabled[name] || slices.Contains(builtin, name) {
			continue
		}
		if v := gameMinor(zipFactorioVersion(root, path.Join(ModsDir, file))); v != "" {
			out[name] = v
		}
	}
	return out
}

// newestZips maps each mod name to the file name of its highest-version
// zip in the mods folder.
func newestZips(root *os.Root) map[string]string {
	entries, err := fs.ReadDir(root.FS(), ModsDir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	versions := map[string]string{}
	for _, e := range entries {
		file := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(file, ".zip") {
			continue
		}
		base := strings.TrimSuffix(file, ".zip")
		i := strings.LastIndexByte(base, '_')
		if i <= 0 {
			continue
		}
		name, version := base[:i], base[i+1:]
		if _, seen := out[name]; !seen || CompareVersions(version, versions[name]) > 0 {
			out[name], versions[name] = file, version
		}
	}
	return out
}

// CompareVersions compares two dotted numbers such as "1.10.2", as -1, 0 or
// 1; a missing part, or one that is not a number, counts as 0.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y uint64
		if i < len(pa) {
			x, _ = strconv.ParseUint(pa[i], 10, 32)
		}
		if i < len(pb) {
			y, _ = strconv.ParseUint(pb[i], 10, 32)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// zipFactorioVersion reads "factorio_version" from the info.json inside a
// mod's zip: the zip holds one top-level folder, and the file is in it.
// It returns "" when anything is missing or unreadable, and for a zip over
// the size or entry limit.
func zipFactorioVersion(root *os.Root, name string) string {
	info, ok := zipModInfo(root, name)
	if !ok {
		return ""
	}
	return info.FactorioVersion
}

// ModInfo is what a mod's info.json says about it.
type ModInfo struct {
	Name, Version, FactorioVersion string
	// Dependencies are the raw strings, such as "? other >= 1.2".
	Dependencies []string
}

// zipInfoCache holds what each installed zip's info.json said, so listing a
// world's mods on every page view does not re-open every zip. One entry per
// path; a changed size or modification time reads the zip again.
var zipInfoCache = struct {
	sync.Mutex
	m map[string]cachedZipInfo
}{m: map[string]cachedZipInfo{}}

type cachedZipInfo struct {
	size  int64
	mtime time.Time
	info  ModInfo
	ok    bool
}

// zipModInfo reads the info.json of the mod zip name inside root.
func zipModInfo(root *os.Root, name string) (ModInfo, bool) {
	key := filepath.Join(root.Name(), name)
	if st, err := root.Lstat(name); err == nil && st.Mode().IsRegular() {
		zipInfoCache.Lock()
		c, hit := zipInfoCache.m[key]
		zipInfoCache.Unlock()
		if hit && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
			return cloneModInfo(c.info), c.ok
		}
	}
	f, st, err := openRegular(root, name)
	if err != nil {
		return ModInfo{}, false
	}
	defer func() { _ = f.Close() }()
	info, ok := readZipInfo(f, st.Size())
	zipInfoCache.Lock()
	zipInfoCache.m[key] = cachedZipInfo{size: st.Size(), mtime: st.ModTime(), info: info, ok: ok}
	zipInfoCache.Unlock()
	return cloneModInfo(info), ok
}

// cloneModInfo copies info so a caller cannot change the cached slice.
func cloneModInfo(info ModInfo) ModInfo {
	info.Dependencies = slices.Clone(info.Dependencies)
	return info
}

// readZipInfo reads info.json from a mod zip: the zip holds one top-level
// folder, and the file is in it. ok is false when anything is missing or
// unreadable, and for a zip over the size or entry limit.
func readZipInfo(r io.ReaderAt, size int64) (ModInfo, bool) {
	if size > maxModZip || zipEntriesOver(r, size, maxModZipEntries) {
		return ModInfo{}, false
	}
	zr, err := zip.NewReader(r, size)
	if err != nil || len(zr.File) > maxModZipEntries {
		return ModInfo{}, false
	}
	for _, zf := range zr.File {
		dir, file, found := strings.Cut(zf.Name, "/")
		if !found || dir == "" || file != "info.json" {
			continue
		}
		return readInfoJSON(zf)
	}
	return ModInfo{}, false
}

// Zip record signatures and sizes (PKWARE APPNOTE 4.3).
const (
	zipDirHeaderSig = 0x02014b50
	zipDirHeaderLen = 46
	zipEndSig       = 0x06054b50
	zipEndLen       = 22
	zip64LocatorSig = 0x07064b50
	zip64LocatorLen = 20
	zip64EndSig     = 0x06064b50
	zip64EndLen     = 56
)

// zipEntriesOver reports whether a zip's central directory holds more than
// limit entries, reading only the fixed part of each header. zip.NewReader
// builds every entry before anyone can count them, and it reads headers
// until one is bad, whatever count the end record gives, so a crafted zip
// of millions of tiny entries would cost that much memory on every page that
// lists mods. The end record is found and the directory start worked out by
// the same steps as readDirectoryEnd in Go's archive/zip, and headers are
// counted from every place it may start reading, so the count is never
// below its own. A zip it would refuse before reading headers is not over.
func zipEntriesOver(r io.ReaderAt, size int64, limit int) bool {
	rec, endAt, ok := zipEndRecord(r, size)
	if !ok {
		return false
	}
	entries := uint64(binary.LittleEndian.Uint16(rec[10:]))
	dirSize := uint64(binary.LittleEndian.Uint32(rec[12:]))
	dirOffset := uint64(binary.LittleEndian.Uint32(rec[16:]))
	if entries == 0xffff || dirSize == 0xffffffff || dirOffset == 0xffffffff {
		at, found, ok := zip64Locator(r, endAt)
		if !ok {
			return false
		}
		if found {
			if entries, dirSize, dirOffset, ok = zip64End(r, at); !ok {
				return false
			}
			endAt = at
		}
	}
	if entries > uint64(limit) { //nolint:gosec // limit is a small positive constant
		return true
	}
	if dirSize > math.MaxInt64 || dirOffset > math.MaxInt64 {
		return false
	}
	// archive/zip starts at base+offset, which is endAt-dirSize, and falls
	// back to the offset alone when base is positive and a header is there.
	off := int64(dirOffset)              //nolint:gosec // checked above
	base := endAt - int64(dirSize) - off //nolint:gosec // checked above
	start := base + off
	if start < 0 || start >= size {
		return false
	}
	if countDirHeaders(r, size, start, limit) > limit {
		return true
	}
	return base > 0 && countDirHeaders(r, size, off, limit) > limit
}

// zipEndRecord finds the end of central directory record as archive/zip
// does: the last signature in the final 1 KiB, else in the final 65 KiB,
// whose comment fits; a signature whose comment does not fit ends that
// search. It returns the record and where it starts.
func zipEndRecord(r io.ReaderAt, size int64) (rec []byte, at int64, ok bool) {
	for _, n := range []int64{1024, 65 * 1024} {
		n = min(n, size)
		buf := make([]byte, n)
		if _, err := r.ReadAt(buf, size-n); err != nil && err != io.EOF {
			return nil, 0, false
		}
		if p := zipEndInBlock(buf); p >= 0 {
			return buf[p:], size - n + int64(p), true
		}
		if n == size {
			break
		}
	}
	return nil, 0, false
}

// zipEndInBlock is archive/zip's findSignatureInBlock.
func zipEndInBlock(b []byte) int {
	for i := len(b) - zipEndLen; i >= 0; i-- {
		if binary.LittleEndian.Uint32(b[i:]) == zipEndSig {
			if i+zipEndLen+int(binary.LittleEndian.Uint16(b[i+20:])) > len(b) {
				return -1
			}
			return i
		}
	}
	return -1
}

// zip64Locator reads the zip64 locator just before the end record at endAt,
// as archive/zip's findDirectory64End does. found is false when there is no
// valid locator; ok is false when archive/zip would refuse the zip.
func zip64Locator(r io.ReaderAt, endAt int64) (at int64, found, ok bool) {
	if endAt < zip64LocatorLen {
		return 0, false, true
	}
	loc := make([]byte, zip64LocatorLen)
	if _, err := r.ReadAt(loc, endAt-zip64LocatorLen); err != nil {
		return 0, false, false
	}
	if binary.LittleEndian.Uint32(loc) != zip64LocatorSig ||
		binary.LittleEndian.Uint32(loc[4:]) != 0 || binary.LittleEndian.Uint32(loc[16:]) != 1 {
		return 0, false, true
	}
	at = int64(binary.LittleEndian.Uint64(loc[8:])) //nolint:gosec // archive/zip converts the same way
	return at, at >= 0, true
}

// zip64End reads the zip64 end record at at: its entry count, directory
// size and offset.
func zip64End(r io.ReaderAt, at int64) (entries, dirSize, dirOffset uint64, ok bool) {
	rec := make([]byte, zip64EndLen)
	if _, err := r.ReadAt(rec, at); err != nil || binary.LittleEndian.Uint32(rec) != zip64EndSig {
		return 0, 0, 0, false
	}
	return binary.LittleEndian.Uint64(rec[32:]), binary.LittleEndian.Uint64(rec[40:]), binary.LittleEndian.Uint64(rec[48:]), true
}

// countDirHeaders counts central directory headers from at, as archive/zip
// reads them, stopping once the count passes limit.
func countDirHeaders(r io.ReaderAt, size, at int64, limit int) int {
	if at < 0 || at >= size {
		return 0
	}
	br := bufio.NewReader(io.NewSectionReader(r, at, size-at))
	var head [zipDirHeaderLen]byte
	n := 0
	for n <= limit {
		if _, err := io.ReadFull(br, head[:]); err != nil || binary.LittleEndian.Uint32(head[:]) != zipDirHeaderSig {
			break
		}
		n++
		skip := int64(binary.LittleEndian.Uint16(head[28:])) + int64(binary.LittleEndian.Uint16(head[30:])) + int64(binary.LittleEndian.Uint16(head[32:]))
		if _, err := br.Discard(int(skip)); err != nil {
			break
		}
	}
	return n
}

// readInfoJSON reads an info.json inside a zip, up to maxInfoJSON bytes.
func readInfoJSON(zf *zip.File) (ModInfo, bool) {
	rc, err := zf.Open()
	if err != nil {
		return ModInfo{}, false
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, maxInfoJSON))
	if err != nil {
		return ModInfo{}, false
	}
	return parseInfoJSON(b)
}

// parseInfoJSON decodes info.json. Dependencies of an odd shape are left
// out rather than spoiling the rest.
func parseInfoJSON(b []byte) (ModInfo, bool) {
	var doc struct {
		Name            string          `json:"name"`
		Version         string          `json:"version"`
		FactorioVersion string          `json:"factorio_version"`
		Dependencies    json.RawMessage `json:"dependencies"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return ModInfo{}, false
	}
	info := ModInfo{Name: doc.Name, Version: doc.Version, FactorioVersion: doc.FactorioVersion}
	if len(doc.Dependencies) > 0 {
		_ = json.Unmarshal(doc.Dependencies, &info.Dependencies)
	}
	// Non-nil says info.json was read; nil is kept for "could not be read".
	if info.Dependencies == nil {
		info.Dependencies = []string{}
	}
	return info, true
}

// modVersionMismatch finds mods made for another Factorio than the world's
// pinned game version, which the game runs. It returns that version and
// each mismatched mod with the major.minor it was made for.
func modVersionMismatch(root *os.Root, meta Meta, mods modList) (running string, wrong map[string]string) {
	running = meta.GameVersion
	want := gameMinor(running)
	if want == "" {
		return running, nil
	}
	for name, v := range modGameVersions(root, mods, running) {
		if v != want {
			if wrong == nil {
				wrong = map[string]string{}
			}
			wrong[name] = v
		}
	}
	return running, wrong
}

// modVersionProblem words the refusal for mods made for another Factorio
// than the one the game runs. mods maps each mismatched mod to the
// major.minor it was made for.
func modVersionProblem(worldName string, running string, mods map[string]string) Problem {
	byVersion := map[string][]string{}
	for name, v := range mods {
		byVersion[v] = append(byVersion[v], name)
	}
	versions := make([]string, 0, len(byVersion))
	for v, names := range byVersion {
		slices.Sort(names)
		versions = append(versions, v)
	}
	// Largest group first.
	slices.SortFunc(versions, func(a, b string) int {
		if d := len(byVersion[b]) - len(byVersion[a]); d != 0 {
			return d
		}
		return CompareVersions(a, b)
	})

	p := Problem{Kind: ProblemModGameVersion, World: worldName, GameVersion: running}
	var text strings.Builder
	for i, v := range versions {
		names := byVersion[v]
		shown, more := shownMods(names)
		p.Groups = append(p.Groups, ModGroup{Version: v, Count: len(names), Shown: shown, More: more})
		count, object := fmt.Sprintf("%d of its mods were", len(names)), "them"
		if len(names) == 1 {
			count, object = "1 of its mods was", "it"
		}
		if i == 0 {
			fmt.Fprintf(&text, "%s cannot run on Factorio %s: ", worldName, running)
		} else {
			text.WriteString(" ")
		}
		fmt.Fprintf(&text, "%s made for Factorio %s, and the game refuses to load %s: %s.", count, v, object, listNames(names))
	}
	// A world's version only moves up (SPEC 10.3), so advise a newer
	// version only, never an older one.
	if newerSupportedLine(versions[0], running) {
		p.UpdateTo = versions[0]
		fmt.Fprintf(&text, " Update the world's game version to a %s release, or update its mods.", versions[0])
	} else {
		p.Minor = gameMinor(running)
		fmt.Fprintf(&text, " Update its mods to versions made for Factorio %s.", p.Minor)
	}
	p.Text = text.String()
	return p
}

// newerSupportedLine reports whether minor (such as "2.1") is a line
// Foreman runs and is newer than the running game version.
func newerSupportedLine(minor, running string) bool {
	return SupportedGameVersion(minor+".0") && CompareVersions(minor, gameMinor(running)) > 0
}

// listNames joins the first listedMods names, each cut to a sensible length,
// and counts the rest.
func listNames(names []string) string {
	cut, rest := shownMods(names)
	out := strings.Join(cut, ", ")
	if rest > 0 {
		out += fmt.Sprintf(" and %d more", rest)
	}
	return out
}

// shownMods is the first listedMods names, each cut to length, and how many
// are left out.
func shownMods(names []string) ([]string, int) {
	shown := names[:min(len(names), listedMods)]
	cut := make([]string, len(shown))
	for i, n := range shown {
		cut[i] = n
		if r := []rune(n); len(r) > maxShownModName {
			cut[i] = string(r[:maxShownModName]) + "…"
		}
	}
	return cut, len(names) - len(shown)
}
