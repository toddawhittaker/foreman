package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The controller gets the host data directory, and accepts what serve gives it.
func TestGameConfig(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	worlds, err := world.NewManager(world.Dirs{
		Worlds:    filepath.Join(dir, "worlds"),
		Snapshots: filepath.Join(dir, "snapshots"),
		Imports:   filepath.Join(dir, "import"),
	}, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := docker.New("unix:///nonexistent.sock")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{GameUID: 845, GameGID: 845, GameNetwork: "games"}
	gc := gameConfig(cfg, d, worlds, st, "/srv/foreman", "self")
	if gc.HostDataDir != "/srv/foreman" || gc.Network != "games" || gc.SelfID != "self" {
		t.Errorf("game config = %+v", gc)
	}
	if _, err := game.New(gc); err != nil {
		t.Errorf("game.New: %v", err)
	}
}
