package web

import (
	"container/list"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Mod icons (SPEC 14.5, 14.6, 14.10). A world's mods can bring their own
// pictures for the objects their settings name. The page never asks for a
// file: it asks for an object, by kind and name, and the server finds the
// path in the cached result of the world's mod settings run, checks it, reads
// the one picture out of the mod's zip and answers a freshly encoded PNG.

// PathModIcon is one object's icon in a world's mods. name ends in ".png".
const PathModIcon = "/worlds/{id}/icons/{kind}/{name}"

// Sizes of the pictures the server makes, in pixels: an object's icon is
// shown at text height, so 64 is plenty; a star map picture is shown larger.
const (
	modIconPixels     = 64
	starMapIconPixels = 128
)

// modIconCacheBytes caps the PNG bytes the cache holds, in all.
const modIconCacheBytes = 8 << 20

// Limits on decoding: one picture at a time, and the rest wait their turn;
// past the queue the answer is 503 (SPEC 14.6). A star map asks for every
// modded planet's picture at once and a browser does not ask again for a
// failed one, so the queue holds a large pack's burst: a waiter costs no
// decoding memory.
const modIconQueue = 64

// modIconRun finds the cached run a world's icons come from, and a key for
// that run: the planets run when the world has a mod-settings.dat that is
// cached, else the definitions run. It never starts a run; false when none
// is cached. A new mod set or settings file gives a new key, so a picture
// of an earlier run is never served. Tests replace it.
var modIconRun = func(s *server, id string) (*game.ModSettingDefs, string, bool) {
	if s.game == nil {
		return nil, "", false
	}
	return s.game.CachedModPlanetDefsKeyed(id)
}

// readModIcon reads one icon from a world's mod zip; tests replace it.
var readModIcon = (*world.Manager).ModIcon

// modIconService is the state of the icon endpoint: the picture cache and the
// one-at-a-time decode.
type modIconService struct {
	cache *iconCache
	// slot holds one token while a decode runs; waiting counts the requests
	// queued for it.
	slot    chan struct{}
	mu      sync.Mutex
	waiting int
}

func newModIconService() *modIconService {
	return &modIconService{cache: newIconCache(modIconCacheBytes), slot: make(chan struct{}, 1)}
}

// acquire waits for the decode slot. It reports false when too many requests
// already wait, or the request ends first; busy tells the two apart.
func (m *modIconService) acquire(ctx context.Context) (ok, busy bool) {
	select {
	case m.slot <- struct{}{}:
		return true, false
	default:
	}
	m.mu.Lock()
	if m.waiting >= modIconQueue {
		m.mu.Unlock()
		return false, true
	}
	m.waiting++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.waiting--
		m.mu.Unlock()
	}()
	select {
	case m.slot <- struct{}{}:
		return true, false
	case <-ctx.Done():
		return false, false
	}
}

func (m *modIconService) release() { <-m.slot }

// modIconRoutes registers the icon endpoint. A signed-in user may fetch an
// icon of any world, as they may open its mod settings page.
func (s *server) modIconRoutes() {
	svc := newModIconService()
	s.mux.HandleFunc("GET "+PathModIcon, s.requireUser(func(w http.ResponseWriter, r *http.Request, _ store.User) {
		s.handleModIcon(svc, w, r)
	}))
}

// iconKindFor is the kind an icon URL names: the tag "planet" is a space
// location, and a kind outside the fixed list is refused.
func iconKindFor(kind string) (string, bool) {
	if kind == "planet" {
		kind = "space-location"
	}
	return kind, modsettings.ValidIconKind(kind)
}

func (s *server) handleModIcon(svc *modIconService, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	kind, kindOK := iconKindFor(r.PathValue("kind"))
	name, hasExt := strings.CutSuffix(r.PathValue("name"), ".png")
	_, nameOK := modsettings.WikiIconFile(name) // the prototype-name rule
	if !kindOK || !hasExt || !nameOK {
		http.NotFound(w, r)
		return
	}
	if _, err := s.worlds.Get(r.Context(), id); err != nil {
		if status, _ := worldError(err); status == http.StatusNotFound {
			http.NotFound(w, r)
			return
		}
		s.log.Error("read world", "path", r.URL.Path, "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defs, run, ok := modIconRun(s, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ref, ok := defs.Icons.Lookup(kind, name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	key := iconKey{world: id, run: run, kind: kind, name: name}
	png, hit := svc.cache.get(key)
	if !hit {
		var done bool
		if png, done = s.makeModIcon(svc, w, r, key, ref); !done {
			return
		}
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=300")
	h.Set("Content-Length", strconv.Itoa(len(png)))
	_, _ = w.Write(png)
}

// makeModIcon reads and caches one picture, one decode at a time. When it
// answers the request itself it returns false.
func (s *server) makeModIcon(svc *modIconService, w http.ResponseWriter, r *http.Request, key iconKey, ref modsettings.IconRef) ([]byte, bool) {
	ok, busy := svc.acquire(r.Context())
	if !ok {
		if busy {
			w.Header().Set("Retry-After", "2")
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		}
		return nil, false
	}
	defer svc.release()
	// Another request may have made it while this one waited.
	if png, hit := svc.cache.get(key); hit {
		return png, true
	}
	out := min(ref.Size, modIconPixels)
	if key.kind == "starmap" {
		out = min(ref.Size, starMapIconPixels)
	}
	// The index holds the path without the "__mod__/" prefix; ModIcon wants
	// the Factorio path.
	png, err := readModIcon(s.worlds, key.world, ref.Mod, ref.Version, "__"+ref.Mod+"__/"+ref.Path, ref.Size, out)
	switch {
	case err == nil:
		svc.cache.put(key, png)
		return png, true
	case errors.Is(err, world.ErrModIconNotFound), errors.Is(err, world.ErrModIconRefused):
		// The reason is Foreman's own word, never a mod's text. The page shows
		// the name.
		s.log.Debug("mod icon not served", "world", key.world, "kind", key.kind, "name", key.name, "reason", err)
		http.NotFound(w, r)
	default:
		s.log.Error("read a mod icon", "world", key.world, "kind", key.kind, "name", key.name, "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
	return nil, false
}

// iconKey names one picture: the world, the run it came from, and the object.
type iconKey struct{ world, run, kind, name string }

// iconCache is a least-recently-used cache of PNG bytes, capped by the bytes
// it holds, not by the count. A picture larger than the cap is not kept.
type iconCache struct {
	mu    sync.Mutex
	max   int
	size  int
	order *list.List // front is the most recently used
	byKey map[iconKey]*list.Element
}

type iconEntry struct {
	key iconKey
	png []byte
}

func newIconCache(max int) *iconCache {
	return &iconCache{max: max, order: list.New(), byKey: map[iconKey]*list.Element{}}
}

func (c *iconCache) get(k iconKey) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byKey[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*iconEntry).png, true
}

func (c *iconCache) put(k iconKey, png []byte) {
	if len(png) > c.max {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.byKey[k]; ok {
		c.size -= len(e.Value.(*iconEntry).png)
		c.order.Remove(e)
		delete(c.byKey, k)
	}
	c.byKey[k] = c.order.PushFront(&iconEntry{k, png})
	c.size += len(png)
	for c.size > c.max {
		last := c.order.Back()
		old := last.Value.(*iconEntry)
		c.size -= len(old.png)
		c.order.Remove(last)
		delete(c.byKey, old.key)
	}
}

// The page's world icons travel in the request's context, as the wiki setting
// does. Only the mod settings page and Generate's mod settings step set
// them, from the world's cached run; a page never starts a run for them.

type modIconsKey struct{}

type pageModIcons struct {
	world string
	index modsettings.IconIndex
}

// withWorldIcons returns r carrying the world's icon index when its mod
// settings run is cached, else r unchanged. Call it after the page's own
// read of the settings, so a run it started is cached.
func (s *server) withWorldIcons(r *http.Request, id string) *http.Request {
	defs, _, ok := modIconRun(s, id)
	if !ok || defs.Icons.Len() == 0 {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), modIconsKey{}, pageModIcons{id, defs.Icons}))
}

// modIconsOf returns the world and icon index r's page shows mod icons for.
func modIconsOf(r *http.Request) (string, modsettings.IconIndex, bool) {
	p, ok := r.Context().Value(modIconsKey{}).(pageModIcons)
	return p.world, p.index, ok
}
