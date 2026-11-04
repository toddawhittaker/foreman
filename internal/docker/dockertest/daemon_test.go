package dockertest

import (
	"net/http"
	"strings"
	"testing"
)

// recorder keeps the test failures the fake reports, so a test can check them.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, strings.TrimSpace(format))
}

func TestFakeFailsTheTestOnCallsOutsideSpec14_3(t *testing.T) {
	cases := []struct {
		method, path string
		wantError    string
	}{
		{"POST", "/v1.52/containers/x/exec", "outside SPEC 14.3"},
		{"GET", "/v1.52/volumes", "outside SPEC 14.3"},
		{"POST", "/v1.52/networks/create", "outside SPEC 14.3"},
		{"DELETE", "/v1.52/images/x", "outside SPEC 14.3"},
		{"GET", "/containers/x/json", "no API version prefix"},
		{"GET", "/v1.52/events", "not implemented"},
	}
	for _, tc := range cases {
		rec := &recorder{TB: t}
		d := New(rec)
		req, _ := http.NewRequest(tc.method, d.srv.URL+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if len(rec.errs) == 0 || !strings.Contains(rec.errs[0], tc.wantError) {
			t.Errorf("%s %s: test failures = %v, want one with %q", tc.method, tc.path, rec.errs, tc.wantError)
		}
	}
}

func TestFakeAcceptsAllowedCalls(t *testing.T) {
	rec := &recorder{TB: t}
	d := New(rec)
	for _, p := range []string{"/_ping", "/version", "/v1.52/containers/json"} {
		resp, err := http.Get(d.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	if len(rec.errs) != 0 {
		t.Errorf("unexpected failures: %v", rec.errs)
	}
}
