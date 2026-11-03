package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, HealthPath, http.StatusOK},
		{http.MethodPost, HealthPath, http.StatusMethodNotAllowed},
		{http.MethodGet, "/nope", http.StatusNotFound},
	}
	h := NewHandler()
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s: got %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}
