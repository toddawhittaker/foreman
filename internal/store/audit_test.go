package store

import (
	"context"
	"slices"
	"strings"
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

// After pages toward the newest, each page still newest first, and the pages
// joined are the same entries as walking down by Before.
func TestListAuditAfterWalksBackUp(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	var rows []AuditEntry
	for i := 0; i < 7; i++ {
		rows = append(rows, AuditEntry{Time: t0.Add(time.Duration(i/3) * time.Second), Username: "ann", Action: "start"})
	}
	all := seedAudit(t, s, rows) // newest first
	oldest := all[len(all)-1]
	var walked []int64 // oldest first
	cur := AuditCursor{Time: oldest.Time, ID: oldest.ID}
	for {
		page, err := s.ListAudit(ctx, AuditFilter{After: cur, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for i := 1; i < len(page); i++ {
			if page[i].ID > page[i-1].ID {
				t.Fatalf("page is not newest first: %v", page)
			}
		}
		// The first of a page is its newest: the cursor for the next one up.
		cur = AuditCursor{Time: page[0].Time, ID: page[0].ID}
		for i := len(page) - 1; i >= 0; i-- {
			walked = append(walked, page[i].ID)
		}
	}
	if len(walked) != len(all)-1 {
		t.Fatalf("walked %d entries, want %d", len(walked), len(all)-1)
	}
	for i, id := range walked {
		if want := all[len(all)-2-i].ID; id != want {
			t.Errorf("position %d: id %d, want %d", i, id, want)
		}
	}
}

func TestListAuditRefusesBeforeAndAfterTogether(t *testing.T) {
	s, _ := openTemp(t)
	c := AuditCursor{Time: t0, ID: 1}
	if _, err := s.ListAudit(context.Background(), AuditFilter{Before: c, After: c, Limit: 1}); err == nil {
		t.Error("both cursors set: want an error")
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

// Paging starts at the cursor through the (time, id) index and does not scan
// the table from the newest entry each time.
func TestListAuditUsesTheTimeIndex(t *testing.T) {
	s, _ := openTemp(t)
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+listAuditSQL, "", "", "", "", "", "", int64(1000), int64(5), 51)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	all := strings.Join(plan, "; ")
	if !strings.Contains(all, "SEARCH audit_log USING INDEX audit_log_time") || strings.Contains(all, "SCAN audit_log") {
		t.Errorf("query plan: %s", all)
	}
}

// A clean-up bigger than one batch deletes everything it should and only that.
func TestDeleteAuditBeforeRunsInBatches(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	const old, kept = 2*auditDeleteBatch + 17, 5
	for _, c := range []struct {
		n  int
		at time.Time
	}{{old, t0.Add(-time.Hour)}, {kept, t0}} {
		_, err := s.db.ExecContext(ctx,
			`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
			 INSERT INTO audit_log (time, username, action) SELECT ?, 'ann', 'x' FROM n`, c.n, c.at.Unix())
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.DeleteAuditBefore(ctx, t0)
	if err != nil || got != old {
		t.Fatalf("deleted %d, err %v, want %d", got, err, old)
	}
	var left int
	if err := s.db.QueryRow("SELECT count(*) FROM audit_log").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != kept {
		t.Errorf("%d entries left, want %d", left, kept)
	}
}

// Pages by cursor stay right at a third page, when entries share a second
// across the page borders.
func TestListAuditCursorThirdPageWithTies(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	var rows []AuditEntry
	for i := 0; i < 12; i++ {
		rows = append(rows, AuditEntry{Time: t0.Add(time.Duration(i/5) * time.Second), Username: "ann", Action: "start"})
	}
	all := seedAudit(t, s, rows)
	cur := AuditCursor{}
	var third []AuditEntry
	for p := 1; p <= 3; p++ {
		page, err := s.ListAudit(ctx, AuditFilter{Before: cur, Limit: 4})
		if err != nil {
			t.Fatal(err)
		}
		last := page[len(page)-1]
		cur = AuditCursor{Time: last.Time, ID: last.ID}
		third = page
	}
	for i, e := range third {
		if e.ID != all[8+i].ID {
			t.Errorf("third page position %d: id %d, want %d", i, e.ID, all[8+i].ID)
		}
	}
}
