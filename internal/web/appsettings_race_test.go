package web

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// Saves at the same time are serialised, so the audit entries chain: each
// "from A to B" starts at the value the one before it ended on.
func TestSaveGeneralSavesOneAtATime(t *testing.T) {
	hn := newHarness(t, nil)
	s := hn.generalServer()
	by := actor(hn.addUser("bob", testPassword))
	var wg sync.WaitGroup
	for n := 11; n <= 30; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.saveGeneral(context.Background(), by, map[string]int{settingSnapshotRetention.Key: n}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rows := hn.audits(AuditSnapshotRetentionSet)
	if len(rows) != 20 {
		t.Fatalf("%d audit rows, want 20", len(rows))
	}
	// Newest first: each row's "from" is the next older row's "to".
	for i := 0; i+1 < len(rows); i++ {
		from := strings.Fields(rows[i])[2]
		older := strings.Fields(rows[i+1])
		if from != older[len(older)-1] {
			t.Errorf("row %q does not follow %q", rows[i], rows[i+1])
		}
	}
}
