package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActiveWorld(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, found, err := s.ActiveWorld(ctx); err != nil || found {
		t.Fatalf("fresh database: found=%v err=%v", found, err)
	}
	if err := s.SetActiveWorld(ctx, "alpha", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveWorld(ctx, "beta", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// One world at a time in v1 (SPEC 5.3): setting replaces.
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM active_worlds").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d err=%v, want 1", n, err)
	}
	if id, found, err := s.ActiveWorld(ctx); err != nil || !found || id != "beta" {
		t.Fatalf("active = %q found=%v err=%v, want beta", id, found, err)
	}
}

func activeIDs(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query("SELECT world_id FROM active_worlds ORDER BY world_id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestReplaceActiveWorld(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetActiveWorld(ctx, "alpha", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceActiveWorld(ctx, "alpha", "beta", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := activeIDs(t, s); len(got) != 1 || got[0] != "beta" {
		t.Errorf("active = %v, want [beta]", got)
	}
}

func TestReplaceActiveWorldLeavesOtherRowsAlone(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, id := range []string{"alpha", "other"} {
		if _, err := s.db.Exec("INSERT INTO active_worlds (world_id, since) VALUES (?, 0)", id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReplaceActiveWorld(ctx, "alpha", "beta", t0); err != nil {
		t.Fatal(err)
	}
	if got := activeIDs(t, s); len(got) != 2 || got[0] != "beta" || got[1] != "other" {
		t.Errorf("active = %v, want [beta other]", got)
	}
}

func TestReplaceActiveWorldRefusesWhenFromNotActive(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetActiveWorld(ctx, "alpha", t0); err != nil {
		t.Fatal(err)
	}
	err := s.ReplaceActiveWorld(ctx, "gamma", "beta", t0)
	if !errors.Is(err, ErrNotActive) {
		t.Fatalf("err = %v, want ErrNotActive", err)
	}
	if got := activeIDs(t, s); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("active = %v, want [alpha] unchanged", got)
	}
}

func TestReplaceActiveWorldForSwitchChangesBothTogether(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetActiveWorld(ctx, "alpha", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginSwitch(ctx, "alpha", "beta", "todd", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceActiveWorldForSwitch(ctx, "alpha", "beta", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := activeIDs(t, s); len(got) != 1 || got[0] != "beta" {
		t.Errorf("active = %v, want [beta]", got)
	}
	if got, _ := s.ListSwitches(ctx); len(got) != 1 || got[0].Step != StepPresented {
		t.Errorf("switch = %v, want step presented", got)
	}
}

func TestReplaceActiveWorldForSwitchFailureChangesNeither(t *testing.T) {
	ctx := context.Background()
	t.Run("no switch record", func(t *testing.T) {
		s, _ := openTemp(t)
		if err := s.SetActiveWorld(ctx, "alpha", t0); err != nil {
			t.Fatal(err)
		}
		err := s.ReplaceActiveWorldForSwitch(ctx, "alpha", "beta", t0)
		if !errors.Is(err, ErrNoSwitch) {
			t.Fatalf("err = %v, want ErrNoSwitch", err)
		}
		if got := activeIDs(t, s); len(got) != 1 || got[0] != "alpha" {
			t.Errorf("active = %v, want [alpha] unchanged", got)
		}
	})
	t.Run("from not active", func(t *testing.T) {
		s, _ := openTemp(t)
		if err := s.SetActiveWorld(ctx, "gamma", t0); err != nil {
			t.Fatal(err)
		}
		if err := s.BeginSwitch(ctx, "alpha", "beta", "todd", t0); err != nil {
			t.Fatal(err)
		}
		err := s.ReplaceActiveWorldForSwitch(ctx, "alpha", "beta", t0)
		if !errors.Is(err, ErrNotActive) {
			t.Fatalf("err = %v, want ErrNotActive", err)
		}
		if got, _ := s.ListSwitches(ctx); got[0].Step != StepBegun {
			t.Errorf("step = %s, want begun unchanged", got[0].Step)
		}
	})
	t.Run("insert fails after the delete", func(t *testing.T) {
		s, _ := openTemp(t)
		for _, id := range []string{"alpha", "beta"} {
			if _, err := s.db.Exec("INSERT INTO active_worlds (world_id, since) VALUES (?, 0)", id); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.BeginSwitch(ctx, "alpha", "beta", "todd", t0); err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceActiveWorldForSwitch(ctx, "alpha", "beta", t0); err == nil {
			t.Fatal("duplicate active row accepted")
		}
		if got := activeIDs(t, s); len(got) != 2 {
			t.Errorf("active = %v, want [alpha beta] unchanged", got)
		}
		if got, _ := s.ListSwitches(ctx); got[0].Step != StepBegun {
			t.Errorf("step = %s, want begun unchanged", got[0].Step)
		}
	})
}
