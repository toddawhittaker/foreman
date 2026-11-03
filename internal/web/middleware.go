package web

import (
	"context"
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

// uploadBody is an upload route's real request body and its limit, kept
// beside the 64 KB cap every request starts with. Only a signed-in request
// gets the larger limit.
type uploadBody struct {
	raw   io.ReadCloser
	limit int64
}

type uploadBodyKey struct{}

// limitBody caps every request body at maxFormBytes. An upload route's own
// limit is applied later, by handleLong, once the request is signed in
// (SPEC 14.6).
func (s *server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Body
		r.Body = http.MaxBytesReader(w, raw, maxFormBytes)
		if _, pattern := s.mux.Handler(r); s.uploadLimits[pattern] > 0 {
			ctx := context.WithValue(r.Context(), uploadBodyKey{}, uploadBody{raw: raw, limit: s.uploadLimits[pattern]})
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// handleLong registers a route that may run longer than the server's read
// and write timeouts: a download, import or restore. For a signed-in
// request it lifts both deadlines and, on an upload route, swaps in the
// route's body limit. Anyone else keeps the short deadlines and the 64 KB
// cap, and h refuses them. h checks the sign-in again (requireUser); the
// second check is cheap and keeps h's signature plain.
func (s *server) handleLong(pattern string, h http.HandlerFunc) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		_, state, err := s.identify(r)
		if err != nil {
			s.fail(w, r, "identify user", err)
			return
		}
		if state == isSignedIn {
			rc := http.NewResponseController(w)
			if err := rc.SetReadDeadline(time.Time{}); err != nil {
				s.fail(w, r, "lift read deadline", err)
				return
			}
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				s.fail(w, r, "lift write deadline", err)
				return
			}
			if ub, ok := r.Context().Value(uploadBodyKey{}).(uploadBody); ok {
				r.Body = http.MaxBytesReader(w, ub.raw, ub.limit)
			}
		}
		h(w, r)
	})
}

// handleUpload registers an upload route with its own body limit instead of
// maxFormBytes, and without the read and write deadlines, for signed-in
// requests only.
func (s *server) handleUpload(pattern string, limit int64, h http.HandlerFunc) {
	s.uploadLimits[pattern] = limit
	s.handleLong(pattern, h)
}
