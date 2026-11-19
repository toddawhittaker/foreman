package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Favorites' paths (SPEC 5.12).
const (
	PathFavorites      = "/favorites"
	PathFavoriteDelete = "/favorites/{fav}/delete"
	PathFavoriteExport = "/favorites/{fav}/export"
	PathFavoriteImport = "/favorites/import"
	PathFavoriteRename = "/favorites/{fav}/rename"
)

// favoriteImportField is the import form's file field.
const favoriteImportField = "favorite"

// favoriteQuery marks a world page that follows making a favorite, and
// Generate's link that starts from one.
const favoriteQuery = "favorite"

func favoriteDeletePath(id string) string { return PathFavorites + "/" + id + "/delete" }
func favoriteMakePath(worldID string) string {
	return worldPath(worldID) + "/favorite"
}
func generateFromPath(id string) string {
	return PathGenerate + "?" + url.Values{favoriteQuery: {id}}.Encode()
}

// FavoriteRow is one favorite on the list, with its links.
type FavoriteRow struct {
	world.Favorite
}

func (f FavoriteRow) DeletePath() string   { return favoriteDeletePath(f.ID) }
func (f FavoriteRow) GeneratePath() string { return generateFromPath(f.ID) }
func (f FavoriteRow) ExportPath() string   { return PathFavorites + "/" + f.ID + "/export" }
func (f FavoriteRow) RenamePath() string   { return PathFavorites + "/" + f.ID + "/rename" }

// FavoriteRename is one favorite's rename field: the name it shows, the
// error of a refused rename, and whether its Details foldout opens.
type FavoriteRename struct {
	Value string
	Error i18n.Msg
	Open  bool
}

// FavoritesPage lists every favorite (SPEC 5.12).
type FavoritesPage struct {
	Favorites []FavoriteRow
	// Deleted names the favorite just deleted.
	Deleted string
	// Imported names the favorite just imported.
	Imported string
	// Renamed is the message after a rename.
	Renamed i18n.Msg
	// RenameID and RenameValue, RenameError are a refused rename's favorite,
	// name and error.
	RenameID, RenameValue string
	RenameError           i18n.Msg
	// ImportError says why the last import was refused.
	ImportError i18n.Msg
	// Exchanges holds each favorite's map exchange string, by favorite id.
	Exchanges map[string]ExchangeField
	// missing says the portal is known not to have a mod, so it is not
	// linked; nil links every mod.
	missing func(string) bool
}

// Mod is a favorite's mod as the mod-name partial shows it: by title,
// linked to the portal unless the portal is known to lack it.
func (p FavoritesPage) Mod(m world.FavoriteMod) ModName {
	return modName(m.Name, m.Title, p.missing == nil || !p.missing(m.Name))
}

// RenameFor is favorite id's rename field; a refused rename keeps its
// value and error and opens that favorite's Details.
func (p FavoritesPage) RenameFor(id string) FavoriteRename {
	if id == p.RenameID {
		return FavoriteRename{Value: p.RenameValue, Error: p.RenameError, Open: true}
	}
	for _, f := range p.Favorites {
		if f.ID == id {
			return FavoriteRename{Value: f.Name}
		}
	}
	return FavoriteRename{}
}

// MaxName is the longest favorite name, which the rename field allows so
// the field and the server cannot disagree.
func (FavoritesPage) MaxName() int { return world.MaxFavoriteName }

// ExchangeFor is the map exchange string field of one favorite.
func (p FavoritesPage) ExchangeFor(id string) ExchangeField { return p.Exchanges[id] }

// Problems is the import form's error summary (DESIGN 7.2), which takes
// focus when an import is refused.
func (p FavoritesPage) Problems() []Problem { return problems(Problem{"favorite-file", p.ImportError}) }

func (p FavoritesPage) hasErrors() bool { return len(p.Problems()) > 0 }

// ImportPath is where the import form posts.
func (FavoritesPage) ImportPath() string { return PathFavoriteImport }

// ImportZone is the import form's file field (DESIGN 7.10).
func (p FavoritesPage) ImportZone() DropZone {
	return DropZone{ID: "favorite-file", Name: favoriteImportField, Label: i18n.M("favorites.import.label"),
		Accept: ".zip", MaxBytes: world.MaxFavoriteUpload, Error: p.ImportError}
}

func (FavoritesPage) templateName() string { return "favorites" }
func (FavoritesPage) navItem() string      { return PathWorlds }

// FavoriteDeletePage confirms deleting a favorite.
type FavoriteDeletePage struct {
	Favorite FavoriteRow
}

func (FavoriteDeletePage) templateName() string { return "favoritedelete" }
func (FavoriteDeletePage) navItem() string      { return PathWorlds }

// FavoriteMade is the confirmation on a world's page after making a favorite.
type FavoriteMade struct {
	Name    string
	LeftOut []string
	// DefaultMap says the favorite holds no map settings.
	DefaultMap bool
}

func (s *server) favoriteRoutes() {
	if s.favorites == nil {
		return
	}
	s.mux.HandleFunc("GET "+PathFavorites, s.requireUser(s.handleFavorites))
	s.mux.HandleFunc("POST "+PathWorlds+"/{id}/favorite", s.requireUser(s.handleMakeFavorite))
	s.mux.HandleFunc("GET "+PathFavoriteDelete, s.requireUser(s.handleFavoriteDeleteForm))
	s.mux.HandleFunc("POST "+PathFavoriteDelete, s.requireUser(s.handleFavoriteDelete))
	s.mux.HandleFunc("POST "+PathFavoriteRename, s.requireUser(s.handleFavoriteRename))
	s.mux.HandleFunc("GET "+PathFavoriteExport, s.requireUser(s.handleFavoriteExport))
	s.handleUpload("POST "+PathFavoriteImport, fixedLimit(favoriteImportLimit), s.requireUser(s.guardUpload(s.handleFavoriteImport)))
}

// favoriteImportLimit is the import body's limit: the file plus room for
// the form's headers.
const favoriteImportLimit = world.MaxFavoriteUpload + 64<<10

func (s *server) favoritesPage(w http.ResponseWriter, r *http.Request, u store.User, status int, p FavoritesPage) {
	list, err := s.favorites.List(r.Context())
	if err != nil {
		s.fail(w, r, "list favorites", err)
		return
	}
	p.Exchanges = map[string]ExchangeField{}
	if s.portal != nil {
		p.missing = s.portal.KnownMissing
	}
	for _, f := range list {
		p.Favorites = append(p.Favorites, FavoriteRow{f})
		if _, files, err := s.favorites.Get(f.ID); err != nil {
			p.Exchanges[f.ID] = ExchangeField{ID: "exchange-" + f.ID, Owner: "favorite-" + f.ID + "-name", Reason: i18n.M("mapgen.exchange.reason.failed")}
		} else {
			p.Exchanges[f.ID] = s.favoriteExchange(f.ID, f, files)
		}
	}
	s.page(w, r, status, u.Username, p)
}

func (s *server) handleFavorites(w http.ResponseWriter, r *http.Request, u store.User) {
	p := FavoritesPage{}
	if id := r.URL.Query().Get("renamed"); id != "" {
		if fav, _, err := s.favorites.Get(id); err == nil {
			p.Renamed = i18n.M("favorites.renamed", "name", fav.Name)
		}
	}
	s.favoritesPage(w, r, u, http.StatusOK, p)
}

// handleFavoriteRename renames a favorite (SPEC 5.12) with Make's name
// rules. It writes only favorite.json, so it takes no snapshot.
func (s *server) handleFavoriteRename(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	fav, ok := s.findFavorite(w, r, u)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	err := s.favorites.Rename(r.Context(), fav.ID, name)
	if err == nil {
		http.Redirect(w, r, PathFavorites+"?"+url.Values{"renamed": {fav.ID}}.Encode(), http.StatusSeeOther)
		return
	}
	msg := favoriteError(err, false)
	switch {
	case !msg.IsZero():
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, world.ErrFavoriteBroken):
		s.notFound(w, r, u)
		return
	default:
		s.fail(w, r, "rename favorite", err)
		return
	}
	s.favoritesPage(w, r, u, http.StatusBadRequest, FavoritesPage{RenameID: fav.ID, RenameValue: name, RenameError: msg})
}

// favoriteError words a refused make or rename of a favorite (SPEC 5.12),
// or is zero when the refusal has no words for the page. making says the
// favorite was being made, where a broken favorite means one too big.
func favoriteError(err error, making bool) i18n.Msg {
	switch {
	case errors.Is(err, world.ErrFavoriteNameEmpty):
		return i18n.M("favorites.error.name_empty")
	case errors.Is(err, world.ErrFavoriteNameBad):
		return i18n.M("favorites.error.name_bad", "count", world.MaxFavoriteName)
	case errors.Is(err, world.ErrFavoriteNameTaken):
		return i18n.M("favorites.error.name_taken")
	case making && errors.Is(err, world.ErrWorldSettingsBroke):
		return i18n.M("favorites.error.settings_broken")
	case making && errors.Is(err, world.ErrFavoriteBroken):
		return i18n.M("favorites.error.too_big")
	}
	return i18n.Msg{}
}

// handleFavoriteExport downloads a favorite as a file (SPEC 5.12).
func (s *server) handleFavoriteExport(w http.ResponseWriter, r *http.Request, u store.User) {
	fav, data, err := s.favorites.Export(r.PathValue("fav"))
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, world.ErrFavoriteBroken):
		s.notFound(w, r, u)
		return
	case err != nil:
		s.fail(w, r, "export favorite", err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachment(fav.Name+"-favorite.zip"))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// handleFavoriteImport reads an uploaded favorite file into memory, which
// its 8 MiB limit allows, and saves it only once every check has passed.
func (s *server) handleFavoriteImport(w http.ResponseWriter, r *http.Request, u store.User) {
	data, err := readFavoriteUpload(r)
	var id string
	if err == nil {
		id, err = s.favorites.Import(r.Context(), data)
	}
	if err == nil {
		fav, _, gerr := s.favorites.Get(id)
		if gerr != nil {
			s.fail(w, r, "read imported favorite", gerr)
			return
		}
		s.favoritesPage(w, r, u, http.StatusOK, FavoritesPage{Imported: fav.Name})
		return
	}
	var up uploadError
	var msg i18n.Msg
	switch {
	case errors.As(err, &up):
		msg = up.msg
	case errors.Is(err, world.ErrFavoriteFileBad):
		msg = i18n.M("favorites.import.bad")
	case errors.Is(err, world.ErrFavoriteNameTaken):
		msg = i18n.M("favorites.import.name_taken")
	default:
		s.fail(w, r, "import favorite", err)
		return
	}
	s.favoritesPage(w, r, u, http.StatusBadRequest, FavoritesPage{ImportError: msg})
}

// readFavoriteUpload returns the form's favorite file, refusing one over
// world.MaxFavoriteUpload.
func readFavoriteUpload(r *http.Request) ([]byte, error) {
	limit := int64(world.MaxFavoriteUpload)
	if r.ContentLength > favoriteImportLimit {
		return nil, tooLarge(limit)
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, uploadError{i18n.M("worlds.upload.choose_file")}
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, uploadError{i18n.M("worlds.upload.choose_file")}
		}
		if err != nil {
			return nil, partError(err, limit)
		}
		if part.FormName() != favoriteImportField || part.FileName() == "" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, limit+1))
		if err != nil {
			return nil, bodyError(err, limit)
		}
		if int64(len(data)) > limit {
			return nil, tooLarge(limit)
		}
		return data, nil
	}
}

// findFavorite reads the path's favorite, or answers 404 or 500 itself.
func (s *server) findFavorite(w http.ResponseWriter, r *http.Request, u store.User) (world.Favorite, bool) {
	fav, _, err := s.favorites.Get(r.PathValue("fav"))
	switch {
	case err == nil:
		return fav, true
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, world.ErrFavoriteBroken):
		s.notFound(w, r, u)
	default:
		s.fail(w, r, "read favorite", err)
	}
	return world.Favorite{}, false
}

func (s *server) handleFavoriteDeleteForm(w http.ResponseWriter, r *http.Request, u store.User) {
	fav, ok := s.findFavorite(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, FavoriteDeletePage{Favorite: FavoriteRow{fav}})
}

// handleFavoriteDelete deletes and shows the list with the result, so no
// link can claim a favorite was deleted.
func (s *server) handleFavoriteDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	fav, ok := s.findFavorite(w, r, u)
	if !ok {
		return
	}
	if err := s.favorites.Delete(fav.ID); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.notFound(w, r, u)
			return
		}
		s.fail(w, r, "delete favorite", err)
		return
	}
	s.favoritesPage(w, r, u, http.StatusOK, FavoritesPage{Deleted: fav.Name})
}

// handleMakeFavorite makes a favorite from the world (SPEC 5.12). It only
// reads the world's files, so it takes no snapshot.
func (s *server) handleMakeFavorite(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	name := r.PostFormValue("favorite_name")
	id, err := s.favorites.Make(r.Context(), sum.ID, name)
	if err == nil {
		http.Redirect(w, r, worldPath(sum.ID)+"?"+url.Values{favoriteQuery: {id}}.Encode(), http.StatusSeeOther)
		return
	}
	msg := favoriteError(err, true)
	if msg.IsZero() {
		s.notFoundOrFail(w, r, u, "make favorite", err)
		return
	}
	p := s.worldPage(r.Context(), sum, sum.Name, i18n.Msg{})
	p.FavName, p.FavError = name, msg
	s.favoriteFacts(&p, r)
	s.page(w, r, http.StatusBadRequest, u.Username, p)
}

// favoriteFacts fills the world page's favorite form and, after one was
// made, its confirmation. It names the favorite only while it exists.
func (s *server) favoriteFacts(p *WorldPage, r *http.Request) {
	if s.favorites == nil || p.World.Newer || p.World.Unreadable {
		return
	}
	p.CanFavorite = true
	if p.FavName == "" {
		p.FavName = p.World.Name
	}
	p.FavDefaultMap = s.worldHasNoMap(p.World.ID)
	id := r.URL.Query().Get(favoriteQuery)
	if id == "" {
		return
	}
	fav, _, err := s.favorites.Get(id)
	if err != nil {
		return
	}
	p.FavMade = &FavoriteMade{Name: fav.Name, LeftOut: fav.LeftOut, DefaultMap: getsDefaultMap(fav.HasMapSettings, fav.Preset)}
}

// worldHasNoMap reports whether a favorite of the world would get the
// default map: it has no map settings files and no preset, as an imported
// world has none.
func (s *server) worldHasNoMap(id string) bool {
	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	m, err := world.ReadMeta(root)
	if err != nil {
		return false
	}
	preset := ""
	if m.NewMap != nil {
		preset = m.NewMap.Preset
	}
	hasFiles := false
	for _, f := range []string{world.MapGenSettingsFile, world.MapSettingsFile} {
		if _, err := root.Stat(path.Join(world.ConfigDir, f)); err == nil {
			hasFiles = true
		}
	}
	return getsDefaultMap(hasFiles, preset)
}

// getsDefaultMap reports whether a world made from these map settings gets
// the game's default map: no settings files and no preset.
func getsDefaultMap(hasFiles bool, preset string) bool { return !hasFiles && preset == "" }

// FavoritesPath is the favorites list.
func (WorldPage) FavoritesPath() string { return PathFavorites }

// FavoritePath is where the world page's favorite form posts.
func (p WorldPage) FavoritePath() string { return favoriteMakePath(p.World.ID) }

// ListPath is the favorites list.
func (FavoriteDeletePage) ListPath() string { return PathFavorites }

// fillFromFiles fills the map settings from a favorite's files (SPEC 5.12),
// so the form, the preview and the world start from its values. Files it
// cannot read put an error on the chooser, so the form never shows other
// settings than the world would get.
func (f *GenerateForm) fillFromFiles(files world.FavoriteFiles) {
	sa := f.SpaceAge && lineHasSpaceAge(f.Line)
	damaged := func() { f.FavoriteError = i18n.M("favorites.error.map_damaged") }
	if len(files.MapGenSettings) > 0 {
		if _, ok := orderedMembers(files.MapGenSettings); !ok {
			damaged()
			return
		}
		f.MapGen = NewMapGenFormFromFile(f.Line, sa, files.MapGenSettings)
		f.MapGen.withPreset(NewMapGenFormForPreset(f.Line, sa, f.Preset))
	}
	if len(files.MapSettings) > 0 {
		vals := mapSettingsValues(f.Version, f.Preset, files.MapSettings)
		if vals == nil {
			damaged()
			return
		}
		var problems []Problem
		if f.MapSettings, _, problems = readMapSettings(f.Version, f.Preset, vals); len(problems) > 0 {
			damaged()
			return
		}
	}
	f.Modified = f.MapGen.Modified() || f.MapSettings.Modified()
}

// mapSettingsValues is the post of the map-settings fields holding a
// map-settings.json's values; a field the file lacks keeps the preset's.
func mapSettingsValues(version, preset string, file []byte) url.Values {
	base, err := presetMapSettings(version, preset)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(file, &m) != nil {
		return nil
	}
	for k, v := range flatPaths(m, "") {
		base[k] = v
	}
	vals := url.Values{}
	for _, g := range msGroups {
		for _, d := range g.defs {
			v, ok := base[d.path]
			if !ok {
				continue
			}
			name := msPrefix + d.path
			switch d.kind {
			case msToggle:
				on, isBool := v.(bool)
				if !isBool {
					return nil
				}
				if on {
					vals.Set(name, "on")
				}
			case msChoice:
				vals.Set(name, presetChoice(v))
			default:
				n, isNum := v.(float64)
				if !isNum {
					return nil
				}
				vals.Set(name, presetShown(n, d))
			}
		}
	}
	return vals
}

// favoriteMapFiles writes the world's map files as the favorite's with the
// form's fields laid over them, so settings the form has no field for are
// kept (SPEC 5.12). It is false, with the form's error set, when they cannot
// be made.
func (s *server) favoriteMapFiles(f *GenerateForm, nw *world.NewWorld, files world.FavoriteFiles, r *http.Request) bool {
	return s.favoriteMapFilesFrom(f, nw, files, r.PostForm)
}

// favoriteMapFilesFrom is favoriteMapFiles on the posted values.
func (s *server) favoriteMapFilesFrom(f *GenerateForm, nw *world.NewWorld, files world.FavoriteFiles, form url.Values) bool {
	if nw.MapGenSettings == nil {
		mg, ok := buildMapGen(f.MapGen, rawOrEmpty(f.MapGen.Raw.Value))
		if !ok {
			f.FormError = i18n.M("worlds.generate.error.settings")
			return false
		}
		nw.MapGenSettings = mg
	}
	nw.Preset = ""
	if len(files.MapSettings) == 0 {
		return true
	}
	formMS := nw.MapSettings
	if formMS == nil {
		_, overrides, _ := readMapSettings(f.Version, f.Preset, form)
		var err error
		if formMS, err = mapgen.MapSettings(f.Version, f.Preset, overrides); err != nil {
			f.FormError = i18n.M("worlds.generate.error.settings")
			return false
		}
	}
	out, err := overlayFields(files.MapSettings, formMS)
	if err != nil {
		f.FormError = i18n.M("worlds.generate.error.settings")
		return false
	}
	nw.MapSettings = out
	return true
}

// overlayFields copies each value the map-settings fields cover from the
// form's file into the favorite's, leaving the rest of the favorite's file
// as it is.
func overlayFields(fav, form []byte) ([]byte, error) {
	var dst, src map[string]any
	if err := decodeNumbers(fav, &dst); err != nil {
		return nil, err
	}
	if err := decodeNumbers(form, &src); err != nil {
		return nil, err
	}
	for _, g := range msGroups {
		for _, d := range g.defs {
			if v, ok := pathValue(src, d.path); ok {
				setPath(dst, d.path, v)
			}
		}
	}
	out, err := json.MarshalIndent(dst, "", "  ")
	return append(out, '\n'), err
}

func decodeNumbers(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

func pathValue(m map[string]any, p string) (any, bool) {
	parts := strings.Split(p, ".")
	for _, k := range parts[:len(parts)-1] {
		sub, ok := m[k].(map[string]any)
		if !ok {
			return nil, false
		}
		m = sub
	}
	v, ok := m[parts[len(parts)-1]]
	return v, ok
}

func setPath(m map[string]any, p string, v any) {
	parts := strings.Split(p, ".")
	for _, k := range parts[:len(parts)-1] {
		sub, ok := m[k].(map[string]any)
		if !ok {
			sub = map[string]any{}
			m[k] = sub
		}
		m = sub
	}
	m[parts[len(parts)-1]] = v
}
