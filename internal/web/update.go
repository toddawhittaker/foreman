package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/releases"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Game version update and rollback (SPEC 10.2 to 10.4, 10.7).
const (
	PathGameUpdate    = "/worlds/{id}/update"
	PathGameRollback  = "/worlds/{id}/rollback"
	PathWorldReleases = "/worlds/{id}/releases"
	PathVersions      = "/versions"
)

// Releases says which Factorio versions have images and which releases are
// newest (SPEC 10.2, 10.7), and how big a game image is (SPEC 3.7).
// *releases.Client implements it.
type Releases interface {
	Versions(context.Context) ([]string, error)
	Latest(context.Context) (releases.Latest, error)
	// ImageSize is the download's size in bytes; never negative.
	ImageSize(ctx context.Context, version string) (int64, error)
}

func updatePath(id string) string   { return gamePath(id, "update") }
func rollbackPath(id string) string { return gamePath(id, "rollback") }
func releasesPath(id string) string { return gamePath(id, "releases") }

// changelogURL is the wiki page for a release's major.minor line (SPEC 10.2).
func changelogURL(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return ""
	}
	return "https://wiki.factorio.com/Version_history/" + parts[0] + "." + parts[1] + ".0"
}

func (s *server) updateRoutes() {
	s.mux.HandleFunc("GET "+PathGameUpdate, s.requireUser(s.handleUpdateForm))
	s.mux.HandleFunc("POST "+PathGameUpdate, s.requireUser(s.handleUpdate))
	s.mux.HandleFunc("GET "+PathGameRollback, s.requireUser(s.handleRollbackForm))
	s.handleLong("POST "+PathGameRollback, s.requireUser(s.handleRollback))
	s.mux.HandleFunc("GET "+PathWorldReleases, s.requireUser(s.handleReleases))
	s.mux.HandleFunc("GET "+PathVersions, s.requireUser(s.handleVersions))
}

func newer(v, than string) bool {
	return v != "" && (than == "" || world.CompareGameVersions(v, than) > 0)
}

// handleVersions fills a game version input's suggestion list (SPEC 10.7).
func (s *server) handleVersions(w http.ResponseWriter, r *http.Request, _ store.User) {
	q := r.URL.Query()
	p := s.versionPicker(r.Context(), strings.TrimSpace(q.Get("above")))
	if sel := strings.TrimSpace(q.Get("selected")); slices.Contains(p.Versions, sel) {
		p.Selected = sel
	}
	s.fragment(w, r, p, "version-list")
}

// versionPicker lists the image versions newer than above. The list needs
// only Versions; Latest only labels it, so its failure leaves no labels.
func (s *server) versionPicker(ctx context.Context, above string) VersionPicker {
	p := VersionPicker{Above: above, Unreachable: true}
	if s.releases == nil {
		return p
	}
	versions, err := s.releases.Versions(ctx)
	if err != nil {
		return p
	}
	p.Unreachable = false
	if latest, err := s.releases.Latest(ctx); err == nil {
		p.Stable, p.Experimental = latest.Stable, latest.Experimental
	}
	for _, v := range versions {
		if newer(v, above) {
			p.Versions = append(p.Versions, v)
		}
	}
	return p
}

// handleReleases says whether a newer release than the world's is out
// (SPEC 10.2).
func (s *server) handleReleases(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	s.fragment(w, r, s.releaseNotice(r.Context(), sum), "release-notice")
}

// releaseNotice needs Latest, and Versions only when a newer release is
// out, to say whether it has an image yet.
func (s *server) releaseNotice(ctx context.Context, sum world.Summary) ReleaseNotice {
	p := ReleaseNotice{Current: sum.GameVersion}
	if s.releases == nil {
		p.Unreachable = true
		return p
	}
	latest, err := s.releases.Latest(ctx)
	if err != nil {
		p.Unreachable = true
		return p
	}
	if newer(latest.Stable, p.Current) {
		p.Stable = latest.Stable
	}
	if newer(latest.Experimental, p.Current) {
		p.Experimental = latest.Experimental
	}
	target := p.Stable
	if target == "" {
		target = p.Experimental
	}
	if target == "" {
		return p
	}
	versions, err := s.releases.Versions(ctx)
	if err != nil {
		return ReleaseNotice{Current: p.Current, Unreachable: true}
	}
	p.ChangelogURL = changelogURL(target)
	for _, v := range versions {
		if v == target {
			p.UpdatePath = updatePath(sum.ID) + "?" + url.Values{"game_version": {target}}.Encode()
			break
		}
	}
	return p
}

// updatePage builds the update confirmation. A refusal that the form
// cannot fix goes in Refusal; a bad version goes on its field.
func (s *server) updatePage(sum world.Summary, target string, form CountdownForm) UpdatePage {
	p := UpdatePage{World: sum, Target: target, CountdownForm: form}
	st := s.game.Status(sum.ID)
	p.Running = game.IsRunning(st.State)
	if st.Operation != nil && !st.Operation.Done {
		p.Refusal = i18n.M("worlds.error.busy")
	}
	return p
}

// updateTargetError words a refused target version for its field.
func updateTargetError(err error) i18n.Msg {
	switch {
	case errors.Is(err, world.ErrVersionNotNewer):
		return i18n.M("server.error.version_not_newer")
	case errors.Is(err, world.ErrVersionUnsupported):
		return i18n.M("server.error.version_unsupported")
	}
	return i18n.Msg{}
}

func (s *server) handleUpdateForm(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.game == nil {
		s.notFound(w, r, u)
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("game_version"))
	s.page(w, r, http.StatusOK, u.Username, s.updatePage(sum, target, s.countdownForm(r.Context())))
}

// handleUpdate moves a world to a newer game version in the background
// (SPEC 10.3). The Server page follows the active world's update; the
// world's own page follows any other world's, since the Server page shows
// only the active world. "anyway" comes from the refusal box (SPEC 7.4).
func (s *server) handleUpdate(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.game == nil {
		s.notFound(w, r, u)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	target := updateVersion(r.PostFormValue("game_version"), r.PostFormValue("game_version_typed"), r.PostFormValue("game_version_opened"))
	form, stop := s.readCountdown(r)
	p := s.updatePage(sum, target, form)
	if !p.Refusal.IsZero() {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if err := world.CheckUpdateTarget(world.Meta{GameVersion: sum.GameVersion}, target); err != nil {
		p.VersionError = updateTargetError(err)
	}
	if !p.Running {
		p.CountdownError = i18n.Msg{}
	}
	if p.hasErrors() {
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	id, by, opts := sum.ID, actor(u), anywayOptions(r)
	to := PathServer
	if !sum.Active {
		to = worldPath(id) + ownResultQuery
	}
	s.beginTo(w, r, to, []string{id}, true, func(ctx context.Context) actionResult {
		res := s.finish(id, game.AuditUpdate, s.game.Update(ctx, id, target, by, stop, opts))
		res.Version = target
		return res
	})
}

// updateOutcome words how the last update of a world that is not active
// ended, for the world's own page: what was done, or why not, with the
// offer to go ahead anyway when the pre-start check allows it (SPEC 10.3,
// 17.9). Both are empty when the world's last action was not an update.
func (s *server) updateOutcome(ctx context.Context, sum world.Summary) (i18n.Msg, *Failure) {
	if s.game == nil || sum.Active || sum.Unreadable || sum.Newer {
		return i18n.Msg{}, nil
	}
	res, ok := s.runs.result(sum.ID)
	if !ok || res.Action != game.AuditUpdate {
		return i18n.Msg{}, nil
	}
	if res.Err == nil {
		return i18n.M("server.done.update", "label", i18n.M("worlds.snapshot.label.before_update")), nil
	}
	f := &Failure{Refused: game.IsRefusal(res.Err)}
	pe, ok := errors.AsType[*game.PrecheckError](res.Err)
	if !ok {
		f.Message = failureText(res.Err)
		return i18n.Msg{}, f
	}
	f.Message = updatePrecheckLead(pe.Check, sum.Name, res.Version)
	if pe.Changed {
		f.Message = join(changedLead, f.Message)
	}
	f.Problems = problemMsgs(pe.Check.Problems)
	f.Fix = syncOffer(pe.Check, sum.ID)
	if pe.Check.CanStartAnyway() {
		f.Anyway = updateAnyway(pe.Check, sum, res.Version, s.stopSeconds(ctx))
	}
	return i18n.Msg{}, f
}

// rollbackPageFor builds the rollback confirmation for the world in the
// path, or answers the request itself and returns false.
func (s *server) rollbackPageFor(w http.ResponseWriter, r *http.Request, u store.User) (RollbackPage, bool) {
	if s.game == nil {
		s.notFound(w, r, u)
		return RollbackPage{}, false
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return RollbackPage{}, false
	}
	p, err := s.rollbackPage(r.Context(), sum)
	if err != nil {
		s.notFoundOrFail(w, r, u, "find the rollback snapshot", err)
		return RollbackPage{}, false
	}
	return p, true
}

// rollbackPage builds the rollback confirmation; Error says why there is
// none. It needs server control.
func (s *server) rollbackPage(ctx context.Context, sum world.Summary) (RollbackPage, error) {
	p := RollbackPage{World: sum, CountdownForm: s.countdownForm(ctx), Running: game.IsRunning(s.game.Status(sum.ID).State)}
	snap, version, err := s.worlds.RollbackTarget(sum.ID)
	switch {
	case errors.Is(err, world.ErrNoRollback):
		p.Error = i18n.M("server.error.no_rollback")
	case err != nil:
		return RollbackPage{}, err
	}
	p.Snapshot, p.Version = snap, version
	return p, nil
}

func (s *server) handleRollbackForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.rollbackPageFor(w, r, u)
	if !ok {
		return
	}
	status := http.StatusOK
	if !p.Error.IsZero() {
		status = http.StatusConflict
	}
	s.page(w, r, status, u.Username, p)
}

// handleRollback restores the "Before update" snapshot (SPEC 10.4). A
// running game is stopped, rolled back and started in the background, and
// the Server page follows it; a stopped world is rolled back before the
// reply, as a restore is (SPEC 6.5).
func (s *server) handleRollback(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	p, ok := s.rollbackPageFor(w, r, u)
	if !ok {
		return
	}
	if !p.Error.IsZero() {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	id, by := p.World.ID, actor(u)
	if p.Running {
		form, stop := s.readCountdown(r)
		if !form.CountdownError.IsZero() {
			p.CountdownForm = form
			s.page(w, r, http.StatusBadRequest, u.Username, p)
			return
		}
		snap := p.Snapshot.Name
		s.begin(w, r, id, true, func(ctx context.Context) actionResult {
			before, err := s.game.Rollback(ctx, id, by, stop)
			res := s.finish(id, game.AuditRollback, err)
			res.Restored, res.Undo = snap, before.Name
			return res
		})
		return
	}
	before, err := s.game.Rollback(r.Context(), id, by, game.StopOptions{Countdown: s.stopCountdown(r.Context())})
	if err == nil {
		http.Redirect(w, r, snapshotsPath(id)+"?undo="+before.Name, http.StatusSeeOther)
		return
	}
	if before.Name == "" {
		if status, msg, ok := s.gameRefusal(w, r, u, "roll back", err); ok {
			p.Error = msg
			s.page(w, r, status, u.Username, p)
		}
		return
	}
	// The undo snapshot exists, so the admin must hear of it (SPEC 6.5).
	status, msg := gameError(err)
	if status == http.StatusInternalServerError || status == http.StatusNotFound {
		s.log.Error("roll back", "world", id, "error", err)
		status, msg = http.StatusInternalServerError, i18n.M("server.rollback.unfinished")
	}
	p.Error = join(msg, i18n.M("server.rollback.undo_note", "label", snapshotLabelArg(before)))
	s.page(w, r, status, u.Username, p)
}
