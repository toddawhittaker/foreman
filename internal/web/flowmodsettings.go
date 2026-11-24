package web

import (
	"context"
	"net/http"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/store"
)

// PathFlowModSettings is step 3 of the guided Generate: the startup settings
// of the world's mods, before the map settings they can change (SPEC 5.6).
const PathFlowModSettings = "/worlds/{id}/new-map/mod-settings"

// flowModSettingsPath is step 3 of the world id.
func flowModSettingsPath(id string) string { return newMapPath(id) + "/mod-settings" }

// flowSaveWait is how long a save waits for its mod change before it shows
// the change's progress on the Mods tab instead of going on to step 4.
var flowSaveWait = 30 * time.Second

// FlowModSettingsPage is step 3. It draws the mod settings page's form part
// with the startup settings only, posting back here (SPEC 5.6, 9.6).
type FlowModSettingsPage struct {
	ModSettingsPage
}

func (FlowModSettingsPage) templateName() string { return "flowmodsettings" }
func (FlowModSettingsPage) navItem() string      { return PathWorlds }

// Path is where the form posts and the retry goes.
func (p FlowModSettingsPage) Path() string { return flowModSettingsPath(p.World.ID) }

// FormPath is the form part, which can take a game run.
func (p FlowModSettingsPage) FormPath() string { return p.Path() + "/form" }

// WaitPath is step 3 with the form read before it answers, for a browser
// without script (SPEC 5.6, 9.6).
func (p FlowModSettingsPage) WaitPath() string { return p.Path() + "?" + newMapWaitQuery + "=1" }

// SkipPath is step 4, the map settings.
func (p FlowModSettingsPage) SkipPath() string { return newMapPath(p.World.ID) }

// SkippedPath is step 4 when step 3 passes itself on, which says why.
func (p FlowModSettingsPage) SkippedPath() string { return skippedPath(p.World.ID) }

// WorldPath is the world's page, for the crumb.
func (p FlowModSettingsPage) WorldPath() string { return worldPath(p.World.ID) }

// Steps is the step bar with step 3 current.
func (p FlowModSettingsPage) Steps() []FlowStep {
	return flowSteps(p.World.ID, flowStepModSettings, false)
}

// Back is step 2, the Mods tab, in the form's footer.
func (p FlowModSettingsPage) Back() FlowLink { return flowBack(p.Steps()) }

// InFlow says the form's footer is the guided Generate's: Back, then Save.
func (FlowModSettingsPage) InFlow() bool { return true }

func (s *server) flowModSettingsRoutes() {
	s.mux.HandleFunc("GET "+PathFlowModSettings, s.requireUser(s.handleFlowModSettings))
	// A definitions cache miss runs the game, as on the mod settings page.
	s.handleLong("GET "+PathFlowModSettings+"/form", s.requireUser(s.handleFlowModSettingsForm))
	s.handleUpload("POST "+PathFlowModSettings, fixedLimit(MaxSettingsUpload), s.requireUser(s.handleFlowModSettingsSave))
}

// noModSettings says step 3 has nothing to set, without a game run: the
// world has only built-in mods, or its cached definitions have no startup
// setting shown. Until a run is cached the step stays (SPEC 5.6).
func (s *server) noModSettings(id string) bool {
	list, err := s.worlds.Mods(id)
	if err != nil {
		return false
	}
	modded := false
	for _, m := range list {
		if !m.BuiltIn {
			modded = true
		}
	}
	if !modded {
		return true
	}
	if s.game == nil {
		return false
	}
	defs, ok := s.game.CachedModSettingDefs(id)
	return ok && !hasStartupSettings(defs)
}

// hasStartupSettings says a startup setting the form shows exists.
func hasStartupSettings(defs *game.ModSettingDefs) bool {
	for _, d := range defs.Defs {
		if d.Setting == modsettings.Startup && !d.Hidden {
			return true
		}
	}
	return false
}

// flowModSettingsPage is the step's form, startup settings only: the map
// settings of the mods wait for the world page (SPEC 5.6).
func (s *server) flowModSettingsPage(ctx context.Context, lang string, p NewMapPage) (FlowModSettingsPage, settingsState, error) {
	mp, st, err := s.modSettingsPage(ctx, lang, p.World, true)
	mp.NewMap = false
	return FlowModSettingsPage{mp}, st, err
}

// handleFlowModSettings shows step 3, whose form loads after it. A world
// with only built-in mods goes straight on to step 4. Without script the
// page reloads itself with wait, and the server answers with the form.
func (s *server) handleFlowModSettings(w http.ResponseWriter, r *http.Request, u store.User) {
	np, ok := s.newMapWorld(w, r, u)
	if !ok {
		return
	}
	if np.NoModSettings {
		http.Redirect(w, r, skippedPath(np.World.ID), http.StatusSeeOther) //nolint:gosec // a world Foreman found, a local path segment
		return
	}
	if r.URL.Query().Get(newMapWaitQuery) == "" {
		s.page(w, r, http.StatusOK, u.Username, FlowModSettingsPage{ModSettingsPage{World: np.World}})
		return
	}
	p, ok := s.loadFlowModSettings(w, r, np)
	if !ok {
		return
	}
	if p.Empty() && p.RunError.IsZero() {
		http.Redirect(w, r, p.SkippedPath(), http.StatusSeeOther) //nolint:gosec // a world Foreman found, a local path segment
		return
	}
	s.page(w, s.withWorldIcons(r, np.World.ID), http.StatusOK, u.Username, p)
}

// handleFlowModSettingsForm is the form part. With no startup setting it
// sends the browser on to step 4 (SPEC 5.6).
func (s *server) handleFlowModSettingsForm(w http.ResponseWriter, r *http.Request, u store.User) {
	np, ok := s.newMapWorld(w, r, u)
	if !ok {
		return
	}
	p, ok := s.loadFlowModSettings(w, r, np)
	if !ok {
		return
	}
	if p.Empty() && p.RunError.IsZero() {
		w.Header().Set("HX-Redirect", p.SkippedPath())
		w.WriteHeader(http.StatusOK)
		return
	}
	p.Fragment = true
	s.fragment(w, s.withWorldIcons(r, np.World.ID), p, "modsettings-form")
}

// loadFlowModSettings reads the form, as the mod settings page's form part
// does: a failure of Foreman's own still shows the retry.
func (s *server) loadFlowModSettings(w http.ResponseWriter, r *http.Request, np NewMapPage) (FlowModSettingsPage, bool) {
	p, _, err := s.flowModSettingsPage(r.Context(), localeOf(r).String(), np)
	if err != nil {
		if r.Context().Err() != nil {
			return p, false
		}
		s.log.Error("read the mod settings", "world", np.World.ID, "error", err)
		p = FlowModSettingsPage{ModSettingsPage{World: np.World, Loaded: true, HasFile: p.HasFile, RunError: i18n.M("mods.settings.edit.run.failed")}}
	}
	return p, true
}

// handleFlowModSettingsSave writes the changed startup settings as one mod
// change, as the mod settings page does (SPEC 9.6, 9.13), then goes on to
// step 4, whose planets now follow them. A change that outlasts
// flowSaveWait shows its progress on the Mods tab.
func (s *server) handleFlowModSettingsSave(w http.ResponseWriter, r *http.Request, u store.User) {
	np, ok := s.newMapWorld(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	countdown, stop := s.readCountdown(r)
	p, st, err := s.flowModSettingsPage(r.Context(), localeOf(r).String(), np)
	if err != nil {
		s.fail(w, r, "read the mod settings", err)
		return
	}
	r = s.withWorldIcons(r, np.World.ID)
	if !p.RunError.IsZero() {
		s.page(w, r, http.StatusBadGateway, u.Username, p)
		return
	}
	// The form was drawn from other files, or for a game that has since
	// started or stopped (SPEC 17.8): show the current form.
	if r.PostFormValue("fingerprint") != p.Fingerprint || r.PostFormValue("seen") != p.Seen ||
		(r.PostFormValue("running") == "1") != p.Running {
		p.Changed = true
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	changes := p.readChanges(func(f *SettingField) (settingChange, bool) { return readField(r, f) })
	p.Countdown = countdown.Countdown
	if p.Running {
		p.CountdownError = countdown.CountdownError
	}
	if p.hasErrors() {
		s.page(w, r, http.StatusUnprocessableEntity, u.Username, p)
		return
	}
	if len(changes) == 0 {
		http.Redirect(w, r, p.SkipPath(), http.StatusSeeOther) //nolint:gosec // a world Foreman found, a local path segment
		return
	}
	if busy := s.worldBusy(p.World.ID); !busy.IsZero() {
		p.Busy = busy
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	op, err := modSettingsOp(p.ModSettingsPage, st, changes)
	if err != nil {
		s.fail(w, r, "write the changed mod settings", err)
		return
	}
	done := make(chan actionResult, 1)
	act := s.modAction(p.World.ID, actor(u), op, stop, "", modResult{Retry: p.Path()})
	if !s.runs.runOn([]string{p.World.ID}, true, func(ctx context.Context) actionResult {
		res := act(ctx)
		done <- res
		return res
	}) {
		s.page(w, r, http.StatusServiceUnavailable, "", ErrorPage{Status: http.StatusServiceUnavailable, Detail: i18n.M("server.shutting_down")})
		return
	}
	progress := modsPath(p.World.ID) + ownResultQuery
	if p.World.Active {
		progress = PathServerResult
	}
	timer := time.NewTimer(flowSaveWait)
	defer timer.Stop()
	select {
	case res := <-done:
		if res.Err == nil {
			progress = p.SkipPath()
		}
	case <-timer.C:
	case <-r.Context().Done():
		return
	}
	http.Redirect(w, r, progress, http.StatusSeeOther) //nolint:gosec // a world Foreman found, a local path segment
}
