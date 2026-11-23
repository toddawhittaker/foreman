package game

import (
	"context"
	"errors"
	"testing"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// SPEC 5.6: a run of the base game keeps the 1 GiB limit; only a run that
// loads a world's mods gets the larger one (previewworldmods_test.go).
func TestPreviewRunsWithoutAWorldKeepTheBaseMemory(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	runs := map[string]func(ctx context.Context) error{
		"preview": func(ctx context.Context) error {
			_, err := r.c.Preview(ctx, previewRequest())
			return err
		},
		"territories": func(ctx context.Context) error {
			_, err := r.c.Territories(ctx, territoriesRequest())
			return err
		},
		"resources": func(ctx context.Context) error {
			_, err := r.c.Resources(ctx, resourcesRequest(), "nauvis")
			return err
		},
	}
	for kind, run := range runs {
		t.Run(kind, func(t *testing.T) {
			h := capturePreview(t, r, kind, run).Spec.HostConfig
			if h.Memory != previewMemory || h.MemorySwap != previewMemory {
				t.Errorf("memory %d, swap %d, want %d", h.Memory, h.MemorySwap, int64(previewMemory))
			}
		})
	}
	if previewMemory != 1<<30 || previewWorldMemory != 2<<30 {
		t.Errorf("limits %d and %d, want 1 GiB for the base game and 2 GiB with mods", int64(previewMemory), int64(previewWorldMemory))
	}
	if r.c.previewWorldLimit != PreviewWorldLimit || r.c.previewWorldMemory != previewWorldMemory {
		t.Errorf("controller limits %v and %d", r.c.previewWorldLimit, r.c.previewWorldMemory)
	}
}

// SPEC 5.6: a run that runs out of memory names itself, so a failed
// resource count is never reported as the map preview.
func TestPreviewOutOfMemoryNamesTheRun(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewOOM); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind string
		run  func() error
		want string
	}{
		{"preview", func() error { _, err := r.c.Preview(context.Background(), previewRequest()); return err }, "the map preview ran out of memory"},
		{gameenv.KindResources, func() error {
			_, err := r.c.Resources(context.Background(), resourcesRequest(), "nauvis")
			return err
		}, "the resource count ran out of memory"},
		{gameenv.KindTerritories, func() error { _, err := r.c.Territories(context.Background(), territoriesRequest()); return err }, "the territory run ran out of memory"},
	} {
		err := tc.run()
		pe, ok := errors.AsType[*PreviewError](err)
		if !ok || !pe.OOM || pe.Kind != tc.kind || err.Error() != tc.want {
			t.Errorf("%s: error %v, want %q", tc.kind, err, tc.want)
		}
	}
	if got := (&PreviewError{Kind: gameenv.KindResources, ExitCode: 1}).Error(); got != "the game failed in the resource count (exit 1)" {
		t.Errorf("a failed count says %q", got)
	}
}
