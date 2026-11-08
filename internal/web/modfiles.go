package web

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Mod files a person sends or takes: a mod zip (SPEC 9.5) and
// mod-settings.dat (SPEC 9.6).
const (
	PathModUpload   = "/worlds/{id}/mods/upload"
	PathModSettings = "/worlds/{id}/mods/" + world.ModSettingsFile
)

// The upload limits in megabytes, as the pages say them: a mod zip's is the
// same as a download's (SPEC 14.6, 14.10); mod-settings.dat holds only
// setting values.
const (
	modUploadMB      = 200
	settingsUploadMB = 4
)

// MaxModUpload and MaxSettingsUpload are those limits in bytes.
const (
	MaxModUpload      = modUploadMB << 20
	MaxSettingsUpload = settingsUploadMB << 20
)

// megabytes says a limit as whole megabytes, "200 MB", the figure the spec
// gives, rather than the "209.7 MB" its bytes read as in decimal units.
func megabytes(n int) i18n.Msg { return i18n.M("common.bytes.mb", "n", n) }

// The upload forms' fields.
const (
	modUploadField      = "mod"
	settingsUploadField = "settings"
)

func (s *server) modFileRoutes() {
	s.handleUpload("POST "+PathModUpload, MaxModUpload, s.requireUser(s.guardUpload(s.handleModUpload)))
	s.handleUpload("POST "+PathModSettings, MaxSettingsUpload, s.requireUser(s.guardUpload(s.handleSettingsUpload)))
	s.handleLong("GET "+PathModSettings, s.requireUser(s.handleSettingsDownload))
}

// modZipError words a refused mod zip.
func modZipError(err error) i18n.Msg {
	switch {
	case errors.Is(err, world.ErrModTooBig):
		return i18n.M("worlds.upload.too_large", "limit", megabytes(modUploadMB))
	case errors.Is(err, world.ErrModName):
		return i18n.M("mods.upload.bad_name")
	}
	return i18n.M("mods.upload.not_mod")
}

// stagedInfo reads the info.json of an uploaded zip waiting to be placed.
// A name that is not a staged mod's, or a file that is gone, is
// errStagedGone, so a crafted name reads nothing.
func (s *server) stagedInfo(name string) (world.ModInfo, error) {
	info, err := s.worlds.ReadStaged(name)
	if errors.Is(err, world.ErrNotStaged) || errors.Is(err, fs.ErrNotExist) {
		return world.ModInfo{}, errStagedGone
	}
	return info, err
}

// receiveStaged receives an upload into a staged mod file, which a mod
// change may read and remove and no other upload's temp file can be taken for.
func (s *server) receiveStaged(r *http.Request, worlds *os.Root, field string, limit int64, values map[string]string) (uploaded, error) {
	create := func() (*os.File, string, error) { return world.CreateModTemp(worlds) }
	return receiveInto(r, worlds, create, field, s.worlds.Dirs().Worlds, limit, values)
}

// hasModSettings reports whether the world has a mod-settings.dat.
func (s *server) hasModSettings(id string) bool {
	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	st, err := root.Lstat(path.Join(world.ModsDir, world.ModSettingsFile))
	return err == nil && st.Mode().IsRegular()
}

// handleModUpload stages an uploaded mod zip and shows the plan that
// installs it (SPEC 9.5). From a sync's plan it answers one unresolved
// mod's upload (SPEC 9.10). Nothing in the world changes until the plan is
// confirmed.
func (s *server) handleModUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	// A sync plan's upload form carries the plan's answers in its query
	// (ModPlanPage.UploadAction), so a refusal or the new plan keeps them.
	q := r.URL.Query()
	plan, sync := readPlanForm(q)
	sync = sync && plan.Kind == mods.KindSync
	countdown := defaultCountdown()
	if c := q.Get("countdown"); c != "" {
		countdown.Countdown = c
	}
	refuse := func(msg i18n.Msg, status int) {
		if sync {
			p := s.planPage(r.Context(), sum, plan, countdown)
			p.UploadError, p.UploadFor = msg, q.Get("for")
			s.page(w, r, status, u.Username, p)
			return
		}
		p, err := s.modsPage(r.Context(), sum)
		if err != nil {
			s.notFoundOrFail(w, r, u, "list mods", err)
			return
		}
		p.UploadError = msg
		s.page(w, r, status, u.Username, p)
	}

	worlds, err := s.worlds.OpenWorlds()
	if err != nil {
		s.fail(w, r, "open the worlds folder", err)
		return
	}
	defer func() { _ = worlds.Close() }()
	up, err := s.receiveStaged(r, worlds, modUploadField, MaxModUpload, map[string]string{})
	if err != nil {
		if status, msg := worldError(err); status != http.StatusInternalServerError && !msg.IsZero() {
			refuse(msg, status)
			return
		}
		s.fail(w, r, "receive a mod zip", err)
		return
	}
	info, err := s.worlds.ReadStaged(up.Temp)
	if err != nil {
		_ = s.worlds.RemoveStaged(up.Temp)
		refuse(modZipError(err), http.StatusUnprocessableEntity)
		return
	}
	f := planForm{Kind: mods.KindUpload, Choices: map[string]mods.ChoiceKind{}, Confirmed: map[string]bool{}}
	if sync {
		if want := q.Get("for"); info.Name != want {
			_ = s.worlds.RemoveStaged(up.Temp)
			refuse(i18n.M("mods.upload.wrong_mod", "mod", want, "got", info.Name), http.StatusUnprocessableEntity)
			return
		}
		f = plan
		f.Choices[info.Name] = mods.ChoiceUpload
	}
	f.Staged = up.Temp
	p := s.planPage(r.Context(), sum, f, countdown)
	s.page(w, r, planStatus(p), u.Username, p)
}

// handleSettingsUpload replaces the world's mod-settings.dat as one
// snapshotted mod change (SPEC 9.6, 9.13). The upload form says
// what will happen, so it is the confirmation (DESIGN 7.4).
func (s *server) handleSettingsUpload(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	values := map[string]string{"countdown": "", "fingerprint": ""}
	refuse := func(msg i18n.Msg, status int, form CountdownForm) {
		p, err := s.modsPage(r.Context(), sum)
		if err != nil {
			s.notFoundOrFail(w, r, u, "list mods", err)
			return
		}
		p.SettingsError, p.CountdownForm = msg, form
		s.page(w, r, status, u.Username, p)
	}
	worlds, err := s.worlds.OpenWorlds()
	if err != nil {
		s.fail(w, r, "open the worlds folder", err)
		return
	}
	defer func() { _ = worlds.Close() }()
	// A plain temp file, so it is never taken for a staged mod zip.
	up, err := receiveFileWithFields(r, worlds, ".", settingsUploadField, s.worlds.Dirs().Worlds, MaxSettingsUpload, values)
	if err != nil {
		if status, msg := worldError(err); status != http.StatusInternalServerError && !msg.IsZero() {
			refuse(msg, status, defaultCountdown())
			return
		}
		s.fail(w, r, "receive mod-settings.dat", err)
		return
	}
	data, err := worlds.ReadFile(up.Temp)
	_ = worlds.Remove(up.Temp)
	if err != nil {
		s.fail(w, r, "read mod-settings.dat", err)
		return
	}
	if len(data) == 0 {
		refuse(i18n.M("mods.settings.empty"), http.StatusUnprocessableEntity, defaultCountdown())
		return
	}
	// The countdown came before the file in the form, so it is read here.
	r.PostForm = url.Values{"countdown": {values["countdown"]}}
	form, stop := readCountdown(r)
	running := game.IsRunning(s.game.Status(sum.ID).State)
	if running && !form.CountdownError.IsZero() {
		refuse(i18n.Msg{}, http.StatusBadRequest, form)
		return
	}
	if fp, err := s.worlds.ModsFingerprint(sum.ID); err != nil {
		s.fail(w, r, "read the mods folder", err)
		return
	} else if fp != values["fingerprint"] {
		refuse(i18n.M("mods.settings.changed"), http.StatusConflict, form)
		return
	}
	if busy := s.worldBusy(sum.ID); !busy.IsZero() {
		refuse(busy, http.StatusConflict, form)
		return
	}
	op := game.ModOp{Change: world.ModChange{Settings: data}, Fingerprint: values["fingerprint"], Summary: "upload " + world.ModSettingsFile}
	s.applyMods(w, r, u, sum, op, stop, "")
}

// handleSettingsDownload sends the world's mod-settings.dat (SPEC 9.6).
func (s *server) handleSettingsDownload(w http.ResponseWriter, r *http.Request, u store.User) {
	root, err := s.worlds.OpenWorld(r.PathValue("id"))
	if err != nil {
		s.notFoundOrFail(w, r, u, "open world", err)
		return
	}
	defer func() { _ = root.Close() }()
	name := path.Join(world.ModsDir, world.ModSettingsFile)
	st, err := root.Lstat(name)
	if err != nil || !st.Mode().IsRegular() {
		s.notFound(w, r, u)
		return
	}
	f, err := root.Open(name)
	if err != nil {
		s.notFoundOrFail(w, r, u, "open mod-settings.dat", err)
		return
	}
	defer func() { _ = f.Close() }()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": world.ModSettingsFile}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, world.ModSettingsFile, st.ModTime(), f)
}
