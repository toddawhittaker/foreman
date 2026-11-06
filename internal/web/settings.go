package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Settings routes (SPEC 11, DESIGN 8.10).
const (
	PathSettings        = "/worlds/{id}/settings"
	PathSettingsRaw     = "/worlds/{id}/settings/raw/{file}"
	PathSettingsConfirm = "/worlds/{id}/settings/confirm"
)

// rawBodyLimit is the largest raw-editor post: the 1 MB the text may hold,
// at its largest when percent-encoded, plus the other fields.
const rawBodyLimit = 4 << 20

func settingsPath(id string) string        { return worldPath(id) + "/settings" }
func settingsConfirmPath(id string) string { return settingsPath(id) + "/confirm" }
func settingsRawPath(id, file string) string {
	return settingsPath(id) + "/raw/" + file
}

// ActiveSettingsPath is the settings page of the world the Server page is
// about (DESIGN 8.10).
func (p ServerPage) ActiveSettingsPath() string { return p.ActivePath() + "/settings" }

type settingsHandlers struct {
	s       *server
	pending *pendingStore
}

func (s *server) settingsRoutes() {
	h := &settingsHandlers{s: s, pending: newPendingStore(s.now)}
	s.mux.HandleFunc("GET "+PathSettings, s.requireUser(h.handleForm))
	s.mux.HandleFunc("POST "+PathSettings, s.requireUser(h.handlePreview))
	s.mux.HandleFunc("GET "+PathSettingsRaw, s.requireUser(h.handleRawForm))
	s.handleUpload("POST "+PathSettingsRaw, rawBodyLimit, s.requireUser(h.handleRawPreview))
	// A running game saves before its snapshot (SPEC 6.10), which can outlast
	// the usual deadlines.
	s.handleLong("POST "+PathSettingsConfirm, s.requireUser(h.handleConfirm))
}

// settingsWorld is a world and its settings files as they are on disk now.
type settingsWorld struct {
	sum  world.Summary
	line string
	// cfg and adminCfg are server-settings.json and server-adminlist.json.
	cfg, adminCfg world.ConfigRead
	admins        []string
	adminsBroken  bool
	running       bool
	// readOnly says why the world cannot be changed; zero when it can.
	readOnly i18n.Msg
}

// text is server-settings.json as the form reads it: a missing file is an
// empty object.
func (w settingsWorld) text() []byte {
	if !w.cfg.Found {
		return []byte("{}")
	}
	return w.cfg.Data
}

// formText is the text the form reads. A world with no file starts with its
// own name as the server name and auto-pause on, so a first save has both
// (SPEC 11.1).
func (w settingsWorld) formText() []byte {
	if w.cfg.Found {
		return w.cfg.Data
	}
	return append(append([]byte(`{"name": `), jsonString(w.sum.Name)...), `, "auto_pause": true}`...)
}

// load finds the world named in the path and reads its two settings files.
// It answers the request itself and returns false when it cannot.
func (h *settingsHandlers) load(w http.ResponseWriter, r *http.Request, u store.User) (settingsWorld, bool) {
	s := h.s
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return settingsWorld{}, false
	}
	sw := settingsWorld{sum: sum}
	sw.line, _ = mapgen.Line(sum.GameVersion)
	var err error
	if sw.cfg, err = s.worlds.ReadConfig(sum.ID, world.ServerSettingsFile); err != nil {
		s.notFoundOrFail(w, r, u, "read server settings", err)
		return settingsWorld{}, false
	}
	if sw.adminCfg, err = s.worlds.ReadConfig(sum.ID, world.ServerAdminListFile); err != nil {
		s.notFoundOrFail(w, r, u, "read admin list", err)
		return settingsWorld{}, false
	}
	if sw.adminCfg.Found {
		if sw.admins, err = gamesettings.ParseAdminList(sw.adminCfg.Data); err != nil {
			sw.admins, sw.adminsBroken = nil, true
		}
	}
	if s.game != nil {
		switch s.game.Status(sum.ID).State {
		case game.StateRunning, game.StateStarting:
			sw.running = true
		}
	}
	switch {
	case sum.Newer:
		sw.readOnly = i18n.M("worlds.error.newer_format")
	case sum.Unreadable:
		sw.readOnly = i18n.M("settings.readonly.unreadable")
	}
	return sw, true
}

// formPage fills the form from the settings text and admin names given.
func (sw settingsWorld) formPage(text []byte, names []string) SettingsPage {
	p := SettingsPage{
		WorldID: sw.sum.ID, WorldName: sw.sum.Name, ReadOnly: sw.readOnly,
		Running: sw.running, AdminsBroken: sw.adminsBroken,
		Admins: strings.Join(names, "\n"),
	}
	cur, ok := readServerValues(text)
	if !ok {
		p.Broken = true
		return p
	}
	p.Name, p.Description = cur.Name, cur.Description
	p.Visibility = string(cur.Visibility)
	switch cur.Visibility {
	case "":
		// The game needs a visibility, so the form saves one (SPEC 11.2).
		p.Visibility = string(gamesettings.VisibilityHidden)
		p.VisibilityHint = i18n.M("settings.visibility.unset")
	case gamesettings.VisibilityPublic:
		p.Visibility = ""
		p.VisibilityHint = i18n.M("settings.visibility.public")
	}
	p.PasswordSet = cur.PasswordSet
	p.MaxPlayers, p.Interval, p.Slots = cur.MaxPlayers, cur.Interval, cur.Slots
	p.Verify, p.AutoPause = cur.Verify, cur.AutoPause
	return p
}

// fromEdit returns the texts of the previewed change under token, for a form
// refilled after "Go back and edit".
func (h *settingsHandlers) fromEdit(token string, u store.User, id string, raw bool) (*pendingChange, bool) {
	c, ok := h.pending.get(token, u.ID, id)
	if !ok || c.Raw != raw {
		return nil, false
	}
	return c, true
}

func (c *pendingChange) file(name string) (pendingFile, bool) {
	for _, f := range c.Files {
		if f.File == name {
			return f, true
		}
	}
	return pendingFile{}, false
}

// fileNames lists the files of a change, for the audit entry and the log.
func fileNames(files []pendingFile) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.File
	}
	return names
}

func (h *settingsHandlers) handleForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sw, ok := h.load(w, r, u)
	if !ok {
		return
	}
	text, names, edit := sw.formText(), sw.admins, ""
	if c, ok := h.fromEdit(r.URL.Query().Get("edit"), u, sw.sum.ID, false); ok {
		edit = r.URL.Query().Get("edit")
		if f, ok := c.file(world.ServerSettingsFile); ok {
			text = f.New
		}
		if f, ok := c.file(world.ServerAdminListFile); ok {
			if n, err := gamesettings.ParseAdminList(f.New); err == nil {
				names = n
			}
		}
	}
	p := sw.formPage(text, names)
	p.Edit = edit
	p.Connected = h.connected(r.Context(), sw, p.Admins)
	p.PasswordPending = p.PasswordSet && !bytes.Equal(carriedPassword(sw.formText()), carriedPassword(text))
	h.fillDone(&p, sw, r)
	h.s.page(w, r, http.StatusOK, u.Username, p)
}

// connected lists who is in the running game and could be made an admin, for
// the form (SPEC 11.1, 8.1). It is empty when the game is not running, when
// nobody is on, and when the game cannot say.
func (h *settingsHandlers) connected(ctx context.Context, sw settingsWorld, adminsText string) []string {
	if h.s.game == nil || !sw.running || sw.adminsBroken || !sw.readOnly.IsZero() {
		return nil
	}
	return connected(h.s.online(ctx, sw.sum.ID).Names, adminsText)
}

// fillDone shows the result of the save the browser was sent back from. The
// link names a snapshot of this world taken for a settings change, so a
// crafted link cannot put other words on the page.
func (h *settingsHandlers) fillDone(p *SettingsPage, sw settingsWorld, r *http.Request) {
	q := r.URL.Query()
	if q.Get("saved") != "1" {
		return
	}
	if name := q.Get("snap"); name != "" {
		sn, err := h.s.worlds.Snapshot(sw.sum.ID, name)
		if err != nil || sn.Reason != world.ReasonSettings {
			return
		}
		p.Done, p.DoneSnap = i18n.M("settings.done.saved", "label", snapshotLabelArg(sn)), sn.Name
	} else {
		p.Done = i18n.M("settings.done.saved_plain")
	}
	p.DoneRestart = sw.running && q.Get("restart") == "1"
}

// handlePreview turns the posted form into a pending change and shows its
// diff (SPEC 11.3).
func (h *settingsHandlers) handlePreview(w http.ResponseWriter, r *http.Request, u store.User) {
	s := h.s
	sw, ok := h.load(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	if !sw.readOnly.IsZero() {
		p := sw.formPage(sw.formText(), sw.admins)
		p.Error = sw.readOnly
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	text := sw.formText()
	_, readable := readServerValues(text)
	if !readable {
		p := sw.formPage(text, sw.admins)
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	in := readPostedForm(r.PostFormValue, func(k string) bool { _, ok := r.PostForm[k]; return ok })
	var carried []byte
	edit := r.PostFormValue("edit")
	if c, ok := h.fromEdit(edit, u, sw.sum.ID, false); ok {
		if f, ok := c.file(world.ServerSettingsFile); ok {
			carried = carriedPassword(f.New)
		}
	} else {
		edit = ""
	}
	if !sw.adminsBroken {
		in.addMadeAdmin()
	}
	var names []string
	var adminErr i18n.Msg
	if in.AdminsPosted && !sw.adminsBroken {
		var bad int
		if names, bad = adminLines(in.Admins); bad > 0 {
			adminErr = i18n.M("settings.admins.bad_name", "line", bad, "max", gamesettings.MaxNameBytes)
		}
	}
	edit2 := formEdit{BaseText: text, BaseAdmins: sw.admins, In: in, Carried: carried, Names: names}
	files, errs, err := buildFiles(sw, edit2)
	if err != nil {
		s.fail(w, r, "apply settings form", err)
		return
	}
	if !adminErr.IsZero() {
		errs.AdminsError = adminErr
	}
	if errs.has() {
		p := sw.formPage(text, sw.admins)
		p.Name, p.Description, p.MaxPlayers = in.Name, in.Description, in.MaxPlayers
		p.Interval, p.Slots, p.Verify, p.AutoPause = in.Interval, in.Slots, in.Verify, in.AutoPause
		p.Admins, p.Edit, p.formErrors = in.Admins, edit, errs
		p.Connected = h.connected(r.Context(), sw, in.Admins)
		p.PasswordDropped = in.Password != ""
		if v := gamesettings.Visibility(in.Visibility); slices.Contains(gamesettings.FormVisibilities, v) {
			p.Visibility = in.Visibility
		}
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	h.showPreview(w, r, u, sw, pendingChange{UserID: u.ID, WorldID: sw.sum.ID, Form: &edit2, Files: files}, false)
}

// formEdit is what a form submission asked for, kept with its pending change
// so the change can be applied again to files that moved before it was
// confirmed, without undoing what moved them.
type formEdit struct {
	// BaseText and BaseAdmins are the files as the form showed them.
	BaseText   []byte
	BaseAdmins []string
	In         postedForm
	Carried    []byte
	// Names are the admins as posted; used only when In.AdminsPosted.
	Names []string
}

// buildFiles works out the files a form edit changes, applied to the files as
// they are now: only fields the person changed from what the form showed are
// written, and only admins they added or removed.
func buildFiles(sw settingsWorld, e formEdit) ([]pendingFile, formErrors, error) {
	shown, ok := readServerValues(e.BaseText)
	if !ok {
		return nil, formErrors{}, errors.New("server-settings.json is not an object")
	}
	text := sw.formText()
	next, errs, err := applyForm(text, shown, e.In, e.Carried)
	if err != nil || errs.has() {
		return nil, errs, err
	}
	var files []pendingFile
	if len(gamesettings.Diff(sw.cfg.Data, next)) > 0 {
		files = append(files, pendingFile{File: world.ServerSettingsFile, Hash: sw.cfg.Hash, New: next})
	}
	if e.In.AdminsPosted && !sw.adminsBroken {
		promote, demote := gamesettings.AdminDiff(e.BaseAdmins, e.Names)
		target := applyAdminChange(sw.admins, promote, demote)
		if p, d := gamesettings.AdminDiff(sw.admins, target); len(p)+len(d) > 0 {
			files = append(files, pendingFile{File: world.ServerAdminListFile, Hash: sw.adminCfg.Hash, New: adminListJSON(target)})
		}
	}
	return files, errs, nil
}

// applyAdminChange adds the promoted names to current and drops the demoted
// ones, comparing without regard to case.
func applyAdminChange(current, promote, demote []string) []string {
	drop := map[string]bool{}
	for _, n := range demote {
		drop[strings.ToLower(n)] = true
	}
	have := map[string]bool{}
	out := []string{}
	for _, n := range current {
		if k := strings.ToLower(n); !drop[k] {
			have[k] = true
			out = append(out, n)
		}
	}
	for _, n := range promote {
		if k := strings.ToLower(n); !have[k] {
			have[k] = true
			out = append(out, n)
		}
	}
	return out
}

// showPreview stores change and shows what it would do. A change with no
// file left is shown as "Nothing changed" and not stored.
func (h *settingsHandlers) showPreview(w http.ResponseWriter, r *http.Request, u store.User, sw settingsWorld, change pendingChange, changedSince bool) {
	p := h.previewPage(sw, change)
	p.FileChanged = changedSince
	if len(change.Files) > 0 {
		p.Token = h.pending.add(change)
	}
	h.s.page(w, r, http.StatusOK, u.Username, p)
}

// handleConfirm saves a previewed change. Under the world's lock the game
// layer saves a running game, takes the settings snapshot, checks the file is
// the one previewed and writes it (SPEC 11.3).
func (h *settingsHandlers) handleConfirm(w http.ResponseWriter, r *http.Request, u store.User) {
	s := h.s
	sw, ok := h.load(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	id := sw.sum.ID
	token := r.PostFormValue("token")
	c, ok := h.pending.take(token, u.ID, id)
	if !ok {
		p := sw.formPage(sw.formText(), sw.admins)
		p.Error = i18n.M("settings.error.expired")
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if !sw.readOnly.IsZero() {
		p := sw.formPage(sw.formText(), sw.admins)
		p.Error = sw.readOnly
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if h.moved(sw, c) {
		h.showPreview(w, r, u, sw, *c, true)
		return
	}
	res, err := h.save(r.Context(), u, id, c.Files)
	if err != nil {
		h.saveFailed(w, r, u, id, c, err)
		return
	}
	to := settingsPath(id) + "?saved=1&snap=" + res.Snapshot.Name
	if res.RestartNeeded {
		to += "&restart=1"
	}
	if res.Live {
		to += "&live=1"
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// moved reports whether a file of c is no longer as it was at the preview.
// If so it brings c up to the files as they are now: a form change is applied
// again to them, so what moved them is kept; any other change keeps its text
// and takes the new hash, so the diff shows what saving would do to the file
// as it is.
func (h *settingsHandlers) moved(sw settingsWorld, c *pendingChange) bool {
	moved := false
	for _, f := range c.Files {
		now := sw.cfg
		if f.File == world.ServerAdminListFile {
			now = sw.adminCfg
		}
		moved = moved || now.Hash != f.Hash
	}
	if !moved {
		return false
	}
	if c.Form != nil {
		if files, errs, err := buildFiles(sw, *c.Form); err == nil && !errs.has() {
			c.Files = files
			return true
		}
	}
	for i, f := range c.Files {
		now := sw.cfg
		if f.File == world.ServerAdminListFile {
			now = sw.adminCfg
		}
		c.Files[i].Hash = now.Hash
	}
	return true
}

// saveFailed answers a confirmation that did not complete. A file that
// changed under the save is shown as a new preview; anything else is an
// error on the form. When the files were written but the running game could
// not be told of the admin change, the error names what was saved.
func (h *settingsHandlers) saveFailed(w http.ResponseWriter, r *http.Request, u store.User, id string, c *pendingChange, err error) {
	s := h.s
	sw, ok := h.load(w, r, u)
	if !ok {
		return
	}
	var saved []string
	if pe, ok := errors.AsType[*game.PartialSaveError](err); ok {
		saved = pe.Saved
	}
	rest := &pendingChange{UserID: c.UserID, WorldID: c.WorldID, Raw: c.Raw, Form: c.Form}
	for _, f := range c.Files {
		if !slices.Contains(saved, f.File) {
			rest.Files = append(rest.Files, f)
		}
	}
	if errors.Is(err, world.ErrSettingsChanged) || h.moved(sw, rest) {
		h.showPreview(w, r, u, sw, *rest, true)
		return
	}
	status, msg := settingsError(err)
	files := fileNames(c.Files)
	if errors.Is(err, game.ErrSettingsInvalid) {
		// The error names a line and a key, never a value.
		s.log.Warn("settings refused", "world", id, "files", files, "error", err)
	}
	if status == http.StatusInternalServerError || status == http.StatusNotFound || msg.IsZero() {
		s.log.Error("save settings", "path", r.URL.Path, "world", id, "files", files, "error", err)
		status, msg = http.StatusInternalServerError, i18n.M("settings.error.failed")
	}
	p := sw.formPage(sw.formText(), sw.admins)
	p.Error = msg
	if len(saved) > 0 {
		p.Error = i18n.M("settings.error.partial", "saved", strings.Join(saved, ", "), "failed", world.ServerAdminListFile, "reason", msg)
	}
	s.page(w, r, status, u.Username, p)
}

// settingsError words a refusal of a settings save.
func settingsError(err error) (int, i18n.Msg) {
	switch {
	case errors.Is(err, game.ErrSettingsInvalid):
		return http.StatusUnprocessableEntity, i18n.M("settings.error.invalid")
	case errors.Is(err, world.ErrConfigTooBig):
		return http.StatusRequestEntityTooLarge, i18n.M("settings.error.too_big", "size", i18n.Size(gamesettings.MaxBytes))
	}
	return gameError(err)
}

// save writes the files of a change. With server control the game layer does
// it, because a running game must be saved first and told of an admin change
// (SPEC 6.10, 11.3). With none there is no game to save, so Foreman takes the
// lock and runs the same world function.
func (h *settingsHandlers) save(ctx context.Context, u store.User, id string, files []pendingFile) (game.SettingsResult, error) {
	s := h.s
	if s.game != nil {
		changes := make([]game.SettingsChange, len(files))
		for i, f := range files {
			changes[i] = game.SettingsChange{File: f.File, Data: f.New, SeenHash: f.Hash}
		}
		return s.game.SaveSettings(ctx, id, actor(u), changes)
	}
	unlock, err := s.worlds.Lock(id)
	if err != nil {
		return game.SettingsResult{}, err
	}
	defer unlock()
	changes := make([]world.ConfigChange, len(files))
	for i, f := range files {
		changes[i] = world.ConfigChange{File: f.File, Data: f.New, SeenHash: f.Hash}
	}
	snap, err := s.worlds.SaveSettingsHeld(id, changes, u.Username, func() error {
		err := s.store.AddAudit(ctx, store.AuditEntry{
			Time: s.now(), UserID: u.ID, Username: u.Username, Action: game.AuditSettingsSave,
			WorldID: id, Detail: strings.Join(fileNames(files), ", "),
		})
		if err != nil {
			return fmt.Errorf("record settings save: %w", err)
		}
		return nil
	})
	return game.SettingsResult{Snapshot: snap}, err
}

// SettingsPreviewPage shows what a settings change would do, before it is
// saved (SPEC 11.3, DESIGN 8.10).
type SettingsPreviewPage struct {
	WorldID, WorldName string
	// Token names the pending change the Save button confirms; empty when
	// there is nothing to save.
	Token string
	// Raw is true when the change came from the JSON editor.
	Raw bool
	// Settings holds the changes to server-settings.json.
	Settings []ChangeRow
	// Promote and Demote are the admins to add and remove.
	Promote, Demote []string
	Running         bool
	// FileChanged is true when the file moved on since the preview, so these
	// are the changes against the file as it is now.
	FileChanged bool
}

// ChangeRow is one key that differs.
type ChangeRow struct {
	Word           i18n.Msg
	Key            string
	Old, New       string
	HasOld, HasNew bool
	// Secret rows show no value, only what will happen to it.
	Secret     bool
	SecretNote i18n.Msg
	Unknown    bool
}

func (SettingsPreviewPage) templateName() string { return "settingspreview" }
func (SettingsPreviewPage) navItem() string      { return PathWorlds }

// Tabs are the world's pages, with Settings current (DESIGN 6).
func (p SettingsPreviewPage) Tabs() WorldTabs {
	return WorldTabs{Path: worldPath(p.WorldID), Current: "settings", Settings: true}
}

func (p SettingsPreviewPage) WorldPath() string     { return worldPath(p.WorldID) }
func (p SettingsPreviewPage) ConfirmPath() string   { return settingsConfirmPath(p.WorldID) }
func (p SettingsPreviewPage) SnapshotsPath() string { return snapshotsPath(p.WorldID) }
func (SettingsPreviewPage) File() string            { return gamesettings.FileServerSettings }
func (SettingsPreviewPage) AdminFile() string       { return world.ServerAdminListFile }

// BackPath returns to the form or editor with the edits kept, or to a fresh
// one when there is nothing to keep.
func (p SettingsPreviewPage) BackPath() string {
	to := settingsPath(p.WorldID)
	if p.Raw {
		to = settingsRawPath(p.WorldID, gamesettings.FileServerSettings)
	}
	if p.Token != "" {
		to += "?edit=" + p.Token
	}
	return to
}

// Nothing is true when the change holds no difference.
func (p SettingsPreviewPage) Nothing() bool {
	return len(p.Settings) == 0 && len(p.Promote)+len(p.Demote) == 0
}

// Admins is true when the admin list changes.
func (p SettingsPreviewPage) Admins() bool { return len(p.Promote)+len(p.Demote) > 0 }

// Restart is true when a running game needs a restart to use the change.
func (p SettingsPreviewPage) Restart() bool { return p.Running && len(p.Settings) > 0 }

// AdminsLive is true when a running game takes the admin change at once.
func (p SettingsPreviewPage) AdminsLive() bool { return p.Running && p.Admins() }

func (p SettingsPreviewPage) RestartPath() string { return gamePath(p.WorldID, "restart") }

// previewPage works out the diff of change against the files as they are now.
func (h *settingsHandlers) previewPage(sw settingsWorld, change pendingChange) SettingsPreviewPage {
	p := SettingsPreviewPage{WorldID: sw.sum.ID, WorldName: sw.sum.Name, Raw: change.Raw, Running: sw.running}
	for _, f := range change.Files {
		switch f.File {
		case world.ServerSettingsFile:
			p.Settings = changeRows(sw.line, gamesettings.Diff(sw.cfg.Data, f.New))
		case world.ServerAdminListFile:
			if next, err := gamesettings.ParseAdminList(f.New); err == nil {
				p.Promote, p.Demote = gamesettings.AdminDiff(sw.admins, next)
			}
		}
	}
	return p
}

func changeRows(line string, changes []gamesettings.Change) []ChangeRow {
	rows := make([]ChangeRow, 0, len(changes))
	for _, c := range changes {
		row := ChangeRow{Key: c.Key, Secret: c.Secret}
		switch c.Kind {
		case gamesettings.KindAdded:
			row.Word = i18n.M("settings.diff.added")
		case gamesettings.KindChanged:
			row.Word = i18n.M("settings.diff.changed")
		default:
			row.Word = i18n.M("settings.diff.removed")
		}
		if c.Secret {
			switch c.Kind {
			case gamesettings.KindAdded:
				row.SecretNote = i18n.M("settings.diff.secret_added")
			case gamesettings.KindChanged:
				row.SecretNote = i18n.M("settings.diff.secret_changed")
			default:
				row.SecretNote = i18n.M("settings.diff.secret_removed")
			}
		} else {
			row.Old, row.HasOld = c.Old, c.Kind != gamesettings.KindAdded
			row.New, row.HasNew = c.New, c.Kind != gamesettings.KindRemoved
		}
		row.Unknown = gamesettings.UnknownKey(gamesettings.FileServerSettings, line, c.Key)
		rows = append(rows, row)
	}
	return rows
}
