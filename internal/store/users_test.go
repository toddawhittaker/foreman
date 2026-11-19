package store

import (
	"context"
	"testing"
	"time"
)

func TestListUsersOldestFirst(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "bob", "", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOIDCUser(ctx, "carol", testIssuer, "sub-c", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFirstAdmin(ctx, "alice", "hash", t0); err == nil {
		t.Fatal("CreateFirstAdmin with users present succeeded")
	}
	if _, err := s.CreateUser(ctx, "alice", "hash", t0); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range got {
		names = append(names, u.Username)
	}
	if len(names) != 3 || names[0] != "alice" || names[1] != "bob" || names[2] != "carol" {
		t.Fatalf("ListUsers = %v", names)
	}
	if got[0].PasswordHash != "hash" || got[2].OIDCSubject != "sub-c" || !got[1].CreatedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("fields not read: %+v", got)
	}
}

// SPEC 12.12: a deleted user's sessions and API tokens go with them by
// cascade; their audit entries keep the name (SPEC 12.9).
func TestDeleteUserCascades(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	alice, err := s.CreateUser(ctx, "alice", "h", t0)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, "bob", "h", t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []User{alice, bob} {
		if err := s.CreateSession(ctx, "session-"+u.Username, u.ID, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateAPIToken(ctx, u.ID, "T", "read", "secret-"+u.Username, t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddAudit(ctx, AuditEntry{Time: t0, UserID: bob.ID, Username: "bob", Action: "start"}); err != nil {
		t.Fatal(err)
	}

	found, err := s.DeleteUser(ctx, bob.ID)
	if err != nil || !found {
		t.Fatalf("DeleteUser = %v, %v", found, err)
	}
	if _, _, ok, _ := s.SessionUser(ctx, "session-bob"); ok {
		t.Error("bob's session survived")
	}
	if _, ok, _ := s.APITokenByHash(ctx, "secret-bob"); ok {
		t.Error("bob's token survived")
	}
	if _, _, ok, _ := s.SessionUser(ctx, "session-alice"); !ok {
		t.Error("alice's session went too")
	}
	if _, ok, _ := s.APITokenByHash(ctx, "secret-alice"); !ok {
		t.Error("alice's token went too")
	}
	entries, err := s.ListAudit(ctx, 10)
	if err != nil || len(entries) != 1 || entries[0].Username != "bob" || entries[0].UserID != 0 {
		t.Errorf("audit after delete = %+v, %v", entries, err)
	}
	if found, err := s.DeleteUser(ctx, bob.ID); err != nil || found {
		t.Errorf("second delete = %v, %v", found, err)
	}
}

func TestDeleteUserSessions(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, "alice", "h", t0)
	bob, _ := s.CreateUser(ctx, "bob", "h", t0)
	for _, h := range []string{"a1", "a2"} {
		if err := s.CreateSession(ctx, h, alice.ID, t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateSession(ctx, "b1", bob.ID, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUserSessions(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"a1", "a2"} {
		if _, _, ok, _ := s.SessionUser(ctx, h); ok {
			t.Errorf("session %s survived", h)
		}
	}
	if _, _, ok, _ := s.SessionUser(ctx, "b1"); !ok {
		t.Error("bob's session went too")
	}
}
