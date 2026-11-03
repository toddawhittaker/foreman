package store

import (
	"context"
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
		Action: "server.start", WorldID: "alpha", Detail: "pinned 2.0.55",
	}
	if err := s.AddAudit(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAudit(ctx, 10)
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
	got, err := s.ListAudit(ctx, 10)
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
	got, err := s.ListAudit(ctx, 3)
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
		if _, err := s.ListAudit(context.Background(), n); err == nil {
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
	got, err := s.ListAudit(ctx, 10)
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
