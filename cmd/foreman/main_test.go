package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The images Foreman runs are the ones the real-game tests check.
func TestGameImagesMatchMakefile(t *testing.T) {
	b, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"GAME_IMAGE_1_1": gameImage11, "GAME_IMAGE_2_0": gameImage20, "GAME_IMAGE_2_1": gameImage21}
	for name, value := range want {
		m := regexp.MustCompile(`(?m)^` + name + `\s*:=\s*(\S+)\s*$`).FindSubmatch(b)
		if m == nil {
			t.Errorf("the Makefile has no %s", name)
			continue
		}
		if string(m[1]) != value {
			t.Errorf("%s is %s in the Makefile but %s in main.go", name, m[1], value)
		}
	}
	if len(gameImages) != len(want) {
		t.Errorf("gameImages has %d entries, want %d", len(gameImages), len(want))
	}
}

// The controller gets the image pins and the host data directory, and
// accepts what serve gives it.
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
	if len(gc.Images) != len(gameImages) || gc.HostDataDir != "/srv/foreman" || gc.Network != "games" || gc.SelfID != "self" {
		t.Errorf("game config = %+v", gc)
	}
	if _, err := game.New(gc); err != nil {
		t.Errorf("game.New: %v", err)
	}
}
