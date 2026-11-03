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
	"sync"
	"syscall"
	"time"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/web"
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

// serve starts in a fixed order: data directory check, host path (managed
// mode only), database, then the server.
func serve(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := datadir.Probe(cfg.DataDir, cfg.GameUID, cfg.GameGID); err != nil {
		return err
	}
	if cfg.Mode == config.ModeManaged {
		hostDir, err := resolveHostDir(ctx, cfg)
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

	handler, err := web.NewHandler(web.Deps{Config: cfg, Store: st, Log: slog.Default()})
	if err != nil {
		return err
	}
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
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// resolveHostDir finds the data directory's host path (SPEC 3.5). The Docker
// client is made only when the path has to be discovered.
func resolveHostDir(ctx context.Context, cfg *config.Config) (string, error) {
	var inspector datadir.Inspector
	if cfg.HostDataDir == "" {
		client, err := docker.New(cfg.DockerHost)
		if err != nil {
			return "", err
		}
		inspector = client
	}
	hostname, _ := os.Hostname()
	return datadir.HostDir(ctx, cfg.DataDir, cfg.HostDataDir, inspector, datadir.MountinfoPath, hostname)
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
