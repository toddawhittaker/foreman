package game

import (
	"context"
	"errors"
	"fmt"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// AuditAutosaveTidy is the audit action of the autosave tidy (SPEC 11.6,
// 12.9).
const AuditAutosaveTidy = "autosave-tidy"

// tidyActor is who the tidy's snapshot and audit entry name: Foreman does
// it on its own at a start, whoever asked for the start.
var tidyActor = Actor{Username: "Foreman"}

// tidyAutosaves removes the autosaves above the world's slot count and
// renames the newest into slots, before the save to load is chosen
// (SPEC 11.6). The caller holds the world's lock and the game is stopped.
// A failure is only logged: the start carries on.
func (c *Controller) tidyAutosaves(ctx context.Context, w *worldRun) {
	if err := c.tidyAutosavesErr(ctx, w); err != nil {
		c.log.Warn("did not tidy the autosaves", "world", w.id, "error", err)
	}
}

func (c *Controller) tidyAutosavesErr(ctx context.Context, w *worldRun) error {
	id := w.id
	conf, err := c.worlds.ReadConfig(id, world.ServerSettingsFile)
	if err != nil {
		return err
	}
	if !conf.Found {
		conf.Data = []byte("{}")
	}
	slots, interval, ok := gamesettings.AutosaveSlots(conf.Data)
	if !ok {
		return errors.New("server-settings.json has no whole autosave_slots or autosave_interval")
	}
	if slots == 0 || interval == 0 {
		return nil
	}
	w.mu.Lock()
	loaded := w.lastLoaded
	w.mu.Unlock()
	plan, ok, err := c.worlds.PlanAutosaveTidyHeld(id, slots, loaded)
	if err != nil || !ok {
		return err
	}
	if _, err := c.worlds.AutoSnapshot(id, world.ReasonAutosaves, world.LabelBeforeAutosaves, tidyActor.Username); err != nil {
		return fmt.Errorf("take the snapshot before the tidy: %w", err)
	}
	detail := fmt.Sprintf("kept %d, renamed %d, removed %d", plan.Kept, len(plan.Moves), plan.Removed)
	if err := c.audit(ctx, tidyActor, AuditAutosaveTidy, id, detail); err != nil {
		return err
	}
	err = c.worlds.ApplyAutosaveTidyHeld(id, plan)
	// The loaded file may now have another name, or none; after a tidy cut
	// short, the newest file stands in for it.
	w.mu.Lock()
	if w.lastLoaded == loaded {
		w.lastLoaded = ""
		if err == nil {
			w.lastLoaded = plan.NewName(loaded)
		}
	}
	w.mu.Unlock()
	return err
}
