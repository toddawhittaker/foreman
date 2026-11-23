package store

import (
	"context"
	"testing"
	"time"
)

func TestAppSettingMissingThenSetThenReplaced(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if v, found, err := s.AppSetting(ctx, "public_mods"); err != nil || found || v != "" {
		t.Fatalf("fresh database: %q found=%v err=%v", v, found, err)
	}
	if err := s.SetAppSetting(ctx, "public_mods", "on", "alice", t0); err != nil {
		t.Fatal(err)
	}
	if v, found, err := s.AppSetting(ctx, "public_mods"); err != nil || !found || v != "on" {
		t.Fatalf("after set: %q found=%v err=%v", v, found, err)
	}
	later := t0.Add(time.Hour)
	if err := s.SetAppSetting(ctx, "public_mods", "off", "bob", later); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := s.AppSetting(ctx, "public_mods"); v != "off" {
		t.Errorf("after replace: %q, want off", v)
	}
	var n int
	var at int64
	var by string
	if err := s.db.QueryRow("SELECT count(*), max(updated_at), max(updated_by) FROM app_settings").Scan(&n, &at, &by); err != nil {
		t.Fatal(err)
	}
	if n != 1 || at != later.Unix() || by != "bob" {
		t.Errorf("rows %d, updated_at %d, updated_by %q: want one row from bob at %d", n, at, by, later.Unix())
	}
}

func TestAppSettingsAreIndependent(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetAppSetting(ctx, "a", "1", "alice", t0); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := s.AppSetting(ctx, "b"); found {
		t.Error("setting b found though only a was set")
	}
}

func TestSetAppSettingsStoresAllOrNone(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	if err := s.SetAppSettings(ctx, []AppSettingWrite{{"a", "1"}, {"b", "2"}}, "alice", t0); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := s.AppSetting(ctx, "b"); v != "2" {
		t.Errorf("b = %q, want 2", v)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER refuse_c BEFORE INSERT ON app_settings WHEN NEW.name = 'c' BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAppSettings(ctx, []AppSettingWrite{{"a", "9"}, {"c", "3"}}, "alice", t0); err == nil {
		t.Fatal("a refused write did not fail the save")
	}
	if v, _, _ := s.AppSetting(ctx, "a"); v != "1" {
		t.Errorf("a = %q after a failed save, want 1 (nothing stored)", v)
	}
}
