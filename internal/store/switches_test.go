package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSwitchLifecycle(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if got, err := s.ListSwitches(ctx); err != nil || len(got) != 0 {
		t.Fatalf("fresh database: %v err=%v", got, err)
	}
	if err := s.BeginSwitch(ctx, "alpha", "beta", "todd", t0); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListSwitches(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("list = %v err=%v", got, err)
	}
	want := Switch{From: "alpha", To: "beta", Step: StepBegun, StartedAt: t0, ByUser: "todd"}
	if got[0] != want {
		t.Errorf("switch = %+v, want %+v", got[0], want)
	}
	for _, step := range []string{StepStopped, StepSnapshotted, StepChecked, StepPresented, StepStarted} {
		if err := s.SetSwitchStep(ctx, "alpha", step); err != nil {
			t.Fatalf("step %s: %v", step, err)
		}
		if got, _ := s.ListSwitches(ctx); len(got) != 1 || got[0].Step != step {
			t.Fatalf("after %s: %v", step, got)
		}
	}
	if err := s.EndSwitch(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSwitches(ctx); len(got) != 0 {
		t.Errorf("record survived EndSwitch: %v", got)
	}
}

func TestBeginSwitchRefusesSecondFromSameWorld(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.BeginSwitch(ctx, "alpha", "beta", "todd", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginSwitch(ctx, "alpha", "gamma", "ann", t0); err == nil {
		t.Fatal("second switch from the same world was accepted")
	}
	if got, _ := s.ListSwitches(ctx); len(got) != 1 || got[0].To != "beta" {
		t.Errorf("first record changed: %v", got)
	}
	// A different world has its own slot.
	if err := s.BeginSwitch(ctx, "delta", "gamma", "ann", t0.Add(time.Second)); err != nil {
		t.Errorf("switch from another world: %v", err)
	}
}

func TestSetSwitchStepMissingAndUnknown(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetSwitchStep(ctx, "alpha", StepStopped); !errors.Is(err, ErrNoSwitch) {
		t.Errorf("missing record: err = %v, want ErrNoSwitch", err)
	}
	if err := s.BeginSwitch(ctx, "alpha", "beta", "todd", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSwitchStep(ctx, "alpha", "halfway"); err == nil {
		t.Error("unknown step accepted")
	}
	if got, _ := s.ListSwitches(ctx); got[0].Step != StepBegun {
		t.Errorf("step = %s after refused update, want begun", got[0].Step)
	}
}

func TestSwitchTableRejectsUnknownStep(t *testing.T) {
	s, _ := openTemp(t)
	_, err := s.db.Exec(
		"INSERT INTO world_switches (from_world, to_world, step, started_at, by_user) VALUES ('a','b','nope',0,'x')")
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("err = %v, want a CHECK failure", err)
	}
}

func TestEndSwitchMissingIsNotAnError(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.EndSwitch(context.Background(), "nothing"); err != nil {
		t.Errorf("EndSwitch of a missing record: %v", err)
	}
}

func TestListSwitchesOldestFirst(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.BeginSwitch(ctx, "b", "x", "u", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginSwitch(ctx, "a", "y", "u", t0); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListSwitches(ctx)
	if err != nil || len(got) != 2 || got[0].From != "a" || got[1].From != "b" {
		t.Errorf("list = %v err=%v, want a then b", got, err)
	}
}
