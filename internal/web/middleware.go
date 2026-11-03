package web

import (
	"errors"
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
)

// statusWriter remembers the status so the access log can report it.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// accessLog writes one Info line per request (SPEC 15.1). It logs the path
// only, never the query string, headers, cookies or body (SPEC 14.8).
func (s *server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			s.log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"client", auth.ClientAddr(r, s.cfg.TrustedProxies))
		}()
		next.ServeHTTP(sw, r)
	})
}

// recoverPanic logs a panic and answers 500, so one bad request cannot
// take the server down.
func (s *server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(v)
			}
			s.log.Error("panic in handler", "method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
			if sw, ok := w.(*statusWriter); ok && sw.wrote {
				return
			}
			s.page(w, r, http.StatusInternalServerError, "", ErrorPage{Status: http.StatusInternalServerError})
		}()
		next.ServeHTTP(w, r)
	})
}

// idleLimit is how long a long request may go without reading or writing
// a byte before it ends (SPEC 14.6). Tests shorten it.
var idleLimit = 60 * time.Second

// limitBody caps every request body at maxFormBytes, except on an upload
// route, which applies its own limit once it knows who is asking (see
// handleLong).
func (s *server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := s.mux.Handler(r); s.uploadLimits[pattern] == 0 {
			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// handleLong registers a route that may run longer than the server's read
// and write timeouts: a download, import or restore. For a signed-in
// request it swaps the fixed deadlines for an idle one: each read or write
// gets idleLimit to make progress, so a slow transfer that keeps moving
// runs to the end and a stalled one ends. On an upload route a signed-in
// request gets the route's body limit; anyone else keeps the short
// deadlines and the 64 KB cap, and h refuses them. h checks the sign-in
// again (requireUser); the second check is cheap and keeps h's signature
// plain.
func (s *server) handleLong(pattern string, h http.HandlerFunc) {
	s.mux.HandleFunc(pattern, func(rw http.ResponseWriter, r *http.Request) {
		// rw is the server's own writer, which MaxBytesReader tells to
		// close the connection after a body that is too large.
		w := rw
		limit := int64(maxFormBytes)
		_, state, err := s.identify(r)
		if err != nil {
			s.fail(w, r, "identify user", err)
			return
		}
		if state == isSignedIn {
			rc := http.NewResponseController(w)
			// Lifted until the first read or write; a handler that is
			// working, not waiting on the client, is not idle.
			if err := rc.SetReadDeadline(time.Time{}); err != nil {
				s.fail(w, r, "lift read deadline", err)
				return
			}
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				s.fail(w, r, "lift write deadline", err)
				return
			}
			r.Body = idleBody{ReadCloser: r.Body, rc: rc}
			w = idleWriter{ResponseWriter: w, rc: rc}
			if l := s.uploadLimits[pattern]; l > 0 {
				limit = l
			}
		}
		if s.uploadLimits[pattern] > 0 {
			r.Body = http.MaxBytesReader(rw, r.Body, limit)
		}
		h(w, r)
	})
}

// handleUpload registers an upload route with its own body limit instead of
// maxFormBytes, for signed-in requests only.
func (s *server) handleUpload(pattern string, limit int64, h http.HandlerFunc) {
	s.uploadLimits[pattern] = limit
	s.handleLong(pattern, h)
}

// idleBody gives each read of a long request idleLimit to make progress.
// Once the body is read to the end the deadline is lifted again, because
// the server's own wait for the next request must not cut short a handler
// still at work.
type idleBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b idleBody) Read(p []byte) (int, error) {
	_ = b.rc.SetReadDeadline(time.Now().Add(idleLimit))
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		_ = b.rc.SetReadDeadline(time.Time{})
	}
	return n, err
}

// idleWriter gives each write of a long response idleLimit to make
// progress.
type idleWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func (w idleWriter) Write(p []byte) (int, error) {
	_ = w.rc.SetWriteDeadline(time.Now().Add(idleLimit))
	return w.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController reach the real writer.
func (w idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
