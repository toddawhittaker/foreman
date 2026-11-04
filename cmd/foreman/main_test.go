package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/secret"
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

func testConfig(t *testing.T, mode string) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		Mode: mode, DataDir: filepath.Join(root, "data"), GameUID: os.Getuid(), GameGID: os.Getgid(),
		DockerHost: "unix:///nonexistent.sock",
	}
	if err := os.Mkdir(cfg.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if mode == config.ModeAdopted {
		cfg.GameDir = filepath.Join(root, "game")
		cfg.GameContainer = "factorio"
		cfg.RCONPassword = secret.New("hunter2-not-for-logs")
		if err := os.Mkdir(cfg.GameDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

// Adopted mode keeps worlds in the game folder and everything else in the
// data directory (SPEC 3.2); managed mode keeps all of it in the data directory.
func TestWorldDirsPerMode(t *testing.T) {
	m := testConfig(t, config.ModeManaged)
	got := worldDirs(m)
	if got.Worlds != filepath.Join(m.DataDir, "worlds") || got.Snapshots != filepath.Join(m.DataDir, "snapshots") || got.Imports != filepath.Join(m.DataDir, "import") {
		t.Errorf("managed dirs = %+v", got)
	}
	a := testConfig(t, config.ModeAdopted)
	got = worldDirs(a)
	if got.Worlds != filepath.Join(a.GameDir, "worlds") {
		t.Errorf("adopted worlds = %q, want inside the game folder", got.Worlds)
	}
	if got.Snapshots != filepath.Join(a.DataDir, "snapshots") || got.Imports != filepath.Join(a.DataDir, "import") {
		t.Errorf("adopted snapshots and imports = %q, %q, want inside the data directory", got.Snapshots, got.Imports)
	}
}

// The worlds folder in the game folder is made when missing, and a link in
// its place is refused (SPEC 14.5).
func TestAdoptedWorldsFolderMadeOrRefused(t *testing.T) {
	cfg := testConfig(t, config.ModeAdopted)
	if _, err := world.NewManager(worldDirs(cfg), nil, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(cfg.GameDir, "worlds"))
	if err != nil || !info.IsDir() || info.Mode().Perm() != world.DirMode.Perm() {
		t.Fatalf("worlds folder: %v, %v", info, err)
	}

	linked := testConfig(t, config.ModeAdopted)
	if err := os.Symlink(t.TempDir(), filepath.Join(linked.GameDir, "worlds")); err != nil {
		t.Fatal(err)
	}
	if _, err := world.NewManager(worldDirs(linked), nil, nil); !errors.Is(err, world.ErrWorldsIsLink) {
		t.Fatalf("a linked worlds folder gave %v, want ErrWorldsIsLink", err)
	}
}

// The ownership and write check runs on the game folder in adopted mode only
// (SPEC 3.6).
func TestProbeFolders(t *testing.T) {
	a := testConfig(t, config.ModeAdopted)
	if err := probeFolders(a); err != nil {
		t.Fatalf("good folders: %v", err)
	}

	a.GameDir = filepath.Join(filepath.Dir(a.GameDir), "missing")
	err := probeFolders(a)
	if err == nil || !strings.Contains(err.Error(), "FOREMAN_GAME_DIR") || !strings.Contains(err.Error(), a.GameDir) {
		t.Errorf("a missing game folder gave %v", err)
	}

	// Managed mode has no game folder, so a stray path is not checked.
	m := testConfig(t, config.ModeManaged)
	m.GameDir = a.GameDir
	if err := probeFolders(m); err != nil {
		t.Errorf("managed mode checked the game folder: %v", err)
	}

	a = testConfig(t, config.ModeAdopted)
	a.GameUID, a.GameGID = os.Getuid()+1, os.Getgid()+1
	if err := probeFolders(a); err == nil {
		t.Error("a game user and group that Foreman does not share passed")
	}
}

// Adopted mode never creates or removes containers or touches images
// (SPEC 4.11).
func TestNewDockerClientAdoptedRefusesManagedCalls(t *testing.T) {
	c, err := newDockerClient(testConfig(t, config.ModeAdopted))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.CreateContainer(ctx, "x", docker.ContainerSpec{}); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("create: %v", err)
	}
	if err := c.RemoveContainer(ctx, "x", true); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("remove: %v", err)
	}
	if err := c.PullImage(ctx, gameImage20); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("pull: %v", err)
	}

	// Managed mode's client passes those calls on, so the failure is the
	// unreachable daemon, not a refusal.
	m, err := newDockerClient(testConfig(t, config.ModeManaged))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.PullImage(ctx, gameImage20); err != nil && strings.Contains(err.Error(), "refusing") {
		t.Errorf("managed mode refused a pull: %v", err)
	}
}

func TestGameConfigPerMode(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{config.ModeManaged, config.ModeAdopted} {
		cfg := testConfig(t, mode)
		st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "foreman.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		worlds, err := world.NewManager(worldDirs(cfg), st, nil)
		if err != nil {
			t.Fatal(err)
		}
		d, err := newDockerClient(cfg)
		if err != nil {
			t.Fatal(err)
		}
		gc := gameConfig(cfg, d, worlds, st, "/srv/foreman", "self")
		if mode == config.ModeAdopted {
			if gc.GameContainer != "factorio" || gc.GameDir != cfg.GameDir || gc.RCONPassword.Reveal() != cfg.RCONPassword.Reveal() {
				t.Errorf("adopted config lost its container, folder or password (container %q)", gc.GameContainer)
			}
			if len(gc.Images) != 0 || gc.HostDataDir != "" {
				t.Error("adopted mode was given image pins or a host data directory")
			}
		} else {
			if gc.GameContainer != "" || gc.GameDir != "" {
				t.Error("managed mode was given a game container, which turns adopted mode on")
			}
			if len(gc.Images) != len(gameImages) || gc.HostDataDir != "/srv/foreman" {
				t.Error("managed mode lost its images or host data directory")
			}
		}
		if _, err := game.New(gc); err != nil {
			t.Errorf("%s: game.New: %v", mode, err)
		}
	}
}
