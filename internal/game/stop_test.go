package game

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func indexOf(list []string, pred func(string) bool) int {
	return slices.IndexFunc(list, pred)
}

// SPEC 7.6: with players on, warn them with a countdown, save and wait for
// the save, then stop the container. The exit is never a crash.
func TestGracefulStopWarnsAndSaves(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Graceful")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	cmds := r.commands(id)
	warn := indexOf(cmds, func(c string) bool { return strings.HasPrefix(c, "The server will save and stop in") })
	save := indexOf(cmds, func(c string) bool { return c == "/server-save" })
	if warn < 0 || save < 0 || warn > save {
		t.Fatalf("commands %q: want a warning, then /server-save", cmds)
	}
	if cs := r.container(id); cs.Status != "exited" {
		t.Errorf("container %s", cs.Status)
	}
	if len(r.calls("POST /containers/")) == 0 || len(r.calls("POST /containers/"+r.container(id).ID+"/kill")) != 0 {
		t.Error("a graceful stop used kill")
	}
	st := r.waitState(id, StateStopped)
	if !st.LastCrash.IsZero() {
		t.Error("the stop was counted as a crash")
	}
	if st.Operation.Action != AuditStop || st.Operation.Error != "" {
		t.Errorf("operation %+v", st.Operation)
	}
	if a := r.actions(); !slices.Equal(a, []string{AuditStart, AuditStop}) {
		t.Errorf("audit %v", a)
	}
}

func TestStopWithNobodyOnSkipsWarning(t *testing.T) {
	r := newRig(t, rigOptions{countdwn: time.Hour})
	id := r.newWorld("Empty server")
	r.start(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.commands(id) {
		if strings.HasPrefix(c, "The server") {
			t.Errorf("warned an empty server: %q", c)
		}
	}
	if st := r.c.Status(id); st.Operation.Steps[0].State != StepSkipped {
		t.Errorf("warn step %+v", st.Operation.Steps[0])
	}
}

func TestWarningMarks(t *testing.T) {
	got := warningMarks(StopCountdown)
	want := []time.Duration{60 * time.Second, 30 * time.Second, 10 * time.Second}
	if !slices.Equal(got, want) {
		t.Errorf("marks %v, want %v", got, want)
	}
	if got := warningMarks(5 * time.Second); !slices.Equal(got, []time.Duration{5 * time.Second}) {
		t.Errorf("short countdown marks %v", got)
	}
	for d, want := range map[time.Duration]string{
		time.Minute: "1 minute", 2 * time.Minute: "2 minutes", 30 * time.Second: "30 seconds",
		time.Second: "1 second", 20 * time.Millisecond: "1 second", 90 * time.Second: "90 seconds",
	} {
		if got := durationWords(d); got != want {
			t.Errorf("durationWords(%s) = %q, want %q", d, got, want)
		}
	}
}

// SPEC 7.6: when RCON cannot be reached, Foreman says so and stops nothing.
func TestStopRefusedWithoutRCON(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Deaf")
	r.start(id)
	r.game.SetRCONRefuse(ContainerName(id), true)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); !errors.Is(err, ErrRCONUnreachable) {
		t.Fatalf("got %v, want ErrRCONUnreachable", err)
	}
	if r.container(id).Status != "running" {
		t.Error("the game was stopped without a save")
	}
	r.waitState(id, StateRunning)
}

// SPEC 7.6: a save that never finishes stops nothing.
func TestStopRefusedWhenSaveHangs(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Hang")
	r.start(id)
	r.c.saveTimeout = 100 * time.Millisecond
	r.game.SetSaveHangs(ContainerName(id), true)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); !errors.Is(err, ErrSaveTimeout) {
		t.Fatalf("got %v, want ErrSaveTimeout", err)
	}
	if r.container(id).Status != "running" {
		t.Error("the game was stopped without a finished save")
	}
}

func TestStopWhenStopped(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Idle")
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("got %v", err)
	}
	if err := r.c.ForceStop(context.Background(), id, admin); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("got %v", err)
	}
}

// SPEC 7.7, R28: a forced stop kills, does not save, and stays stopped.
func TestForceStop(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Force")
	r.start(id)
	cid := r.container(id).ID
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if len(r.calls("POST /containers/"+cid+"/kill")) != 1 {
		t.Error("forced stop did not kill")
	}
	if slices.Contains(r.commands(id), "/server-save") {
		t.Error("forced stop saved")
	}
	st := r.waitState(id, StateStopped)
	if !st.LastCrash.IsZero() {
		t.Error("the forced stop was counted as a crash")
	}
	time.Sleep(300 * time.Millisecond)
	if cs := r.container(id); cs.Status != "exited" || cs.RestartCount != 0 {
		t.Errorf("container %s, restarts %d: Docker restarted it", cs.Status, cs.RestartCount)
	}
	if a := r.actions(); !slices.Equal(a, []string{AuditStart, AuditForceStop}) {
		t.Errorf("audit %v", a)
	}
}

// A forced stop interrupts a graceful stop's countdown.
func TestForceStopInterruptsCountdown(t *testing.T) {
	r := newRig(t, rigOptions{countdwn: time.Hour})
	id := r.newWorld("Interrupt")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.c.Stop(context.Background(), id, admin, StopOptions{}) }()
	eventually(t, func() bool {
		op := r.c.Status(id).Operation
		return op != nil && op.Action == AuditStop && op.Steps[0].State == StepProgress
	}, "the countdown to begin")
	if st := r.c.Status(id); st.State != StateStopping {
		t.Errorf("state %s during the countdown", st.State)
	}
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrCancelled) {
		t.Errorf("stop returned %v, want ErrCancelled", err)
	}
	r.waitState(id, StateStopped)
	if op := r.c.Status(id).Operation; op.Steps[0].State != StepFailed || op.Error == "" {
		t.Errorf("operation %+v", op)
	}
}

// SPEC 7.8: restart is a graceful stop then a start, in a new container.
func TestRestart(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Again")
	r.start(id)
	first := r.container(id).ID
	if err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if r.container(id).ID == first {
		t.Error("restart reused the container")
	}
	if !slices.Contains(r.commands(id), "/server-save") {
		t.Error("restart did not save first")
	}
	st := r.c.Status(id)
	if st.State != StateRunning || st.Operation.Action != AuditRestart {
		t.Errorf("state %s, op %+v", st.State, st.Operation)
	}
	if a := r.actions(); !slices.Equal(a, []string{AuditStart, AuditRestart}) {
		t.Errorf("audit %v", a)
	}
}

// A restart whose start would be refused does not stop the game.
func TestRestartRefusedBeforeStopping(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Keep")
	r.start(id)
	if err := r.worlds.SetLoadSave(id, "base.zip"); err != nil {
		t.Fatal(err)
	}
	r.writeFile(id, world.SavesDir+"/base.zip", []byte("now broken"))
	var pe *PrecheckError
	if err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{}); !errors.As(err, &pe) {
		t.Fatalf("got %v, want a pre-start refusal", err)
	}
	if r.container(id).Status != "running" {
		t.Error("the game was stopped although the start would be refused")
	}
}

// SPEC 6.10, R9: a snapshot of a running world saves first.
func TestSnapshotOfRunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Snap")
	r.start(id)
	snap, err := r.c.Snapshot(context.Background(), id, admin, "Before the boss")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.commands(id), "/server-save") {
		t.Error("no /server-save before the snapshot")
	}
	if snap.Label != "Before the boss" || snap.TakenBy != admin.Username || snap.Reason != world.ReasonManual {
		t.Errorf("snapshot %+v", snap.Manifest)
	}
	if r.container(id).Status != "running" {
		t.Error("the snapshot stopped the game")
	}
}

func TestSnapshotRefusedWhenSaveHangs(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Snap hang")
	r.start(id)
	r.c.saveTimeout = 100 * time.Millisecond
	r.game.SetSaveHangs(ContainerName(id), true)
	if _, err := r.c.Snapshot(context.Background(), id, admin, "x"); !errors.Is(err, ErrSaveTimeout) {
		t.Fatalf("got %v, want ErrSaveTimeout", err)
	}
	if snaps, _ := r.worlds.Snapshots(id); len(snaps) != 0 {
		t.Errorf("a snapshot was taken without a finished save: %d", len(snaps))
	}
}

func TestSnapshotOfStoppedWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Snap stopped")
	if _, err := r.c.Snapshot(context.Background(), id, admin, "cold"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls("POST")) != 0 {
		t.Error("a stopped world's snapshot touched Docker")
	}
}

// SPEC 6.5, R9: restoring a running world stops it gracefully, restores
// (with the before-restore snapshot as the undo) and starts it again.
func TestRestoreRunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Restore")
	snap, err := r.c.Snapshot(context.Background(), id, admin, "good")
	if err != nil {
		t.Fatal(err)
	}
	r.start(id)
	first := r.container(id).ID
	before, err := r.c.Restore(context.Background(), id, admin, snap.Name, StopOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if before.Reason != world.ReasonBeforeRestore {
		t.Errorf("undo snapshot %+v", before.Manifest)
	}
	if !slices.Contains(r.commands(id), "/server-save") {
		t.Error("restore did not stop gracefully")
	}
	if r.container(id).ID == first {
		t.Error("the game was not started again in a fresh container")
	}
	st := r.c.Status(id)
	if st.State != StateRunning || st.Operation.Action != ActionRestore || st.Operation.Error != "" {
		t.Errorf("state %s, op %+v", st.State, st.Operation)
	}
}

func TestRestoreStoppedWorldStaysStopped(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Restore cold")
	snap, err := r.c.Snapshot(context.Background(), id, admin, "good")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.Restore(context.Background(), id, admin, snap.Name, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls("POST /containers/create")) != 0 {
		t.Error("restoring a stopped world started it")
	}
}

func TestRestoreMissingSnapshotKeepsGameRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Restore none")
	r.start(id)
	if _, err := r.c.Restore(context.Background(), id, admin, "20260101T000000Z", StopOptions{}); err == nil {
		t.Fatal("restored a snapshot that does not exist")
	}
	if r.container(id).Status != "running" {
		t.Error("the game was stopped for a restore that could not happen")
	}
}

// SPEC 7.6: the countdown is chosen per action; zero means the default.
func TestStopCountdownPerAction(t *testing.T) {
	r := newRig(t, rigOptions{countdwn: time.Hour})
	id := r.newWorld("Per action")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.commands(id), "The server will save and stop in 1 second.") {
		t.Errorf("the chosen countdown was not used: %q", r.commands(id))
	}
}

func TestStopWithoutCountdown(t *testing.T) {
	r := newRig(t, rigOptions{countdwn: time.Hour})
	id := r.newWorld("No countdown")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	if err := r.c.Restart(context.Background(), id, admin, StopOptions{NoCountdown: true}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	cmds := r.commands(id)
	if slices.ContainsFunc(cmds, func(c string) bool { return strings.HasPrefix(c, "The server") }) {
		t.Errorf("warned with no countdown: %q", cmds)
	}
	if !slices.Contains(cmds, "/server-save") {
		t.Error("did not save")
	}
	if st := r.c.Status(id); st.Operation.Steps[0].State != StepSkipped {
		t.Errorf("warn step %+v", st.Operation.Steps[0])
	}
}

func TestStopCountdownOutOfRange(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Range")
	r.start(id)
	for _, d := range []time.Duration{-time.Second, MaxStopCountdown + time.Second} {
		if err := r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: d}); !errors.Is(err, ErrCountdown) {
			t.Errorf("countdown %s: got %v", d, err)
		}
		if err := r.c.Restart(context.Background(), id, admin, StopOptions{Countdown: d}, StartOptions{}); !errors.Is(err, ErrCountdown) {
			t.Errorf("restart countdown %s: got %v", d, err)
		}
		if _, err := r.c.Restore(context.Background(), id, admin, "x", StopOptions{Countdown: d}); !errors.Is(err, ErrCountdown) {
			t.Errorf("restore countdown %s: got %v", d, err)
		}
	}
	if a := r.actions(); !slices.Equal(a, []string{AuditStart}) {
		t.Errorf("refused stops were acted on: %v", a)
	}
	if r.container(id).Status != "running" {
		t.Error("the game stopped")
	}
	// Nobody is on, so the longest countdown is accepted without a wait.
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown}); err != nil {
		t.Errorf("the longest countdown was refused: %v", err)
	}
}
