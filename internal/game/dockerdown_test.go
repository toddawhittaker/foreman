package game

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// SPEC 7.1: with Docker unreachable the state is unknown and the reason is a
// plain sentence that names DOCKER_HOST. The raw error, with the request's
// URL and filter, goes to the log, once while the fault lasts.
func TestDockerUnreachableReasonIsPlain(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	sock := filepath.Join(t.TempDir(), "no.sock")
	dc, err := docker.New("unix://" + sock)
	if err != nil {
		t.Fatal(err)
	}
	r.docker = dc
	c := r.newController(rigOptions{})
	var logs bytes.Buffer
	c.log = slog.New(slog.NewTextHandler(&logs, nil))
	stop := r.run(c)

	var st Status
	eventually(t, func() bool {
		st = c.Status("any")
		return strings.Contains(st.Reason, "cannot reach Docker")
	}, "the unreachable state")
	// Several polls pass, so a log line per poll would show.
	time.Sleep(10 * c.pollInterval)
	stop()

	if !strings.Contains(st.Reason, "unix://"+sock) || !strings.Contains(st.Reason, "DOCKER_HOST") {
		t.Errorf("reason %q does not name DOCKER_HOST and where it points", st.Reason)
	}
	for _, raw := range []string{"filters", "%7B", "dial", "http://", "/containers"} {
		if strings.Contains(st.Reason, raw) {
			t.Errorf("reason %q holds raw error detail %q", st.Reason, raw)
		}
	}
	if n := strings.Count(logs.String(), "cannot reach Docker"); n != 1 {
		t.Errorf("%d log lines for one unbroken fault, want 1:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "dial") {
		t.Errorf("the raw error is not in the log:\n%s", logs.String())
	}
}
