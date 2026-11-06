//go:build game

package game

// A world with no server-settings.json starts without --server-settings; its
// first settings save must write a file the game then starts with (SPEC 11.1,
// 11.2). Run by `make test-game`, never by `make verify`.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameFirstSettingsSave(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runFirstSave(t, g.tag, g.version)
		})
	}
}

func (e *gameEnv) runFirstSave(t *testing.T, tag, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	id := e.generate(t, tag+" first save", world.NewWorld{GameVersion: version})
	if cfg, err := e.worlds.ReadConfig(id, world.ServerSettingsFile); err != nil || cfg.Found {
		t.Fatalf("a generated world has a server-settings.json: %v %v", cfg.Found, err)
	}
	save := func(data []byte) error {
		read, err := e.worlds.ReadConfig(id, world.ServerSettingsFile)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.SaveSettings(ctx, id, admin, []SettingsChange{{File: world.ServerSettingsFile, Data: data, SeenHash: read.Hash}})
		return err
	}

	// What the form wrote before: a name and a password only. The game would
	// refuse it at the next start, so the save is refused first.
	partial := []byte(`{"name": "e3test first save", "game_password": "e3test-pw"}`)
	if err := save(partial); !errors.Is(err, ErrSettingsInvalid) {
		t.Fatalf("a file without description and visibility was not refused: %v", err)
	}
	if cfg, _ := e.worlds.ReadConfig(id, world.ServerSettingsFile); cfg.Found {
		t.Fatal("the refused file was written")
	}

	// What the form writes now for a first save of a name and a password.
	data, err := gamesettings.SetFields([]byte(`{"name": "e3test first save"}`), []gamesettings.Field{
		{Key: "description", JSON: []byte(`""`)},
		{Key: "game_password", JSON: []byte(`"e3test-pw"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err = gamesettings.SetVisibility(data, gamesettings.VisibilityHidden); err != nil {
		t.Fatal(err)
	}
	if err := save(data); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// The first start creates the map and passes --server-settings.
	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	if st := c.Status(id); st.State != StateRunning {
		t.Fatalf("state %s; want running", st.State)
	}
	if !logHas(c, id, "Loading map /factorio/saves/"+id+".zip") {
		t.Error("the log has no line for loading the new save")
	}
	e.noSecretInLog(t, c, id)
	if err := c.Stop(ctx, id, admin, StopOptions{}); err != nil {
		t.Fatalf("stop: %v", err)
	}
}
