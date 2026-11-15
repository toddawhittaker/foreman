package modportal

import (
	"context"
	"errors"
	"testing"
)

func TestLookup(t *testing.T) {
	p, c, _ := fakePortal(t)
	ctx := context.Background()
	p.AddZipFor("2.1", "only-new", "1.0.0")

	// In the list: found by exact name in any case, with no /full call.
	h, err := c.Lookup(ctx, "2.0", "krastorio2")
	if err != nil || h.Name != "Krastorio2" || h.LatestVersion == "" {
		t.Fatalf("list hit: %+v, %v", h, err)
	}
	// A longer name that only starts the same is not the mod.
	if _, err := c.Lookup(ctx, "2.0", "krastorio"); !errors.Is(err, ErrNotOnPortal) {
		t.Errorf("prefix of a name: got %v, want ErrNotOnPortal", err)
	}
	if p.FullCalls("Krastorio2") != 0 {
		t.Error("a mod in the list needed a /full call")
	}
	// Not in the list but on the portal with a release for the line.
	h, err = c.Lookup(ctx, "2.0", "chest-auto-sort")
	if err != nil || h.Name != "chest-auto-sort" || h.FactorioVersion != "2.0" || h.LatestVersion == "" {
		t.Errorf("full hit: %+v, %v", h, err)
	}
	// On the portal, but with no release for the line.
	if _, err := c.Lookup(ctx, "2.0", "only-new"); !errors.Is(err, ErrNoRelease) {
		t.Errorf("no release for the line: got %v, want ErrNoRelease", err)
	}
	// Not on the portal at all.
	if _, err := c.Lookup(ctx, "2.0", "no-such-mod"); !errors.Is(err, ErrNotOnPortal) {
		t.Errorf("unknown: got %v, want ErrNotOnPortal", err)
	}
}
