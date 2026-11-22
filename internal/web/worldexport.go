package web

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/version"
	"github.com/toddawhittaker/foreman/internal/world"
)

// maxExports is how many world file downloads may run at once (SPEC 5.13).
// Each holds a staging folder of hard links, and a copy where a link fails.
const maxExports = 2

// ExportPath is where the export card posts (SPEC 5.13).
func (p WorldPage) ExportPath() string { return worldPath(p.World.ID) + "/export" }

// exportJob is a world staged for export: what the handler uses of
// *world.WorldExport, so its own rules are tested apart from the core.
type exportJob interface {
	Name() string
	WriteZip(w io.Writer, foremanVersion string) error
	Close() error
}

// worldExportRoutes registers the world file export (SPEC 5.13). It is a
// long route: the idle deadline lets a slow download keep running.
func (s *server) worldExportRoutes() {
	h := s.exportHandler(s.prepareExport, make(chan struct{}, maxExports))
	s.handleLong(patternWorldExport, s.requireUser(h))
}

// prepareExport stages the world with the world manager. It returns a nil
// job, not a nil pointer inside one, on an error.
func (s *server) prepareExport(ctx context.Context, id string, saves world.ExportSaves) (exportJob, error) {
	e, err := s.worlds.PrepareExport(ctx, id, saves)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// exportSaves reads the form's choice of saves: every save unless the form
// asks for the one that loads next (SPEC 5.13, plan ruling 25).
func exportSaves(v string) (world.ExportSaves, bool) {
	switch world.ExportSaves(v) {
	case "", world.ExportAllSaves:
		return world.ExportAllSaves, true
	case world.ExportLoadedSave:
		return world.ExportLoadedSave, true
	}
	return "", false
}

// auditExportDetail is the audit entry's words for the choice of saves.
func auditExportDetail(saves world.ExportSaves) string {
	if saves == world.ExportLoadedSave {
		return "loaded save"
	}
	return "all saves"
}

// exportHandler is the export post. Only slots (one place per running
// export) and prepare (the staging) come from outside, for tests.
//
// The order matters. The place is taken first, so a third export is
// refused before it stages anything. The audit entry is written once the
// files are staged, before the first byte goes out: the data is then
// leaving the server whether or not the browser keeps it.
func (s *server) exportHandler(prepare func(context.Context, string, world.ExportSaves) (exportJob, error), slots chan struct{}) func(http.ResponseWriter, *http.Request, store.User) {
	return func(w http.ResponseWriter, r *http.Request, u store.User) {
		if !s.readForm(w, r) {
			return
		}
		sum, ok := s.findWorld(w, r, u)
		if !ok {
			return
		}
		if why := cloneRefusal(sum); !why.IsZero() {
			s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict, Detail: why})
			return
		}
		saves, ok := exportSaves(r.PostFormValue("saves"))
		if !ok {
			s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest,
				Detail: i18n.M("worldexport.error.saves")})
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			s.page(w, r, http.StatusTooManyRequests, u.Username, ErrorPage{Status: http.StatusTooManyRequests,
				Detail: i18n.M("worldexport.busy")})
			return
		}
		job, err := prepare(r.Context(), sum.ID, saves)
		if err != nil {
			s.exportRefused(w, r, u, err)
			return
		}
		defer func() {
			if err := job.Close(); err != nil {
				s.log.Warn("remove the export staging folder", "world", sum.ID, "error", err)
			}
		}()
		s.audit(r.Context(), actor(u), AuditWorldExport, sum.ID, auditExportDetail(saves))
		s.streamExport(w, r, sum.ID, job)
	}
}

// exportRefused words a refusal of the export core.
func (s *server) exportRefused(w http.ResponseWriter, r *http.Request, u store.User, err error) {
	// A file the world file cannot carry, or too many files (SPEC 5.13,
	// 14.6). The words name the path and never the file's contents.
	if fe, ok := errors.AsType[*world.ExportFileError](err); ok {
		s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict,
			Detail: i18n.M("worldexport.error.file", "path", fe.Path)})
		return
	}
	if errors.Is(err, world.ErrExportTooManyFiles) {
		s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict,
			Detail: i18n.M("worldexport.error.too_many")})
		return
	}
	status, msg, ok := s.refusal(w, r, u, "export world", err)
	if !ok {
		return
	}
	s.page(w, r, status, u.Username, ErrorPage{Status: status, Detail: msg})
}

// writeCounter counts the bytes that reached the browser.
type writeCounter struct {
	io.Writer
	n int64
}

func (c *writeCounter) Write(p []byte) (int, error) {
	n, err := c.Writer.Write(p)
	c.n += int64(n)
	return n, err
}

// streamExport sends the staged files as a zip with no length: it is
// written as it is read, never held in memory (SPEC 5.13).
func (s *server) streamExport(w http.ResponseWriter, r *http.Request, id string, job exportJob) {
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", attachment(downloadStem(job.Name(), "world", "")+".zip"))
	out := &writeCounter{Writer: w}
	err := job.WriteZip(out, version.Get().Version)
	if err == nil {
		return
	}
	if !clientGone(r, err) {
		s.log.Warn("stream the world file", "world", id, "error", err)
	}
	if out.n == 0 {
		// Nothing has gone out, so the browser can still be told.
		for _, k := range []string{"Content-Type", "Cache-Control", "X-Content-Type-Options", "Content-Disposition"} {
			h.Del(k)
		}
		if !clientGone(r, err) {
			s.fail(w, r, "export world", err)
		}
		return
	}
	// The reply has started, so end the connection: a download that stops
	// early must not look like a whole file.
	panic(http.ErrAbortHandler)
}
