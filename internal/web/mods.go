package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// A world's mods pages (SPEC 9).
const (
	PathWorldMods = "/worlds/{id}/mods"
	PathModStatus = "/worlds/{id}/mods/status"
	PathModNotice = "/worlds/{id}/mods/notice"
	PathModSearch = "/worlds/{id}/mods/search"
	PathModPlan   = "/worlds/{id}/mods/plan"
	// PathModPlanRedraw takes the review page's answers and draws the plan
	// again, keeping them in the page's draft.
	PathModPlanRedraw = "/worlds/{id}/mods/plan/redraw"
)

// ModPortal is what the mods pages ask the mod portal; *modportal.Client
// has it.
type ModPortal interface {
	mods.Portal
	Search(ctx context.Context, line, query string) ([]modportal.Hit, error)
	Lookup(ctx context.Context, line, name string) (modportal.Hit, error)
	Download(ctx context.Context, username, token string, r modportal.Release, dst *os.File, maxBytes int64) error
	// KnownMissing says the portal's cached answer is "no such mod"; it never asks the portal.
	KnownMissing(name string) bool
}

// accountFetcher downloads with the factorio.com account the service holds
// now, so a newly entered account works without a restart (SPEC 9.11).
type accountFetcher struct {
	account *factorioaccount.Service
	portal  ModPortal
	// limit is the mod upload limit, which also caps a download (SPEC 14.10).
	limit limitFunc
	// log records each failed download; nil logs nothing.
	log *slog.Logger
}

func (f accountFetcher) Download(ctx context.Context, r modportal.Release, dst *os.File) error {
	acct, src, _, err := f.account.Get(ctx)
	if err != nil {
		return err
	}
	if !canDownload(src) {
		return game.ErrNoModFetcher
	}
	err = f.portal.Download(ctx, acct.Username.Reveal(), acct.Token.Reveal(), r, dst, f.limit(ctx))
	if err != nil && f.log != nil {
		// Only a failure of the portal download is logged here. The release
		// text comes from the portal, so each part is cut short; no error
		// from the download carries the account's username or token (SPEC 14.10).
		f.log.Warn("download a mod", "mod", clipLogged(strings.TrimSuffix(r.FileName, "_"+r.Version+".zip")),
			"version", clipLogged(r.Version), "reason", clipLogged(err.Error()))
	}
	return err
}

// maxLoggedPortalText is the most bytes of portal-supplied text one log
// field holds.
const maxLoggedPortalText = 200

// clipLogged cuts s for the log, never inside a character, and marks the cut.
func clipLogged(s string) string {
	if cut, ok := cutUTF8(s, maxLoggedPortalText); ok {
		return cut + "…"
	}
	return s
}

func modsPath(id string) string { return worldPath(id) + "/mods" }

// planPath is the plan page for a request given as query values. Links
// into the review page carry the kind and at most one name, a favorite's
// id, or a draft's token; the page keeps the rest in the draft, so no
// address grows with the plan (SPEC 17).
func planPath(id string, q url.Values) string {
	return modsPath(id) + "/plan?" + q.Encode()
}

// kindPlanPath opens a plan of kind for one mod, or for every mod the kind
// covers when name is "": the saved picks of an install, every update.
func kindPlanPath(id string, kind mods.Kind, name string) string {
	q := url.Values{"kind": {string(kind)}}
	if name != "" {
		q.Set("name", name)
	}
	return planPath(id, q)
}

// draftPath is the review page drawn from a draft, with one change to it
// when more is given as a key and a value.
func draftPath(id, token string, more ...string) string {
	q := url.Values{draftField: {token}}
	for i := 0; i+1 < len(more); i += 2 {
		q.Set(more[i], more[i+1])
	}
	return planPath(id, q)
}

// syncPath is the plan that matches a world's mods to the save that loads
// (SPEC 9.8, 7.11).
func syncPath(id string) string { return kindPlanPath(id, mods.KindSync, "") }

func (s *server) modRoutes() {
	s.mux.HandleFunc("GET "+PathWorldMods, s.requireUser(s.handleMods))
	s.mux.HandleFunc("GET "+PathModStatus, s.requireUser(s.handleModStatus))
	s.mux.HandleFunc("GET "+PathModNotice, s.requireUser(s.handleModNotice))
	s.mux.HandleFunc("GET "+PathModSearch, s.requireUser(s.handleModSearch))
	s.mux.HandleFunc("GET "+PathModPlan, s.requireUser(s.handlePlanForm))
	s.mux.HandleFunc("POST "+PathModPlan, s.requireUser(s.handlePlan))
	s.mux.HandleFunc("POST "+PathModPlanRedraw, s.requireUser(s.handlePlanRedraw))
	s.mux.HandleFunc("POST "+PathModPlan+"/gone", s.requireUser(s.handlePlanGone))
	s.modPlanRoutes()
	s.modFileRoutes()
	s.modSettingsRoutes()
}

// modsWorld finds the world in the path when Foreman can change its mods,
// or answers the request itself.
func (s *server) modsWorld(w http.ResponseWriter, r *http.Request, u store.User) (world.Summary, bool) {
	if s.game == nil || s.planner == nil {
		s.notFound(w, r, u)
		return world.Summary{}, false
	}
	return s.findWorld(w, r, u)
}

// downloadsOn reports whether a factorio.com account is set, so mods can
// be downloaded (SPEC 9.11). A database failure counts as no account.
func (s *server) downloadsOn(ctx context.Context) bool {
	_, src, _, err := s.account.Get(ctx)
	if err != nil {
		s.log.Error("read the factorio.com account", "error", err)
		return false
	}
	return canDownload(src)
}

// canDownload reports whether an account from src can download mods: one
// from the environment or stored in the UI, not a missing or unreadable one
// (SPEC 9.11).
func canDownload(src factorioaccount.Source) bool {
	return src == factorioaccount.Env || src == factorioaccount.Stored
}

// accountLink is the account page, returning to back after saving.
func accountLink(back string) string {
	return PathAccount + "?" + url.Values{"return": {back}}.Encode()
}

// loadingSave is the save the game loads next: the chosen one, else the
// newest (SPEC 7.11); "" when the world has none.
func (s *server) loadingSave(id string) (string, error) {
	list, err := s.worlds.Saves(id)
	if err != nil {
		return "", err
	}
	if list.LoadSave != "" && slices.ContainsFunc(list.Saves, func(sv world.Save) bool { return sv.Name == list.LoadSave }) {
		return list.LoadSave, nil
	}
	if len(list.Saves) == 0 {
		return "", nil
	}
	return list.Saves[0].Name, nil
}

// ModsPage is a world's Mods tab (SPEC 9).
type ModsPage struct {
	// UploadLimit is the mod upload limit in bytes, which the drop zone shows.
	UploadLimit uint64
	World       world.Summary
	Mods        []world.InstalledMod
	// NoAccount says mod downloads are off (SPEC 9.11).
	NoAccount bool
	// LoadSave is the save that loads, whose mods "Match" reads; "" for none.
	LoadSave string
	// SaveDiff is how the world's mods differ from LoadSave's; nil when
	// there is no save or Foreman cannot read it (SPEC 9.8, 9.9).
	SaveDiff *SaveDiff
	// HasSettings says the world has a mod-settings.dat to download.
	HasSettings bool
	Running     bool
	CountdownForm
	// Fingerprint is the mods folder the settings upload was offered for.
	Fingerprint   string
	UploadError   i18n.Msg
	SettingsError i18n.Msg
	// Busy, ServerLink and Working follow an action on the world, as the
	// world page does; Done, Failure and Undo say how a mod change of a
	// world that is not active ended (SPEC 17.9).
	Busy       i18n.Msg
	ServerLink bool
	Working    bool
	Done       i18n.Msg
	Failure    *Failure
	Undo       *Offer
	// Search is the empty results panel the typing fills; Plan is the
	// world's unfinished plan beside it (SPEC 9.1, 9.2).
	Search ModSearchPage
	Plan   PlanPanel
	// NoModSettings says step 3 of the guided Generate has nothing to set
	// (SPEC 5.6).
	NoModSettings bool
	// Generated says step 1 has just made the world (SPEC 5.6, 17.9).
	Generated bool
}

// InFlow says the tab is step 2 of the guided Generate: the world has
// new_map and no save (SPEC 5.6).
func (p ModsPage) InFlow() bool { return p.World.Unfinished }

// Steps is the step bar with step 2 current.
func (p ModsPage) Steps() []FlowStep { return flowSteps(p.World.ID, flowStepMods, p.NoModSettings) }

// NextPath is step 3, the mod settings, which passes on to the map
// settings when there is nothing to set.
func (p ModsPage) NextPath() string { return flowModSettingsPath(p.World.ID) }

// ShowSkip says the tab offers a way past the mods. After a mod change the
// result offers Next instead (SPEC 17.9).
func (p ModsPage) ShowSkip() bool { return p.InFlow() && p.Done.IsZero() }

// UploadZone and SettingsZone are the tab's two upload fields (DESIGN 7.10).
func (p ModsPage) UploadZone() DropZone {
	return DropZone{ID: "mod-file", Name: modUploadField, Label: i18n.M("mods.upload.label"),
		Accept: ".zip", MaxBytes: p.UploadLimit, Error: p.UploadError}
}

func (p ModsPage) SettingsZone() DropZone {
	return DropZone{ID: "settings-file", Name: settingsUploadField, Label: i18n.M("mods.settings.label", "file", i18n.Code(world.ModSettingsFile)),
		Accept: ".dat", MaxBytes: MaxSettingsUpload, Error: p.SettingsError, DescribedBy: "settings-file-help"}
}

// SettingsTabPath is the world's Settings tab, where Space Age is turned on
// or off for the built-in mods.
func (p ModsPage) SettingsTabPath() string { return settingsPath(p.World.ID) }

func (ModsPage) templateName() string { return "mods" }
func (ModsPage) navItem() string      { return PathWorlds }

func (p ModsPage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists the upload fields' errors.
func (p ModsPage) Problems() []Problem {
	return problems(Problem{"mod-file", p.UploadError}, Problem{"settings-file", p.SettingsError}, Problem{"countdown", p.CountdownError})
}

// Paths the tab links and posts to.
func (p ModsPage) Path() string         { return modsPath(p.World.ID) }
func (p ModsPage) StatusPath() string   { return modsPath(p.World.ID) + "/status" }
func (p ModsPage) SearchPath() string   { return modsPath(p.World.ID) + "/search" }
func (p ModsPage) SyncPath() string     { return syncPath(p.World.ID) }
func (p ModsPage) UploadPath() string   { return modsPath(p.World.ID) + "/upload" }
func (p ModsPage) SettingsPath() string { return modsPath(p.World.ID) + "/" + world.ModSettingsFile }
func (p ModsPage) SavesPath() string    { return savesPath(p.World.ID) }
func (p ModsPage) AccountPath() string  { return accountLink(p.Path()) }
func (p ModsPage) Tabs() WorldTabs      { return worldTabs(p.World, "mods") }

// RowPath is the plan page for one mod and one change.
func (p ModsPage) RowPath(kind, name string) string {
	return kindPlanPath(p.World.ID, mods.Kind(kind), name)
}

// VersionSlot is the id of a row's place for Change version, which the
// portal status fills in when the portal has another release on the
// world's line.
func (ModsPage) VersionSlot(name string) string { return slotID("version", name) }

// UpdateSlot is the id of a row's place for its Update button, which the
// portal status fills in later.
func (ModsPage) UpdateSlot(name string) string { return slotID("update", name) }

// slotID is the id of a mod row's place for one kind of late content; the
// name is hashed so any mod name makes a valid id.
func slotID(kind, name string) string {
	sum := sha256.Sum256([]byte(name))
	return "mod-" + kind + "-" + hex.EncodeToString(sum[:6])
}

// Line is the world's game line, such as "2.0".
func (p ModsPage) Line() string { return mods.GameLine(p.World.GameVersion) }

// SettingsFile is the mod settings file's name.
func (ModsPage) SettingsFile() string { return world.ModSettingsFile }

// Rows are the table's mods: every mod but the built-in ones, which the
// line above the table names (SPEC 9.7).
func (p ModsPage) Rows() []world.InstalledMod {
	var out []world.InstalledMod
	for _, m := range p.Mods {
		if !m.BuiltIn {
			out = append(out, m)
		}
	}
	return out
}

// BuiltIns names the built-in mods that are on. They follow the world's
// Space Age flag (SPEC 9.7), which the pre-start check holds mod-list.json
// to, so the flag is read rather than a list the game has not written yet.
func (p ModsPage) BuiltIns() []string { return p.builtIns(true) }

// BuiltInsOff names the built-in mods that are off: the Space Age ones
// while Space Age is off.
func (p ModsPage) BuiltInsOff() []string { return p.builtIns(false) }

func (p ModsPage) builtIns(on bool) []string {
	spaceAge := world.SpaceAgeMods(p.World.GameVersion)
	var out []string
	for _, m := range p.Mods {
		if m.BuiltIn && (p.World.SpaceAge || !slices.Contains(spaceAge, m.Name)) == on {
			out = append(out, m.Name)
		}
	}
	return out
}

// ShowSettings says the tab offers mod-settings.dat: a world with only
// built-in mods has no settings to keep, unless it has the file or an
// upload of it was just refused.
func (p ModsPage) ShowSettings() bool {
	return len(p.Rows()) > 0 || p.HasSettings || !p.SettingsError.IsZero() || !p.CountdownError.IsZero()
}

// NoteSlot is the id of a row's place for what the portal says about the
// mod, which the portal status fills in later.
func (ModsPage) NoteSlot(name string) string { return slotID("note", name) }

// BuiltInVersion is the built-in mods' version, the game's own.
func (p ModsPage) BuiltInVersion() string {
	for _, m := range p.Mods {
		if m.BuiltIn {
			return m.Version
		}
	}
	return p.World.GameVersion
}

// HasSpaceAge says the world's game has Space Age, whose setting the
// built-in mods follow.
func (p ModsPage) HasSpaceAge() bool { return len(world.SpaceAgeMods(p.World.GameVersion)) > 0 }

// Enabled counts the table's mods that are on.
func (p ModsPage) Enabled() int {
	n := 0
	for _, m := range p.Rows() {
		if m.Enabled {
			n++
		}
	}
	return n
}

// PortalSlot is the id of a row's portal link, which the portal status
// takes away for a mod the portal does not have.
func (ModsPage) PortalSlot(name string) string { return slotID("portal", name) }

// portalPath is a mod's page on the mod portal.
func portalPath(name string) string { return "https://mods.factorio.com/mod/" + url.PathEscape(name) }

// ModName is what the mod-name partial shows: a mod's title, or its name
// when it has none, linked to its portal page when Link is set.
type ModName struct {
	Name, Title string
	Link        bool
}

// IsName says the title shown is the mod's name, which never hyphenates.
func (m ModName) IsName() bool { return m.Title == m.Name }

// modName is the mod-name partial's input. title falls back to name; link
// is false for a folder mod or one the portal lacks, and a built-in mod is
// never linked (SPEC 9.15).
func modName(name, title string, link bool) ModName {
	if strings.TrimSpace(title) == "" {
		title = name
	}
	return ModName{Name: name, Title: title, Link: link && !builtinName(name)}
}

// builtinName says name is a built-in mod of any supported game line.
func builtinName(name string) bool {
	for _, l := range mapgen.Lines {
		if slices.Contains(world.BuiltinMods(l+".0"), name) {
			return true
		}
	}
	return false
}

func (s *server) handleMods(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	// The result is read before modsPage reads whether an action runs. An
	// action that ends between the two reads then still shows as running, so
	// the page reloads to its result; read the other way round, the page
	// showed the result and then refreshed itself away from it.
	var res actionResult
	var ended bool
	if followsOwnAction(r) {
		res, ended = s.runs.result(sum.ID)
	}
	p, err := s.modsPage(r.Context(), sum)
	if err != nil {
		s.notFoundOrFail(w, r, u, "list mods", err)
		return
	}
	// Only the activity block polls; once the action ends the page reloads
	// whole, so the list is current.
	if isHTMX(r) && !p.Working {
		w.Header().Set("HX-Refresh", "true")
	}
	if ended {
		s.modsOutcome(&p, res, isAdmin(u))
	}
	if r.URL.Query().Get(generatedQuery) != "" && p.InFlow() && p.Done.IsZero() {
		picks, err := s.store.ModPlan(r.Context(), sum.ID)
		p.Generated = err == nil && justMade(p.Mods, picks)
	}
	if !p.World.Newer && !p.World.Unreadable {
		if p.Plan, err = s.planPanel(r.Context(), sum, p.Path()); err != nil {
			s.fail(w, r, "read the mod plan", err)
			return
		}
		if r.URL.Query().Get("full") == "1" {
			p.Plan.Error = planFull()
		}
		p.Search = ModSearchPage{World: sum, Tab: true, Plan: p.Plan, Return: p.Path()}
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// modsPage reads the world's mods and what the tab offers.
func (s *server) modsPage(ctx context.Context, sum world.Summary) (ModsPage, error) {
	p := ModsPage{World: sum, CountdownForm: s.countdownForm(ctx), UploadLimit: zoneLimit(s.modUploadLimit(ctx))}
	if sum.Newer || sum.Unreadable {
		return p, nil
	}
	list, err := s.worlds.Mods(sum.ID)
	if err != nil {
		return ModsPage{}, err
	}
	p.Mods = list
	if p.Fingerprint, err = s.worlds.ModsFingerprint(sum.ID); err != nil {
		return ModsPage{}, err
	}
	if p.LoadSave, err = s.loadingSave(sum.ID); err != nil {
		return ModsPage{}, err
	}
	if p.LoadSave != "" {
		// Only the save's header is read, so this is quick (SPEC 9.9). A save
		// Foreman cannot read leaves the offer to match as it is.
		if save, serr := s.worlds.SaveMods(sum.ID, p.LoadSave); serr == nil {
			d := compareSave(followSpaceAge(list, p.BuiltInsOff()), save)
			p.SaveDiff = &d
		} else {
			s.log.Warn("read the mods of the save", "world", sum.ID, "error", serr)
		}
	}
	p.HasSettings = s.hasModSettings(sum.ID)
	p.NoAccount = !s.downloadsOn(ctx)
	st := s.game.Status(sum.ID)
	op := st.Operation
	p.Running = game.IsRunning(st.State)
	p.Busy = busyText(op, s.worldName)
	p.ServerLink = !p.Busy.IsZero() && (sum.Active || op.Switch != nil)
	p.Working = !p.Busy.IsZero() || s.runs.inFlight(sum.ID)
	p.NoModSettings = s.noModSettings(sum.ID)
	return p, nil
}

// modsOutcome says how the last mod change of a world that is not active
// ended; the Server page says it for the active world (SPEC 17.9).
func (s *server) modsOutcome(p *ModsPage, res actionResult, admin bool) {
	if p.World.Active || res.Action != game.AuditMods {
		return
	}
	p.Undo = modsUndo(p.World.ID, res)
	res.Mods.Titles = s.worlds.InstalledTitles(p.World.ID)
	if res.Err == nil {
		p.Done = modsDone(res.Mods, p.World.Name)
		return
	}
	p.Failure = &Failure{Refused: game.IsRefusal(res.Err)}
	p.Failure.Message, p.Failure.Fix = modsFailure(res.Err, res.Mods, p.World.Name, s.game.Status(p.World.ID).State, p.Path(), admin)
	if errors.Is(res.Err, world.ErrModsChanged) {
		p.Failure.Next = &Offer{Path: p.Path(), Label: i18n.M("mods.changed.next")}
	}
}

// ModStatus is what the portal says about a world's mods: updates and
// mods players cannot get (SPEC 9.3). It loads after the tab: one
// summary line above the table, and each mod's news in its own row.
type ModStatus struct {
	World       world.Summary
	Unreachable bool
	Updates     []mods.Update
	Flags       []mods.Flag
	// Current counts the mods the portal has at their installed version.
	Current int
	// Changeable maps the mods that get Change version to their installed
	// version (SPEC 9.3).
	Changeable map[string]string
	// titles maps each installed mod to its title from info.json.
	titles map[string]string
}

// Title is an installed mod's title, or its name when it has none.
func (p ModStatus) Title(name string) string {
	if t := p.titles[name]; t != "" {
		return t
	}
	return name
}

func (ModStatus) templateName() string { return "mods" }

// UpdateAllPath is the plan that updates every mod with an update.
func (p ModStatus) UpdateAllPath() string { return kindPlanPath(p.World.ID, mods.KindUpdate, "") }
func (p ModStatus) Path() string          { return modsPath(p.World.ID) }

// UpdatePath is the plan for one mod's update.
func (p ModStatus) UpdatePath(name string) string {
	return kindPlanPath(p.World.ID, mods.KindUpdate, name)
}

// VersionPath is the plan page for one mod held at its installed version,
// where its version list offers every release on the world's line (SPEC 9.3).
func (p ModStatus) VersionPath(name, version string) string {
	q := url.Values{"kind": {string(mods.KindUpdate)}, "name": {name}, versionPrefix + name: {version}}
	return planPath(p.World.ID, q)
}

// Slot ids of a row's places the status fills.
func (ModStatus) UpdateSlot(name string) string  { return slotID("update", name) }
func (ModStatus) PortalSlot(name string) string  { return slotID("portal", name) }
func (ModStatus) NoteSlot(name string) string    { return slotID("note", name) }
func (ModStatus) VersionSlot(name string) string { return slotID("version", name) }

// Count counts the flags of one severity, for the summary line.
func (p ModStatus) Count(sev mods.Severity) int {
	n := 0
	for _, f := range p.Flags {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// StatusRow is what the status puts in one mod's row.
type StatusRow struct {
	Name string
	// UpdateTo is the version its Update button offers; "" for none.
	UpdateTo string
	// Installed is the version Change version holds the mod at; "" when
	// the portal has no other release to change to.
	Installed   string
	Flags       []mods.Flag
	NotOnPortal bool
}

// Rows gathers the updates and flags by mod. A mod whose installed
// version is gone gets its fix, the update, even when that release is not
// newer than the installed one.
func (p ModStatus) Rows() []StatusRow {
	var out []StatusRow
	at := map[string]int{}
	row := func(name string) *StatusRow {
		i, ok := at[name]
		if !ok {
			i = len(out)
			at[name] = i
			out = append(out, StatusRow{Name: name})
		}
		return &out[i]
	}
	for _, u := range p.Updates {
		row(u.Name).UpdateTo = u.To
	}
	for _, f := range p.Flags {
		r := row(f.Name)
		r.Flags = append(r.Flags, f)
		switch {
		case f.Code == mods.FlagNotOnPortal:
			r.NotOnPortal = true
		case f.Code == mods.FlagVersionGone && f.Update != "" && r.UpdateTo == "":
			r.UpdateTo = f.Update
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Changeable)) {
		row(name).Installed = p.Changeable[name]
	}
	return out
}

// FlagText words one flag in its mod's row, so it need not name the mod
// (SPEC 9.3).
func (ModStatus) FlagText(f mods.Flag) i18n.Msg {
	switch f.Code {
	case mods.FlagVersionGone:
		if f.Update == "" {
			return i18n.M("mods.row.gone_none", "version", f.Version)
		}
		return i18n.M("mods.row.gone", "version", f.Version)
	case mods.FlagDeprecated:
		return i18n.M("mods.row.deprecated")
	}
	return i18n.M("mods.row.not_on_portal")
}

// FlagClass is the kind of a flag, which colours its leading word.
func (ModStatus) FlagClass(f mods.Flag) string {
	switch f.Severity {
	case mods.SevError:
		return "mod-flag--error"
	case mods.SevWarning:
		return "mod-flag--warning"
	}
	return "mod-flag--note"
}

// FlagWord is the leading word of a flag (DESIGN 7.3).
func (ModStatus) FlagWord(f mods.Flag) i18n.Msg {
	switch f.Severity {
	case mods.SevError:
		return i18n.M("common.error.label")
	case mods.SevWarning:
		return i18n.M("common.label.warning")
	}
	return i18n.M("common.label.note")
}

// handleModStatus asks the portal for updates and availability. A portal
// that cannot be reached is said once, never read as "no updates".
func (s *server) handleModStatus(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	ps, err := s.planner.Status(r.Context(), sum.ID)
	st := ModStatus{World: sum, Updates: ps.Updates, Flags: ps.Flags, Changeable: ps.Changeable}
	if err != nil {
		s.portalTrouble("check the mods on the portal", sum.ID, err)
		st = ModStatus{World: sum, Unreachable: true}
	}
	if !st.Unreachable {
		st.Current = currentCount(ps.Installed, st.Flags)
		st.titles = map[string]string{}
		for _, m := range ps.Installed {
			st.titles[m.Name] = m.Title
		}
	}
	s.fragment(w, r, st, "mod-status")
}

// currentCount counts the enabled portal mods the portal has at their
// installed version: not those it lacks, nor those whose version is gone.
func currentCount(list []world.InstalledMod, flags []mods.Flag) int {
	missing := map[string]bool{}
	for _, f := range flags {
		if f.Code == mods.FlagNotOnPortal || f.Code == mods.FlagVersionGone {
			missing[f.Name] = true
		}
	}
	n := 0
	for _, m := range list {
		if m.Enabled && !m.BuiltIn && !m.Local && !missing[m.Name] {
			n++
		}
	}
	return n
}

// portalTrouble logs a portal failure: an unreachable portal is the
// network's business, anything else Foreman's.
func (s *server) portalTrouble(what, id string, err error) {
	if errors.Is(err, modportal.ErrUnreachable) || errors.Is(err, context.Canceled) {
		s.log.Warn(what, "world", id, "error", err)
		return
	}
	s.log.Error(what, "world", id, "error", err)
}

// ModNotice is the Server page's one line about mods players cannot get
// (SPEC 9.3). It is empty when there are none or the portal
// cannot be asked.
type ModNotice struct {
	World  world.Summary
	Errors int
	// Mod and Version name the mod when there is exactly one; Title is its
	// title from info.json, "" when it has none (SPEC 9.15).
	Mod, Version, Title string
	Fix                 Offer
}

func (ModNotice) templateName() string { return "server" }

// handleModNotice loads after the Server page, so the portal never slows
// its first paint.
func (s *server) handleModNotice(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	n := ModNotice{World: sum}
	flags, err := s.planner.Availability(r.Context(), sum.ID)
	if err != nil {
		s.portalTrouble("check the mods on the portal", sum.ID, err)
	}
	var gone []world.GoneMod
	titles := s.worlds.InstalledTitles(sum.ID)
	for _, f := range flags {
		if f.Severity != mods.SevError {
			continue
		}
		n.Errors++
		n.Mod, n.Version, n.Title = f.Name, f.Version, titles[f.Name]
		gone = append(gone, world.GoneMod{Name: f.Name, Version: f.Version, Update: f.Update, Title: titles[f.Name]})
	}
	if n.Errors > 0 {
		n.Fix = goneFix(sum.ID, gone)
		if s.refusalOffers(sum, n.Fix) {
			n.Fix = Offer{}
		}
	}
	s.fragment(w, r, n, "mods-notice")
}

// refusalOffers reports whether the Server page shows a refusal whose main
// fix is the offer, so the page never carries two buttons for one plan.
func (s *server) refusalOffers(sum world.Summary, fix Offer) bool {
	res, ok := s.runs.result(sum.ID)
	if !ok {
		return false
	}
	f := ServerPage{World: sum, HaveResult: true, Result: res}.Failure()
	return f != nil && f.Fix != nil && f.Fix.Path == fix.Path
}

// ActiveModsPath is the Mods tab of the world the Server page is about.
func (p ServerPage) ActiveModsPath() string { return p.ActivePath() + "/mods" }

// ModNoticePath loads the Server page's mods notice, or is "" when Foreman
// has no portal to ask.
func (p ServerPage) ModNoticePath() string {
	if !p.HasWorld || !p.ModsNotice {
		return ""
	}
	return modsPath(p.World.ID) + "/notice"
}

// ModSearchPage lists the portal's mods that match a search, beside the
// world's plan (SPEC 9.1, 9.2). The Mods tab shows the same results.
type ModSearchPage struct {
	World      world.Summary
	Query      string
	QueryError i18n.Msg
	Results    ModHits
	// Tab says the results go into the Mods tab, which keeps its address.
	Tab bool
	// Paged says a page control brought the person here, so focus goes to
	// the results.
	Paged bool
	// LinkMiss says why a pasted portal link found no mod.
	LinkMiss i18n.Msg
	// Installed names the mods the world has already.
	Installed   map[string]bool
	Unreachable bool
	NoAccount   bool
	// Plan is the world's unfinished plan; Return is this page, for the
	// forms that change it without script.
	Plan   PlanPanel
	Return string
}

func (ModSearchPage) templateName() string { return "modsearch" }
func (ModSearchPage) navItem() string      { return PathWorlds }
func (p ModSearchPage) hasErrors() bool    { return len(p.Problems()) > 0 }

// Problems lists the search field's error.
func (p ModSearchPage) Problems() []Problem {
	return problems(Problem{"mod-search", p.QueryError})
}

func (p ModSearchPage) Path() string     { return modsPath(p.World.ID) + "/search" }
func (p ModSearchPage) ModsPath() string { return modsPath(p.World.ID) }
func (p ModSearchPage) Line() string     { return mods.GameLine(p.World.GameVersion) }
func (p ModSearchPage) AccountPath() string {
	return accountLink(p.Path() + "?" + url.Values{"q": {p.Query}}.Encode())
}

// Tabs mark the Mods tab as the section the search belongs to.
func (p ModSearchPage) Tabs() WorldTabs {
	t := worldTabs(p.World, "mods")
	t.Under = true
	return t
}

// Searched says a search ran, so the panel lists what it found.
func (p ModSearchPage) Searched() bool {
	return p.Query != "" && p.QueryError.IsZero() && !p.Unreachable
}

// ModID is modID for the template.
func (ModSearchPage) ModID(name, part string) string { return modID(name, part) }

// PagePath is a page of the results shown. The Mods tab's own requests say
// so, so their answers keep the tab's address; the tab shows results only
// with script.
func (p ModSearchPage) PagePath(page int) string {
	q := url.Values{"q": {p.Query}, "page": {strconv.Itoa(page)}}
	if p.Tab {
		q.Set("in", "tab")
	}
	return p.Path() + "?" + q.Encode()
}

// Pager is the page controls above or below the results.
func (p ModSearchPage) Pager(where string) ResultPager { return ResultPager{Search: p, Where: where} }

// ResultPager is one set of page controls; Where is "top" or "bottom".
type ResultPager struct {
	Search ModSearchPage
	Where  string
}

// HitAct is what one result offers: State is "installed", "in_plan",
// "needed" (by the mod NeededBy) or "add" (SPEC 9.1).
type HitAct struct {
	Name, State, NeededBy string
	// ID is the slot's, swapped when the plan changes; ControlID is the
	// button's or, without one, the badge's, so focus has a place to go.
	ID, ControlID string
	Path, Return  string
	// OOB marks a slot sent after a change, to swap in place.
	OOB bool
}

// Act is a result's state and control.
func (p ModSearchPage) Act(name string) HitAct {
	a := HitAct{Name: name, ID: modID(name, "act"), ControlID: modID(name, "plan"), Path: p.Plan.ChangePath(), Return: p.Return}
	switch {
	case p.Installed[name]:
		a.State = "installed"
	case p.Plan.Picked(name):
		a.State = "in_plan"
	case p.Plan.NeededBy(name) != "":
		a.State, a.NeededBy = "needed", p.Plan.NeededBy(name)
	default:
		a.State = "add"
	}
	return a
}

// handleModSearch searches the portal's list for the world's game line. It
// needs no account (SPEC 9.11). With script the Mods tab and this page ask
// it for their results as the person types.
func (s *server) handleModSearch(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	q := r.URL.Query()
	p := ModSearchPage{World: sum, Query: strings.TrimSpace(q.Get("q")), Installed: s.installedNames(sum.ID),
		Paged: q.Has("page"), Tab: q.Get("in") == "tab"}
	p.Return = planReturn(sum.ID, r.URL.RequestURI())
	if p.Tab {
		p.Return = modsPath(sum.ID)
	}
	panel, err := s.planPanel(r.Context(), sum, p.Return)
	if err != nil {
		s.fail(w, r, "read the mod plan", err)
		return
	}
	p.Plan = panel
	if q.Get("full") == "1" {
		p.Plan.Error = planFull()
	}
	// A search left empty without script is refused. Other text under 2
	// characters asks the portal nothing, with or without script: the
	// results say what to type (SPEC 9.1).
	if p.Query == "" && !isHTMX(r) {
		p.QueryError = i18n.M("mods.search.missing")
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	if searchTooShort(p.Query) {
		p.Query = ""
		s.page(w, r, http.StatusOK, u.Username, p)
		return
	}
	if searchTooLong(p.Query) {
		p.QueryError = i18n.M("mods.search.long", "count", maxSearchQuery)
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	p.NoAccount = !s.downloadsOn(r.Context())
	hits, miss, err := s.findMods(r.Context(), mods.GameLine(sum.GameVersion), p.Query)
	if err != nil {
		s.portalTrouble("search the mod portal", sum.ID, err)
		p.Unreachable = true
		s.page(w, r, http.StatusBadGateway, u.Username, p)
		return
	}
	p.LinkMiss = miss
	p.Results = pageOfHits(hits, readPage(q.Get("page")))
	s.page(w, r, http.StatusOK, u.Username, p)
}

// findMods answers a search. A pasted mod portal link finds that one mod by
// its exact name; a mod that is not there comes back as a message, not an
// error (SPEC 9.1). Any other text is an ordinary search.
func (s *server) findMods(ctx context.Context, line, query string) ([]modportal.Hit, i18n.Msg, error) {
	name, isLink := modportal.PortalLinkName(query)
	if !isLink {
		hits, err := s.portal.Search(ctx, line, query)
		return hits, i18n.Msg{}, err
	}
	hit, err := s.portal.Lookup(ctx, line, name)
	switch {
	case err == nil:
		return []modportal.Hit{hit}, i18n.Msg{}, nil
	case errors.Is(err, modportal.ErrNotOnPortal):
		return nil, i18n.M("mods.search.link_unknown", "name", name), nil
	case errors.Is(err, modportal.ErrNoRelease):
		return nil, i18n.M("mods.search.link_no_release", "name", name, "line", line), nil
	}
	return nil, i18n.Msg{}, err
}

// maxSearchQuery bounds a search, which is only a name or a few words.
const maxSearchQuery = 100

// searchTooLong says a search is too long to run. A pasted link may carry a
// sub-page and tracking query, so only the mod name in it counts; plain text
// counts whole.
func searchTooLong(query string) bool {
	if name, isLink := modportal.PortalLinkName(query); isLink {
		query = name
	}
	return len(query) > maxSearchQuery
}

// searchTooShort says a search is too short to send to the portal, for the
// page and the MCP tool alike (SPEC 9.1).
func searchTooShort(query string) bool { return utf8.RuneCountInString(query) < 2 }

// maxPickName bounds one picked mod name, which the plan saves; the
// portal's names are far shorter.
const maxPickName = 100

// planForm is a mod change request as the plan page's form carries it, so
// the server can recompute the plan instead of storing one (SPEC 9.14).
type planForm struct {
	Kind     mods.Kind
	Names    []string
	Optional []string
	// Declined are recommended mods the admin unticked. A box cannot send
	// "unticked", so each recommended box sends its name as "offered" too.
	Declined []string
	// Kept are offered orphans the admin unticked; each orphan box sends
	// its name as "orphan-offered" too, for the same reason (SPEC 9.4).
	Kept []string
	// Save is the save a sync reads; Staged is an uploaded zip waiting in
	// the staging area (SPEC 9.5, 9.10).
	Save, Staged string
	// Choices and Confirmed are the admin's answers for unresolved mods.
	Choices   map[string]mods.ChoiceKind
	Confirmed map[string]bool
	// Hash and Fingerprint are the plan and mods folder the page showed.
	Hash, Fingerprint string
	// DropsConfirmed is the box that accepts losing what dropped mods
	// built. The names are worked out again on the post, never read.
	DropsConfirmed bool
	// Removed names the mod a click just took out of the plan, for the
	// sentence the redraw announces. The link carries it; the plan does not.
	Removed string
	// AlsoDisable and AlsoRemove are installed mods a conflict's fix turns
	// off in the same plan; Fixed names the one a click just added, for
	// the sentence the redraw announces (SPEC 9.2, 17.9).
	AlsoDisable, AlsoRemove []string
	Fixed                   string
	// Versions are the releases the admin chose for asked mods of an
	// install or update, by name; a mod without one gets the newest that
	// fits (SPEC 9.2, 9.3).
	Versions map[string]string
	// FavoriteVersion is the game version of the favorite the plan came
	// from, when this Foreman does not support it: the review shows it as a
	// conflict (SPEC 5.12). Only a draft holds it.
	FavoriteVersion string
}

const (
	choicePrefix       = "choice-"
	confirmPrefix      = "confirm-"
	alsoDisableField   = "also-disable"
	alsoRemoveField    = "also-remove"
	orphanField        = "orphan"
	orphanOfferedField = "orphan-offered"
	keptField          = "kept"
	versionPrefix      = "version-"
	draftField         = "draft"
)

var planKinds = []mods.Kind{mods.KindInstall, mods.KindUpdate, mods.KindSync, mods.KindEnable, mods.KindDisable, mods.KindRemove, mods.KindUpload}

// readPlanForm reads a request from a query or a posted form; ok is false
// for one no page offers.
func readPlanForm(v url.Values) (planForm, bool) {
	f := planForm{
		Kind: mods.Kind(v.Get("kind")), Names: v["name"], Optional: v["optional"],
		Save: v.Get("save"), Staged: v.Get("staged"), Hash: v.Get("hash"), Fingerprint: v.Get("fingerprint"),
		DropsConfirmed: v.Get(dropsField) == "1",
		Choices:        map[string]mods.ChoiceKind{}, Confirmed: map[string]bool{},
		Removed:     v.Get("removed"),
		AlsoDisable: v[alsoDisableField], AlsoRemove: v[alsoRemoveField], Fixed: v.Get("fixed"),
	}
	// The de-duplication below is quadratic, so the raw lists are bounded first.
	if len(v["declined"])+len(v["offered"]) > store.MaxModPlanNames {
		return planForm{}, false
	}
	for _, n := range append(slices.Clone(v["declined"]), v["offered"]...) {
		if !slices.Contains(f.Optional, n) && !slices.Contains(f.Declined, n) {
			f.Declined = append(f.Declined, n)
		}
	}
	if len(v[keptField])+len(v[orphanOfferedField]) > store.MaxModPlanNames {
		return planForm{}, false
	}
	for _, n := range append(slices.Clone(v[keptField]), v[orphanOfferedField]...) {
		if !slices.Contains(v[orphanField], n) && !slices.Contains(f.Kept, n) {
			f.Kept = append(f.Kept, n)
		}
	}
	// Each name can cost a portal request, so a crafted list is bounded.
	if !slices.Contains(planKinds, f.Kind) || len(f.Names) > store.MaxModPlanNames || len(f.Optional) > store.MaxModPlanNames || len(f.Declined) > store.MaxModPlanNames ||
		len(f.AlsoDisable)+len(f.AlsoRemove) > store.MaxModPlanNames {
		return planForm{}, false
	}
	// Only an asked mod of an install or update has a version list. The
	// resolver checks the version against the portal's releases; here it
	// need only look like one.
	if f.Kind == mods.KindInstall || f.Kind == mods.KindUpdate {
		f.Versions = map[string]string{}
		for _, n := range f.Names {
			ver := v.Get(versionPrefix + n)
			if ver == "" {
				continue
			}
			if _, ok := world.ParseGameVersion(ver); !ok {
				return planForm{}, false
			}
			f.Versions[n] = ver
		}
	}
	for k, vals := range v {
		switch {
		case strings.HasPrefix(k, choicePrefix):
			switch c := mods.ChoiceKind(vals[0]); c {
			case mods.ChoiceUpload, mods.ChoiceNearest, mods.ChoiceWithout:
				f.Choices[strings.TrimPrefix(k, choicePrefix)] = c
			}
		case strings.HasPrefix(k, confirmPrefix):
			f.Confirmed[strings.TrimPrefix(k, confirmPrefix)] = vals[0] == "1"
		}
	}
	return f, true
}

// ModPlanPage shows a mod change before it applies (SPEC 9.2, 17.5, 17.8).
type ModPlanPage struct {
	// UploadLimit is the mod upload limit in bytes, which an unresolved
	// mod's drop zone shows.
	UploadLimit uint64
	World       world.Summary
	Form        planForm
	Plan        mods.Plan
	// Hash is the plan's digest the form posts back; Fingerprint the mods
	// folder's.
	Hash, Fingerprint string
	// Uploaded is the uploaded zip's mod, for an upload's heading.
	Uploaded world.ModInfo
	Running  bool
	// RunSnapshot is when the snapshot of an open run of mod changes was
	// taken, on a stopped world: the change keeps it and takes no new one
	// (SPEC 9.4). Zero when the change takes a snapshot.
	RunSnapshot time.Time
	CountdownForm
	NoAccount bool
	// Changed says the plan is not the one the admin confirmed, so nothing
	// was applied (SPEC 17.8).
	Changed bool
	// Error says why there is no plan; Retry offers to try again.
	Error i18n.Msg
	Retry bool
	// Draft is the token of the draft that holds the page's answers;
	// DraftLost says the draft a link named was gone, so the page started
	// again from the saved picks.
	Draft     string
	DraftLost bool
	// RetryTo is where Try again goes when it is not the draft, as on a
	// refused cross-site request, which makes no draft.
	RetryTo string
	// ConfirmErrors are unticked "continue without" boxes, by mod name.
	ConfirmErrors map[string]i18n.Msg
	Busy          i18n.Msg
	// UploadError is a refused zip for the unresolved mod UploadFor.
	UploadError i18n.Msg
	UploadFor   string
	// Drops are the mods the plan turns off or removes that the loading save
	// uses: the game deletes what they built once it starts without them.
	// DropsError is the unticked box after a post.
	Drops      []string
	DropsError i18n.Msg
	// Redraw says a script asked for the plan after a change on the page:
	// the plan is announced and focus stays where it was (WCAG 4.1.3).
	Redraw bool
	// Generated says the world was just made from a favorite, whose mods
	// this plan installs (SPEC 5.12).
	Generated bool
	// Have are the world's mods now, for the conflicts' fixes.
	Have []world.InstalledMod
	// facts are the titles and offered mods' details, by mod name.
	facts map[string]ModFacts
}

// FromPicks says the plan installs mods the admin picked on the Mods tab,
// so it lists them with a way to take one out (SPEC 9.2).
func (p ModPlanPage) FromPicks() bool { return p.Form.Kind == mods.KindInstall && p.Form.Staged == "" }

// RemovePath takes name out of the draft's plan.
func (p ModPlanPage) RemovePath(name string) string {
	return draftPath(p.World.ID, p.Draft, "removed", name)
}

// DraftPath is the page drawn from its draft, for the address bar and to
// try again.
func (p ModPlanPage) DraftPath() string { return draftPath(p.World.ID, p.Draft) }

// RetryPath is where Try again goes.
func (p ModPlanPage) RetryPath() string {
	if p.RetryTo != "" {
		return p.RetryTo
	}
	return p.DraftPath()
}

// maxAddress is the longest address Foreman builds (SPEC 17).
const maxAddress = 512

// RedrawPath takes the form's answers and draws the plan again.
func (p ModPlanPage) RedrawPath() string { return p.Path() + "/redraw" }

// Danger says the plan destroys something, so its box and button are the
// danger style: it removes a mod, drops what the save built, or goes on
// without a save's mod. Anything else is the primary action, on a running
// world too, whose restart the steps say in words (DESIGN 7.1, 8.11).
func (p ModPlanPage) Danger() bool {
	if len(p.Drops) > 0 || slices.ContainsFunc(p.Plan.Changes, func(c mods.Change) bool { return c.Action == mods.ActRemove }) {
		return true
	}
	return slices.ContainsFunc(p.Plan.Unresolved, func(u mods.Unresolved) bool {
		return u.Choice != nil && u.Choice.Kind == mods.ChoiceWithout
	})
}

// dropsMissing is the error for an unticked drops box: it names the one
// mod, or counts several.
func dropsMissing(drops []string) i18n.Msg {
	if len(drops) == 1 {
		return i18n.M("mods.drops.confirm_missing_one", "mod", i18n.Ident(drops[0]))
	}
	return i18n.M("mods.drops.confirm_missing", "count", len(drops))
}

// dropsField is the box that confirms the Drops.
const dropsField = "confirm-drops"

func (ModPlanPage) templateName() string { return "modplan" }
func (ModPlanPage) navItem() string      { return PathWorlds }
func (p ModPlanPage) hasErrors() bool {
	return len(p.Problems()) > 0 || !p.Error.IsZero() || len(p.Plan.Conflicts) > 0 || p.Form.FavoriteVersion != ""
}

// Problems lists the form's field errors in page order.
func (p ModPlanPage) Problems() []Problem {
	var out []Problem
	for _, u := range p.Plan.Unresolved {
		if msg, ok := p.ConfirmErrors[u.Name]; ok {
			out = append(out, Problem{modID(u.Name, "confirm"), msg})
		}
		if u.Name == p.UploadFor && !p.UploadError.IsZero() {
			out = append(out, Problem{modID(u.Name, "zip"), p.UploadError})
		}
	}
	return append(out, problems(Problem{dropsField, p.DropsError}, Problem{"countdown", p.CountdownError})...)
}

// modID is the id of a part of a mod's controls, made from the mod's name,
// so focus stays on the same mod when the plan is redrawn. Bytes an id
// cannot hold plainly are written as ".xx", which no name has, so two names
// never share an id.
func modID(name, part string) string {
	var b strings.Builder
	b.WriteString("mod-")
	for _, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteByte(c)
		default:
			b.WriteString("." + hex.EncodeToString([]byte{c}))
		}
	}
	return b.String() + "-" + part
}

// ModID is modID for the template.
func (ModPlanPage) ModID(name, part string) string { return modID(name, part) }

// UploadAction is where an unresolved mod's zip is posted: its draft holds
// the page's answers, so the plan that comes back keeps them.
func (p ModPlanPage) UploadAction(name string) string {
	q := url.Values{draftField: {p.Draft}, "for": {name}}
	if p.Countdown != "" {
		q.Set("countdown", p.Countdown)
	}
	return p.UploadPath() + "?" + q.Encode()
}

// UploadZone is an unresolved mod's zip field. It sits outside the plan's
// form, which cannot hold a second form, and posts as the mod's own form
// (DESIGN 7.10).
func (p ModPlanPage) UploadZone(name string, version string) DropZone {
	z := DropZone{ID: modID(name, "zip"), Name: modUploadField, Accept: ".zip", MaxBytes: p.UploadLimit,
		Label: i18n.M("mods.unresolved.upload_label", "mod", i18n.Ident(name), "version", version),
		Form:  modID(name, "zip") + "-form"}
	if p.UploadFor == name {
		z.Error = p.UploadError
	}
	return z
}

// AsksRequired reports whether the form has a box or a field that must be
// filled, so it explains the required mark (DESIGN 7.2).
func (p ModPlanPage) AsksRequired() bool {
	if len(p.Drops) > 0 {
		return true
	}
	for _, u := range p.Plan.Unresolved {
		switch p.Choice(u.Name) {
		case string(mods.ChoiceWithout):
			return true
		case string(mods.ChoiceUpload):
			if p.Form.Staged == "" {
				return true
			}
		}
	}
	return false
}

// Announce is the sentence a screen reader hears after a redraw. After a
// conflict fix focus moves to the conflicts or the changes, which the
// reader then reads out, so that sentence stays short.
func (p ModPlanPage) Announce() i18n.Msg {
	switch {
	case !p.Error.IsZero():
		return p.Error
	case p.Form.Fixed != "" && len(p.Plan.Conflicts) > 0:
		return i18n.M("mods.plan.fixed_conflicts", "count", len(p.Plan.Conflicts))
	case len(p.Plan.Conflicts) > 0:
		return i18n.M("mods.plan.redrawn_conflicts", "count", len(p.Plan.Conflicts))
	case p.Nothing() && len(p.Versions()) > 0:
		return i18n.M("mods.version.same")
	case p.Nothing():
		return i18n.M("mods.plan.nothing")
	case p.Form.Fixed != "":
		return i18n.M("mods.plan.fixed", "count", p.Count())
	case p.Form.Removed != "":
		return i18n.M("mods.plan.removed", "mod", p.Form.Removed, "count", p.Count())
	}
	return i18n.M("mods.plan.redrawn", "count", p.Count())
}

func (p ModPlanPage) Path() string       { return modsPath(p.World.ID) + "/plan" }
func (p ModPlanPage) ModsPath() string   { return modsPath(p.World.ID) }
func (p ModPlanPage) UploadPath() string { return modsPath(p.World.ID) + "/upload" }
func (p ModPlanPage) SavesPath() string  { return savesPath(p.World.ID) }
func (p ModPlanPage) Line() string       { return mods.GameLine(p.World.GameVersion) }

// AccountPath returns here after the account is entered. An upload cannot
// be posted again from a link, so it returns to the Mods tab.
func (p ModPlanPage) AccountPath() string {
	if p.Form.Staged != "" {
		return accountLink(p.ModsPath())
	}
	return accountLink(p.DraftPath())
}

// Tabs mark the Mods tab as the section the plan belongs to.
func (p ModPlanPage) Tabs() WorldTabs {
	t := worldTabs(p.World, "mods")
	t.Under = true
	return t
}

// InFlow says the review is step 2 of the guided Generate (SPEC 5.6).
func (p ModPlanPage) InFlow() bool { return p.World.Unfinished }

// Steps is the step bar with step 2 current. The plan is about to change
// the mods, so step 3 stays a link, which passes on when it has nothing.
func (p ModPlanPage) Steps() []FlowStep { return flowSteps(p.World.ID, flowStepMods, false) }

// NextPath is step 3, the mod settings.
func (p ModPlanPage) NextPath() string { return flowModSettingsPath(p.World.ID) }

// Heading is the page's question, naming the world (DESIGN 7.4).
func (p ModPlanPage) Heading() i18n.Msg {
	w := p.World.Name
	var one any = ""
	if len(p.Form.Names) == 1 {
		one = p.ident(p.Form.Names[0])
	}
	if p.extraKinds() {
		return i18n.M("mods.plan.heading.change", "world", w)
	}
	switch p.Form.Kind {
	case mods.KindInstall:
		if len(p.Form.Names) == 1 {
			return i18n.M("mods.plan.heading.install", "mod", one, "world", w)
		}
		if len(p.Form.Names) > 1 {
			return i18n.M("mods.plan.heading.install_some", "world", w)
		}
	case mods.KindUpdate:
		switch {
		case len(p.Form.Versions) > 0 && len(p.Form.Names) == 1:
			return i18n.M("mods.plan.heading.version", "mod", one, "world", w)
		case len(p.Form.Versions) > 0:
			return i18n.M("mods.plan.heading.change", "world", w)
		case len(p.Form.Names) == 1:
			return i18n.M("mods.plan.heading.update_one", "mod", one, "world", w)
		}
		return i18n.M("mods.plan.heading.update", "world", w)
	case mods.KindSync:
		return i18n.M("mods.plan.heading.sync", "world", w, "save", p.Form.Save)
	case mods.KindEnable:
		return i18n.M("mods.plan.heading.enable", "mod", one, "world", w)
	case mods.KindDisable:
		return i18n.M("mods.plan.heading.disable", "mod", one, "world", w)
	case mods.KindRemove:
		return i18n.M("mods.plan.heading.remove", "mod", one, "world", w)
	case mods.KindUpload:
		return i18n.M("mods.plan.heading.upload", "mod", p.ident(p.Uploaded.Name), "version", p.Uploaded.Version, "world", w)
	}
	return i18n.M("mods.plan.heading.change", "world", w)
}

// Count is how many mods the plan changes.
func (p ModPlanPage) Count() int {
	n := 0
	for _, c := range p.Plan.Changes {
		if c.Action != mods.ActKeep {
			n++
		}
	}
	return n
}

// Downloads is how many mods the plan downloads.
func (p ModPlanPage) Downloads() int {
	n := 0
	for _, c := range p.Plan.Changes {
		if c.Release != nil {
			n++
		}
	}
	return n
}

// Button is the confirming button's words, repeating the verb (DESIGN 7.4).
func (p ModPlanPage) Button() i18n.Msg {
	n := p.Count()
	// A chosen version may go back, so an update with one says "Change".
	if p.extraKinds() || p.Form.Kind == mods.KindUpdate && len(p.Form.Versions) > 0 {
		return i18n.M("mods.plan.button.change", "count", n)
	}
	switch p.Form.Kind {
	case mods.KindUpdate:
		return i18n.M("mods.plan.button.update", "count", n)
	case mods.KindSync:
		return i18n.M("mods.plan.button.sync", "count", n)
	case mods.KindEnable:
		return i18n.M("mods.plan.button.enable", "count", n)
	case mods.KindDisable:
		return i18n.M("mods.plan.button.disable", "count", n)
	case mods.KindRemove:
		return i18n.M("mods.plan.button.remove", "count", n)
	}
	return i18n.M("mods.plan.button.install", "count", n-p.recommendedCount())
}

// ButtonRecommended is the words after the install button's label that
// count the ticked recommended mods (SPEC 9.14); empty when none is ticked.
func (p ModPlanPage) ButtonRecommended() i18n.Msg {
	if n := p.recommendedCount(); n > 0 && p.installLabel() {
		return i18n.M("mods.plan.button.recommended", "count", n)
	}
	return i18n.Msg{}
}

// installLabel says the button is the plain "Install" one.
func (p ModPlanPage) installLabel() bool {
	if p.extraKinds() || p.Form.Kind == mods.KindUpdate && len(p.Form.Versions) > 0 {
		return false
	}
	switch p.Form.Kind {
	case mods.KindUpdate, mods.KindSync, mods.KindEnable, mods.KindDisable, mods.KindRemove:
		return false
	}
	return true
}

// recommendedCount is how many recommended mods the plan changes: the
// ones left ticked.
func (p ModPlanPage) recommendedCount() int {
	if !p.installLabel() {
		return 0
	}
	n := 0
	for _, c := range p.Plan.Changes {
		if c.Action != mods.ActKeep && c.Why.Code == mods.WhyRecommendedBy {
			n++
		}
	}
	return n
}

// Nothing says the plan changes no mod and asks nothing.
func (p ModPlanPage) Nothing() bool {
	return p.Error.IsZero() && p.Count() == 0 && len(p.Plan.Conflicts) == 0 && len(p.Plan.Unresolved) == 0
}

// CanApply says the confirming button is shown: every check the apply runs
// has passed, apart from an unticked "continue without" box, which is a
// field error after the post (DESIGN 7.1, 7.2, SPEC 17.8).
func (p ModPlanPage) CanApply() bool {
	if !p.Error.IsZero() || !p.Busy.IsZero() || len(p.Plan.Conflicts) > 0 || p.Form.FavoriteVersion != "" || p.Count() == 0 && len(p.Plan.Unresolved) == 0 {
		return false
	}
	if p.NoAccount && p.Downloads() > 0 {
		return false
	}
	for _, u := range p.Plan.Unresolved {
		c := u.Choice
		if c == nil || c.Kind == mods.ChoiceUpload && c.Upload == nil || c.Kind == mods.ChoiceNearest && u.Nearest == "" {
			return false
		}
	}
	return true
}

// changeText words c after the mod's name, which its row shows as a
// title; a mod named in the reason shows by title too (SPEC 9.15).
func (p ModPlanPage) changeText(c mods.Change) i18n.Msg {
	var what i18n.Msg
	switch {
	case c.Action == mods.ActRemove:
		what = i18n.M("mods.plan.row.version", "version", c.From)
	case c.From != "" && c.From != c.To:
		what = i18n.M("mods.plan.row.from_to", "from", c.From, "to", c.To)
	case c.Upload:
		what = i18n.M("mods.plan.row.uploaded", "version", c.To)
	default:
		what = i18n.M("mods.plan.row.version", "version", c.To)
	}
	var why i18n.Msg
	switch c.Why.Code {
	case mods.WhyRequiredBy:
		needers := p.neededBy(c)
		why = i18n.M("mods.plan.why.required_by", "mod", needers[0])
		if len(needers) > 1 {
			why = i18n.M("mods.plan.why.required_by_several", "mods", andNames(needers))
		}
	case mods.WhyOptionalOf:
		why = i18n.M("mods.plan.why.optional_of", "mod", p.ident(c.Why.Of))
	case mods.WhyRecommendedBy:
		why = i18n.M("mods.plan.why.recommended_by", "mod", p.ident(c.Why.Of))
	case mods.WhyFromSave:
		why = i18n.M("mods.plan.why.from_save")
	case mods.WhyNotInSave:
		why = i18n.M("mods.plan.why.not_in_save")
	case mods.WhyOnlyNeededBy:
		why = i18n.M("mods.plan.why.only_needed_by", "mod", p.ident(c.Why.Of))
	}
	if !why.IsZero() {
		what = i18n.M("mods.plan.item.why", "item", what, "why", why)
	}
	if c.Warning == mods.WarnOlderThanSave {
		what = join(what, i18n.M("mods.plan.warn.older"))
	}
	return what
}

// neededBy names every mod in the plan that needs c, the one its reason
// names first (SPEC 9.2).
func (p ModPlanPage) neededBy(c mods.Change) []i18n.Msg {
	out := []i18n.Msg{i18n.M("server.list.names", "names", p.ident(c.Why.Of))}
	for _, o := range p.Plan.Changes {
		if o.Name != c.Why.Of && slices.Contains(p.Plan.Requires[o.Name], c.Name) {
			out = append(out, i18n.M("server.list.names", "names", p.ident(o.Name)))
		}
	}
	return out
}

func (p ModPlanPage) conflictText(c mods.Conflict) i18n.Msg {
	switch c.Code {
	case mods.ConflictIncompatible:
		return i18n.M("mods.conflict.incompatible", "mod", p.ident(c.Mod), "other", p.ident(c.Other))
	case mods.ConflictSpaceAgeOff:
		if c.Other == "" {
			return i18n.M("mods.conflict.space_age_save", "mod", p.ident(c.Mod))
		}
		return i18n.M("mods.conflict.space_age_off", "mod", p.ident(c.Mod), "other", p.ident(c.Other))
	case mods.ConflictNoRelease:
		return i18n.M("mods.conflict.no_release", "mod", p.ident(c.Mod), "needs", needsText(c.Needs))
	case mods.ConflictNoSuchRelease:
		return i18n.M("mods.conflict.no_such_release", "mod", p.ident(c.Mod), "version", c.Version)
	case mods.ConflictVersion:
		return i18n.M("mods.conflict.version", "mod", p.ident(c.Mod), "version", c.Version, "needs", needsText(c.Needs))
	case mods.ConflictNotOnPortal:
		return i18n.M("mods.conflict.not_on_portal", "mod", p.ident(c.Mod))
	case mods.ConflictRequiredBy:
		return i18n.M("mods.conflict.required_by", "mod", p.ident(c.Mod), "other", p.ident(c.Other))
	case mods.ConflictBuiltIn:
		return i18n.M("mods.conflict.built_in", "mod", p.ident(c.Mod))
	case mods.ConflictLocal:
		return i18n.M("mods.conflict.local", "mod", p.ident(c.Mod))
	case mods.ConflictNotInstalled:
		return i18n.M("mods.conflict.not_installed", "mod", p.ident(c.Mod))
	case mods.ConflictBadDep:
		return i18n.M("mods.conflict.bad_dependency", "mod", p.ident(c.Mod), "text", c.Text)
	case mods.ConflictTooComplex:
		return i18n.M("mods.conflict.too_complex")
	}
	return i18n.M("mods.conflict.bad_request")
}

// needsText lists the version constraints, such as "base >= 2.0 (from
// space-exploration)".
func needsText(needs []mods.Need) string {
	parts := make([]string, len(needs))
	for i, n := range needs {
		parts[i] = strings.TrimSpace(n.Op+" "+n.Version) + " (" + n.By + ")"
	}
	return strings.Join(parts, ", ")
}

// Choice is the answer the form holds for an unresolved mod; upload by
// default (SPEC 9.10).
func (p ModPlanPage) Choice(name string) string {
	if c, ok := p.Form.Choices[name]; ok {
		return string(c)
	}
	return string(mods.ChoiceUpload)
}

// UploadedFor reports whether the staged zip answers name's upload.
func (p ModPlanPage) UploadedFor(name string) bool {
	return p.Form.Staged != "" && p.Uploaded.Name == name
}

// HasRecommended reports whether any offered mod is recommended, for the
// hint that says those start ticked.
func (p ModPlanPage) HasRecommended() bool {
	return slices.ContainsFunc(p.Plan.Optional, func(o mods.Offer) bool { return o.Recommended })
}

func (s *server) handlePlanForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	q := r.URL.Query()
	// A plan asks the portal about every mod it names, so another site's
	// page may not make a signed-in browser ask for one; nor may it make
	// drafts, which would push out the admin's own. The admin's click on
	// Try again is same-origin and opens the same short address.
	if !fromThisSite(r) {
		p := ModPlanPage{World: sum, Error: i18n.M("mods.error.cross_site"), Retry: true, RetryTo: modsPath(sum.ID)}
		if to := r.URL.RequestURI(); len(to) <= maxAddress {
			p.RetryTo = to
		}
		s.page(w, r, http.StatusForbidden, u.Username, p)
		return
	}
	f, lost, ok := s.entryForm(r.Context(), u, sum, q)
	if !ok {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	token := q.Get(draftField)
	p := s.planPage(r.Context(), sum, f, s.carriedCountdown(r.Context(), q))
	p.Draft, p.DraftLost = s.drafts.keep(token, u.ID, sum.ID, p.Form), lost
	p.Generated = q.Get(generatedQuery) != ""
	// htmx swaps only a 2xx reply, so a redraw that finds a conflict or a
	// portal failure still answers 200 for the page to show it.
	if isHTMX(r) {
		p.Redraw = true
		s.page(w, r, http.StatusOK, u.Username, p)
		return
	}
	s.page(w, r, planStatus(p), u.Username, p)
}

// entryForm is the plan a link asks for: a draft with at most one change
// to it, a favorite's mods, the saved picks, or a kind and one name. A
// draft that is gone, as after it expired or Foreman restarted, starts
// again from the saved picks, and lost says so.
func (s *server) entryForm(ctx context.Context, u store.User, sum world.Summary, q url.Values) (f planForm, lost, ok bool) {
	if token := q.Get(draftField); token != "" {
		f, ok := s.drafts.get(token, u.ID, sum.ID)
		if !ok {
			return s.picksForm(ctx, sum.ID), true, true
		}
		switch {
		case q.Has("removed"):
			f.Removed = q.Get("removed")
			f.Names = slices.DeleteFunc(f.Names, func(n string) bool { return n == f.Removed })
		case q.Has(alsoDisableField):
			f.AlsoDisable = alsoAdd(&f, f.AlsoDisable, q.Get(alsoDisableField), q.Has("fixed"))
		case q.Has(alsoRemoveField):
			f.AlsoRemove = alsoAdd(&f, f.AlsoRemove, q.Get(alsoRemoveField), q.Has("fixed"))
		}
		return f, false, true
	}
	if id := q.Get(favoriteQuery); id != "" {
		return s.favoriteForm(ctx, sum, id)
	}
	f, ok = readPlanForm(q)
	if ok && f.Kind == mods.KindInstall && len(f.Names) == 0 {
		return s.picksForm(ctx, sum.ID), false, true
	}
	return f, false, ok
}

// alsoAdd adds a conflict's fix to list once, so a reload of its link
// changes nothing, and marks it Fixed for the announcer when fixed.
func alsoAdd(f *planForm, list []string, name string, fixed bool) []string {
	if fixed {
		f.Fixed = name
	}
	if name == "" || slices.Contains(list, name) || len(f.AlsoDisable)+len(f.AlsoRemove) >= store.MaxModPlanNames {
		return list
	}
	return append(list, name)
}

// newInstallForm is an install plan with nothing chosen yet.
func newInstallForm() planForm {
	return planForm{Kind: mods.KindInstall, Choices: map[string]mods.ChoiceKind{}, Confirmed: map[string]bool{}, Versions: map[string]string{}}
}

// continuePath is where an Unfinished world's Continue opens: step 2 while
// it has saved mod picks that are not installed, then step 3 while the
// world has neither saved mod settings nor saved map settings, which passes
// on to step 4 when no mod has a startup setting, otherwise step 4 (SPEC
// 5.6, 9.14). A pick installed another way, such as an upload, is not work that
// remains. A picks table that cannot be read opens step 2, which shows them.
func (s *server) continuePath(ctx context.Context, sum world.Summary) string {
	picks, err := s.store.ModPlan(ctx, sum.ID)
	if err != nil {
		s.log.Warn("read the mod plan", "world", sum.ID, "error", err)
		return modsPath(sum.ID)
	}
	installed := s.installedNames(sum.ID)
	for _, m := range picks {
		if !installed[m.Name] {
			return modsPath(sum.ID)
		}
	}
	// A saved settings file means step 3 is done; saved map settings mean
	// step 3 was passed, done or skipped.
	if s.noModSettings(sum.ID) || s.hasModSettings(sum.ID) || s.mapSettingsSaved(ctx, sum.ID) {
		return newMapPath(sum.ID)
	}
	return flowModSettingsPath(sum.ID)
}

// mapSettingsSaved says step 4 has been saved for the world: its save is
// in the audit log (SPEC 12.9). A log that cannot be read says no, so
// Continue shows step 3, which can be skipped.
func (s *server) mapSettingsSaved(ctx context.Context, id string) bool {
	list, err := s.store.ListAudit(ctx, store.AuditFilter{Action: AuditNewMap, WorldID: id, Limit: 1})
	if err != nil {
		s.log.Warn("read the audit log", "world", id, "error", err)
		return false
	}
	return len(list) > 0
}

// picksForm installs the world's saved picks (SPEC 9.14).
func (s *server) picksForm(ctx context.Context, id string) planForm {
	f := newInstallForm()
	picks, err := s.store.ModPlan(ctx, id)
	if err != nil {
		s.log.Warn("read the mod plan", "world", id, "error", err)
	}
	for _, m := range picks {
		f.Names = append(f.Names, m.Name)
	}
	return f
}

// favoriteForm installs a favorite's mods, at its pinned versions when the
// world is on the favorite's game line; on another line each gets the
// newest that fits. A game version this Foreman does not support is a
// conflict on the page (SPEC 5.12). A favorite that is gone leaves the
// saved picks, which Generate saved from it.
func (s *server) favoriteForm(ctx context.Context, sum world.Summary, id string) (planForm, bool, bool) {
	if s.favorites == nil {
		return s.picksForm(ctx, sum.ID), true, true
	}
	fav, _, err := s.favorites.Get(id)
	if err != nil {
		return s.picksForm(ctx, sum.ID), true, true
	}
	f := newInstallForm()
	favLine, known := mapgen.Line(fav.GameVersion)
	if !known || !world.SupportedGameVersion(fav.GameVersion) {
		f.FavoriteVersion = fav.GameVersion
	}
	for _, m := range fav.Mods {
		f.Names = append(f.Names, m.Name)
		if known && favLine == mods.GameLine(sum.GameVersion) && m.Version != "" {
			f.Versions[m.Name] = m.Version
		}
	}
	return f, false, true
}

// handlePlanRedraw takes the review page's answers, keeps them in its
// draft and draws the plan again: in place with script, and without it
// through the draft's address, so a reload asks nothing again.
func (s *server) handlePlanRedraw(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	f, ok := s.postedForm(u, sum, r.PostForm)
	if !ok {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	token := r.PostForm.Get(draftField)
	if !isHTMX(r) {
		token = s.drafts.keep(token, u.ID, sum.ID, f)
		var more []string
		if c := r.PostForm.Get("countdown"); c != "" && len(c) <= maxCountdownField {
			more = []string{"countdown", c}
		}
		http.Redirect(w, r, draftPath(sum.ID, token, more...), http.StatusSeeOther) //nolint:gosec // a local path under this world's mods pages
		return
	}
	p := s.planPage(r.Context(), sum, f, s.carriedCountdown(r.Context(), r.PostForm))
	p.Draft, p.Redraw = s.drafts.keep(token, u.ID, sum.ID, p.Form), true
	s.page(w, r, http.StatusOK, u.Username, p)
}

// carriedCountdown is the stop countdown a plan page shows: the one v
// carries from the page before, or Foreman's default when it carries none.
func (s *server) carriedCountdown(ctx context.Context, v url.Values) CountdownForm {
	f := s.countdownForm(ctx)
	if c := v.Get("countdown"); c != "" {
		f.Countdown = c
	}
	return f
}

// maxCountdownField bounds the countdown a redraw's address carries; a
// real one is a few digits.
const maxCountdownField = 8

// postedForm reads the review page's form; what only the draft holds
// comes from it.
func (s *server) postedForm(u store.User, sum world.Summary, v url.Values) (planForm, bool) {
	f, ok := readPlanForm(v)
	if !ok {
		return planForm{}, false
	}
	if d, ok := s.drafts.get(v.Get(draftField), u.ID, sum.ID); ok {
		f.FavoriteVersion = d.FavoriteVersion
	}
	return f, true
}

// fromThisSite reports whether a request came from Foreman's own pages or
// the browser itself (an address typed or a bookmark). Browsers that send
// no Sec-Fetch-Site are let through, as the cross-origin check does.
func fromThisSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

func planStatus(p ModPlanPage) int {
	switch {
	case !p.Error.IsZero():
		return http.StatusBadGateway
	case len(p.Plan.Conflicts) > 0, p.Form.FavoriteVersion != "":
		return http.StatusConflict
	}
	return http.StatusOK
}

// planPage works out the plan for f now. A failure to do so is in Error.
func (s *server) planPage(ctx context.Context, sum world.Summary, f planForm, form CountdownForm) ModPlanPage {
	p := ModPlanPage{World: sum, Form: f, CountdownForm: form, ConfirmErrors: map[string]i18n.Msg{}, UploadLimit: zoneLimit(s.modUploadLimit(ctx))}
	st := s.game.Status(sum.ID)
	p.Running = game.IsRunning(st.State)
	if !p.Running {
		if run, ok := s.worlds.ModRunSnapshot(sum.ID); ok {
			p.RunSnapshot = run.TakenAt
		}
	}
	p.Busy = busyText(st.Operation, s.worldName)
	p.NoAccount = !s.downloadsOn(ctx)
	if sum.Newer || sum.Unreadable {
		p.Error = i18n.M("mods.error.unreadable")
		return p
	}
	req, err := s.planRequest(sum.ID, &p)
	if err == nil {
		p.Plan, p.Fingerprint, err = s.planner.Plan(ctx, sum.ID, req)
	}
	if err != nil {
		p.Error, p.Retry = s.planError(sum.ID, err), true
		return p
	}
	p.Drops = s.planDrops(ctx, sum.ID, p.Plan, p.Running)
	if p.Have, err = s.worlds.Mods(sum.ID); err != nil {
		s.log.Warn("list the mods for the plan's fixes", "world", sum.ID, "error", err)
	}
	p.Hash = mods.HashPlan(p.Plan, p.Drops)
	p.facts = s.planFacts(ctx, p)
	return p
}

// planFacts gathers each mod's title, and the details of the offered ones,
// from info.json and then the portal (SPEC 9.2, 9.15). The plan has already
// looked up the mods it changes, so those come from the portal client's
// cache. An offered mod may be one the plan never asked about, so at most
// store.MaxModPlanNames offered mods are asked for; the rest show by title
// alone, or by name (SPEC 9.2).
func (s *server) planFacts(ctx context.Context, p ModPlanPage) map[string]ModFacts {
	out := map[string]ModFacts{}
	installed := map[string]world.InstalledMod{}
	for _, m := range p.Have {
		installed[m.Name] = m
		out[m.Name] = ModFacts{Title: m.Title, Version: m.Version}
	}
	// Details are a help, not the plan: a slow portal leaves them out
	// rather than holding the page.
	ctx, cancel := context.WithTimeout(ctx, planFactsWait)
	defer cancel()
	seen := map[string]bool{}
	// pick lists the names not seen yet that the portal may have, at most
	// limit of them (no limit when limit < 0).
	pick := func(limit int, names ...string) []string {
		var got []string
		for _, n := range names {
			if n == "" || seen[n] || installed[n].BuiltIn || installed[n].Local {
				continue
			}
			if limit >= 0 && len(got) >= limit {
				break
			}
			seen[n] = true
			got = append(got, n)
		}
		return got
	}
	var mine []string
	for _, c := range p.Plan.Changes {
		mine = append(mine, c.Name, c.Why.Of)
	}
	offers := slices.Concat(p.Plan.Optional, p.Plan.Orphans)
	var offered, of []string
	for _, o := range offers {
		offered, of = append(offered, o.Name), append(of, o.Of)
	}
	plan := pick(-1, slices.Concat(p.Form.Names, mine)...)
	asked := pick(store.MaxModPlanNames, offered...)
	why := pick(store.MaxModPlanNames, of...)
	for name, r := range s.portal.Mods(ctx, slices.Concat(plan, why)) {
		if r.Err == nil {
			out[name] = modFacts(out[name], r.Mod)
		}
	}
	for name, r := range s.portal.Mods(ctx, asked) {
		if r.Err == nil {
			_, have := installed[name]
			out[name] = offerFacts(modFacts(out[name], r.Mod), r.Mod, p, have)
		}
	}
	return out
}

// planFactsWait bounds the portal lookups for the review page's details.
var planFactsWait = 3 * time.Second

// modFacts adds the portal's word on a mod to what info.json gave.
func modFacts(f ModFacts, m modportal.Mod) ModFacts {
	f.OnPortal = true
	if f.Title == "" {
		f.Title = m.Title
	}
	return f
}

// offerFacts adds an offered mod's summary and the release it would
// install, or has installed, with that release's date.
func offerFacts(f ModFacts, m modportal.Mod, p ModPlanPage, installed bool) ModFacts {
	f.Summary = world.CutText(world.CleanText(m.Summary), maxPortalSummary)
	want := f.Version
	if c := slices.IndexFunc(p.Plan.Changes, func(c mods.Change) bool { return c.Name == m.Name }); c >= 0 && p.Plan.Changes[c].To != "" {
		want = p.Plan.Changes[c].To
	}
	if !installed && want == "" {
		for _, r := range m.Releases {
			if r.FactorioVersion == p.Line() && (want == "" || world.CompareVersions(r.Version, want) > 0) {
				want = r.Version
			}
		}
	}
	f.Version = want
	for _, r := range m.Releases {
		if r.Version == want {
			f.Released = r.ReleasedAt
		}
	}
	return f
}

// maxPortalSummary bounds a portal summary on the review page; it is
// untrusted.
const maxPortalSummary = 300

// planDrops lists the plan's dropped mods, leaving out unresolved mods,
// which have their own box. A save that cannot be read drops nothing here;
// the pre-start check reports it.
func (s *server) planDrops(ctx context.Context, id string, plan mods.Plan, running bool) []string {
	drops, err := s.planner.SaveDrops(ctx, id, plan, running)
	if err != nil {
		s.log.Warn("read the mods of the save that loads", "world", id, "error", err)
		return nil
	}
	return slices.DeleteFunc(drops, func(n string) bool {
		return slices.ContainsFunc(plan.Unresolved, func(u mods.Unresolved) bool { return u.Name == n })
	})
}

// errStagedGone says an uploaded zip is no longer waiting, as after a
// restart of Foreman.
var errStagedGone = errors.New("the uploaded mod zip is gone")

// planRequest builds the resolver's request from the form, reading a
// sync's save and an uploaded zip's info.json again.
func (s *server) planRequest(id string, p *ModPlanPage) (mods.Request, error) {
	req, err := s.kindRequest(id, p)
	req.AlsoDisable, req.AlsoRemove = p.Form.AlsoDisable, p.Form.AlsoRemove
	return req, err
}

// kindRequest is the request for the form's kind of change.
func (s *server) kindRequest(id string, p *ModPlanPage) (mods.Request, error) {
	f := &p.Form
	var up *world.ModInfo
	if f.Staged != "" {
		info, err := s.stagedInfo(f.Staged)
		if err != nil {
			return mods.Request{}, err
		}
		p.Uploaded = info
		up = &info
	}
	switch f.Kind {
	case mods.KindUpload:
		if up == nil {
			return mods.Request{}, errStagedGone
		}
		return mods.Request{Kind: mods.KindUpload, Upload: up, Optional: f.Optional, Declined: f.Declined}, nil
	case mods.KindSync:
		if f.Save == "" {
			save, err := s.loadingSave(id)
			if err != nil {
				return mods.Request{}, err
			}
			if save == "" {
				return mods.Request{}, world.ErrNoSaves
			}
			f.Save = save
		}
		// Upload is the default choice, so a staged zip answers its mod
		// unless another choice was made.
		if _, chose := f.Choices[p.Uploaded.Name]; up != nil && !chose {
			f.Choices[up.Name] = mods.ChoiceUpload
		}
		choices := map[string]mods.Choice{}
		for name, kind := range f.Choices {
			c := mods.Choice{Kind: kind, Confirmed: f.Confirmed[name]}
			if kind == mods.ChoiceUpload && up != nil && up.Name == name {
				c.Upload = up
			}
			choices[name] = c
		}
		req, err := s.planner.SyncRequest(id, f.Save, choices)
		req.Optional = f.Optional
		return req, err
	}
	return mods.Request{Kind: f.Kind, Names: f.Names, Optional: f.Optional, Declined: f.Declined, Kept: f.Kept, Versions: f.Versions}, nil
}

// planError words why no plan could be made.
func (s *server) planError(id string, err error) i18n.Msg {
	switch {
	case errors.Is(err, modportal.ErrUnreachable):
		s.portalTrouble("make a mod plan", id, err)
		return i18n.M("mods.error.portal")
	case errors.Is(err, errStagedGone), errors.Is(err, os.ErrNotExist) && strings.Contains(err.Error(), world.PrefixPartial):
		return i18n.M("mods.error.staged_gone")
	case errors.Is(err, world.ErrModZip), errors.Is(err, world.ErrModName), errors.Is(err, world.ErrModTooBig):
		return modZipError(err)
	case errors.Is(err, world.ErrNoSaves):
		return i18n.M("mods.error.no_save")
	case errors.Is(err, world.ErrSaveNotFound):
		return i18n.M("mods.error.save_gone")
	}
	if status, msg := worldError(err); status != http.StatusInternalServerError && !msg.IsZero() {
		return msg
	}
	for _, e := range []error{savefile.ErrNotZip, savefile.ErrNoLevelData, savefile.ErrFormat, savefile.ErrTruncated} {
		if errors.Is(err, e) {
			return i18n.M("mods.error.save_unreadable")
		}
	}
	s.log.Error("make a mod plan", "world", id, "error", err)
	return i18n.M("mods.error.plan")
}

// handlePlan applies a plan the admin confirmed. It recomputes the plan
// first; when that is not the plan shown, or the mods folder moved on, it
// applies nothing and shows the new plan (SPEC 9.14, 17.8).
func (s *server) handlePlan(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	f, ok := s.postedForm(u, sum, r.PostForm)
	if !ok {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	form, stop := s.readCountdown(r)
	p := s.planPage(r.Context(), sum, f, form)
	p.Draft = s.drafts.keep(r.PostForm.Get(draftField), u.ID, sum.ID, p.Form)
	if !p.Running {
		p.CountdownError = i18n.Msg{}
	}
	switch {
	case !p.Error.IsZero():
		s.page(w, r, http.StatusBadGateway, u.Username, p)
		return
	case p.Hash != f.Hash || p.Fingerprint != f.Fingerprint:
		p.Changed, p.CountdownForm = true, CountdownForm{Countdown: form.Countdown}
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	p.confirmProblems(f)
	if len(p.Problems()) > 0 {
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	if !p.CanApply() {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	op, unused, err := s.planOp(p, f)
	if err != nil {
		s.fail(w, r, "turn the plan into a change", err)
		return
	}
	// The installed picks leave the saved ones; a refused request keeps
	// them, and so do picks this plan did not install (SPEC 9.14).
	s.applyModsThen(w, r, u, sum, op, stop, unused, p.shownResult(), func(ctx context.Context) { s.dropInstalledPicks(ctx, sum.ID, f) })
}

// confirmProblems marks the boxes the plan needs that f leaves unticked:
// each "continue without" and the drops (SPEC 9.4, 9.10).
func (p *ModPlanPage) confirmProblems(f planForm) {
	for _, un := range p.Plan.Unresolved {
		if un.Choice != nil && un.Choice.Kind == mods.ChoiceWithout && !un.Choice.Confirmed {
			p.ConfirmErrors[un.Name] = i18n.M("mods.unresolved.confirm_missing", "mod", un.Name)
		}
	}
	if len(p.Drops) > 0 && !f.DropsConfirmed {
		p.DropsError = dropsMissing(p.Drops)
	}
}

// planOp turns a confirmed plan into the controller's mod change. unused is
// a staged zip the plan does not use, removed once the change ends.
func (s *server) planOp(p ModPlanPage, f planForm) (op game.ModOp, unused string, err error) {
	installed, err := s.worlds.Mods(p.World.ID)
	if err != nil {
		return game.ModOp{}, "", fmt.Errorf("list mods: %w", err)
	}
	staged := ""
	if p.UsesUpload() {
		staged = f.Staged
	}
	downloads, change, err := mods.ToChange(p.Plan, installed, staged)
	if err != nil {
		return game.ModOp{}, "", err
	}
	op = game.ModOp{Change: change, Fingerprint: p.Fingerprint, Summary: planSummary(p.Plan)}
	// A save's mod the admin chose to go on without is dropped knowingly too.
	confirmed := slices.Clone(p.Drops)
	for _, un := range p.Plan.Unresolved {
		if un.Choice != nil && un.Choice.Kind == mods.ChoiceWithout && un.Choice.Confirmed {
			confirmed = append(confirmed, un.Name)
		}
	}
	op.ConfirmedDrops = confirmed
	for _, d := range downloads {
		op.Downloads = append(op.Downloads, game.ModDownload{Mod: d.Name, Release: d.Release})
	}
	// A zip the plan does not use is not kept waiting.
	if staged == "" {
		unused = f.Staged
	}
	return op, unused, nil
}

// shownResult is what the result says changed. Trying again opens this
// plan anew, unless it used the uploaded zip.
func (p ModPlanPage) shownResult() modResult {
	shown := modResult{Changes: p.Plan.Changes}
	if !p.UsesUpload() {
		shown.Retry = p.DraftPath()
	}
	return shown
}

// dropInstalledPicks takes an install's mods out of the saved picks once
// its change has installed them (SPEC 9.14).
func (s *server) dropInstalledPicks(ctx context.Context, id string, f planForm) {
	if f.Kind != mods.KindInstall {
		return
	}
	for _, name := range f.Names {
		if err := s.store.RemoveModPlanName(ctx, id, name); err != nil {
			s.log.Warn("take an installed mod out of the mod plan", "world", id, "error", err)
		}
	}
}

// UsesUpload reports whether a change in the plan comes from the staged zip.
func (p ModPlanPage) UsesUpload() bool {
	return slices.ContainsFunc(p.Plan.Changes, func(c mods.Change) bool { return c.Upload })
}

// planSummary is what the audit log records: mod names and versions only.
func planSummary(p mods.Plan) string {
	var parts []string
	for _, c := range p.Changes {
		if c.Action == mods.ActKeep {
			continue
		}
		parts = append(parts, string(c.Action)+" "+strings.TrimSpace(c.Name+" "+c.To))
	}
	return clipSummary(strings.Join(parts, ", "))
}

const maxSummary = 500

// clipSummary cuts an audit summary at maxSummary bytes, never inside a
// character, and marks the cut.
func clipSummary(s string) string {
	if cut, ok := cutUTF8(s, maxSummary); ok {
		return cut + "…"
	}
	return s
}

// applyMods runs one mod change in the background (SPEC 9.13). The
// Server page follows the active world's; the Mods tab any other world's.
// The result names the "Before mod change" snapshot, for undo. It reports
// whether the change was started.
func (s *server) applyMods(w http.ResponseWriter, r *http.Request, u store.User, sum world.Summary, op game.ModOp, stop game.StopOptions, unused string, shown modResult) bool {
	return s.applyModsThen(w, r, u, sum, op, stop, unused, shown, nil)
}

// applyModsThen is applyMods that runs done when the change ended without
// an error, so a failed install keeps the picks it did not install (SPEC 9.14).
func (s *server) applyModsThen(w http.ResponseWriter, r *http.Request, u store.User, sum world.Summary, op game.ModOp, stop game.StopOptions, unused string, shown modResult, done func(context.Context)) bool {
	to := PathServer
	if !sum.Active {
		to = modsPath(sum.ID) + ownResultQuery
	}
	return s.beginTo(w, r, to, []string{sum.ID}, true, s.modActionThen(sum.ID, actor(u), op, stop, unused, shown, done))
}

// modAction is a mod change's background work, for the Mods pages and the
// MCP tools alike; its result names the snapshot that undoes it.
func (s *server) modAction(id string, by game.Actor, op game.ModOp, stop game.StopOptions, unused string, shown modResult) func(ctx context.Context) actionResult {
	return s.modActionThen(id, by, op, stop, unused, shown, nil)
}

// modActionThen is modAction that runs done after a change that ended
// without an error.
func (s *server) modActionThen(id string, by game.Actor, op game.ModOp, stop game.StopOptions, unused string, shown modResult, done func(context.Context)) func(ctx context.Context) actionResult {
	newest := s.newestModSnapshot(id)
	run, inRun := s.worlds.ModRunSnapshot(id)
	return func(ctx context.Context) actionResult {
		err := s.game.ApplyMods(ctx, id, by, op, stop)
		if err == nil && done != nil {
			done(ctx)
		}
		if unused != "" {
			s.removeStaged(unused)
		}
		res := s.finish(id, game.AuditMods, err)
		res.Mods = shown
		res.Undo, res.Mods.Since = s.modUndoSnapshot(id, err, newest, run, inRun)
		return res
	}
}

// newestModSnapshot is the name of the world's newest "Before mod change"
// snapshot, or "".
func (s *server) newestModSnapshot(id string) string {
	list, err := s.worlds.Snapshots(id)
	if err != nil {
		s.log.Error("list snapshots", "world", id, "error", err)
		return ""
	}
	for _, sn := range list {
		if sn.Reason == world.ReasonMods {
			return sn.Name
		}
	}
	return ""
}

// syncOffer is the link that matches a world's mods to its save, when the
// pre-start check found a mod of the save missing or turned off (SPEC 9.8,
// 17.9), or nil.
func syncOffer(check world.PreCheck, id string) *Offer {
	for _, pr := range check.Problems {
		if pr.Kind == world.ProblemModMissing || pr.Kind == world.ProblemModDisabled {
			return &Offer{Path: syncPath(id), Label: i18n.M("mods.sync.offer")}
		}
	}
	return nil
}

// UndoName is the label of the snapshot the failure's undo restores.
func (f *Failure) UndoName() i18n.Msg {
	if !f.UndoLabel.IsZero() {
		return f.UndoLabel
	}
	return i18n.M("worlds.snapshot.label.before_restore")
}
