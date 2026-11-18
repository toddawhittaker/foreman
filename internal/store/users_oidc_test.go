package store

import (
	"context"
	"errors"
	"testing"
)

const testIssuer = "https://id.example.com/"

func TestLinkOIDCAndFind(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "carol", "", t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.UserByOIDC(ctx, testIssuer, "sub-1"); err != nil || found {
		t.Fatalf("before the link: found=%v err=%v", found, err)
	}
	if err := s.LinkOIDC(ctx, u.ID, testIssuer, "sub-1"); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.UserByOIDC(ctx, testIssuer, "sub-1")
	if err != nil || !found || got.ID != u.ID || got.OIDCIssuer != testIssuer || got.OIDCSubject != "sub-1" {
		t.Fatalf("UserByOIDC = %+v found=%v err=%v", got, found, err)
	}
	// The subject is compared exactly, and so is the issuer.
	for _, k := range [][2]string{{testIssuer, "SUB-1"}, {"https://id.example.com", "sub-1"}} {
		if _, found, _ := s.UserByOIDC(ctx, k[0], k[1]); found {
			t.Errorf("found a user by %q %q", k[0], k[1])
		}
	}
	// A linked user cannot be linked again, even to the same subject.
	if err := s.LinkOIDC(ctx, u.ID, testIssuer, "sub-2"); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("second link: %v", err)
	}
}

func TestLinkOIDCRefusesPasswordUsersAndTakenSubjects(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	pw, err := s.CreateUser(ctx, "alice", "hash", t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkOIDC(ctx, pw.ID, testIssuer, "sub-1"); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("a password user was linked: %v", err)
	}
	a, _ := s.CreateUser(ctx, "bob", "", t0)
	b, _ := s.CreateUser(ctx, "dave", "", t0)
	if err := s.LinkOIDC(ctx, a.ID, testIssuer, "sub-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkOIDC(ctx, b.ID, testIssuer, "sub-1"); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("one subject linked to two users: %v", err)
	}
	if err := s.LinkOIDC(ctx, 9999, testIssuer, "sub-9"); !errors.Is(err, ErrNotLinkable) {
		t.Errorf("missing user: %v", err)
	}
	if u, _, _ := s.UserByID(ctx, pw.ID); u.OIDCIssuer != "" || u.OIDCSubject != "" {
		t.Errorf("unlinked user reads %+v", u)
	}
}
