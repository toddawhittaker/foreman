package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// SPEC 4.8: one adoption is recorded at a time, and clearing it is safe
// to repeat.
func TestAdoptionRecord(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, found, err := s.Adoption(ctx); found || err != nil {
		t.Fatalf("fresh database: found %v, %v", found, err)
	}
	if err := s.SetAdoption(ctx, "gate", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAdoption(ctx, "other", t0); !errors.Is(err, ErrAdoptionExists) {
		t.Fatalf("second adoption: %v, want ErrAdoptionExists", err)
	}
	if rec, found, err := s.Adoption(ctx); rec.WorldID != "gate" || !rec.StartedAt.Equal(t0.Truncate(time.Second)) || !found || err != nil {
		t.Errorf("adoption = %+v %v %v, want gate at %v", rec, found, err, t0)
	}
	for range 2 {
		if err := s.ClearAdoption(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, _ := s.Adoption(ctx); found {
		t.Error("the record survived ClearAdoption")
	}
}
