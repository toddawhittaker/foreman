package store

import (
	"context"
	"testing"
	"time"
)

func TestCountAuditBeforeCountsOnlyOlderEntries(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	for _, age := range []time.Duration{48 * time.Hour, 24 * time.Hour, time.Hour} {
		if err := s.AddAudit(ctx, AuditEntry{Time: t0.Add(-age), Username: "system", Action: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	for cutoff, want := range map[time.Duration]int{72 * time.Hour: 0, 30 * time.Hour: 1, 2 * time.Hour: 2, 0: 3} {
		got, err := s.CountAuditBefore(ctx, t0.Add(-cutoff))
		if err != nil || got != want {
			t.Errorf("cutoff %v: %d, %v; want %d", cutoff, got, err, want)
		}
	}
}
