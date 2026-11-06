//go:build game

package game

// The map preview at its size of 1280 tiles, one planet at a time, against
// the real game images (SPEC 5.6, 14.3). Run by `make test-game`, never by
// `make verify`.

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// previewSlowest is far above what a planet takes (about 1 to 2 s on a
// 32-core host), so only a stuck preview trips it; the timings are logged.
const previewSlowest = 20 * time.Second

func TestRealGameUIBPreviews(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runUIBPreviews(t, g.tag, g.version)
		})
	}
}

// previewLeftovers fails the test if a preview container carrying this
// run's label, or a work folder, is still there.
func (e *gameEnv) previewLeftovers(t *testing.T, when string) {
	t.Helper()
	list, err := e.docker.ListContainers(context.Background(), true, []string{gameenv.LabelPreview + "=" + e.data})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		t.Errorf("%s: preview container %v is still there", when, s.Names)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.data, previewDir)); len(entries) != 0 {
		t.Errorf("%s: %d preview folders left", when, len(entries))
	}
}

func (e *gameEnv) runUIBPreviews(t *testing.T, line, version string) {
	c, _ := e.controller(t)
	ages := []bool{false}
	if l, _ := mapgen.LineOf(line); len(l.SpaceAgeMods) > 0 {
		ages = append(ages, true)
	}
	var total time.Duration
	for _, spaceAge := range ages {
		for _, planet := range mapgen.Planets(line, spaceAge) {
			if !spaceAge && len(ages) > 1 && planet != "nauvis" {
				continue
			}
			name := planet
			if spaceAge {
				name += " with Space Age"
			}
			t.Run(name, func(t *testing.T) {
				begin := time.Now()
				pngs, err := c.Preview(context.Background(), PreviewRequest{
					GameVersion: version, SpaceAge: spaceAge, Seed: 1234, Planets: []string{planet},
				})
				took := time.Since(begin)
				total += took
				if err != nil {
					t.Fatalf("preview of %s: %v", planet, err)
				}
				t.Logf("TIMING %s %s: %s", version, name, took.Round(10*time.Millisecond))
				if took > previewSlowest {
					t.Errorf("took %s, more than %s", took, previewSlowest)
				}
				if len(pngs) != 1 || pngs[0].Planet != planet {
					t.Fatalf("pictures: %d, first for %q; want one, for %s", len(pngs), func() string {
						if len(pngs) > 0 {
							return pngs[0].Planet
						}
						return ""
					}(), planet)
				}
				img, err := png.Decode(bytes.NewReader(pngs[0].Data))
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %d bytes, %dx%d", planet, len(pngs[0].Data), img.Bounds().Dx(), img.Bounds().Dy())
				if img.Bounds().Dx() != PreviewSize || img.Bounds().Dy() != PreviewSize {
					t.Errorf("picture is %dx%d, want %dx%d tiles", img.Bounds().Dx(), img.Bounds().Dy(), PreviewSize, PreviewSize)
				}
				if len(pngs[0].Data) > MaxPreviewPNG {
					t.Errorf("picture is %d bytes, over the %d cap", len(pngs[0].Data), MaxPreviewPNG)
				}
				e.previewLeftovers(t, "after a normal run")
			})
		}
	}
	t.Logf("TIMING %s all previews, one at a time: %s", version, total.Round(10*time.Millisecond))

	// A request cancelled while the game is drawing: the call ends with the
	// caller's error, and no container or folder is left (SPEC 14.3).
	t.Run("cancelled mid-run", func(t *testing.T) {
		spaceAge := len(ages) > 1
		planets := mapgen.Planets(line, spaceAge)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := c.Preview(ctx, PreviewRequest{GameVersion: version, SpaceAge: spaceAge, Seed: 99, Planets: planets})
			done <- err
		}()
		// Cancel as soon as the container exists.
		deadline := time.Now().Add(10 * time.Second)
		for {
			list, err := e.docker.ListContainers(context.Background(), true, []string{gameenv.LabelPreview + "=" + e.data})
			if err != nil {
				t.Fatal(err)
			}
			if len(list) > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no preview container appeared")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancelled := time.Now()
		cancel()
		select {
		case err := <-done:
			t.Logf("TIMING the call returned %s after the cancel: %v", time.Since(cancelled).Round(10*time.Millisecond), err)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("error %v; want context.Canceled (the run finished before the cancel, or the cancel was not honoured)", err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("the call did not return within 15 s of the cancel")
		}
		e.previewLeftovers(t, "after a cancelled request")

		// The slot is free again.
		if _, err := c.Preview(context.Background(), PreviewRequest{GameVersion: version, Seed: 1, Planets: []string{"nauvis"}}); err != nil {
			t.Errorf("a preview after the cancelled one: %v", err)
		}
		e.previewLeftovers(t, "after the next preview")
	})
}
