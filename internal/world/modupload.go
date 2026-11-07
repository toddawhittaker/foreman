package world

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Refusals of a mod zip (SPEC 9.5, 14.6).
var (
	ErrModZip    = errors.New("this is not a mod zip: it has no readable info.json in a top-level folder")
	ErrModName   = errors.New("the mod's info.json gives a name or version Foreman cannot use as a file name")
	ErrModTooBig = errors.New("the mod zip is too big (over 200 MB)")
)

var modVersionRE = regexp.MustCompile(`^\d+(\.\d+){1,3}$`)

// maxModNameLen keeps <name>_<version>.zip inside a file name's 255 bytes.
const maxModNameLen = 200

// ReadModZip reads the info.json of the mod zip at path, such as an
// uploaded or downloaded file, and refuses one Foreman cannot name safely.
// The zip is placed as ModFileName(info).
func ReadModZip(path string) (ModInfo, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return ModInfo{}, err
	}
	defer func() { _ = root.Close() }()
	return readModZipIn(root, filepath.Base(path))
}

// readModZipIn is ReadModZip for the file name inside root.
func readModZipIn(root *os.Root, name string) (ModInfo, error) {
	f, st, err := openRegular(root, name)
	if err != nil {
		return ModInfo{}, err
	}
	defer func() { _ = f.Close() }()
	if st.Size() > maxModZip {
		return ModInfo{}, ErrModTooBig
	}
	info, ok := readZipInfo(f, st.Size())
	if !ok {
		return ModInfo{}, ErrModZip
	}
	if !validModName(info.Name) || !modVersionRE.MatchString(info.Version) {
		return ModInfo{}, ErrModName
	}
	return info, nil
}

// ModFileName is the file name the game looks for: <name>_<version>.zip.
func ModFileName(info ModInfo) string {
	return info.Name + "_" + info.Version + ".zip"
}

// validModName refuses a name that could leave the mods folder, hide
// itself, look like a leftover of an interrupted write, or carry control
// characters.
func validModName(name string) bool {
	if name == "" || len(name) > maxModNameLen || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}
