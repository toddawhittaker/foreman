package world

import (
	"archive/zip"
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
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

// zipModInfo reads the info.json of the mod zip name inside root.
func zipModInfo(root *os.Root, name string) (ModInfo, bool) {
	f, st, err := openRegular(root, name)
	if err != nil {
		return ModInfo{}, false
	}
	defer func() { _ = f.Close() }()
	return readZipInfo(f, st.Size())
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
	zipDirHeaderSig   = 0x02014b50
	zipDirHeaderLen   = 46
	zipEndSig         = 0x06054b50
	zipEndLen         = 22
	zip64LocatorSig   = 0x07064b50
	zip64LocatorLen   = 20
	zip64EndSig       = 0x06064b50
	zip64EndLen       = 56
	zipMaxCommentLen  = 0xffff
	zip64EntriesValue = 0xffff
)

// zipEntriesOver reports whether a zip's central directory holds more than
// limit entries, reading only the fixed part of each header. zip.NewReader
// builds every entry before anyone can count them, and it reads headers
// until one is bad, whatever count the end record gives, so a crafted zip
// of millions of tiny entries would cost that much memory on every page that
// lists mods. Headers are counted from both places archive/zip may start
// reading, so the count is never below its own. A zip whose end record
// cannot be found is not over: zip.NewReader then refuses it.
func zipEntriesOver(r io.ReaderAt, size int64, limit int) bool {
	tail := min(size, zipEndLen+zipMaxCommentLen)
	buf := make([]byte, tail)
	if _, err := r.ReadAt(buf, size-tail); err != nil && err != io.EOF {
		return false
	}
	end := -1
	for i := len(buf) - zipEndLen; i >= 0; i-- {
		if binary.LittleEndian.Uint32(buf[i:]) == zipEndSig &&
			i+zipEndLen+int(binary.LittleEndian.Uint16(buf[i+20:])) <= len(buf) {
			end = i
			break
		}
	}
	if end < 0 {
		return false
	}
	endAt := size - tail + int64(end)
	rec := buf[end:]
	entries := uint64(binary.LittleEndian.Uint16(rec[10:]))
	dirSize := uint64(binary.LittleEndian.Uint32(rec[12:]))
	dirOffset := uint64(binary.LittleEndian.Uint32(rec[16:]))
	// archive/zip looks for a zip64 record on a size of 0xffff too.
	if entries == zip64EntriesValue || dirSize == 0xffff || dirSize == 0xffffffff || dirOffset == 0xffffffff {
		if e, s, o, at, ok := zip64End(r, endAt); ok {
			entries, dirSize, dirOffset, endAt = e, s, o, at
		}
	}
	if entries > uint64(limit) { //nolint:gosec // limit is a small positive constant
		return true
	}
	// archive/zip starts at the offset itself, or past data put before the
	// zip. A value too large for int64 turns negative and reads nothing.
	at, size64 := int64(dirOffset), int64(dirSize) //nolint:gosec // see above
	starts := []int64{at}
	if base := endAt - size64 - at; base > 0 {
		starts = append(starts, base+at)
	}
	for _, at := range starts {
		if countDirHeaders(r, size, at, limit) > limit {
			return true
		}
	}
	return false
}

// zip64End reads the zip64 end record that the locator just before the end
// record at endAt points to: its entry count, directory size and offset, and
// where it sits.
func zip64End(r io.ReaderAt, endAt int64) (entries, dirSize, dirOffset uint64, at int64, ok bool) {
	if endAt < zip64LocatorLen {
		return 0, 0, 0, 0, false
	}
	loc := make([]byte, zip64LocatorLen)
	if _, err := r.ReadAt(loc, endAt-zip64LocatorLen); err != nil || binary.LittleEndian.Uint32(loc) != zip64LocatorSig {
		return 0, 0, 0, 0, false
	}
	at = int64(binary.LittleEndian.Uint64(loc[8:])) //nolint:gosec // a negative offset fails the read
	rec := make([]byte, zip64EndLen)
	if _, err := r.ReadAt(rec, at); err != nil || binary.LittleEndian.Uint32(rec) != zip64EndSig {
		return 0, 0, 0, 0, false
	}
	return binary.LittleEndian.Uint64(rec[32:]), binary.LittleEndian.Uint64(rec[40:]), binary.LittleEndian.Uint64(rec[48:]), at, true
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
