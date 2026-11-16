package web

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The public mods page and its downloads (SPEC 9.16, 14.18). Nothing under
// PathPublic reads a session, trusted headers or an API token.
const (
	// PathPublic is the prefix of every public path; the operator guide tells
	// the proxy to let it through.
	PathPublic     = "/public/"
	PathPublicMods = PathPublic + "mods"

	pathPublicModFile = PathPublicMods + "/{world}/files/{file}"
	pathPublicModsAll = PathPublicMods + "/{world}/all.zip"
)

// SettingPublicMods is the app setting that turns the public mods page on;
// its values are PublicModsOn and PublicModsOff, and a missing one means off.
const (
	SettingPublicMods = "public_mods"
	PublicModsOn      = "on"
	PublicModsOff     = "off"
)

// Limits of the public routes (SPEC 14.18, 9.16).
const (
	// maxPublicDownloads is how many public downloads run at once across all
	// visitors.
	maxPublicDownloads = 4
	// publicRetryAfter is the seconds a refused download is told to wait.
	publicRetryAfter = "5"
	// marksFresh and marksFailed are how long an answer from the portal and
	// a failure to get one are kept.
	marksFresh  = 15 * time.Minute
	marksFailed = time.Minute
	// marksWait is the longest a visitor waits for the portal check.
	marksWait = 5 * time.Second
	// marksCheckLimit bounds a check that nobody is waiting for any more.
	marksCheckLimit = 2 * time.Minute
)

// publicModsOn reports whether the page is on. A setting that cannot be read
// counts as off, so a database fault never opens the page.
func (s *server) publicModsOn(ctx context.Context) bool {
	v, found, err := s.store.AppSetting(ctx, SettingPublicMods)
	if err != nil {
		s.log.Error("read the public mods setting", "error", err)
		return false
	}
	return found && v == PublicModsOn
}

// setPublicMods stores the admin's choice and takes effect at once. The caller
// audits it (SPEC 9.16).
func (s *server) setPublicMods(ctx context.Context, on bool, by string) error {
	v := PublicModsOff
	if on {
		v = PublicModsOn
	}
	return s.store.SetAppSetting(ctx, SettingPublicMods, v, by, s.now())
}

// publicClosed reports whether r is for a public path while the page is off.
// Such a request is answered as an unknown path, whatever its method.
func (s *server) publicClosed(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, PathPublic) && !s.publicModsOn(r.Context())
}

// pageNotFound answers as a path no route claims.
func (s *server) pageNotFound(w http.ResponseWriter, r *http.Request) {
	s.page(w, r, http.StatusNotFound, "", ErrorPage{Status: http.StatusNotFound})
}

// setFrameRefusal asks browsers never to show the response in a frame
// (SPEC 14.13).
func setFrameRefusal(h http.Header) {
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
}

// handlePublic registers a public route. While the page is off the route
// answers as an unknown path; otherwise h runs with the public headers set.
// h must not call identify or anything that reads a session.
func (s *server) handlePublic(pattern string, h http.HandlerFunc) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if !s.publicModsOn(r.Context()) {
			s.pageNotFound(w, r)
			return
		}
		hd := w.Header()
		hd.Set("Cache-Control", "no-cache")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		setFrameRefusal(hd)
		h(w, r)
	})
}

// publicIdle lifts the server's fixed deadlines for a download anyone may
// ask for and gives each write idleLimit to make progress instead (SPEC
// 14.6, 14.18). handleLong does the same for signed-in users only.
func publicIdle(w http.ResponseWriter) (http.ResponseWriter, error) {
	rc := http.NewResponseController(w)
	if err := rc.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return idleWriter{ResponseWriter: w, rc: rc}, nil
}

func (s *server) publicModsRoutes() {
	s.handlePublic("GET "+PathPublicMods, s.handlePublicModsPage)
	s.handlePublic("GET "+pathPublicModFile, s.handlePublicModFile)
	s.handlePublic("GET "+pathPublicModsAll, s.handlePublicModsAll)
}

// PublicModFilePath is the download address of one mod zip.
func PublicModFilePath(worldID, file string) string {
	return PathPublicMods + "/" + url.PathEscape(worldID) + "/files/" + url.PathEscape(file)
}

// PublicModsAllPath is the download address of all of a world's mod zips.
func PublicModsAllPath(worldID string) string {
	return PathPublicMods + "/" + url.PathEscape(worldID) + "/all.zip"
}

// PublicModsData is what the public page shows (SPEC 9.16).
type PublicModsData struct {
	// Worlds holds the world whose game is running, or nothing when no game
	// is running, whatever the reason.
	Worlds []PublicWorld
	// PortalUnchecked is true when the portal could not be asked in time, so
	// no mod carries a mark.
	PortalUnchecked bool
}

// PublicWorld is one running world and its shareable mods, by name.
type PublicWorld struct {
	ID, Name, GameVersion string
	Mods                  []PublicMod
	// AllPath is the address of the zip of every downloadable mod, or empty
	// when the world has none.
	AllPath string
}

// PublicMod is one mod of a PublicWorld. A folder mod has no DownloadPath.
type PublicMod struct {
	Name, Title, Version string
	Folder               bool
	DownloadPath         string
	// NotOnPortal: the portal does not have the mod or this version of it.
	// Hidden: the mod is deprecated there.
	NotOnPortal, Hidden bool
}

// runningWorld returns the world whose game is running now. A game that is
// starting, stopping, crash-looping or stopped is not running (SPEC 9.16).
// Only the active world is asked about, so a visitor cannot make the
// controller keep a record for any id they choose.
func (s *server) runningWorld(ctx context.Context) (world.Summary, bool, error) {
	if s.game == nil {
		return world.Summary{}, false, nil
	}
	id, found, err := s.store.ActiveWorld(ctx)
	if err != nil || !found {
		return world.Summary{}, false, err
	}
	if s.game.Status(id).State != game.StateRunning {
		return world.Summary{}, false, nil
	}
	sum, err := s.worlds.Get(ctx, id)
	if errors.Is(err, fs.ErrNotExist) || err == nil && sum.Unreadable {
		return world.Summary{}, false, nil
	}
	return sum, err == nil, err
}

// publicModsData builds the public page's content. An error is a fault of
// Foreman's own, never of the portal.
func (s *server) publicModsData(ctx context.Context) (PublicModsData, error) {
	var data PublicModsData
	sum, running, err := s.runningWorld(ctx)
	if err != nil || !running {
		return data, err
	}
	share, err := s.worlds.ModShare(sum.ID)
	if err != nil {
		return data, err
	}
	marks, checked := s.publicMarks(ctx, sum.ID)
	data.PortalUnchecked = !checked
	pw := PublicWorld{ID: sum.ID, Name: sum.Name, GameVersion: sum.GameVersion}
	for _, mod := range share.Mods {
		pm := PublicMod{Name: mod.Name, Title: mod.Title, Version: mod.Version, Folder: mod.Local}
		if !mod.Local {
			pm.DownloadPath = PublicModFilePath(sum.ID, mod.File)
			pw.AllPath = PublicModsAllPath(sum.ID)
		}
		m := marks[modKey{mod.Name, mod.Version}]
		pm.NotOnPortal, pm.Hidden = m.notOnPortal, m.hidden
		pw.Mods = append(pw.Mods, pm)
	}
	data.Worlds = []PublicWorld{pw}
	return data, nil
}

type modKey struct{ name, version string }

// mark is what the portal check says about one mod.
type mark struct{ notOnPortal, hidden bool }

// publicMarks returns the portal marks of a world's mods. checked is false
// when the portal could not be asked, in time or at all, or when Foreman has
// no portal.
func (s *server) publicMarks(ctx context.Context, id string) (marks map[modKey]mark, checked bool) {
	if s.marks == nil {
		return nil, false
	}
	fp, err := s.worlds.ModsFingerprint(id)
	if err != nil {
		s.log.Warn("fingerprint the mods for the public page", "world", id, "error", err)
		return nil, false
	}
	flags, ok := s.marks.get(ctx, id, fp)
	if !ok {
		return nil, false
	}
	marks = map[modKey]mark{}
	for _, f := range flags {
		k := modKey{f.Name, f.Version}
		m := marks[k]
		switch f.Code {
		case mods.FlagNotOnPortal, mods.FlagVersionGone:
			m.notOnPortal = true
		case mods.FlagDeprecated:
			m.hidden = true
		}
		marks[k] = m
	}
	return marks, true
}

// checkPortalForPublic is the check the Mods tab makes, with a failure logged.
func (s *server) checkPortalForPublic(ctx context.Context, id string) ([]mods.Flag, error) {
	flags, err := s.planner.Availability(ctx, id)
	if err != nil {
		s.portalTrouble("check the mods on the portal for the public page", id, err)
	}
	return flags, err
}

// portalMarks keeps the portal's answer for each world and mods fingerprint,
// so visitors never make the portal work harder than the admin does
// (SPEC 9.16). One check runs at a time per world.
type portalMarks struct {
	now   func() time.Time
	check func(ctx context.Context, worldID string) ([]mods.Flag, error)
	// wait is how long get waits for a check.
	wait time.Duration

	mu      sync.Mutex
	byWorld map[string]*marksEntry
}

type marksEntry struct {
	fingerprint string
	flags       []mods.Flag
	failed      bool
	until       time.Time
	// running is open while a check runs and closed when it ends.
	running chan struct{}
}

func newPortalMarks(now func() time.Time, check func(context.Context, string) ([]mods.Flag, error)) *portalMarks {
	return &portalMarks{now: now, check: check, wait: marksWait, byWorld: map[string]*marksEntry{}}
}

// get returns the flags for the world's mods as fp names them. ok is false
// when the check failed or took longer than p.wait.
func (p *portalMarks) get(ctx context.Context, id, fp string) (flags []mods.Flag, ok bool) {
	timeout := time.NewTimer(p.wait)
	defer timeout.Stop()
	for {
		p.mu.Lock()
		e := p.byWorld[id]
		if e == nil {
			e = &marksEntry{}
			p.byWorld[id] = e
		}
		if e.running == nil && e.fingerprint == fp && p.now().Before(e.until) {
			flags, ok = e.flags, !e.failed
			p.mu.Unlock()
			return flags, ok
		}
		done := e.running
		if done == nil {
			done = make(chan struct{})
			e.running = done
			go p.run(id, fp, e, done) //nolint:gosec // the check must outlive the visitor who started it
		}
		p.mu.Unlock()
		select {
		case <-done:
		case <-timeout.C:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
}

// run asks the portal and keeps the answer. It does not end when the visitor
// who started it leaves, so the next visitor finds the answer.
func (p *portalMarks) run(id, fp string, e *marksEntry, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), marksCheckLimit)
	defer cancel()
	flags, err := p.check(ctx, id)
	p.mu.Lock()
	e.fingerprint, e.flags, e.failed = fp, flags, err != nil
	keep := marksFresh
	if err != nil {
		keep = marksFailed
	}
	e.until = p.now().Add(keep)
	e.running = nil
	p.mu.Unlock()
	close(done)
}

// publicShare finds the mod list of the world in the address, which must be
// the world whose game is running. It answers 404 itself and returns false
// for any other world. Whatever the address holds is only compared, never
// used to build a path (SPEC 14.18).
func (s *server) publicShare(w http.ResponseWriter, r *http.Request) (*world.ModShare, world.Summary, bool) {
	sum, running, err := s.runningWorld(r.Context())
	if err != nil {
		s.fail(w, r, "find the running world", err)
		return nil, sum, false
	}
	if !running || sum.ID != r.PathValue("world") {
		s.pageNotFound(w, r)
		return nil, sum, false
	}
	share, err := s.worlds.ModShare(sum.ID)
	if errors.Is(err, fs.ErrNotExist) {
		s.pageNotFound(w, r)
		return nil, sum, false
	}
	if err != nil {
		s.fail(w, r, "list the mods for download", err)
		return nil, sum, false
	}
	return share, sum, true
}

// takePublicSlot reserves one of the download places, or answers 503 and
// returns false. The caller gives the place back with releasePublicSlot.
func (s *server) takePublicSlot(w http.ResponseWriter, r *http.Request) bool {
	select {
	case s.publicSlots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", publicRetryAfter)
		s.page(w, r, http.StatusServiceUnavailable, "", ErrorPage{Status: http.StatusServiceUnavailable})
		return false
	}
}

func (s *server) releasePublicSlot() { <-s.publicSlots }

// handlePublicModFile sends one mod zip, answering conditional and range
// requests as a save download does (SPEC 9.16, 5.11).
func (s *server) handlePublicModFile(w http.ResponseWriter, r *http.Request) {
	share, _, ok := s.publicShare(w, r)
	if !ok {
		return
	}
	f, info, err := share.Open(r.PathValue("file"))
	if errors.Is(err, world.ErrModFileNotFound) {
		s.pageNotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, r, "open a mod zip for download", err)
		return
	}
	defer func() { _ = f.Close() }()
	if !s.takePublicSlot(w, r) {
		return
	}
	defer s.releasePublicSlot()
	iw, err := publicIdle(w)
	if err != nil {
		s.fail(w, r, "lift the download deadlines", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", attachment(info.Name()))
	http.ServeContent(iw, r, info.Name(), info.ModTime(), f)
}

// handlePublicModsAll streams every downloadable mod zip as one zip, stored
// without compression, named for the world (SPEC 9.16).
func (s *server) handlePublicModsAll(w http.ResponseWriter, r *http.Request) {
	share, sum, ok := s.publicShare(w, r)
	if !ok {
		return
	}
	var files []world.InstalledMod
	for _, mod := range share.Mods {
		if !mod.Local {
			files = append(files, mod)
		}
	}
	if len(files) == 0 {
		s.pageNotFound(w, r)
		return
	}
	if !s.takePublicSlot(w, r) {
		return
	}
	defer s.releasePublicSlot()
	iw, err := publicIdle(w)
	if err != nil {
		s.fail(w, r, "lift the download deadlines", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", attachment(previewFileName(sum.Name, "mods", "")+".zip"))
	zw := zip.NewWriter(iw)
	for _, mod := range files {
		if err := addModZip(zw, share, mod.File); err != nil {
			// The reply has started, so end the connection: a download that
			// stops early must not look like a whole zip.
			if !clientGone(r, err) {
				s.log.Warn("stream the mods zip", "world", sum.ID, "error", err)
			}
			panic(http.ErrAbortHandler)
		}
	}
	if err := zw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// addModZip copies one mod zip into zw as it is, without compressing it again.
func addModZip(zw *zip.Writer, share *world.ModShare, file string) error {
	f, info, err := share.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: file, Method: zip.Store, Modified: info.ModTime()})
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, f)
	return err
}
