package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
)

// brokenWriter is a connection whose peer has gone: every body write fails.
type brokenWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (b brokenWriter) Write([]byte) (int, error) { return 0, b.err }

// A browser that cancels a request (the preview page does, SPEC 5.6) is not
// a server fault: page logs no error and sends no 500 for it.
func TestPageIgnoresAbandonedRequests(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"broken pipe", context.Background(), fmt.Errorf("write tcp 1.2.3.4:80->5.6.7.8:9: %w", syscall.EPIPE)},
		{"connection reset", context.Background(), &net.OpError{Op: "write", Err: syscall.ECONNRESET}},
		{"closed connection", context.Background(), net.ErrClosed},
		{"cancelled request", cancelled, errors.New("write failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hn := newHarness(t, nil)
			s := &server{cfg: hn.cfg, log: hn.slog, render: mustRenderer(t)}
			r := httptest.NewRequest(http.MethodPost, "/generate/preview", nil).WithContext(tc.ctx)
			rec := httptest.NewRecorder()
			s.page(brokenWriter{rec, tc.err}, r, http.StatusOK, "", SignedOutPage{})
			for _, l := range logLines(t, hn) {
				if l["level"] == "ERROR" {
					t.Errorf("logged an error: %v", l)
				}
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status %d: a 500 was sent to a connection nobody reads", rec.Code)
			}
		})
	}
}

// A write that fails for any other reason is still a server fault.
func TestPageLogsRealWriteFailure(t *testing.T) {
	hn := newHarness(t, nil)
	s := &server{cfg: hn.cfg, log: hn.slog, render: mustRenderer(t)}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s.page(brokenWriter{httptest.NewRecorder(), errors.New("disk full")}, r, http.StatusOK, "", SignedOutPage{})
	lines := logLines(t, hn)
	if len(lines) != 1 || lines[0]["level"] != "ERROR" {
		t.Errorf("a real failure was not logged as an error: %v", lines)
	}
}
