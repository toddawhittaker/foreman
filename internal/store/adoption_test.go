package store

import (
	"context"
	"errors"
	"testing"
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
	if id, found, err := s.Adoption(ctx); id != "gate" || !found || err != nil {
		t.Errorf("adoption = %q %v %v, want gate", id, found, err)
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
