package world

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ctxActive is an active-world store that behaves like the real one: it
// refuses a cancelled context. It cancels the caller's context just after
// answering call number cancelAfter, which models a client that goes away
// while the operation is running.
type ctxActive struct {
	fakeActive
	cancel      context.CancelFunc
	cancelAfter int
	setErr      error

	mu2   sync.Mutex
	calls int
}

func (c *ctxActive) ActiveWorld(ctx context.Context) (string, bool, error) {
	c.mu2.Lock()
	c.calls++
	n := c.calls
	c.mu2.Unlock()
	if n == c.cancelAfter {
		defer c.cancel()
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	return c.fakeActive.ActiveWorld(ctx)
}

func (c *ctxActive) SetActiveWorld(ctx context.Context, id string, now time.Time) error {
	if c.setErr != nil {
		return c.setErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.fakeActive.SetActiveWorld(ctx, id, now)
}

func listedIDs(t *testing.T, m *Manager) []string {
	t.Helper()
	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, w := range list {
		ids = append(ids, w.ID)
	}
	return ids
}

// Once the new world's folder is in place the creation has worked, so a
// failure to make it active is logged, not returned (SPEC 5.5).
func TestCreateSucceedsWhenActivationFailsAfterTheRename(t *testing.T) {
	cases := map[string]func() (*ctxActive, context.Context){
		"store refuses to set": func() (*ctxActive, context.Context) {
			return &ctxActive{setErr: errors.New("the active world cannot be saved")}, context.Background()
		},
		"context cancelled after the name check": func() (*ctxActive, context.Context) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			return &ctxActive{cancel: cancel, cancelAfter: 1}, ctx
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := newTestManager(t)
			active, ctx := setup()
			m.active = active
			id, err := m.Create(ctx, "Base", "2.0.72", false)
			if err != nil {
				t.Fatalf("Create = %v, want success: the world exists", err)
			}
			if got := listedIDs(t, m); len(got) != 1 || got[0] != id {
				t.Errorf("listed %v, want [%s]", got, id)
			}
		})
	}
}

// Same rule for restoring a deleted world: it is back once renamed.
func TestRestoreDeletedSucceedsWhenActivationFailsAfterTheRename(t *testing.T) {
	cases := map[string]func() (*ctxActive, context.Context){
		"store refuses to set": func() (*ctxActive, context.Context) {
			return &ctxActive{setErr: errors.New("the active world cannot be saved")}, context.Background()
		},
		"context cancelled after the name checks": func() (*ctxActive, context.Context) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			// Calls: name check in checkRestore, name check in commitStaged, then activation.
			return &ctxActive{cancel: cancel, cancelAfter: 2}, ctx
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m, fake := newTestManager(t)
			id := seedWorld(t, m)
			fake.id = "" // no active world, so the deleted one can go and may be reactivated
			if err := m.Delete(t.Context(), id, "Base", true, "a"); err != nil {
				t.Fatal(err)
			}
			list, _ := m.Snapshots(id)
			active, ctx := setup()
			m.active = active
			if _, err := m.Restore(ctx, id, list[0].Name, "a"); err != nil {
				t.Fatalf("Restore = %v, want success: the world is back", err)
			}
			if got := listedIDs(t, m); len(got) != 1 || got[0] != id {
				t.Errorf("listed %v, want [%s]", got, id)
			}
		})
	}
}
