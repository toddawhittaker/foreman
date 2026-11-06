package web

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
)

// The map preview's paths under PathGeneratePreview: one picture to show,
// the same picture to download, and all drawn planets as a zip.
const (
	pathPicture         = PathGeneratePreview + "/{token}/{planet}"
	pathPictureDownload = pathPicture + "/download"
	pathPicturesZip     = PathGeneratePreview + "/{token}/all.zip"
	zipName             = "all.zip"
)

// Form fields of the preview request beyond Generate's own.
const (
	// previewPlanetField names the one planet to draw; blank draws all.
	previewPlanetField = "planet"
	// previewAutoField is "1" on a request the page sends by itself as the
	// person types. Settings that are not valid then get the short
	// "generate-preview-invalid" fragment, with status 200, in the preview's
	// slot, and the form is left alone.
	previewAutoField = "auto"
)

// previewDeadline is how long a preview request may take to answer: the
// wait for the slot, the run, and room to write the reply. The server's own
// write timeout is shorter.
const previewDeadline = game.PreviewWait + game.PreviewLimit + 30*time.Second

// MapPreviewer makes the map preview pictures: one PNG for each planet
// asked for, from the settings Generate would write (SPEC 5.6, 14.3). The
// game controller provides it. Seed is always set, so the world made later
// has the same map.
type MapPreviewer interface {
	Preview(ctx context.Context, req game.PreviewRequest) ([]game.PNG, error)
}

// previewTTL is how long a preview's pictures are kept (SPEC 5.6).
const previewTTL = 15 * time.Minute

// The pictures held at once: 8 users' previews and 48 MB in all, so a few
// admins cannot fill the memory (SPEC 5.6).
const (
	maxPreviews     = 8
	maxPreviewBytes = 48 << 20
)

// previewEntry is one user's current preview: the pictures drawn so far
// from one set of settings, under a random token.
type previewEntry struct {
	token   string
	key     string // settingsKey of the settings drawn
	name    string // the world name typed, for download names
	seed    string
	expires time.Time
	planets []string // in the order drawn
	images  map[string][]byte
	bytes   int
}

// previewStore holds each user's current preview (SPEC 5.6). Nothing else
// is served from it, and only to the user it belongs to.
type previewStore struct {
	mu     sync.Mutex
	now    func() time.Time
	byUser map[int64]*previewEntry
	order  []int64 // least recently added to first
}

func newPreviewStore(now func() time.Time) *previewStore {
	return &previewStore{now: now, byUser: map[int64]*previewEntry{}}
}

// settingsKey is a hash of everything that changes the map, so the same
// settings find the pictures already drawn. The planet and the world name
// are not part of it.
func settingsKey(req game.PreviewRequest) string {
	b, _ := json.Marshal(struct {
		Version, Preset     string
		SpaceAge            bool
		Seed                uint32
		MapGen, MapSettings []byte
	}{req.GameVersion, req.Preset, req.SpaceAge, req.Seed, req.MapGenSettings, req.MapSettings})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// missing returns the planets the user's current preview lacks for these
// settings, and the preview's token when it has all of them. It renames
// the preview, since the name is not part of the key.
func (p *previewStore) missing(user int64, key, name string, planets []string) (string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok || e.key != key {
		return "", planets
	}
	e.name = name
	var out []string
	for _, pl := range planets {
		if _, ok := e.images[pl]; !ok {
			out = append(out, pl)
		}
	}
	if len(out) > 0 {
		return "", out
	}
	return e.token, nil
}

// add keeps new pictures and returns the preview's token. Pictures of the
// same settings join the user's current preview; other settings replace
// it. Other users' previews go, oldest first, to keep the limits.
func (p *previewStore) add(user int64, key, name, seed string, images []game.PNG) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok || e.key != key {
		e = &previewEntry{token: newToken(), key: key, seed: seed, images: map[string][]byte{}}
	}
	grown := e.bytes
	for _, im := range images {
		if len(im.Data) > game.MaxPreviewPNG {
			return "", fmt.Errorf("the picture of %s is too large", im.Planet)
		}
		grown += len(im.Data) - len(e.images[im.Planet])
	}
	if grown > maxPreviewBytes {
		return "", errors.New("the preview pictures are too large")
	}
	for _, im := range images {
		if _, had := e.images[im.Planet]; !had {
			e.planets = append(e.planets, im.Planet)
		}
		e.images[im.Planet] = im.Data
	}
	e.bytes, e.name = grown, name
	e.expires = p.now().Add(previewTTL)
	p.dropLocked(user)
	for len(p.order) > 0 && (len(p.order) >= maxPreviews || p.totalLocked()+e.bytes > maxPreviewBytes) {
		p.dropLocked(p.order[0])
	}
	p.byUser[user] = e
	p.order = append(p.order, user)
	return e.token, nil
}

// entry returns a copy of the user's preview under token, if it has not
// expired.
func (p *previewStore) entry(token string, user int64) (previewEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok || e.token != token {
		return previewEntry{}, false
	}
	return *e, true
}

// get returns a planet's picture when the token is the user's and has not
// expired.
func (p *previewStore) get(token string, user int64, planet string) ([]byte, bool) {
	e, ok := p.entry(token, user)
	if !ok {
		return nil, false
	}
	png, ok := e.images[planet]
	return png, ok
}

func (p *previewStore) sweepLocked() {
	now := p.now()
	for _, u := range slices.Clone(p.order) {
		if !now.Before(p.byUser[u].expires) {
			p.dropLocked(u)
		}
	}
}

func (p *previewStore) dropLocked(user int64) {
	delete(p.byUser, user)
	p.order = slices.DeleteFunc(p.order, func(u int64) bool { return u == user })
}

func (p *previewStore) totalLocked() int {
	n := 0
	for _, e := range p.byUser {
		n += e.bytes
	}
	return n
}

// PreviewView is the Map preview section. Error holds why no pictures
// were made.
type PreviewView struct {
	Token    string
	Seed     string
	Pictures []PreviewPicture
	Error    i18n.Msg
}

// PreviewPicture is one planet's picture.
type PreviewPicture struct {
	Planet string
	Name   i18n.Msg
	Path   string
}

// DownloadPath is where the picture is served as a file.
func (p PreviewPicture) DownloadPath() string { return p.Path + "/download" }

// ZipPath is where all the drawn planets are served as one zip.
func (v PreviewView) ZipPath() string { return PathGeneratePreview + "/" + v.Token + "/" + zipName }

func (s *server) previewRoutes() {
	s.handleUpload("POST "+PathGeneratePreview, generateFormLimit, s.requireUser(s.handleGeneratePreview))
	s.mux.HandleFunc("GET "+pathPicture, s.requireUser(s.handlePicture))
	// A zip of five planets is several MB; a slow link needs the idle deadline.
	s.handleLong("GET "+pathPictureDownload, s.requireUser(s.handlePictureDownload))
	s.handleLong("GET "+pathPicturesZip, s.requireUser(s.handlePicturesZip))
}

// handleGeneratePreview makes the map pictures from the settings Generate
// would write, and shows the form again with them under it (SPEC 5.6). A
// planet field draws only that planet. Pictures already drawn from the
// same settings are not drawn again.
func (s *server) handleGeneratePreview(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.previewer == nil {
		s.notFound(w, r, u)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	auto := r.PostFormValue(previewAutoField) == "1"
	f, nw, ok := s.readGenerate(r, kindPreview)
	if !ok {
		if auto {
			s.fragment(w, r, s.generatePage(f, nil), "generate-preview-invalid")
			return
		}
		// htmx swaps only a 2xx answer, and then the whole form, with its errors.
		status := http.StatusBadRequest
		if r.Header.Get("HX-Request") == "true" {
			status = http.StatusOK
			h := w.Header()
			h.Set("HX-Retarget", "#generate-form")
			h.Set("HX-Reselect", "#generate-form")
			h.Set("HX-Reswap", "outerHTML")
		}
		if f.Reset {
			status = http.StatusOK
		}
		s.page(w, r, status, u.Username, s.generatePage(f, nil))
		return
	}
	line, _ := mapgen.Line(nw.GameVersion)
	planets := mapgen.Planets(line, nw.SpaceAge)
	if p := r.PostFormValue(previewPlanetField); p != "" {
		if !slices.Contains(planets, p) {
			s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
			return
		}
		planets = []string{p}
	}
	req := game.PreviewRequest{
		GameVersion: nw.GameVersion, SpaceAge: nw.SpaceAge, Preset: nw.Preset, Seed: *nw.Seed,
		MapGenSettings: nw.MapGenSettings, MapSettings: nw.MapSettings,
	}
	view := &PreviewView{Seed: strconv.FormatUint(uint64(req.Seed), 10)}
	key := settingsKey(req)
	view.Token, req.Planets = s.previews.missing(u.ID, key, f.Name, planets)
	var err error
	if view.Token == "" {
		view.Token, err = s.drawPreview(w, r, u, req, key, f.Name, view.Seed)
	}
	if err == nil {
		for _, planet := range planets {
			view.Pictures = append(view.Pictures, PreviewPicture{Planet: planet, Name: planetMsg(planet), Path: pictureURL(view.Token, planet)})
		}
	} else {
		view.Token = ""
		view.Error = s.previewFailure(r, nw.GameVersion, err)
	}
	s.page(w, r, http.StatusOK, u.Username, s.generatePage(f, view))
}

// drawPreview runs the preview for req.Planets and keeps the pictures. The
// request gets its own write deadline, as the run may outlast the server's.
func (s *server) drawPreview(w http.ResponseWriter, r *http.Request, u store.User, req game.PreviewRequest, key, name, seed string) (string, error) {
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(previewDeadline)); err != nil {
		s.log.Warn("set the map preview's write deadline", "error", err)
	}
	images, err := s.previewer.Preview(r.Context(), req)
	if err != nil {
		return "", err
	}
	token, err := s.previews.add(u.ID, key, name, seed, images)
	if err != nil {
		s.log.Error("keep map preview", "error", err)
		return "", errPreviewNotKept
	}
	return token, nil
}

// errPreviewNotKept means the pictures were drawn but could not be held.
var errPreviewNotKept = errors.New("the map preview could not be kept")

// previewFailure words why a preview made no pictures, and logs what is
// Foreman's fault.
func (s *server) previewFailure(r *http.Request, version string, err error) i18n.Msg {
	pf, failure := errors.AsType[*game.PreviewError](err)
	switch {
	case errors.Is(err, game.ErrPreviewImageMissing):
		return i18n.M("worlds.generate.preview.no_image", "version", version)
	case errors.Is(err, game.ErrPreviewBusy):
		return i18n.M("worlds.generate.preview.busy")
	case errors.Is(err, game.ErrPreviewTimeout):
		return i18n.M("worlds.generate.preview.timeout")
	case failure:
		s.log.Warn("map preview failed", "version", version, "oom", pf.OOM)
		if pf.OOM {
			return i18n.M("worlds.generate.preview.oom")
		}
	case errors.Is(err, errPreviewNotKept):
	default:
		if r.Context().Err() == nil {
			s.log.Error("map preview", "version", version, "error", err)
		}
	}
	return i18n.M("worlds.generate.preview.failed")
}

// pictureURL is where a planet's picture is served.
func pictureURL(token, planet string) string {
	return PathGeneratePreview + "/" + token + "/" + planet
}

// handlePicture serves one planet's preview picture to the user who asked
// for it. Nothing else is read through this route.
func (s *server) handlePicture(w http.ResponseWriter, r *http.Request, u store.User) {
	png, ok := s.previews.get(r.PathValue("token"), u.ID, r.PathValue("planet"))
	if !ok {
		s.notFound(w, r, u)
		return
	}
	writePNG(w, png)
}

// handlePictureDownload serves one planet's picture as a file named after
// the world, seed and planet (SPEC 5.6).
func (s *server) handlePictureDownload(w http.ResponseWriter, r *http.Request, u store.User) {
	planet := r.PathValue("planet")
	e, ok := s.previews.entry(r.PathValue("token"), u.ID)
	png, have := e.images[planet]
	if !ok || !have {
		s.notFound(w, r, u)
		return
	}
	w.Header().Set("Content-Disposition", attachment(previewFileName(e.name, e.seed, planet)+".png"))
	writePNG(w, png)
}

// handlePicturesZip serves every planet drawn so far as one zip, each
// picture named as its own download would be.
func (s *server) handlePicturesZip(w http.ResponseWriter, r *http.Request, u store.User) {
	e, ok := s.previews.entry(r.PathValue("token"), u.ID)
	if !ok || len(e.planets) == 0 {
		s.notFound(w, r, u)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", attachment(previewFileName(e.name, e.seed, "")+".zip"))
	zw := zip.NewWriter(w)
	for _, planet := range e.planets {
		// A PNG is compressed already.
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: previewFileName(e.name, e.seed, planet) + ".png", Method: zip.Store})
		if err != nil {
			return
		}
		if _, err := fw.Write(e.images[planet]); err != nil {
			return
		}
	}
	_ = zw.Close()
}

func writePNG(w http.ResponseWriter, png []byte) {
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(png)))
	_, _ = w.Write(png)
}

// maxFileStem caps the world name's part of a download name, in characters.
const maxFileStem = 60

// previewFileName is a download's name without its extension: the world's
// name, the seed and the planet, joined by dashes. The world name loses
// path, control and quoting characters; a blank one is "map". planet may
// be blank, for the zip.
func previewFileName(world, seed, planet string) string {
	var b strings.Builder
	n := 0
	for _, c := range strings.TrimSpace(world) {
		if n == maxFileStem {
			break
		}
		switch {
		case unicode.IsControl(c) || unicode.Is(unicode.Cf, c) || strings.ContainsRune(`/\:*?"<>|;%`, c):
			continue
		case unicode.IsSpace(c):
			c = '_'
		}
		b.WriteRune(c)
		n++
	}
	stem := strings.Trim(b.String(), "._")
	if stem == "" {
		stem = "map"
	}
	parts := []string{stem, seed}
	if planet != "" {
		parts = append(parts, planet)
	}
	return strings.Join(parts, "-")
}

// attachment is a Content-Disposition header for name: an ASCII filename
// for old clients and the full name as RFC 5987 filename*.
func attachment(name string) string {
	return `attachment; filename="` + asciiName(name) + `"; filename*=UTF-8''` + rfc5987(name)
}

// asciiName keeps the letters, digits, dot, dash and underscore of name and
// turns the rest into underscores.
func asciiName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c < utf8.RuneSelf && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// rfc5987 percent-encodes every byte of s that is not an attr-char.
func rfc5987(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
