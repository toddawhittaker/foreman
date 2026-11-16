package world

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
)

// FavoriteFile is the description file in each favorite's folder.
const FavoriteFile = "favorite.json"

// MaxFavoriteName is the longest favorite name, in characters.
const MaxFavoriteName = 100

// maxFavoriteJSON bounds favorite.json; a real one is a few kilobytes.
const maxFavoriteJSON = 1 << 20

// maxFavoriteMods bounds the mods one favorite lists.
const maxFavoriteMods = 1000

// favoriteFormat is the format_version of favorite.json.
const favoriteFormat = 1

// Refusals of the favorite functions. None names a file's contents.
var (
	ErrFavoriteNameEmpty  = errors.New("enter a name for the favorite")
	ErrFavoriteNameBad    = fmt.Errorf("the favorite's name must be at most %d characters, with no control characters", MaxFavoriteName)
	ErrFavoriteNameTaken  = errors.New("another favorite already has this name. Choose a different name")
	ErrFavoriteBroken     = errors.New("this favorite's files are damaged or too big, so Foreman will not use it")
	ErrBadServerSettings  = errors.New("the server settings are not one JSON object")
	ErrModSettingsTooBig  = errors.New("mod-settings.dat is too big")
	ErrWorldSettingsBroke = errors.New("the world's server settings are not valid JSON, so no favorite was made. Fix them on the Settings tab first")
)

// favoriteIDRE is the shape of the ids Foreman makes: randomSuffix's 16
// hex characters. Anything else is refused before it reaches a path.
var favoriteIDRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

// FavoriteMod is one portal mod of a favorite with its pinned version.
type FavoriteMod struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Title is the mod's cleaned title from info.json; "" when it had none
	// or the favorite predates titles.
	Title string `json:"title,omitempty"`
}

// Favorite describes a saved bundle to start new worlds from (SPEC 5.12).
// The settings files live beside favorite.json; Files reads them.
type Favorite struct {
	ID          string    `json:"-"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	SourceWorld string    `json:"source_world"`
	GameVersion string    `json:"game_version"`
	SpaceAge    bool      `json:"space_age"`
	// Preset is the game's preset name when the world still records it.
	Preset string `json:"preset,omitempty"`
	// HasMapSettings is false when the world had no map settings files, as
	// an imported world has none; the new world then gets the default map.
	HasMapSettings bool `json:"has_map_settings"`
	// Mods are the world's portal mods, by name, with exact versions. They
	// fill the new world's review page as version-<name> values.
	Mods []FavoriteMod `json:"mods"`
	// LeftOut names the mods not kept: folder mods and zips whose name or
	// version cannot be read.
	LeftOut []string `json:"left_out"`

	FormatVersion int `json:"format_version"`
}

// FavoriteFiles are a favorite's settings files. Empty means none.
type FavoriteFiles struct {
	MapGenSettings []byte
	MapSettings    []byte
	// ServerSettings never holds a secret (SPEC 14.8).
	ServerSettings []byte
	ModSettings    []byte
}

// Favorites owns the favorites folder of the data directory.
type Favorites struct {
	dir    string
	worlds *Manager
	now    func() time.Time
	// makeMu is held from the taken-name check to the rename, so two
	// favorites cannot get the same name.
	makeMu sync.Mutex
}

// NewFavorites makes the favorites folder if it is missing and clears any
// favorite left half-made by a crash. now may be nil for time.Now.
func NewFavorites(dir string, worlds *Manager, now func() time.Time) (*Favorites, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.Mkdir(dir, DirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("make folder %s: %w", dir, err)
	}
	root, err := openFolder(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), PrefixStaging) {
			_ = root.RemoveAll(e.Name())
		}
	}
	return &Favorites{dir: dir, worlds: worlds, now: now}, nil
}

// checkFavoriteName trims name and refuses a blank, overlong or
// control-character name.
func checkFavoriteName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrFavoriteNameEmpty
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxFavoriteName {
		return "", ErrFavoriteNameBad
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrFavoriteNameBad
		}
	}
	return name, nil
}

// Make saves a new favorite called name from world id and returns its id.
// It only reads the world's files, so no snapshot is needed (SPEC 6.2).
// The favorite is built in a staging folder and renamed into place, so a
// failure leaves nothing behind.
func (f *Favorites) Make(ctx context.Context, worldID, name string) (string, error) {
	name, err := checkFavoriteName(name)
	if err != nil {
		return "", err
	}
	fav, files, err := f.fromWorld(worldID)
	if err != nil {
		return "", err
	}
	fav.Name = name
	fav.CreatedAt = f.now().UTC()
	fav.FormatVersion = favoriteFormat
	return f.save(ctx, fav, files)
}

// save writes a checked favorite under a new id. The name check and the
// rename happen under makeMu, and the folder is built in a staging folder
// and renamed into place, so a failure leaves nothing behind.
func (f *Favorites) save(ctx context.Context, fav Favorite, files FavoriteFiles) (string, error) {
	f.makeMu.Lock()
	defer f.makeMu.Unlock()
	existing, err := f.List(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, fav.Name) {
			return "", ErrFavoriteNameTaken
		}
	}

	root, err := openFolder(f.dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	id := randomSuffix()
	staging := PrefixStaging + id
	if err := Mkdir(root, staging); err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = root.RemoveAll(staging)
		}
	}()
	desc, err := json.MarshalIndent(fav, "", "  ")
	if err != nil {
		return "", err
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{FavoriteFile, desc},
		{MapGenSettingsFile, files.MapGenSettings},
		{MapSettingsFile, files.MapSettings},
		{ServerSettingsFile, files.ServerSettings},
		{ModSettingsFile, files.ModSettings},
	} {
		if len(file.data) == 0 {
			continue
		}
		if err := WriteFileAtomic(root, path.Join(staging, file.name), file.data); err != nil {
			return "", err
		}
	}
	if err := root.Rename(staging, id); err != nil {
		return "", err
	}
	committed = true
	if err := syncDir(root, "."); err != nil {
		return "", err
	}
	return id, nil
}

// fromWorld reads what a favorite keeps from a world. It never writes.
func (f *Favorites) fromWorld(worldID string) (Favorite, FavoriteFiles, error) {
	root, err := f.worlds.OpenWorld(worldID)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	fav := Favorite{
		SourceWorld: meta.Name,
		GameVersion: meta.GameVersion,
		SpaceAge:    meta.SpaceAge,
		Mods:        []FavoriteMod{},
		LeftOut:     []string{},
	}
	if meta.NewMap != nil {
		fav.Preset = meta.NewMap.Preset
	}

	var files FavoriteFiles
	read := func(name string, limit int64) ([]byte, error) {
		b, err := ReadSmallFile(root, name, limit)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return b, err
	}
	if files.MapGenSettings, err = read(path.Join(ConfigDir, MapGenSettingsFile), maxConfigFile); err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	if files.MapSettings, err = read(path.Join(ConfigDir, MapSettingsFile), maxConfigFile); err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	fav.HasMapSettings = len(files.MapGenSettings) > 0 || len(files.MapSettings) > 0
	server, err := read(path.Join(ConfigDir, ServerSettingsFile), maxConfigFile)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	if len(server) > 0 {
		// Unparsable text could hide a secret, so it is refused, not copied.
		stripped, _ := gamesettings.StripSecrets(server)
		if stripped == nil {
			return Favorite{}, FavoriteFiles{}, ErrWorldSettingsBroke
		}
		files.ServerSettings = stripped
	}
	if files.ModSettings, err = read(path.Join(ModsDir, ModSettingsFile), MaxModSettings); err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}

	mods, err := f.worlds.Mods(worldID)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	for _, mod := range mods {
		switch {
		case mod.BuiltIn || !mod.Enabled:
			// Built-in mods come with the game; a turned-off mod is not part
			// of what the world plays.
		case mod.Local || !ValidModName(mod.Name) || !modVersionRE.MatchString(mod.Version):
			fav.LeftOut = append(fav.LeftOut, mod.Name)
		default:
			fav.Mods = append(fav.Mods, FavoriteMod{Name: mod.Name, Version: mod.Version, Title: CleanTitle(mod.Title)})
		}
	}
	if len(fav.Mods) > maxFavoriteMods {
		return Favorite{}, FavoriteFiles{}, ErrFavoriteBroken
	}
	return fav, files, nil
}

// List returns every favorite, by name ignoring case. A favorite whose
// files are damaged is skipped, so one bad folder does not hide the rest.
func (f *Favorites) List(ctx context.Context) ([]Favorite, error) {
	root, err := openFolder(f.dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	out := []Favorite{}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !favoriteIDRE.MatchString(e.Name()) {
			continue
		}
		fav, err := readFavorite(root, e.Name())
		if err != nil {
			continue
		}
		out = append(out, fav)
	}
	slices.SortFunc(out, func(a, b Favorite) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.ID, b.ID))
	})
	return out, nil
}

// Get returns one favorite and its settings files. An id Foreman could not
// have made is fs.ErrNotExist.
func (f *Favorites) Get(id string) (Favorite, FavoriteFiles, error) {
	if !favoriteIDRE.MatchString(id) {
		return Favorite{}, FavoriteFiles{}, fs.ErrNotExist
	}
	root, err := openFolder(f.dir)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	defer func() { _ = root.Close() }()
	fav, err := readFavorite(root, id)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	dir, err := openDir(root, id)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	defer func() { _ = dir.Close() }()
	files, err := readFavoriteFiles(dir)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, err
	}
	return fav, files, nil
}

// Delete removes a favorite. It is renamed aside first so a half-removed
// folder is never listed.
func (f *Favorites) Delete(id string) error {
	if !favoriteIDRE.MatchString(id) {
		return fs.ErrNotExist
	}
	root, err := openFolder(f.dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(id)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fs.ErrNotExist
	}
	aside := PrefixStaging + "deleting-" + id
	if err := root.Rename(id, aside); err != nil {
		return err
	}
	if err := syncDir(root, "."); err != nil {
		return err
	}
	return root.RemoveAll(aside)
}

// readFavorite reads and checks favorite.json in folder id. Its contents
// are untrusted: a planted or damaged file is ErrFavoriteBroken.
func readFavorite(root *os.Root, id string) (Favorite, error) {
	dir, err := openDir(root, id)
	if err != nil {
		return Favorite{}, err
	}
	defer func() { _ = dir.Close() }()
	b, err := ReadSmallFile(dir, FavoriteFile, maxFavoriteJSON)
	if errors.Is(err, fs.ErrNotExist) {
		return Favorite{}, err
	}
	if err != nil {
		return Favorite{}, ErrFavoriteBroken
	}
	fav, err := decodeFavorite(b, false)
	if err != nil {
		return Favorite{}, err
	}
	fav.ID = id
	return fav, nil
}

// decodeFavorite decodes and checks favorite.json. strict refuses unknown
// fields, for a file from outside.
func decodeFavorite(b []byte, strict bool) (Favorite, error) {
	var fav Favorite
	dec := json.NewDecoder(bytes.NewReader(b))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(&fav); err != nil || dec.More() {
		return Favorite{}, ErrFavoriteBroken
	}
	if !validFavorite(fav) {
		return Favorite{}, ErrFavoriteBroken
	}
	return fav, nil
}

// validFavorite checks every field of a decoded favorite.json.
func validFavorite(fav Favorite) bool {
	if fav.FormatVersion != favoriteFormat {
		return false
	}
	if n, err := checkFavoriteName(fav.Name); err != nil || n != fav.Name {
		return false
	}
	if !modVersionRE.MatchString(fav.GameVersion) || !presetRE.MatchString(fav.Preset) {
		return false
	}
	if utf8.RuneCountInString(fav.SourceWorld) > 1000 || len(fav.Mods) > maxFavoriteMods || len(fav.LeftOut) > maxFavoriteMods {
		return false
	}
	for _, m := range fav.Mods {
		if !ValidModName(m.Name) || !modVersionRE.MatchString(m.Version) || !validTitle(m.Title) {
			return false
		}
	}
	for _, n := range fav.LeftOut {
		if !ValidModName(n) {
			return false
		}
	}
	return true
}

// validTitle says t is what CleanTitle could have made: no control or
// format characters, no outer space, and no longer than its cut.
func validTitle(t string) bool {
	if !utf8.ValidString(t) || strings.TrimSpace(t) != t || utf8.RuneCountInString(t) > maxModTitle+1 {
		return false
	}
	for _, r := range t {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// readFavoriteFiles reads a favorite's settings files with the limits and
// checks world files get.
func readFavoriteFiles(dir *os.Root) (FavoriteFiles, error) {
	read := func(name string, limit int64) ([]byte, error) {
		b, err := ReadSmallFile(dir, name, limit)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, ErrFavoriteBroken
		}
		return b, nil
	}
	var files FavoriteFiles
	var err error
	for _, j := range []struct {
		name  string
		dst   *[]byte
		limit int64
	}{
		{MapGenSettingsFile, &files.MapGenSettings, maxConfigFile},
		{MapSettingsFile, &files.MapSettings, maxConfigFile},
		{ServerSettingsFile, &files.ServerSettings, maxConfigFile},
		{ModSettingsFile, &files.ModSettings, MaxModSettings},
	} {
		if *j.dst, err = read(j.name, j.limit); err != nil {
			return FavoriteFiles{}, err
		}
	}
	return checkFavoriteFiles(files)
}

// checkFavoriteFiles checks a favorite's settings files: the JSON ones must
// be valid, and server settings are stripped of secrets again in case one
// was planted.
func checkFavoriteFiles(files FavoriteFiles) (FavoriteFiles, error) {
	for _, b := range [][]byte{files.MapGenSettings, files.MapSettings, files.ServerSettings} {
		if int64(len(b)) > maxConfigFile || len(b) > 0 && !json.Valid(b) {
			return FavoriteFiles{}, ErrFavoriteBroken
		}
	}
	if len(files.ModSettings) > MaxModSettings {
		return FavoriteFiles{}, ErrFavoriteBroken
	}
	if len(files.ServerSettings) > 0 {
		stripped, _ := gamesettings.StripSecrets(files.ServerSettings)
		if stripped == nil {
			return FavoriteFiles{}, ErrFavoriteBroken
		}
		files.ServerSettings = stripped
	}
	return files, nil
}
