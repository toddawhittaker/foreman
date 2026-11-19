package web

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/store"
)

const oneDay = 24 * time.Hour

// sweptAudit records one entry per age (a0, a1, ...), runs the hourly sweep
// and returns the actions that are left, newest first.
func sweptAudit(t *testing.T, hn *harness, ages ...time.Duration) []string {
	t.Helper()
	ctx := context.Background()
	for i, age := range ages {
		e := store.AuditEntry{Time: hn.clock.Now().Add(-age), Username: "ann", Action: "a" + strconv.Itoa(i)}
		if err := hn.store.AddAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	sweepOnce(ctx, hn.store, oneDay, hn.clock.Now(), hn.slog)
	list, err := hn.store.ListAudit(ctx, store.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range list {
		left = append(left, e.Action)
	}
	return left
}

// With no setting, entries older than 365 days go, and the sweep adds no
// entry of its own (SPEC 12.9).
func TestSweepDeletesAuditOlderThanTheDefault(t *testing.T) {
	hn := newHarness(t, nil)
	got := sweptAudit(t, hn, 366*oneDay, 364*oneDay, time.Hour)
	if !slices.Equal(got, []string{"a2", "a1"}) {
		t.Errorf("left %v, want a2 and a1 and nothing from the sweep", got)
	}
}

func TestSweepUsesTheAuditRetentionSetting(t *testing.T) {
	hn := newHarness(t, nil)
	if err := hn.store.SetAppSetting(context.Background(), "audit_retention_days", "30", "ann", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	got := sweptAudit(t, hn, 31*oneDay, 29*oneDay)
	if !slices.Equal(got, []string{"a1"}) {
		t.Errorf("left %v, want a1", got)
	}
}

// A value outside 7 to 3650, or not a number, never deletes more than the
// default would.
func TestSweepIgnoresABadAuditRetention(t *testing.T) {
	for _, v := range []string{"1", "0", "-5", "99999", "soon", ""} {
		hn := newHarness(t, nil)
		if err := hn.store.SetAppSetting(context.Background(), "audit_retention_days", v, "ann", hn.clock.Now()); err != nil {
			t.Fatal(err)
		}
		got := sweptAudit(t, hn, 366*oneDay, 100*oneDay, time.Hour)
		if !slices.Equal(got, []string{"a2", "a1"}) {
			t.Errorf("setting %q: left %v, want the default's a2 and a1", v, got)
		}
	}
}
