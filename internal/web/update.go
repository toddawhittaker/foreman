package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
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
// newest (SPEC 10.2, 10.7). *releases.Client implements it.
type Releases interface {
	Versions(context.Context) ([]string, error)
	Latest(context.Context) (releases.Latest, error)
}

const notActiveText = "Switch to this world to update it."

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

// fragment answers with one named template of a page, for htmx.
func (s *server) fragment(w http.ResponseWriter, r *http.Request, p Page, name string) {
	var b strings.Builder
	if err := s.render.Fragment(&b, p, name); err != nil {
		s.fail(w, r, "render "+name, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

func newer(v, than string) bool {
	return v != "" && (than == "" || world.CompareGameVersions(v, than) > 0)
}

// handleVersions fills a game version input's suggestion list (SPEC 10.7).
func (s *server) handleVersions(w http.ResponseWriter, r *http.Request, _ store.User) {
	above := strings.TrimSpace(r.URL.Query().Get("above"))
	s.fragment(w, r, s.versionPicker(r.Context(), above), "version-list")
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
	switch {
	case !sum.Active:
		p.Refusal = notActiveText
	case st.Operation != nil && !st.Operation.Done:
		p.Refusal = "Another operation is running on this world. Wait for it to finish, then try again."
	}
	return p
}

// updateTargetError words a refused target version for its field.
func updateTargetError(err error) string {
	switch {
	case errors.Is(err, world.ErrVersionNotNewer), errors.Is(err, world.ErrVersionUnsupported):
		return sentence(err)
	}
	return ""
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
	s.page(w, r, http.StatusOK, u.Username, s.updatePage(sum, target, defaultCountdown()))
}

// handleUpdate moves the active world to a newer game version in the
// background; the Server page follows it (SPEC 10.3). "anyway" comes from
// the refusal box (SPEC 7.4).
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
	target := strings.TrimSpace(r.PostFormValue("game_version"))
	form, stop := readCountdown(r)
	p := s.updatePage(sum, target, form)
	if p.Refusal != "" {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if err := world.CheckUpdateTarget(world.Meta{GameVersion: sum.GameVersion}, target); err != nil {
		p.VersionError = updateTargetError(err)
	}
	if !p.Running {
		p.CountdownError = ""
	}
	if p.hasErrors() {
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	id, by, opts := sum.ID, actor(u), anywayOptions(r)
	s.begin(w, r, id, true, func(ctx context.Context) actionResult {
		res := s.finish(id, game.AuditUpdate, s.game.Update(ctx, id, target, by, stop, opts))
		res.Version = target
		return res
	})
}

// rollbackPage builds the rollback confirmation, or says why there is none.
func (s *server) rollbackPage(w http.ResponseWriter, r *http.Request, u store.User) (RollbackPage, bool) {
	if s.game == nil {
		s.notFound(w, r, u)
		return RollbackPage{}, false
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return RollbackPage{}, false
	}
	p := RollbackPage{World: sum, CountdownForm: defaultCountdown(), Running: game.IsRunning(s.game.Status(sum.ID).State)}
	snap, version, err := s.worlds.RollbackTarget(sum.ID)
	switch {
	case errors.Is(err, world.ErrNoRollback):
		p.Error = sentence(err)
	case err != nil:
		s.notFoundOrFail(w, r, u, "find the rollback snapshot", err)
		return RollbackPage{}, false
	}
	p.Snapshot, p.Version = snap, version
	return p, true
}

func (s *server) handleRollbackForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.rollbackPage(w, r, u)
	if !ok {
		return
	}
	status := http.StatusOK
	if p.Error != "" {
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
	p, ok := s.rollbackPage(w, r, u)
	if !ok {
		return
	}
	if p.Error != "" {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	id, by := p.World.ID, actor(u)
	if p.Running {
		form, stop := readCountdown(r)
		if form.CountdownError != "" {
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
	before, err := s.game.Rollback(r.Context(), id, by, game.StopOptions{Countdown: game.StopCountdown})
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
		status, msg = http.StatusInternalServerError, "Foreman could not finish the rollback."
	}
	p.Error = msg + " Foreman took the snapshot \"" + before.Label + "\" first; to undo, restore it from the Snapshots page."
	s.page(w, r, status, u.Username, p)
}
