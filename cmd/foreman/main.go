// Command foreman runs the Foreman web app.
//
//	foreman [serve]       serve the web app (the default)
//	foreman healthcheck   exit 0 when the local server answers its health path
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/web"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The pinned game images, one per supported game version, by tag and
// digest (SPEC 10). They must equal the Makefile's GAME_IMAGE_* values,
// which the real-game tests use; a test compares them.
const (
	gameImage11 = "factoriotools/factorio:1.1.110@sha256:6fb9b7bfb12fb51bf668fce7e29cb1e98053f248201e64dfee870ef73b2f31cb"
	gameImage20 = "factoriotools/factorio:2.0.77@sha256:312341f29987b5a6b11f00ba8240192f7564936af9913384d13f7f4c535708ef"
	gameImage21 = "factoriotools/factorio:2.1.24@sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28"
)

var gameImages = map[string]string{"1.1": gameImage11, "2.0": gameImage20, "2.1": gameImage21}

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		cfg, err := config.Load(os.LookupEnv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "foreman: the configuration has problems:\n%v\n", err)
			os.Exit(2)
		}
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
		if err := serve(cfg); err != nil {
			slog.Error("foreman stopped", "error", err)
			os.Exit(1)
		}
	case "healthcheck":
		if err := healthcheck(listenAddr()); err != nil {
			fmt.Fprintln(os.Stderr, "foreman:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "foreman: unknown command %q; use serve or healthcheck\n", cmd)
		os.Exit(1)
	}
}

func listenAddr() string {
	if v := os.Getenv("FOREMAN_LISTEN"); v != "" {
		return v
	}
	return config.DefaultListen
}

// serve starts in a fixed order: folder checks, host path (managed mode
// only), database, world recovery, game recovery, the game monitor, then
// the server. It stops in the reverse order.
func serve(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := probeFolders(cfg); err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	// Making the client contacts nothing, so an unreachable Docker never
	// stops Foreman from serving pages.
	dockerClient, err := newDockerClient(cfg)
	if err != nil {
		return err
	}
	var hostDir string
	if cfg.Mode == config.ModeManaged {
		hostDir, err = datadir.HostDir(ctx, cfg.DataDir, cfg.HostDataDir, dockerClient, datadir.MountinfoPath, hostname)
		if err != nil {
			return err
		}
		slog.Info("data directory", "container_path", cfg.DataDir, "host_path", hostDir)
	}
	st, err := store.Open(ctx, datadir.Database(cfg.DataDir))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	worlds, err := world.NewManager(worldDirs(cfg), st, nil)
	if err != nil {
		return err
	}
	// Recovery finishes before the first request, so no page sees half-done work.
	if err := worlds.Recover(slog.Default()); err != nil {
		return err
	}

	games, err := game.New(gameConfig(cfg, dockerClient, worlds, st, hostDir, docker.SelfID(docker.MountinfoPath, hostname)))
	if err != nil {
		return err
	}
	// Recovery reports what a stop left part way and never starts or stops
	// a game (SPEC 3.8). It finishes before the monitor and the first request.
	if err := games.Recover(ctx); err != nil {
		return fmt.Errorf("foreman could not recover from its last stop, so it did not start: %w", err)
	}
	// The monitor writes to the store, so it stops before the store closes.
	// Stopping it leaves every game running (SPEC 3.8).
	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	var monitoring sync.WaitGroup
	monitoring.Go(func() { games.Run(monitorCtx) })
	defer func() { stopMonitor(); monitoring.Wait() }()

	handler, err := web.NewHandler(web.Deps{Config: cfg, Store: st, Worlds: worlds, Game: games, Log: slog.Default()})
	if err != nil {
		return err
	}
	// Live pages and actions in flight end before the monitor stops.
	defer handler.Close()
	srv := web.NewHTTPServer(cfg.Listen, handler)

	sweepCtx, stopSweep := context.WithCancel(ctx)
	var sweeping sync.WaitGroup
	sweeping.Add(1)
	go func() {
		defer sweeping.Done()
		web.SweepSessions(sweepCtx, st, cfg.SessionIdle, slog.Default())
	}()
	// The sweeper uses the store, so it must stop before the store closes.
	defer func() { stopSweep(); sweeping.Wait() }()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", cfg.Listen, "mode", cfg.Mode)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// Live pages never finish on their own, so they end before the server
	// waits for its requests to finish.
	handler.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// probeFolders checks the data directory and, in adopted mode, the game
// folder (SPEC 3.6).
func probeFolders(cfg *config.Config) error {
	if err := datadir.Probe(cfg.DataDir, cfg.GameUID, cfg.GameGID); err != nil {
		return err
	}
	if cfg.Mode != config.ModeAdopted {
		return nil
	}
	if err := datadir.Probe(cfg.GameDir, cfg.GameUID, cfg.GameGID); err != nil {
		return fmt.Errorf("the game folder (FOREMAN_GAME_DIR) failed the same check as the data directory: %w", err)
	}
	return nil
}

// worldDirs puts the worlds in the game folder in adopted mode, so the
// game can reach them, and everything else in the data directory
// (SPEC 3.2, 5.2).
func worldDirs(cfg *config.Config) world.Dirs {
	dirs := world.Dirs{
		Worlds:    datadir.Worlds(cfg.DataDir),
		Snapshots: datadir.Snapshots(cfg.DataDir),
		Imports:   datadir.Imports(cfg.DataDir),
	}
	if cfg.Mode == config.ModeAdopted {
		dirs.Worlds = filepath.Join(cfg.GameDir, "worlds")
	}
	return dirs
}

// newDockerClient gives adopted mode the client that refuses to create or
// remove containers or touch images (SPEC 4.11, 14.3).
func newDockerClient(cfg *config.Config) (*docker.Client, error) {
	if cfg.Mode == config.ModeAdopted {
		return docker.NewAdopted(cfg.DockerHost)
	}
	return docker.New(cfg.DockerHost)
}

// gameConfig fills the controller's settings. Giving it a game container
// is what turns adopted mode on (SPEC 4.7); that mode needs no image pins.
func gameConfig(cfg *config.Config, d *docker.Client, worlds *world.Manager, st *store.Store, hostDir, selfID string) game.Config {
	gc := game.Config{
		Docker: d, Worlds: worlds, Audit: st, Switches: st,
		GameUID: cfg.GameUID, GameGID: cfg.GameGID,
		Network: cfg.GameNetwork, DockerHost: cfg.DockerHost, TrustedProxies: cfg.TrustedProxies,
		SelfID: selfID, Log: slog.Default(),
	}
	if cfg.Mode == config.ModeAdopted {
		gc.GameContainer, gc.GameDir, gc.RCONPassword = cfg.GameContainer, cfg.GameDir, cfg.RCONPassword
	} else {
		gc.HostDataDir, gc.Images = hostDir, gameImages
	}
	return gc
}

// healthcheck exists because the runtime image has no shell or curl (STACK 10).
func healthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + web.HealthPath)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %s", resp.Status)
	}
	return nil
}
