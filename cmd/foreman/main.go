// Command foreman runs the Foreman web app.
//
//	foreman [serve]       serve the web app (the default)
//	foreman healthcheck   exit 0 when the local server answers its health path
//	foreman reset-password <username>
//	                      set a user's new password, read as one line from
//	                      standard input, and end their sessions
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/toddawhittaker/foreman/internal/releases"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/schedule"
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
	case "reset-password":
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, resetPasswordUsage)
			os.Exit(2)
		}
		if err := resetPasswordCommand(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "foreman:", err)
			os.Exit(1)
		}
		fmt.Printf("The password of %s is changed, and their sessions are ended.\n", os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "foreman: unknown command %q; use serve, healthcheck or reset-password\n", cmd)
		os.Exit(1)
	}
}

const resetPasswordUsage = `usage: foreman reset-password <username>

Reads the new password as one line from standard input and never prints it.
For example, from the host:

  docker exec -i foreman foreman reset-password alice < new-password.txt

The password needs at least 12 characters. Only a user who signs in with a
password can be reset. The user's sessions end, and the change is recorded
in the audit log as password-change by system.`

// resetPasswordCommand opens the database in the data directory and resets
// the user's password (SPEC 12.12).
func resetPasswordCommand(username string) error {
	dataDir := os.Getenv("FOREMAN_DATA_DIR")
	if dataDir == "" {
		dataDir = config.DefaultDataDir
	}
	ctx := context.Background()
	st, err := store.Open(ctx, datadir.Database(dataDir))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	return resetPassword(ctx, st, username, os.Stdin, time.Now())
}

// resetPassword reads one line from in as username's new password, stores
// its hash, ends the user's sessions and records password-change by
// system. A user without a password is refused: a password would let a
// proxy or single sign-on user past the proxy or identity provider.
func resetPassword(ctx context.Context, st *store.Store, username string, in io.Reader, now time.Time) error {
	u, found, err := st.UserByUsername(ctx, username)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("there is no user named %q", username)
	}
	if u.PasswordHash == "" {
		return fmt.Errorf("%s signs in through the proxy or single sign-on, so has no password to reset", u.Username)
	}
	// One byte past the longest password, so a longer one is refused, not cut.
	line, err := bufio.NewReader(io.LimitReader(in, auth.MaxPasswordBytes+2)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read the new password: %w", err)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if password == "" {
		return errors.New("no password was given on standard input")
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}
	if err := st.SetPasswordEndSessions(ctx, u.ID, hash); err != nil {
		return err
	}
	return st.AddAudit(ctx, store.AuditEntry{Time: now, Username: "system", Action: web.AuditPasswordChange, Detail: u.Username})
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

	portalAccount, err := factorioaccount.New(factorioaccount.Account{Username: cfg.FactorioUsername, Token: cfg.FactorioToken}, cfg.SecretKey, st)
	if err != nil {
		return err
	}
	// The notifier reads the store, so it stops before the store closes.
	// Events still queued then are not sent (SPEC 8.12).
	notifier, err := notify.New(st, cfg.SecretKey, slog.Default())
	if err != nil {
		return err
	}
	notifyCtx, stopNotify := context.WithCancel(context.Background())
	var notifying sync.WaitGroup
	notifying.Go(func() { notifier.Run(notifyCtx) })
	defer func() { stopNotify(); notifying.Wait() }()
	games.SetNotifier(notifier)
	// The schedule uses the controller, the notifier and the store, so it
	// stops first; a restart it began ends before Run returns.
	scheduler, err := schedule.New(ctx, schedule.Config{Game: games, Store: st, Active: activeWorld(st, worlds), Notifier: notifier, Log: slog.Default()})
	if err != nil {
		return err
	}
	scheduleCtx, stopSchedule := context.WithCancel(context.Background())
	var scheduling sync.WaitGroup
	scheduling.Go(func() { scheduler.Run(scheduleCtx) })
	defer func() { stopSchedule(); scheduling.Wait() }()
	favorites, err := world.NewFavorites(datadir.Favorites(cfg.DataDir), worlds, time.Now)
	if err != nil {
		return err
	}
	handler, err := web.NewHandler(web.Deps{Config: cfg, Store: st, Worlds: worlds, Favorites: favorites, Game: games, Releases: releases.New(), Previewer: games, Account: portalAccount, Portal: modportal.New(), Notifier: notifier, Scheduler: scheduler, HostDataDir: hostDir, Log: slog.Default()})
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
	// Token reminders go out once at start, so one that came due while
	// Foreman was down is not missed (SPEC 8.10).
	sweeping.Add(1)
	go func() {
		defer sweeping.Done()
		web.RemindTokens(sweepCtx, auth.NewAPITokens(st, time.Now), notifier, slog.Default())
	}()
	// The sweepers use the store, so they must stop before the store closes.
	defer func() { stopSweep(); sweeping.Wait() }()

	ln, err := listen(cfg.Listen)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
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

// listen binds before it logs, so the logged address is the real one even for
// port 0, and the e2e harness reads its port from that line.
func listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	slog.Info("listening", "addr", ln.Addr().String())
	return ln, nil
}

// activeWorld names the active world for the restart schedule.
func activeWorld(st *store.Store, worlds *world.Manager) schedule.ActiveWorld {
	return func(ctx context.Context) (string, string, bool, error) {
		id, ok, err := st.ActiveWorld(ctx)
		if err != nil || !ok {
			return "", "", false, err
		}
		w, err := worlds.Get(ctx, id)
		if err != nil {
			return "", "", false, err
		}
		return id, w.Name, true, nil
	}
}

// gameConfig fills the controller's settings.
func gameConfig(cfg *config.Config, d *docker.Client, worlds *world.Manager, st *store.Store, hostDir, selfID string) game.Config {
	return game.Config{
		Docker: d, Worlds: worlds, Audit: st, Switches: st,
		HostDataDir: hostDir, GameUID: cfg.GameUID, GameGID: cfg.GameGID,
		Network: cfg.GameNetwork, NetworkContainer: cfg.GameNetworkContainer,
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
