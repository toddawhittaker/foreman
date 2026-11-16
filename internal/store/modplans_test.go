package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

func planNames(t *testing.T, s *Store, world string) []string {
	t.Helper()
	plan, err := s.ModPlan(context.Background(), world)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range plan {
		names = append(names, m.Name)
	}
	return names
}

func TestModPlanListsOldestFirst(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if plan, err := s.ModPlan(ctx, "w1"); err != nil || len(plan) != 0 {
		t.Fatalf("empty plan = %v, %v", plan, err)
	}
	// Added out of alphabetical order, two in each second.
	for i, name := range []string{"zeta", "alpha", "mid", "beta"} {
		at := t0.Add(time.Duration(i/2) * time.Second)
		if err := s.AddModPlanName(ctx, "w1", name, "todd", at); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := s.ModPlan(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if got := planNames(t, s, "w1"); !slices.Equal(got, []string{"zeta", "alpha", "mid", "beta"}) {
		t.Errorf("order = %v", got)
	}
	if !plan[0].AddedAt.Equal(t0) || plan[0].AddedBy != "todd" || !plan[2].AddedAt.Equal(t0.Add(time.Second)) {
		t.Errorf("added fields = %+v", plan)
	}
}

func TestModPlanAddTwiceKeepsFirst(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.AddModPlanName(ctx, "w1", "a", "ann", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.AddModPlanName(ctx, "w1", "a", "bob", t0.Add(time.Hour)); err != nil {
		t.Fatalf("second add: %v", err)
	}
	plan, _ := s.ModPlan(ctx, "w1")
	if len(plan) != 1 || plan[0].AddedBy != "ann" || !plan[0].AddedAt.Equal(t0) {
		t.Errorf("plan = %+v, want the first add kept", plan)
	}
}

func TestModPlanRemoveAndClear(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, n := range []string{"a", "b", "c"} {
		if err := s.AddModPlanName(ctx, "w1", n, "u", t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddModPlanName(ctx, "w2", "a", "u", t0); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveModPlanName(ctx, "w1", "b"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveModPlanName(ctx, "w1", "nope"); err != nil {
		t.Errorf("removing an absent name: %v", err)
	}
	if got := planNames(t, s, "w1"); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("after remove = %v", got)
	}
	if err := s.ClearModPlan(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	if got := planNames(t, s, "w1"); len(got) != 0 {
		t.Errorf("after clear = %v", got)
	}
	if got := planNames(t, s, "w2"); !slices.Equal(got, []string{"a"}) {
		t.Errorf("clearing w1 touched w2: %v", got)
	}
	if err := s.ClearModPlan(ctx, "w1"); err != nil {
		t.Errorf("clearing an empty plan: %v", err)
	}
}

func TestModPlanLimit(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if MaxModPlanNames != 500 {
		t.Fatalf("limit = %d, want 500", MaxModPlanNames)
	}
	for i := range MaxModPlanNames {
		if err := s.AddModPlanName(ctx, "w1", fmt.Sprintf("mod-%02d", i), "u", t0); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if err := s.AddModPlanName(ctx, "w1", "one-more", "u", t0); !errors.Is(err, ErrModPlanFull) {
		t.Errorf("51st add: %v, want ErrModPlanFull", err)
	}
	// A name already in a full plan is not an error.
	if err := s.AddModPlanName(ctx, "w1", "mod-07", "u", t0); err != nil {
		t.Errorf("re-adding in a full plan: %v", err)
	}
	// The limit is per world.
	if err := s.AddModPlanName(ctx, "w2", "one-more", "u", t0); err != nil {
		t.Errorf("other world: %v", err)
	}
	// Room again after a removal.
	if err := s.RemoveModPlanName(ctx, "w1", "mod-00"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddModPlanName(ctx, "w1", "one-more", "u", t0); err != nil {
		t.Errorf("add after remove: %v", err)
	}
	if got := len(planNames(t, s, "w1")); got != MaxModPlanNames {
		t.Errorf("plan size = %d", got)
	}
}

func TestModPlanWorldsKeptApart(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, add := range []struct{ world, name string }{{"w1", "a"}, {"w2", "a"}, {"w2", "b"}} {
		if err := s.AddModPlanName(ctx, add.world, add.name, "u", t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RemoveModPlanName(ctx, "w1", "a"); err != nil {
		t.Fatal(err)
	}
	if got := planNames(t, s, "w1"); len(got) != 0 {
		t.Errorf("w1 = %v", got)
	}
	if got := planNames(t, s, "w2"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("w2 = %v", got)
	}
}

func TestModPlanConcurrentAddsAreAllKept(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	const n = 30
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.AddModPlanName(ctx, "w1", fmt.Sprintf("mod-%02d", i), "u", t0)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent add: %v", err)
		}
	}
	if got := len(planNames(t, s, "w1")); got != n {
		t.Errorf("kept %d of %d names", got, n)
	}
}

func TestModPlanConcurrentAddsStopAtLimit(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	const n = MaxModPlanNames + 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.AddModPlanName(ctx, "w1", fmt.Sprintf("mod-%02d", i), "u", t0); err != nil && !errors.Is(err, ErrModPlanFull) {
				t.Errorf("concurrent add: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := len(planNames(t, s, "w1")); got != MaxModPlanNames {
		t.Errorf("plan size = %d, want %d", got, MaxModPlanNames)
	}
}

// Adding a name while another caller removes it must never look like a full
// plan: the plan holds 10 names at most, far from the limit. It is a stress
// test, as the race cannot be forced without a hook in the code.
func TestModPlanAddRacingRemoveIsNeverFull(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for i := range 9 {
		if err := s.AddModPlanName(ctx, "w1", fmt.Sprintf("mod-%02d", i), "u", t0); err != nil {
			t.Fatal(err)
		}
	}
	const rounds = 5000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range rounds {
			if err := s.AddModPlanName(ctx, "w1", "racer", "u", t0); err != nil {
				t.Errorf("add: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range rounds {
			if err := s.RemoveModPlanName(ctx, "w1", "racer"); err != nil {
				t.Errorf("remove: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}
