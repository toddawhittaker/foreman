package schedule

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// A restart cut short because Foreman is stopping is not a fault, so it
// logs no ERROR line; one that fails with a live context still does.
func TestRestartEndedLogsOnlyRealFailures(t *testing.T) {
	failed := errors.New("docker said no")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []struct {
		name      string
		ctx       context.Context
		wantError bool
	}{
		{"foreman stopping", cancelled, false},
		{"a real failure", context.Background(), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			x := newRig(t, at(3, 0), nightly(1))
			var logs strings.Builder
			x.s.cfg.Log = slog.New(slog.NewTextHandler(&logs, nil))
			x.s.mu.Lock()
			x.s.st.Phase = PhaseCountdown
			x.s.mu.Unlock()

			x.s.restartEnded(c.ctx, failed)

			if got := strings.Contains(logs.String(), "level=ERROR"); got != c.wantError {
				t.Errorf("logged an error = %v, want %v: %s", got, c.wantError, logs.String())
			}
			if st := x.s.State(); st.Phase != PhaseNone {
				t.Errorf("phase = %q, want the day settled either way", st.Phase)
			}
		})
	}
}
