//go:build game

package game

// With fork saving (non_blocking_saving) a save is written by a child
// process while the game keeps running. A snapshot taken while another
// save is still being written must hold the save begun after it was
// asked, complete and valid (SPEC 6.10). Run by `make test-game`, never by
// `make verify`.

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameForkSaveSnapshot(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runForkSave(t, g.tag, g.version)
		})
	}
}

func (e *gameEnv) runForkSave(t *testing.T, tag, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	id := e.generate(t, tag+" fork save", world.NewWorld{GameVersion: version})
	data, err := gamesettings.SetFields([]byte(`{"name": "e3test fork save"}`), []gamesettings.Field{
		{Key: "description", JSON: []byte(`""`)},
		{Key: "non_blocking_saving", JSON: []byte(`true`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err = gamesettings.SetVisibility(data, gamesettings.VisibilityHidden); err != nil {
		t.Fatal(err)
	}
	read, err := e.worlds.ReadConfig(id, world.ServerSettingsFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaveSettings(ctx, id, admin, []SettingsChange{{File: world.ServerSettingsFile, Data: data, SeenHash: read.Hash}}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	t.Cleanup(func() { _ = c.ForceStop(context.Background(), id, admin) })

	// A map large enough that the child takes a measurable time to write it.
	// The first Lua command of a 1.1 game only warns; the second runs it.
	// Each step is forced in its own command, so none outlasts the RCON
	// call limit.
	for r := 5; r <= 60; r += 5 {
		grow := fmt.Sprintf("/silent-command local s=game.surfaces[1] s.request_to_generate_chunks({0,0}, %d) s.force_generate_chunk_requests() rcon.print('grown')", r)
		for range 2 {
			if _, _, err := c.Console(ctx, id, admin, grow, grow); err != nil {
				t.Fatalf("RCON at radius %d: %v", r, err)
			}
			if tag != "1.1" {
				break
			}
		}
	}

	// An autosave is being written by a child when the snapshot is asked.
	auto := "/silent-command game.auto_save('e3fork')"
	if _, _, err := c.Console(ctx, id, admin, auto, auto); err != nil {
		t.Fatalf("RCON: %v", err)
	}
	begin := time.Now()
	snap, err := c.Snapshot(ctx, id, admin, "fork")
	if err != nil {
		t.Fatalf("snapshot: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	t.Logf("TIMING snapshot with a save queued behind a forked autosave: %s", time.Since(begin).Round(10*time.Millisecond))
	lines := logLines(c, id)

	// The game forked for both saves.
	if !logHas(c, id, "(non-blocking)") || !logHas(c, id, "Saving process PID") {
		t.Errorf("the game did not fork the save; log:\n%s", strings.Join(lines, "\n"))
	}
	// The autosave began, then Foreman's save; both had finished when the
	// snapshot was taken.
	after := -1
	for i, l := range lines {
		if strings.Contains(l, "Saving to _autosave-e3fork") {
			after = i
		}
	}
	if after < 0 {
		t.Fatalf("no autosave start in the log:\n%s", strings.Join(lines, "\n"))
	}
	var started, finished int
	for _, l := range lines[after:] {
		if strings.Contains(l, "Saving game as ") {
			started++
		}
		if strings.HasSuffix(l, "Saving finished") {
			finished++
		}
	}
	if started < 1 || finished < 2 {
		t.Errorf("snapshot taken before Foreman's own save finished: %d starts, %d finishes after the autosave\n%s",
			started, finished, strings.Join(lines[after:], "\n"))
	}

	// Every save in the snapshot is complete and reads back.
	dir := filepath.Join(e.worlds.Dirs().Snapshots, id, snap.Name)
	var zips int
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".zip") {
			return err
		}
		if strings.HasSuffix(p, ".tmp.zip") {
			t.Errorf("the snapshot holds a save still being written: %s", p)
			return nil
		}
		zips++
		checkZip(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if zips == 0 {
		t.Fatalf("the snapshot in %s holds no save", dir)
	}
}

// checkZip reads every entry of a zip, so a truncated file or a bad CRC
// fails.
func checkZip(t *testing.T, p string) {
	t.Helper()
	r, err := zip.OpenReader(p)
	if err != nil {
		t.Errorf("%s: %v", p, err)
		return
	}
	defer func() { _ = r.Close() }()
	for _, f := range r.File {
		rc, err := f.Open()
		if err == nil {
			// A save the game just wrote; the limit only bounds a bad file.
			_, err = io.Copy(io.Discard, io.LimitReader(rc, 1<<30))
			_ = rc.Close()
		}
		if err != nil {
			t.Errorf("%s: %s: %v", p, f.Name, err)
		}
	}
}
