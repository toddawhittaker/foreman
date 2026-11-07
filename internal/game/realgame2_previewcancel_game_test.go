//go:build game

package game

// A map preview request that is waiting for the one preview slot and is then
// cancelled (the client went away) must leave no container and no work folder
// (SPEC 14.3), against the real Docker daemon and game images. Run by `make
// test-game`, never by `make verify`.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestRealGameRG2WaitingPreviewCancel(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			c, _ := e.controller(t)
			// The slot is held, as by a preview that is drawing.
			c.previewSlot <- struct{}{}
			held := true
			release := func() {
				if held {
					held = false
					<-c.previewSlot
				}
			}
			defer release()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := c.Preview(ctx, PreviewRequest{GameVersion: g.version, Seed: 5, Planets: []string{"nauvis"}})
				done <- err
			}()
			deadline := time.Now().Add(5 * time.Second)
			for len(c.previewWaiting) == 0 {
				if time.Now().After(deadline) {
					t.Fatal("the second request never started waiting for the slot")
				}
				time.Sleep(5 * time.Millisecond)
			}
			// While it waits, nothing is made for it.
			e.previewLeftovers(t, "while the request waits")

			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error %v; want context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the waiting request did not return within 5 s of the cancel")
			}
			e.previewLeftovers(t, "after the waiting request was cancelled")
			if n := len(c.previewWaiting); n != 0 {
				t.Errorf("%d waiting places still taken after the cancel", n)
			}
			if n := len(c.previewSlot); n != 1 {
				t.Errorf("the slot holds %d tokens; the cancelled request must not take or free the first one's", n)
			}

			// The first preview's slot, once freed, serves the next request.
			release()
			if _, err := c.Preview(context.Background(), PreviewRequest{GameVersion: g.version, Seed: 6, Planets: []string{"nauvis"}}); err != nil {
				t.Errorf("a preview after the cancelled one: %v", err)
			}
			e.previewLeftovers(t, "after the next preview")
		})
	}
}
