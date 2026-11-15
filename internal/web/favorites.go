package web

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"path"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Favorites' paths (SPEC 5.12).
const (
	PathFavorites      = "/favorites"
	PathFavoriteDelete = "/favorites/{fav}/delete"
)

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

// FavoritesPage lists every favorite (SPEC 5.12).
type FavoritesPage struct {
	Favorites []FavoriteRow
	// Deleted names the favorite just deleted.
	Deleted string
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
}

func (s *server) favoritesPage(w http.ResponseWriter, r *http.Request, u store.User, deleted string) {
	list, err := s.favorites.List(r.Context())
	if err != nil {
		s.fail(w, r, "list favorites", err)
		return
	}
	p := FavoritesPage{Deleted: deleted}
	for _, f := range list {
		p.Favorites = append(p.Favorites, FavoriteRow{f})
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

func (s *server) handleFavorites(w http.ResponseWriter, r *http.Request, u store.User) {
	s.favoritesPage(w, r, u, "")
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
	s.favoritesPage(w, r, u, fav.Name)
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
	var msg i18n.Msg
	switch {
	case errors.Is(err, world.ErrFavoriteNameEmpty):
		msg = i18n.M("favorites.error.name_empty")
	case errors.Is(err, world.ErrFavoriteNameBad):
		msg = i18n.M("favorites.error.name_bad", "count", world.MaxFavoriteName)
	case errors.Is(err, world.ErrFavoriteNameTaken):
		msg = i18n.M("favorites.error.name_taken")
	case errors.Is(err, world.ErrWorldSettingsBroke):
		msg = i18n.M("favorites.error.settings_broken")
	case errors.Is(err, world.ErrFavoriteBroken):
		msg = i18n.M("favorites.error.too_big")
	default:
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
	p.FavMade = &FavoriteMade{Name: fav.Name, LeftOut: fav.LeftOut, DefaultMap: !fav.HasMapSettings && fav.Preset == ""}
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
	if m, err := world.ReadMeta(root); err != nil || (m.NewMap != nil && m.NewMap.Preset != "") {
		return false
	}
	for _, f := range []string{world.MapGenSettingsFile, world.MapSettingsFile} {
		if _, err := root.Stat(path.Join(world.ConfigDir, f)); err == nil {
			return false
		}
	}
	return true
}

// FavoritesPath is the favorites list.
func (WorldPage) FavoritesPath() string { return PathFavorites }

// FavoritePath is where the world page's favorite form posts.
func (p WorldPage) FavoritePath() string { return favoriteMakePath(p.World.ID) }

// ListPath is the favorites list.
func (FavoriteDeletePage) ListPath() string { return PathFavorites }
