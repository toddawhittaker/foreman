// Command foreman runs the Foreman web app.
//
//	foreman [serve]       serve the web app (the default)
//	foreman healthcheck   exit 0 when the local server answers its health path
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/toddawhittaker/foreman/internal/releases"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
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

// serve starts in a fixed order: data directory check, host path, database,
// world recovery, game recovery, the game monitor, then the server. It
// stops in the reverse order.
func serve(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := datadir.Probe(cfg.DataDir, cfg.GameUID, cfg.GameGID); err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	// Making the client contacts nothing, so an unreachable Docker never
	// stops Foreman from serving pages.
	dockerClient, err := docker.New(cfg.DockerHost)
	if err != nil {
		return err
	}
	hostDir, err := datadir.HostDir(ctx, cfg.DataDir, cfg.HostDataDir, dockerClient, datadir.MountinfoPath, hostname)
	if err != nil {
		return err
	}
	slog.Info("data directory", "container_path", cfg.DataDir, "host_path", hostDir)
	st, err := store.Open(ctx, datadir.Database(cfg.DataDir))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	worlds, err := world.NewManager(world.Dirs{
		Worlds:    datadir.Worlds(cfg.DataDir),
		Snapshots: datadir.Snapshots(cfg.DataDir),
		Imports:   datadir.Imports(cfg.DataDir),
	}, st, nil)
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

	handler, err := web.NewHandler(web.Deps{Config: cfg, Store: st, Worlds: worlds, Game: games, Releases: releases.New(), Previewer: games, HostDataDir: hostDir, Log: slog.Default()})
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
	slog.Info("listening", "addr", cfg.Listen)
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

// gameConfig fills the controller's settings.
func gameConfig(cfg *config.Config, d *docker.Client, worlds *world.Manager, st *store.Store, hostDir, selfID string) game.Config {
	return game.Config{
		Docker: d, Worlds: worlds, Audit: st, Switches: st,
		HostDataDir: hostDir, GameUID: cfg.GameUID, GameGID: cfg.GameGID,
		Network:    cfg.GameNetwork,
		DockerHost: cfg.DockerHost, TrustedProxies: cfg.TrustedProxies,
		SelfID: selfID, Log: slog.Default(),
	}
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
