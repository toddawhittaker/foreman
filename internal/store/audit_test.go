package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestAuditRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, err := s.CreateFirstAdmin(ctx, "ann", "h", t0)
	if err != nil {
		t.Fatal(err)
	}
	want := AuditEntry{
		Time: t0, UserID: u.ID, Username: "ann",
		Action: "server.start", WorldID: "alpha", Detail: "pinned 2.0.55", TokenName: "Claude",
	}
	if err := s.AddAudit(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAudit(ctx, AuditFilter{Limit: 10})
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v, %d rows", err, len(got))
	}
	want.ID = got[0].ID
	if got[0] != want {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

func TestAuditEntryWithoutUserOrWorld(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.AddAudit(ctx, AuditEntry{Time: t0, Username: "system", Action: "image.download"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAudit(ctx, AuditFilter{Limit: 10})
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v, %d rows", err, len(got))
	}
	if got[0].UserID != 0 || got[0].WorldID != "" || got[0].Detail != "" {
		t.Errorf("got %+v", got[0])
	}
	// Stored as NULL, not as 0 or an empty string.
	var nulls int
	err = s.db.QueryRow("SELECT count(*) FROM audit_log WHERE user_id IS NULL AND world_id IS NULL").Scan(&nulls)
	if err != nil || nulls != 1 {
		t.Errorf("null columns: count=%d err=%v", nulls, err)
	}
}

func TestListAuditNewestFirstWithLimit(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	// Insert out of time order, and two with the same second.
	for i, at := range []time.Time{t0.Add(2 * time.Minute), t0, t0.Add(time.Minute), t0.Add(time.Minute)} {
		e := AuditEntry{Time: at, Username: "ann", Action: "console.command", Detail: string(rune('a' + i))}
		if err := s.AddAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListAudit(ctx, AuditFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	var details string
	for _, e := range got {
		details += e.Detail
	}
	// 2 minutes (a), then the tie broken by newest id (d, c). The oldest (b) is cut off.
	if details != "adc" {
		t.Errorf("order = %q, want adc", details)
	}
}

func TestListAuditRejectsBadLimit(t *testing.T) {
	s, _ := openTemp(t)
	for _, n := range []int{0, -1} {
		if _, err := s.ListAudit(context.Background(), AuditFilter{Limit: n}); err == nil {
			t.Errorf("limit %d accepted", n)
		}
	}
}

func TestDeletingUserKeepsAuditRows(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, err := s.CreateFirstAdmin(ctx, "ann", "h", t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAudit(ctx, AuditEntry{Time: t0, UserID: u.ID, Username: "ann", Action: "server.stop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DELETE FROM users WHERE id = ?", u.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAudit(ctx, AuditFilter{Limit: 10})
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v, %d rows", err, len(got))
	}
	if got[0].UserID != 0 || got[0].Username != "ann" {
		t.Errorf("got %+v, want no user id and the name kept", got[0])
	}
}

func TestAddAuditRefusesUnknownUser(t *testing.T) {
	s, _ := openTemp(t)
	err := s.AddAudit(context.Background(), AuditEntry{Time: t0, UserID: 999, Username: "ghost", Action: "x"})
	if err == nil {
		t.Error("an entry for a user that does not exist was accepted")
	}
}

// seedAudit records one entry per row and returns the stored entries, newest first.
func seedAudit(t *testing.T, s *Store, rows []AuditEntry) []AuditEntry {
	t.Helper()
	for _, e := range rows {
		if err := s.AddAudit(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListAudit(context.Background(), AuditFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestListAuditFiltersAreExactAndCombine(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	seedAudit(t, s, []AuditEntry{
		{Time: t0, Username: "ann", Action: "start", WorldID: "alpha"},
		{Time: t0.Add(time.Second), Username: "ann", Action: "stop", WorldID: "beta"},
		{Time: t0.Add(2 * time.Second), Username: "bob", Action: "start", WorldID: "beta"},
		{Time: t0.Add(3 * time.Second), Username: "anna", Action: "start"},
	})
	for _, c := range []struct {
		name string
		f    AuditFilter
		want int
	}{
		{"user is exact, not a prefix", AuditFilter{Username: "ann"}, 2},
		{"action", AuditFilter{Action: "start"}, 3},
		{"world", AuditFilter{WorldID: "beta"}, 2},
		{"all three", AuditFilter{Username: "ann", Action: "start", WorldID: "alpha"}, 1},
		{"nothing matches", AuditFilter{Username: "bob", Action: "stop"}, 0},
	} {
		c.f.Limit = 10
		got, err := s.ListAudit(ctx, c.f)
		if err != nil || len(got) != c.want {
			t.Errorf("%s: %d entries, err %v, want %d", c.name, len(got), err, c.want)
		}
	}
}

// Filter text is a value, never SQL.
func TestListAuditFilterIsNotSQL(t *testing.T) {
	s, _ := openTemp(t)
	seedAudit(t, s, []AuditEntry{{Time: t0, Username: "ann", Action: "start"}})
	got, err := s.ListAudit(context.Background(), AuditFilter{Username: "x' OR '1'='1", Limit: 10})
	if err != nil || len(got) != 0 {
		t.Errorf("got %d entries, err %v, want none", len(got), err)
	}
}

// Pages by cursor neither skip nor repeat an entry, even across entries
// that share a second.
func TestListAuditCursorWalksEveryEntryOnce(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	var rows []AuditEntry
	for i := 0; i < 7; i++ {
		rows = append(rows, AuditEntry{Time: t0.Add(time.Duration(i/3) * time.Second), Username: "ann", Action: "start"})
	}
	all := seedAudit(t, s, rows)
	var walked []int64
	var cur AuditCursor
	for {
		page, err := s.ListAudit(ctx, AuditFilter{Before: cur, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, e := range page {
			walked = append(walked, e.ID)
		}
		last := page[len(page)-1]
		cur = AuditCursor{Time: last.Time, ID: last.ID}
	}
	if len(walked) != len(all) {
		t.Fatalf("walked %d entries, want %d", len(walked), len(all))
	}
	for i, e := range all {
		if walked[i] != e.ID {
			t.Errorf("position %d: id %d, want %d", i, walked[i], e.ID)
		}
	}
}

func TestAuditFilterValues(t *testing.T) {
	s, _ := openTemp(t)
	seedAudit(t, s, []AuditEntry{
		{Time: t0, Username: "bob", Action: "stop", WorldID: "beta"},
		{Time: t0, Username: "ann", Action: "start", WorldID: "alpha"},
		{Time: t0, Username: "ann", Action: "start"},
	})
	got, err := s.AuditFilterValues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		got, want []string
	}{
		{"users", got.Users, []string{"ann", "bob"}},
		{"actions", got.Actions, []string{"start", "stop"}},
		{"worlds", got.Worlds, []string{"alpha", "beta"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestDeleteAuditBeforeKeepsTheBoundary(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	seedAudit(t, s, []AuditEntry{
		{Time: t0.Add(-time.Second), Username: "ann", Action: "old"},
		{Time: t0, Username: "ann", Action: "edge"},
		{Time: t0.Add(time.Second), Username: "ann", Action: "new"},
	})
	n, err := s.DeleteAuditBefore(ctx, t0)
	if err != nil || n != 1 {
		t.Fatalf("deleted %d, err %v, want 1", n, err)
	}
	got, _ := s.ListAudit(ctx, AuditFilter{Limit: 10})
	if len(got) != 2 || got[0].Action != "new" || got[1].Action != "edge" {
		t.Errorf("left %+v, want new and edge", got)
	}
}
