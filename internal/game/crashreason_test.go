package game

import (
	"context"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

func TestLogBufferLinesSince(t *testing.T) {
	b := newLogBuffer(3)
	b.Add("old")
	mark := b.Mark()
	if got := b.LinesSince(mark); len(got) != 0 {
		t.Errorf("got %q right after the mark", got)
	}
	b.Add("a")
	b.Add("b")
	if got := b.LinesSince(mark); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("got %q", got)
	}
	b.Add("c")
	b.Add("d") // the ring drops "a"
	if got := b.LinesSince(mark); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Errorf("got %q after wrapping", got)
	}
}

// SPEC 8.7: a crash reason comes only from the current container's log,
// never from an earlier run's lines still in the buffer.
func TestCrashReasonIgnoresOlderRuns(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Old lines")
	r.start(id)
	for _, l := range []string{"   1.000 Error Util.cpp:81: Failed to load mods", "   1.001 • old-mod",
		"   1.002 Incompatible Factorio version (current: 2.1.24, required: 2.0)"} {
		r.game.Daemon().EmitLog(r.container(id).ID, docker.Stdout, l)
	}
	eventually(t, func() bool {
		return len(r.c.Lines(id)) > 0 && r.c.Lines(id)[len(r.c.Lines(id))-1] == "   1.002 Incompatible Factorio version (current: 2.1.24, required: 2.0)"
	},
		"the old lines to be kept")
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	r.start(id)
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	if st := r.waitState(id, StateCrashed); st.CrashReason != "" {
		t.Errorf("crash reason %q came from an older run; want none (could not tell why)", st.CrashReason)
	}
}
