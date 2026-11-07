//go:build game

package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/world"
)

// activeIs reports a fixed active world and nothing else; the update, the
// rollback and a refused start ask no more of the store.
type activeIs struct {
	SwitchStore
	mu sync.Mutex
	id string
}

func (a *activeIs) ActiveWorld(context.Context) (string, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id, true, nil
}

func (a *activeIs) set(id string) {
	a.mu.Lock()
	a.id = id
	a.mu.Unlock()
}

// TestRealGameUpdateOfAWorldThatIsNotActive updates a world that is not
// active from 2.0.77 to 2.1.24 and rolls it back (SPEC 10.3, 10.4). No
// container is made for it until it is made active, and the updated save
// then loads on the new version.
func TestRealGameUpdateOfAWorldThatIsNotActive(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.0" && only != "2.1" {
		t.Skip("needs the 2.0.77 and 2.1.24 images")
	}
	const from, to = "2.0.77", "2.1.24"
	ctx := context.Background()
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(from))
	e.requireImage(t, ImageRef(to))
	c, _ := e.controller(t)

	save, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", "2.0.77-no-space-age.zip"))
	if err != nil {
		t.Fatal(err)
	}
	other := e.newWorld(t, "inact", to, "active", "", nil)
	id := e.newWorld(t, "inact", from, "idle", "", nil)
	e.putSave(t, id, "main.zip", save, time.Now().Add(-time.Hour))
	active := &activeIs{id: other}
	c.switches = active

	if err := c.Start(ctx, id, admin, StartOptions{}); !errors.Is(err, ErrWorldNotActive) {
		t.Fatalf("start of the world that is not active = %v, want ErrWorldNotActive", err)
	}

	began := time.Now()
	if err := c.Update(ctx, id, to, admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatalf("update: %v", err)
	}
	t.Logf("update of a world that is not active took %s", time.Since(began).Round(100*time.Millisecond))
	if _, err := e.docker.InspectContainer(ctx, ContainerName(id)); !errors.Is(err, docker.ErrNotFound) {
		t.Fatalf("a container exists for the world that is not active after the update: %v", err)
	}
	if v := e.metaVersion(t, id); v != to {
		t.Fatalf("world.json says %s, want %s", v, to)
	}
	snap := e.onlySnapshot(t, id, world.ReasonGameUpdate)
	beforeHash := e.snapshotSaveHash(t, id, snap, "main.zip")
	if got := e.liveSaveHash(t, id, "main.zip"); got != beforeHash {
		t.Fatalf("the update changed the save: %s, the snapshot holds %s", got, beforeHash)
	}

	// Made active, the updated world loads on the new version.
	active.set(id)
	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start on %s: %v", to, err)
	}
	if st := c.Status(id); st.State != StateRunning || st.GameVersion != to || !logHas(c, id, inGameLine) {
		t.Fatalf("state %s (%s), game version %q, want running on %s", st.State, st.Reason, st.GameVersion, to)
	}
	if err := c.Stop(ctx, id, admin, StopOptions{Countdown: 2 * time.Second}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	active.set(other)

	undo, err := c.Rollback(ctx, id, admin, StopOptions{})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if v := e.metaVersion(t, id); v != from {
		t.Fatalf("world.json says %s after rollback, want %s", v, from)
	}
	if got := e.liveSaveHash(t, id, "main.zip"); got != beforeHash {
		t.Fatalf("the save after rollback hashes to %s, the Before update snapshot's to %s", got, beforeHash)
	}
	if undo.Label != world.LabelBeforeRestore {
		t.Fatalf("undo snapshot label %q, want %q", undo.Label, world.LabelBeforeRestore)
	}
	if st := c.Status(id); IsRunning(st.State) {
		t.Fatalf("the rollback started the game: %s", st.State)
	}
}
