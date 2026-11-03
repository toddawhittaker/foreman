package store

import (
	"context"
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
	if err := s.ClearActiveWorld(ctx); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.ActiveWorld(ctx); err != nil || found {
		t.Fatalf("after clear: found=%v err=%v", found, err)
	}
	if err := s.ClearActiveWorld(ctx); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
}
