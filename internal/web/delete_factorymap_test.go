package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 5.16: a world's factory map goes with the world when it is deleted.
func TestDeleteRemovesTheFactoryMap(t *testing.T) {
	fake := fakegame.New(t, fakegame.Config{Images: []string{image21}})
	hn := newHarnessWith(t, nil, func(worlds *world.Manager, st *store.Store) *game.Controller {
		ctrl, _ := startController(t, fake.Host(), worlds, st, &syncBuffer{})
		return ctrl
	})
	c := hn.signIn()
	activeID, id := hn.seedTwo()
	data := filepath.Dir(hn.worlds.Dirs().Worlds)
	for _, w := range []string{activeID, id} {
		s, err := factorymap.Open(data, w)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Replace(factorymap.Drawing{Failure: "timeout"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	rec := hn.post(deletePath(id), url.Values{"confirm_name": {"Second"}}, withCookie(c))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d\n%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(data, "factorymaps", id)); !os.IsNotExist(err) {
		t.Errorf("the deleted world's factory map remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "factorymaps", activeID)); err != nil {
		t.Errorf("another world's factory map went too: %v", err)
	}
}
