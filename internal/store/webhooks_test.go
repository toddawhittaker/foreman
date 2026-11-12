package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWebhookLifecycle(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if _, err := s.Webhook(ctx, 1); !errors.Is(err, ErrNoWebhook) {
		t.Fatalf("missing: err = %v, want ErrNoWebhook", err)
	}
	var sealedFor int64
	id, err := s.AddWebhook(ctx, Webhook{Preset: "discord", Host: "discord.com", EventsOff: "stop", Lang: "de", CreatedBy: "todd"},
		func(id int64) ([]byte, error) { sealedFor = id; return []byte{7, 7}, nil }, t0)
	if err != nil {
		t.Fatal(err)
	}
	if sealedFor != id {
		t.Errorf("sealed for row %d, inserted %d", sealedFor, id)
	}
	w, err := s.Webhook(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if w.Preset != "discord" || w.Host != "discord.com" || string(w.URL) != "\x07\x07" || w.EventsOff != "stop" || w.Lang != "de" ||
		w.CreatedBy != "todd" || w.UpdatedBy != "todd" || !w.CreatedAt.Equal(t0) {
		t.Errorf("read back %+v", w)
	}

	// Without a new address, only the events and language change.
	later := t0.Add(time.Hour)
	if err := s.UpdateWebhook(ctx, Webhook{ID: id, Preset: "ntfy", Host: "ntfy.sh", EventsOff: "", Lang: "fr", UpdatedBy: "ann"}, nil, later); err != nil {
		t.Fatal(err)
	}
	w, _ = s.Webhook(ctx, id)
	if w.Preset != "discord" || w.Host != "discord.com" || string(w.URL) != "\x07\x07" || w.EventsOff != "" || w.Lang != "fr" || w.UpdatedBy != "ann" || !w.UpdatedAt.Equal(later) {
		t.Errorf("after edit without address: %+v", w)
	}
	if err := s.UpdateWebhook(ctx, Webhook{ID: id, Preset: "ntfy", Host: "ntfy.sh", Lang: "fr", UpdatedBy: "ann"}, []byte{9}, later); err != nil {
		t.Fatal(err)
	}
	w, _ = s.Webhook(ctx, id)
	if w.Preset != "ntfy" || w.Host != "ntfy.sh" || string(w.URL) != "\x09" {
		t.Errorf("after edit with address: %+v", w)
	}

	id2, err := s.AddWebhook(ctx, Webhook{Preset: "generic", Host: "example.com", Lang: "en", CreatedBy: "todd"},
		func(int64) ([]byte, error) { return []byte{1}, nil }, t0)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.ListWebhooks(ctx)
	if err != nil || len(all) != 2 || all[0].ID != id || all[1].ID != id2 {
		t.Fatalf("list = %+v, %v", all, err)
	}
	if err := s.DeleteWebhook(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWebhook(ctx, id); !errors.Is(err, ErrNoWebhook) {
		t.Errorf("second delete: %v", err)
	}
	if err := s.UpdateWebhook(ctx, Webhook{ID: id, Lang: "en"}, nil, later); !errors.Is(err, ErrNoWebhook) {
		t.Errorf("update of a deleted row: %v", err)
	}
}

// A seal that fails leaves no row without an address.
func TestAddWebhookSealFailureLeavesNoRow(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	boom := errors.New("seal failed")
	if _, err := s.AddWebhook(ctx, Webhook{Preset: "discord", Host: "discord.com", Lang: "en", CreatedBy: "todd"},
		func(int64) ([]byte, error) { return nil, boom }, t0); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if all, _ := s.ListWebhooks(ctx); len(all) != 0 {
		t.Errorf("rows after a failed seal: %+v", all)
	}
}
