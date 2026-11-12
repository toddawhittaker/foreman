package store

import (
	"context"
	"testing"
	"time"
)

func TestRestartSchedule(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, found, err := s.RestartSchedule(ctx); err != nil || found {
		t.Fatalf("empty store: found = %v, err = %v", found, err)
	}
	want := RestartSchedule{Enabled: true, Minute: 4 * 60, Zone: "Europe/London", LimitHours: 1, UpdatedBy: "todd"}
	if err := s.SetRestartSchedule(ctx, want, t0); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.RestartSchedule(ctx)
	want.UpdatedAt = t0
	if err != nil || !found || got != want {
		t.Fatalf("read back %+v, %v, %v; want %+v", got, found, err, want)
	}

	// Saving again replaces the one row.
	later := t0.Add(time.Hour)
	next := RestartSchedule{Enabled: false, Minute: 0, Zone: "UTC", LimitHours: 0, UpdatedBy: "ann"}
	if err := s.SetRestartSchedule(ctx, next, later); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.RestartSchedule(ctx)
	next.UpdatedAt = later
	if got != next {
		t.Errorf("after replace %+v, want %+v", got, next)
	}
}

func TestRestartScheduleRefusesOutOfRange(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, r := range []RestartSchedule{
		{Minute: 1440, Zone: "UTC", LimitHours: 1},
		{Minute: -1, Zone: "UTC", LimitHours: 1},
		{Minute: 0, Zone: "UTC", LimitHours: 13},
	} {
		if err := s.SetRestartSchedule(ctx, r, t0); err == nil {
			t.Errorf("saved %+v, want a refusal", r)
		}
	}
}
