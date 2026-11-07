package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSecretLifecycle(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, _, _, err := s.GetSecret(ctx, "x"); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("missing: err = %v, want ErrNoSecret", err)
	}
	if err := s.PutSecret(ctx, "x", []byte{1, 2, 3}, "todd", t0); err != nil {
		t.Fatal(err)
	}
	v, at, by, err := s.GetSecret(ctx, "x")
	if err != nil || string(v) != "\x01\x02\x03" || !at.Equal(t0) || by != "todd" {
		t.Fatalf("Get = %v %v %q %v", v, at, by, err)
	}
	later := t0.Add(time.Hour)
	if err := s.PutSecret(ctx, "x", []byte{9}, "ann", later); err != nil {
		t.Fatal(err)
	}
	v, at, by, _ = s.GetSecret(ctx, "x")
	if string(v) != "\x09" || !at.Equal(later) || by != "ann" {
		t.Errorf("after replace: %v %v %q", v, at, by)
	}
	if err := s.DeleteSecret(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.GetSecret(ctx, "x"); !errors.Is(err, ErrNoSecret) {
		t.Errorf("after delete: err = %v", err)
	}
	if err := s.DeleteSecret(ctx, "x"); err != nil {
		t.Errorf("deleting a missing secret: %v", err)
	}
}
