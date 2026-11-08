package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
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
)

// ModPortal is what the mods pages ask the mod portal; *modportal.Client
// has it.
type ModPortal interface {
	mods.Portal
	Search(ctx context.Context, line, query string) ([]modportal.Hit, error)
	Download(ctx context.Context, username, token string, r modportal.Release, dst *os.File) error
}

// accountFetcher downloads with the factorio.com account the service holds
// now, so a newly entered account works without a restart (SPEC 9.11).
type accountFetcher struct {
	account *factorioaccount.Service
	portal  ModPortal
}

func (f accountFetcher) Download(ctx context.Context, r modportal.Release, dst *os.File) error {
	acct, src, _, err := f.account.Get(ctx)
	if err != nil {
		return err
	}
	if src != factorioaccount.Env && src != factorioaccount.Stored {
		return game.ErrNoModFetcher
	}
	return f.portal.Download(ctx, acct.Username.Reveal(), acct.Token.Reveal(), r, dst)
}

func modsPath(id string) string { return worldPath(id) + "/mods" }

// planPath is the plan page for a request given as query values.
func planPath(id string, q url.Values) string {
	return modsPath(id) + "/plan?" + q.Encode()
}

func kindPlanPath(id string, kind mods.Kind, names ...string) string {
	q := url.Values{"kind": {string(kind)}}
	if len(names) > 0 {
		q["name"] = names
	}
	return planPath(id, q)
}

// syncPath is the plan that matches a world's mods to the save that loads
// (SPEC 9.8, 7.11).
func syncPath(id string) string { return kindPlanPath(id, mods.KindSync) }

func (s *server) modRoutes() {
	s.mux.HandleFunc("GET "+PathWorldMods, s.requireUser(s.handleMods))
	s.mux.HandleFunc("GET "+PathModStatus, s.requireUser(s.handleModStatus))
	s.mux.HandleFunc("GET "+PathModNotice, s.requireUser(s.handleModNotice))
	s.mux.HandleFunc("GET "+PathModSearch, s.requireUser(s.handleModSearch))
	s.mux.HandleFunc("GET "+PathModPlan, s.requireUser(s.handlePlanForm))
	s.mux.HandleFunc("POST "+PathModPlan, s.requireUser(s.handlePlan))
	s.modFileRoutes()
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
	World world.Summary
	Mods  []world.InstalledMod
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
}

// UploadLimit and SettingsLimit say the upload size limits.
func (ModsPage) UploadLimit() i18n.Msg   { return megabytes(modUploadMB) }
func (ModsPage) SettingsLimit() i18n.Msg { return megabytes(settingsUploadMB) }

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
func (p ModsPage) SavesPath() string    { return worldPath(p.World.ID) + "/saves" }
func (p ModsPage) AccountPath() string  { return accountLink(p.Path()) }
func (p ModsPage) Tabs() WorldTabs      { return worldTabs(p.World, "mods") }

// RowPath is the plan page for one mod and one change.
func (p ModsPage) RowPath(kind, name string) string {
	return kindPlanPath(p.World.ID, mods.Kind(kind), name)
}

// UpdateSlot is the id of a row's place for its Update button, which the
// portal status fills in later.
func (ModsPage) UpdateSlot(name string) string { return updateSlot(name) }

func updateSlot(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "mod-update-" + hex.EncodeToString(sum[:6])
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
func (ModsPage) NoteSlot(name string) string { return noteSlot(name) }

func noteSlot(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "mod-note-" + hex.EncodeToString(sum[:6])
}

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
func (ModsPage) PortalSlot(name string) string { return portalSlot(name) }

// portalPath is a mod's page on the mod portal.
func portalPath(name string) string { return "https://mods.factorio.com/mod/" + url.PathEscape(name) }

func portalSlot(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "mod-portal-" + hex.EncodeToString(sum[:6])
}

func (s *server) handleMods(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	p, err := s.modsPage(r.Context(), sum)
	if err != nil {
		s.notFoundOrFail(w, r, u, "list mods", err)
		return
	}
	// Only the activity block polls; once the action ends the page reloads
	// whole, so the list is current.
	if r.Header.Get("HX-Request") != "" && !p.Working {
		w.Header().Set("HX-Refresh", "true")
	}
	if followsOwnAction(r) {
		s.modsOutcome(&p)
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// modsPage reads the world's mods and what the tab offers.
func (s *server) modsPage(ctx context.Context, sum world.Summary) (ModsPage, error) {
	p := ModsPage{World: sum, CountdownForm: defaultCountdown()}
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
	op := s.game.Status(sum.ID).Operation
	p.Running = game.IsRunning(s.game.Status(sum.ID).State)
	p.Busy = busyText(op, s.worldName)
	p.ServerLink = !p.Busy.IsZero() && (sum.Active || op.Switch != nil)
	p.Working = !p.Busy.IsZero() || s.runs.inFlight(sum.ID)
	return p, nil
}

// modsOutcome says how the last mod change of a world that is not active
// ended; the Server page says it for the active world (SPEC 17.9).
func (s *server) modsOutcome(p *ModsPage) {
	if p.World.Active {
		return
	}
	res, ok := s.runs.result(p.World.ID)
	if !ok || res.Action != game.AuditMods {
		return
	}
	p.Undo = modsUndo(p.World.ID, res)
	if res.Err == nil {
		p.Done = modsDone(res.Mods, p.World.Name)
		return
	}
	p.Failure = &Failure{Refused: game.IsRefusal(res.Err)}
	p.Failure.Message, p.Failure.Fix = modsFailure(res.Err, res.Mods, p.World.Name, s.game.Status(p.World.ID).State, p.Path())
	if errors.Is(res.Err, world.ErrModsChanged) {
		p.Failure.Next = &Offer{Path: p.Path(), Label: i18n.M("mods.changed.next")}
	}
}

// ModStatus is what the portal says about a world's mods: updates (SPEC
// 9.3) and mods players cannot get (#131). It loads after the tab: one
// summary line above the table, and each mod's news in its own row.
type ModStatus struct {
	World       world.Summary
	Unreachable bool
	Updates     []mods.Update
	Flags       []mods.Flag
	// Current counts the mods the portal has at their installed version.
	Current int
}

func (ModStatus) templateName() string { return "mods" }

// UpdateAllPath is the plan that updates every mod with an update.
func (p ModStatus) UpdateAllPath() string { return kindPlanPath(p.World.ID, mods.KindUpdate) }
func (p ModStatus) Path() string          { return modsPath(p.World.ID) }

// UpdatePath is the plan for one mod's update.
func (p ModStatus) UpdatePath(name string) string {
	return kindPlanPath(p.World.ID, mods.KindUpdate, name)
}

// Slot ids of a row's places the status fills.
func (ModStatus) UpdateSlot(name string) string { return updateSlot(name) }
func (ModStatus) PortalSlot(name string) string { return portalSlot(name) }
func (ModStatus) NoteSlot(name string) string   { return noteSlot(name) }

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
	UpdateTo    string
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
	return out
}

// FlagText words one flag in its mod's row, so it need not name the mod
// (#131).
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
	st := ModStatus{World: sum}
	var err error
	if st.Updates, err = s.planner.Updates(r.Context(), sum.ID); err == nil {
		st.Flags, err = s.planner.Availability(r.Context(), sum.ID)
	}
	if err != nil {
		s.portalTrouble("check the mods on the portal", sum.ID, err)
		st = ModStatus{World: sum, Unreachable: true}
	}
	if list, lerr := s.worlds.Mods(sum.ID); lerr == nil && !st.Unreachable {
		st.Current = currentCount(list, st.Flags)
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
// (#131, SPEC 9.3). It is empty when there are none or the portal
// cannot be asked.
type ModNotice struct {
	World  world.Summary
	Errors int
	// Mod and Version name the mod when there is one; Update says its fix
	// is an update, else a removal.
	Mod, Version string
	Update       bool
	FixPath      string
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
	for _, f := range flags {
		if f.Severity != mods.SevError {
			continue
		}
		n.Errors++
		n.Mod, n.Version = f.Name, f.Version
		gone = append(gone, world.GoneMod{Name: f.Name, Version: f.Version, Update: f.Update})
	}
	n.Update = !slices.ContainsFunc(gone, func(g world.GoneMod) bool { return g.Update == "" })
	if n.Errors > 0 {
		n.FixPath = goneFix(sum.ID, gone).Path
	}
	s.fragment(w, r, n, "mods-notice")
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

// ModSearchPage lists the portal's mods that match a search (SPEC 9.1).
type ModSearchPage struct {
	World      world.Summary
	Query      string
	QueryError i18n.Msg
	Results    ModHits
	// Picks are the mods ticked to add, on this page of results or another;
	// PickError says none was ticked when "Add ticked mods" was chosen.
	Picks     []string
	PickError i18n.Msg
	// Paged says a page button brought the admin here, so focus goes to
	// the results.
	Paged bool
	// Installed names the mods the world has already.
	Installed   map[string]bool
	Unreachable bool
	NoAccount   bool
}

func (ModSearchPage) templateName() string { return "modsearch" }
func (ModSearchPage) navItem() string      { return PathWorlds }
func (p ModSearchPage) hasErrors() bool    { return len(p.Problems()) > 0 }

// Problems lists the search field's error, then the ticks'.
func (p ModSearchPage) Problems() []Problem {
	return problems(Problem{"mod-search", p.QueryError}, Problem{p.firstPick(), p.PickError})
}

// firstPick is the id of the first box on the page, where the ticks' error
// points.
func (p ModSearchPage) firstPick() string {
	for _, h := range p.Results.Rows {
		if !p.Installed[h.Name] {
			return modID(h.Name, "pick")
		}
	}
	return "pick-error"
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

// Picking says the results have boxes to tick several mods at once.
func (ModSearchPage) Picking() bool { return true }

// HitState is what a result offers: "installed" or "add".
func (p ModSearchPage) HitState(name string) string {
	if p.Installed[name] {
		return "installed"
	}
	return "add"
}

// Picked reports whether a result's box is ticked.
func (p ModSearchPage) Picked(name string) bool { return slices.Contains(p.Picks, name) }

// PicksElsewhere are the ticked mods not on this page of results, which the
// form carries along unseen.
func (p ModSearchPage) PicksElsewhere() []string {
	return slices.DeleteFunc(slices.Clone(p.Picks), func(n string) bool {
		return slices.ContainsFunc(p.Results.Rows, func(h modportal.Hit) bool { return h.Name == n })
	})
}

// CanPick says the form has a mod that "Add ticked mods" could add.
func (p ModSearchPage) CanPick() bool {
	return len(p.Picks) > 0 || slices.ContainsFunc(p.Results.Rows, func(h modportal.Hit) bool { return !p.Installed[h.Name] })
}

// ModID is modID for the template.
func (ModSearchPage) ModID(name, part string) string { return modID(name, part) }

// AddPath is the plan that installs one mod, keeping the search and its
// page so the plan page can offer the same results to add more.
func (p ModSearchPage) AddPath(name string) string {
	return planPath(p.World.ID, p.planForm([]string{name}).query())
}

func (p ModSearchPage) planForm(names []string) planForm {
	return planForm{Kind: mods.KindInstall, Names: names, Query: p.Query, Page: p.Results.Page}
}

// handleModSearch searches the portal's list for the world's game line. It
// needs no account (SPEC 9.11). "Add ticked mods" sends the ticked mods to
// one plan; a page button keeps them ticked.
func (s *server) handleModSearch(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	q := r.URL.Query()
	p := ModSearchPage{World: sum, Query: strings.TrimSpace(q.Get("q")), Installed: map[string]bool{}, Paged: q.Has("page")}
	if p.Query == "" {
		p.QueryError = i18n.M("mods.search.missing")
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	if len(p.Query) > maxSearchQuery {
		p.QueryError = i18n.M("mods.search.long", "count", maxSearchQuery)
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	// A plan names at most maxPlanNames mods, so more ticks are not kept.
	for _, n := range q["pick"] {
		if len(n) > maxPickName {
			p.QueryError = i18n.M("mods.search.long", "count", maxPickName)
			s.page(w, r, http.StatusBadRequest, u.Username, p)
			return
		}
		if !slices.Contains(p.Picks, n) && len(p.Picks) < maxPlanNames {
			p.Picks = append(p.Picks, n)
		}
	}
	page := readPage(q.Get("page"))
	adding := q.Get("picked") == "1"
	if adding {
		page = readPage(q.Get("at"))
	}
	if adding && len(p.Picks) > 0 {
		p.Results.Page = page
		http.Redirect(w, r, planPath(sum.ID, p.planForm(p.Picks).query()), http.StatusSeeOther) //nolint:gosec // a local path; the ticked names are only query values
		return
	}
	p.NoAccount = !s.downloadsOn(r.Context())
	if list, err := s.worlds.Mods(sum.ID); err == nil {
		for _, m := range list {
			p.Installed[m.Name] = true
		}
	}
	hits, err := s.portal.Search(r.Context(), mods.GameLine(sum.GameVersion), p.Query)
	if err != nil {
		s.portalTrouble("search the mod portal", sum.ID, err)
		p.Unreachable = true
		s.page(w, r, http.StatusBadGateway, u.Username, p)
		return
	}
	p.Results = pageOfHits(hits, page)
	if adding {
		p.PickError = i18n.M("mods.search.pick_none")
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// maxSearchQuery bounds a search, which is only a name or a few words.
const maxSearchQuery = 100

// maxPickName bounds one ticked mod name, which goes into the redirect to the
// plan page; the portal's names are far shorter.
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
	// Query is the search the install plan's "Add another mod" shows, and
	// Page the page of its results.
	Query string
	Page  int
	// Added and Removed name the mod a click just added to or took out of
	// the plan, for the sentence the redraw announces. Links carry them;
	// the plan does not.
	Added, Removed string
	// Searched is a search from "Add another mod": its results heading
	// takes focus, not the messages above the plan.
	Searched bool
	// AlsoDisable and AlsoRemove are installed mods a conflict's fix turns
	// off in the same plan; Fixed names the one a click just added, for
	// the sentence the redraw announces (SPEC 9.2, 17.9).
	AlsoDisable, AlsoRemove []string
	Fixed                   string
}

const (
	choicePrefix     = "choice-"
	confirmPrefix    = "confirm-"
	alsoDisableField = "also-disable"
	alsoRemoveField  = "also-remove"
)

// maxPlanNames bounds the mods one plan request names, and the optional
// mods it ticks.
const maxPlanNames = 50

var planKinds = []mods.Kind{mods.KindInstall, mods.KindUpdate, mods.KindSync, mods.KindEnable, mods.KindDisable, mods.KindRemove, mods.KindUpload}

// readPlanForm reads a request from a query or a posted form; ok is false
// for one no page offers.
func readPlanForm(v url.Values) (planForm, bool) {
	f := planForm{
		Kind: mods.Kind(v.Get("kind")), Names: v["name"], Optional: v["optional"],
		Save: v.Get("save"), Staged: v.Get("staged"), Hash: v.Get("hash"), Fingerprint: v.Get("fingerprint"),
		DropsConfirmed: v.Get(dropsField) == "1",
		Choices:        map[string]mods.ChoiceKind{}, Confirmed: map[string]bool{},
		Query: strings.TrimSpace(v.Get("q")), Added: v.Get("added"), Removed: v.Get("removed"),
		Searched: v.Get("add") == "1", Page: readPage(v.Get("page")),
		AlsoDisable: v[alsoDisableField], AlsoRemove: v[alsoRemoveField], Fixed: v.Get("fixed"),
	}
	for _, n := range append(slices.Clone(v["declined"]), v["offered"]...) {
		if !slices.Contains(f.Optional, n) && !slices.Contains(f.Declined, n) {
			f.Declined = append(f.Declined, n)
		}
	}
	// Each name can cost a portal request, so a crafted list is bounded.
	if !slices.Contains(planKinds, f.Kind) || len(f.Names) > maxPlanNames || len(f.Optional) > maxPlanNames || len(f.Declined) > maxPlanNames ||
		len(f.AlsoDisable)+len(f.AlsoRemove) > maxPlanNames {
		return planForm{}, false
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

// query is the form as query values, for a link back to the same plan.
func (f planForm) query() url.Values {
	q := url.Values{"kind": {string(f.Kind)}}
	if len(f.Names) > 0 {
		q["name"] = f.Names
	}
	if len(f.Optional) > 0 {
		q["optional"] = f.Optional
	}
	if len(f.Declined) > 0 {
		q["declined"] = f.Declined
	}
	if len(f.AlsoDisable) > 0 {
		q[alsoDisableField] = f.AlsoDisable
	}
	if len(f.AlsoRemove) > 0 {
		q[alsoRemoveField] = f.AlsoRemove
	}
	if f.Save != "" {
		q.Set("save", f.Save)
	}
	for name, c := range f.Choices {
		q.Set(choicePrefix+name, string(c))
	}
	if f.Query != "" {
		q.Set("q", f.Query)
		if f.Page > 1 {
			q.Set("page", strconv.Itoa(f.Page))
		}
	}
	return q
}

// withNames is the form asking for names instead.
func (f planForm) withNames(names []string) planForm {
	f.Names = names
	return f
}

// answers is the form with the "continue without" boxes too, for the
// upload form of an unresolved mod, so a refused zip keeps every answer.
func (f planForm) answers() url.Values {
	q := f.query()
	for name, ok := range f.Confirmed {
		if ok {
			q.Set(confirmPrefix+name, "1")
		}
	}
	return q
}

// ModPlanPage shows a mod change before it applies (SPEC 9.2, 17.5, 17.8).
type ModPlanPage struct {
	World world.Summary
	Form  planForm
	Plan  mods.Plan
	// Hash is the plan's digest the form posts back; Fingerprint the mods
	// folder's.
	Hash, Fingerprint string
	// Uploaded is the uploaded zip's mod, for an upload's heading.
	Uploaded world.ModInfo
	Running  bool
	CountdownForm
	NoAccount bool
	// Changed says the plan is not the one the admin confirmed, so nothing
	// was applied (SPEC 17.8).
	Changed bool
	// Error says why there is no plan; Retry offers to try again.
	Error i18n.Msg
	Retry string
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
	// Results are the "Add another mod" search's page of results; Installed
	// names the world's mods. AddUnreachable says the portal could not be
	// asked.
	Results        ModHits
	Installed      map[string]bool
	AddUnreachable bool
	// Have are the world's mods now, for the conflicts' fixes.
	Have []world.InstalledMod
}

// Adding says the plan installs mods the admin picks, so it lists them
// with a way to take one out and offers to add another (SPEC 9.1, 9.2).
func (p ModPlanPage) Adding() bool { return p.Form.Kind == mods.KindInstall && p.Form.Staged == "" }

// InPlan reports whether the plan already installs or changes a mod.
func (p ModPlanPage) InPlan(name string) bool {
	return slices.Contains(p.Form.Names, name) || slices.ContainsFunc(p.Plan.Changes, func(c mods.Change) bool {
		return c.Name == name && c.Action != mods.ActKeep
	})
}

// CanAdd says another mod fits in one plan request.
func (p ModPlanPage) CanAdd() bool { return len(p.Form.Names) < maxPlanNames }

// Picking is false: each result's Add redraws the plan at once, so a box to
// tick would only cost a click more.
func (ModPlanPage) Picking() bool { return false }

// HitState is what an "Add another mod" result offers: "in_plan",
// "installed", "add", or nothing when the plan is full.
func (p ModPlanPage) HitState(name string) string {
	switch {
	case p.InPlan(name):
		return "in_plan"
	case p.Installed[name]:
		return "installed"
	case p.CanAdd():
		return "add"
	}
	return ""
}

// PagePath is the plan with another page of the search's results; the
// marker puts focus on them as a new search does.
func (p ModPlanPage) PagePath(page int) string {
	f := p.Form
	f.Page = page
	q := f.query()
	q.Set("add", "1")
	return planPath(p.World.ID, q) + "#add-heading"
}

// AddPath is the plan with name added; PlanWith is the same plan without
// the note of what was added, for the address bar.
func (p ModPlanPage) AddPath(name string) string {
	q := p.Form.withNames(append(slices.Clone(p.Form.Names), name)).query()
	q.Set("added", name)
	return planPath(p.World.ID, q)
}

func (p ModPlanPage) PlanWith(name string) string {
	return planPath(p.World.ID, p.Form.withNames(append(slices.Clone(p.Form.Names), name)).query())
}

// RemovePath is the plan without name; PlanWithout the same for the
// address bar.
func (p ModPlanPage) RemovePath(name string) string {
	q := p.Form.withNames(without(p.Form.Names, name)).query()
	q.Set("removed", name)
	return planPath(p.World.ID, q)
}

func (p ModPlanPage) PlanWithout(name string) string {
	return planPath(p.World.ID, p.Form.withNames(without(p.Form.Names, name)).query())
}

func without(names []string, name string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == name })
}

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
	return len(p.Problems()) > 0 || !p.Error.IsZero() || len(p.Plan.Conflicts) > 0
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

// UploadAction is where an unresolved mod's zip is posted: its query holds
// every answer on the page, so the plan that comes back keeps them.
func (p ModPlanPage) UploadAction(name string) string {
	q := p.Form.answers()
	q.Set("for", name)
	if p.Countdown != "" {
		q.Set("countdown", p.Countdown)
	}
	return p.UploadPath() + "?" + q.Encode()
}

// UploadLimit says the zip size limit.
func (ModPlanPage) UploadLimit() i18n.Msg { return megabytes(modUploadMB) }

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

// Announce is the sentence a screen reader hears after a redraw.
func (p ModPlanPage) Announce() i18n.Msg {
	switch {
	case !p.Error.IsZero():
		return p.Error
	case len(p.Plan.Conflicts) > 0:
		return i18n.M("mods.plan.redrawn_conflicts", "count", len(p.Plan.Conflicts))
	case p.Nothing():
		return i18n.M("mods.plan.nothing")
	case p.Form.Fixed != "":
		return i18n.M("mods.plan.fixed", "mod", p.Form.Fixed, "count", p.Count())
	case p.Form.Added != "":
		return i18n.M("mods.plan.added", "mod", p.Form.Added, "count", p.Count())
	case p.Form.Removed != "":
		return i18n.M("mods.plan.removed", "mod", p.Form.Removed, "count", p.Count())
	}
	return i18n.M("mods.plan.redrawn", "count", p.Count())
}

func (p ModPlanPage) Path() string       { return modsPath(p.World.ID) + "/plan" }
func (p ModPlanPage) ModsPath() string   { return modsPath(p.World.ID) }
func (p ModPlanPage) UploadPath() string { return modsPath(p.World.ID) + "/upload" }
func (p ModPlanPage) SavesPath() string  { return worldPath(p.World.ID) + "/saves" }
func (p ModPlanPage) Line() string       { return mods.GameLine(p.World.GameVersion) }

// AccountPath returns here after the account is entered. An upload cannot
// be posted again from a link, so it returns to the Mods tab.
func (p ModPlanPage) AccountPath() string {
	if p.Form.Staged != "" {
		return accountLink(p.ModsPath())
	}
	return accountLink(planPath(p.World.ID, p.Form.query()))
}

// Tabs mark the Mods tab as the section the plan belongs to.
func (p ModPlanPage) Tabs() WorldTabs {
	t := worldTabs(p.World, "mods")
	t.Under = true
	return t
}

// Heading is the page's question, naming the world (DESIGN 7.4).
func (p ModPlanPage) Heading() i18n.Msg {
	w := p.World.Name
	var one any = ""
	if len(p.Form.Names) == 1 {
		one = i18n.Ident(p.Form.Names[0])
	}
	if len(p.Form.AlsoDisable)+len(p.Form.AlsoRemove) > 0 {
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
		if len(p.Form.Names) == 1 {
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
		return i18n.M("mods.plan.heading.upload", "mod", i18n.Ident(p.Uploaded.Name), "version", p.Uploaded.Version, "world", w)
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
	if p.extraKinds() {
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
	return i18n.M("mods.plan.button.install", "count", n)
}

// Nothing says the plan changes no mod and asks nothing.
func (p ModPlanPage) Nothing() bool {
	return p.Error.IsZero() && p.Count() == 0 && len(p.Plan.Conflicts) == 0 && len(p.Plan.Unresolved) == 0
}

// CanApply says the confirming button is shown: every check the apply runs
// has passed, apart from an unticked "continue without" box, which is a
// field error after the post (DESIGN 7.1, 7.2, SPEC 17.8).
func (p ModPlanPage) CanApply() bool {
	if !p.Error.IsZero() || !p.Busy.IsZero() || len(p.Plan.Conflicts) > 0 || p.Count() == 0 && len(p.Plan.Unresolved) == 0 {
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

// changeText words one change and why it is in the plan.
func changeText(c mods.Change) i18n.Msg {
	var what i18n.Msg
	switch {
	case c.Action == mods.ActRemove:
		what = i18n.M("mods.plan.item.name", "mod", i18n.Ident(c.Name), "version", c.From)
	case c.From != "" && c.From != c.To:
		what = i18n.M("mods.plan.item.from_to", "mod", i18n.Ident(c.Name), "from", c.From, "to", c.To)
	case c.Upload:
		what = i18n.M("mods.plan.item.uploaded", "mod", i18n.Ident(c.Name), "version", c.To)
	default:
		what = i18n.M("mods.plan.item.name", "mod", i18n.Ident(c.Name), "version", c.To)
	}
	var why i18n.Msg
	switch c.Why.Code {
	case mods.WhyRequiredBy:
		why = i18n.M("mods.plan.why.required_by", "mod", i18n.Ident(c.Why.Of))
	case mods.WhyOptionalOf:
		why = i18n.M("mods.plan.why.optional_of", "mod", i18n.Ident(c.Why.Of))
	case mods.WhyRecommendedBy:
		why = i18n.M("mods.plan.why.recommended_by", "mod", i18n.Ident(c.Why.Of))
	case mods.WhyFromSave:
		why = i18n.M("mods.plan.why.from_save")
	case mods.WhyNotInSave:
		why = i18n.M("mods.plan.why.not_in_save")
	}
	if !why.IsZero() {
		what = i18n.M("mods.plan.item.why", "item", what, "why", why)
	}
	if c.Warning == mods.WarnOlderThanSave {
		what = join(what, i18n.M("mods.plan.warn.older"))
	}
	return what
}

func conflictText(c mods.Conflict) i18n.Msg {
	switch c.Code {
	case mods.ConflictIncompatible:
		return i18n.M("mods.conflict.incompatible", "mod", i18n.Ident(c.Mod), "other", i18n.Ident(c.Other))
	case mods.ConflictSpaceAgeOff:
		if c.Other == "" {
			return i18n.M("mods.conflict.space_age_save", "mod", i18n.Ident(c.Mod))
		}
		return i18n.M("mods.conflict.space_age_off", "mod", i18n.Ident(c.Mod), "other", i18n.Ident(c.Other))
	case mods.ConflictNoRelease:
		return i18n.M("mods.conflict.no_release", "mod", i18n.Ident(c.Mod), "needs", needsText(c.Needs))
	case mods.ConflictVersion:
		return i18n.M("mods.conflict.version", "mod", i18n.Ident(c.Mod), "version", c.Version, "needs", needsText(c.Needs))
	case mods.ConflictNotOnPortal:
		return i18n.M("mods.conflict.not_on_portal", "mod", i18n.Ident(c.Mod))
	case mods.ConflictRequiredBy:
		return i18n.M("mods.conflict.required_by", "mod", i18n.Ident(c.Mod), "other", i18n.Ident(c.Other))
	case mods.ConflictBuiltIn:
		return i18n.M("mods.conflict.built_in", "mod", i18n.Ident(c.Mod))
	case mods.ConflictLocal:
		return i18n.M("mods.conflict.local", "mod", i18n.Ident(c.Mod))
	case mods.ConflictNotInstalled:
		return i18n.M("mods.conflict.not_installed", "mod", i18n.Ident(c.Mod))
	case mods.ConflictBadDep:
		return i18n.M("mods.conflict.bad_dependency", "mod", i18n.Ident(c.Mod), "text", c.Text)
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
	f, ok := readPlanForm(q)
	if !ok {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	// A plan asks the portal about every mod it names, so another site's
	// page may not make a signed-in browser ask for one. The admin's own
	// click on Try again is same-origin.
	if !fromThisSite(r) {
		p := ModPlanPage{World: sum, Form: f, Error: i18n.M("mods.error.cross_site"), Retry: planPath(sum.ID, f.query())}
		s.page(w, r, http.StatusForbidden, u.Username, p)
		return
	}
	form := defaultCountdown()
	if c, ok := q["countdown"]; ok {
		form.Countdown = c[0]
	}
	p := s.planPage(r.Context(), sum, f, form)
	// Only the plan page searches; applying a plan never does.
	if p.Error.IsZero() {
		s.addSearch(r.Context(), &p)
	}
	// htmx swaps only a 2xx reply, so a redraw that finds a conflict or a
	// portal failure still answers 200 for the page to show it.
	if r.Header.Get("HX-Request") != "" {
		p.Redraw = true
		s.page(w, r, http.StatusOK, u.Username, p)
		return
	}
	s.page(w, r, planStatus(p), u.Username, p)
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
	case len(p.Plan.Conflicts) > 0:
		return http.StatusConflict
	}
	return http.StatusOK
}

// planPage works out the plan for f now. A failure to do so is in Error.
func (s *server) planPage(ctx context.Context, sum world.Summary, f planForm, form CountdownForm) ModPlanPage {
	p := ModPlanPage{World: sum, Form: f, CountdownForm: form, ConfirmErrors: map[string]i18n.Msg{}}
	st := s.game.Status(sum.ID)
	p.Running = game.IsRunning(st.State)
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
		p.Error, p.Retry = s.planError(sum.ID, err), planPath(sum.ID, f.query())
		return p
	}
	p.Drops = s.planDrops(ctx, sum.ID, p.Plan, p.Running)
	if p.Have, err = s.worlds.Mods(sum.ID); err != nil {
		s.log.Warn("list the mods for the plan's fixes", "world", sum.ID, "error", err)
	}
	p.Hash = mods.HashPlan(p.Plan, p.Drops)
	return p
}

// addSearch fills the install plan's "Add another mod" results. A portal
// that cannot be reached is said there; the plan above still stands.
func (s *server) addSearch(ctx context.Context, p *ModPlanPage) {
	// The field's maxlength keeps a typed search short; a longer one is a
	// crafted link, and is not searched.
	if !p.Adding() || p.Form.Query == "" || len(p.Form.Query) > maxSearchQuery {
		return
	}
	p.Installed = map[string]bool{}
	for _, m := range p.Have {
		p.Installed[m.Name] = true
	}
	hits, err := s.portal.Search(ctx, mods.GameLine(p.World.GameVersion), p.Form.Query)
	if err != nil {
		s.portalTrouble("search the mod portal", p.World.ID, err)
		p.AddUnreachable = true
		return
	}
	p.Results = pageOfHits(hits, p.Form.Page)
}

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
	return mods.Request{Kind: f.Kind, Names: f.Names, Optional: f.Optional, Declined: f.Declined}, nil
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
	f, ok := readPlanForm(r.PostForm)
	if !ok {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	form, stop := readCountdown(r)
	p := s.planPage(r.Context(), sum, f, form)
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
	for _, un := range p.Plan.Unresolved {
		if un.Choice != nil && un.Choice.Kind == mods.ChoiceWithout && !un.Choice.Confirmed {
			p.ConfirmErrors[un.Name] = i18n.M("mods.unresolved.confirm_missing", "mod", un.Name)
		}
	}
	if len(p.Drops) > 0 && !f.DropsConfirmed {
		p.DropsError = dropsMissing(p.Drops)
	}
	if len(p.Problems()) > 0 {
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	if !p.CanApply() {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	installed, err := s.worlds.Mods(sum.ID)
	if err != nil {
		s.fail(w, r, "list mods", err)
		return
	}
	staged := ""
	if p.UsesUpload() {
		staged = f.Staged
	}
	downloads, change, err := mods.ToChange(p.Plan, installed, staged)
	if err != nil {
		s.fail(w, r, "turn the plan into a change", err)
		return
	}
	op := game.ModOp{Change: change, Fingerprint: p.Fingerprint, Summary: planSummary(p.Plan)}
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
	unused := ""
	if staged == "" {
		unused = f.Staged
	}
	// Trying again opens this plan anew, unless it used the uploaded zip.
	shown := modResult{Changes: p.Plan.Changes}
	if !p.UsesUpload() {
		shown.Retry = planPath(sum.ID, f.query())
	}
	s.applyMods(w, r, u, sum, op, stop, unused, shown)
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
	out := strings.Join(parts, ", ")
	if len(out) > maxSummary {
		out = out[:maxSummary] + "…"
	}
	return out
}

const maxSummary = 500

// applyMods runs one mod change in the background (SPEC 9.13). The
// Server page follows the active world's; the Mods tab any other world's.
// The result names the "Before mod change" snapshot, for undo.
func (s *server) applyMods(w http.ResponseWriter, r *http.Request, u store.User, sum world.Summary, op game.ModOp, stop game.StopOptions, unused string, shown modResult) {
	id, by := sum.ID, actor(u)
	to := PathServer
	if !sum.Active {
		to = modsPath(id) + ownResultQuery
	}
	newest := s.newestModSnapshot(id)
	run, inRun := s.worlds.ModRunSnapshot(id)
	s.beginTo(w, r, to, []string{id}, true, func(ctx context.Context) actionResult {
		err := s.game.ApplyMods(ctx, id, by, op, stop)
		if unused != "" {
			_ = s.worlds.RemoveStaged(unused)
		}
		res := s.finish(id, game.AuditMods, err)
		res.Mods = shown
		res.Undo, res.Mods.Since = s.modUndoSnapshot(id, err, newest, run, inRun)
		return res
	})
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
