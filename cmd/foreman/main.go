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
	"syscall"
	"time"

	"git.thewhittakers.org/todd/foreman/internal/web"
)

const defaultListen = ":8080"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(listenAddr())
	case "healthcheck":
		err = healthcheck(listenAddr())
	default:
		err = fmt.Errorf("unknown command %q; use serve or healthcheck", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "foreman:", err)
		os.Exit(1)
	}
}

func listenAddr() string {
	if v := os.Getenv("FOREMAN_LISTEN"); v != "" {
		return v
	}
	return defaultListen
}

func serve(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           web.NewHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", addr)
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
