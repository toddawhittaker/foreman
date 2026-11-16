package web

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapexchange"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

// ExchangeField is the map exchange string a page offers beside a copy
// button, or the reason it offers none (SPEC 5.6). ID is the base of the
// element ids, so one page can hold several. Owner is the id of the heading
// that names what the string is for, which the controls point to.
type ExchangeField struct {
	ID     string
	Owner  string
	Value  string
	Reason i18n.Msg
}

// Have reports whether there is a string to show.
func (e ExchangeField) Have() bool { return e.Value != "" }

// Each read of a save can hold up to 32 MiB (savefile.ExchangeString), so
// only a few run at once, and a result is kept per save file so a page view
// does not read it again.
const (
	maxSaveReads     = 2
	maxCachedStrings = 64
)

// saveExchanges reads strings from saves, at most maxSaveReads at a time.
type saveExchanges struct {
	slots chan struct{}
	mu    sync.Mutex
	done  map[saveKey]saveResult
}

// saveKey names one version of one save file.
type saveKey struct {
	path  string
	size  int64
	mtime int64
}

type saveResult struct {
	value string
	err   error
}

func newSaveExchanges(reads int) *saveExchanges {
	return &saveExchanges{slots: make(chan struct{}, reads), done: map[saveKey]saveResult{}}
}

// saveReader is shared by every page, as the memory it guards is the
// process's.
var saveReader = newSaveExchanges(maxSaveReads)

func (c *saveExchanges) cached(k saveKey) (saveResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.done[k]
	return r, ok
}

func (c *saveExchanges) remember(k saveKey, r saveResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.done) >= maxCachedStrings {
		clear(c.done)
	}
	c.done[k] = r
}

// fromSave gives the string for one of the world's saves. A save that
// cannot give one is remembered too, with its reason; a failure of the disk
// or of the request is not.
func (c *saveExchanges) fromSave(ctx context.Context, m *world.Manager, id string, sv world.Save) (string, error) {
	key := saveKey{filepath.Join(m.Dirs().Worlds, id, world.SavesDir, sv.Name), sv.Size, sv.ModTime.UnixNano()}
	if r, ok := c.cached(key); ok {
		return r.value, r.err
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	// Another request may have read it while this one waited.
	if r, ok := c.cached(key); ok {
		return r.value, r.err
	}
	f, info, err := m.OpenSave(id, sv.Name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	value, err := savefile.ExchangeString(f, info.Size())
	if err == nil || knownSaveReason(err) {
		c.remember(saveKey{key.path, info.Size(), info.ModTime().UnixNano()}, saveResult{value, err})
	}
	return value, err
}

// exchangeFromFiles makes the string for map files as Generate writes them.
// A file that is missing counts as empty, so the seed decides.
func exchangeFromFiles(gameVersion string, mapGen, mapSettings []byte) (string, error) {
	if len(mapGen) == 0 {
		mapGen = []byte("{}")
	}
	if len(mapSettings) == 0 {
		mapSettings = []byte("{}")
	}
	return mapexchange.Encode(gameVersion, mapGen, mapSettings)
}

// knownSaveReason reports whether err is one of the typed reasons a save
// gives no string, which stay true until the file changes.
func knownSaveReason(err error) bool {
	var unsupportedSave *savefile.UnsupportedVersionError
	var unsupported *mapexchange.UnsupportedError
	for _, e := range []error{savefile.ErrNotZip, savefile.ErrNoLevelInit, savefile.ErrTooLarge, savefile.ErrFormat,
		savefile.ErrTruncated, savefile.ErrNoMapSettings} {
		if errors.Is(err, e) {
			return true
		}
	}
	return errors.As(err, &unsupportedSave) || errors.As(err, &unsupported)
}

// exchangeReason says in words why a string could not be made. ok is false
// for an error that is not one of the typed reasons, which the caller logs.
func exchangeReason(err error) (reason i18n.Msg, ok bool) {
	var unsupportedSave *savefile.UnsupportedVersionError
	var unsupported *mapexchange.UnsupportedError
	switch {
	case errors.Is(err, mapexchange.ErrNoSeed):
		return i18n.M("mapgen.exchange.reason.no_seed"), true
	case errors.Is(err, savefile.ErrNoLevelInit), errors.Is(err, savefile.ErrNoMapSettings):
		return i18n.M("mapgen.exchange.reason.no_settings"), true
	case errors.As(err, &unsupportedSave):
		return i18n.M("mapgen.exchange.reason.unsupported", "version", unsupportedSave.Version.String()), true
	case errors.As(err, &unsupported):
		v := unsupported.Version
		return i18n.M("mapgen.exchange.reason.unsupported", "version", fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])), true
	case errors.Is(err, savefile.ErrNotZip), errors.Is(err, savefile.ErrTooLarge), errors.Is(err, savefile.ErrFormat),
		errors.Is(err, savefile.ErrTruncated):
		return i18n.M("mapgen.exchange.reason.unreadable"), true
	}
	return i18n.M("mapgen.exchange.reason.failed"), false
}

// worldExchange gives the string for a world's map: from its map files when
// Generate made it, else, or when the files leave the seed to chance, from
// its newest save, which is how an imported world gets one (SPEC 5.6).
func (s *server) worldExchange(ctx context.Context, sum world.Summary) ExchangeField {
	out := ExchangeField{ID: "exchange"}
	fail := func(err error) ExchangeField {
		reason, known := exchangeReason(err)
		// A request the visitor canceled is not a fault.
		if !known && ctx.Err() == nil {
			s.log.Warn("make a map exchange string", "world", sum.ID, "error", err)
		}
		out.Reason = reason
		return out
	}
	gen, err := s.worlds.ReadConfig(sum.ID, world.MapGenSettingsFile)
	if err != nil {
		return fail(err)
	}
	ms, err := s.worlds.ReadConfig(sum.ID, world.MapSettingsFile)
	if err != nil {
		return fail(err)
	}
	noMap := i18n.M("mapgen.exchange.reason.no_map_world")
	if gen.Found {
		value, err := exchangeFromFiles(sum.GameVersion, gen.Data, ms.Data)
		if err == nil {
			out.Value = value
			return out
		}
		if !errors.Is(err, mapexchange.ErrNoSeed) {
			return fail(err)
		}
		// The map was made from a random seed, which only the save knows.
		noMap = i18n.M("mapgen.exchange.reason.no_seed")
	}
	save, found, err := s.worlds.NewestSave(sum.ID)
	if err != nil {
		return fail(err)
	}
	if !found {
		out.Reason = noMap
		return out
	}
	value, err := saveReader.fromSave(ctx, s.worlds, sum.ID, save)
	if err != nil {
		return fail(err)
	}
	out.Value = value
	return out
}

// favoriteExchange gives the string for a favorite's stored map files.
func (s *server) favoriteExchange(id string, fav world.Favorite, files world.FavoriteFiles) ExchangeField {
	out := ExchangeField{ID: "exchange-" + id, Owner: "favorite-" + id + "-name"}
	if len(files.MapGenSettings) == 0 {
		out.Reason = i18n.M("mapgen.exchange.reason.no_map_favorite")
		return out
	}
	value, err := exchangeFromFiles(fav.GameVersion, files.MapGenSettings, files.MapSettings)
	if err != nil {
		reason, known := exchangeReason(err)
		if !known {
			s.log.Warn("make a map exchange string", "favorite", id, "error", err)
		}
		out.Reason = reason
		return out
	}
	out.Value = value
	return out
}
