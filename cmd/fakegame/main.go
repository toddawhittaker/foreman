// Command fakegame is a fake Docker host with a fake Factorio game inside,
// for the end-to-end tests. It is built by `make e2e` and is not part of the
// image. It serves the Docker API on a Unix socket in the folder given by
// -dir, serves the test hooks (internal/fakegame, HookHandler) on a loopback
// TCP port, and prints one line of JSON, {"socket": "...", "hook":
// "http://127.0.0.1:PORT"}, once both are ready. It stops on SIGINT or
// SIGTERM.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/toddawhittaker/foreman/internal/fakegame"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakegame:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "", "existing folder for the Docker socket")
	images := flag.String("images", "", "comma-separated image references that are present at the start")
	loadDelay := flag.Duration("load-delay", 0, "time a start takes between loading the map and InGame")
	flag.Parse()
	if *dir == "" {
		return errors.New("-dir is required")
	}
	cfg := fakegame.Config{Dir: *dir, LoadDelay: *loadDelay}
	if *images != "" {
		cfg.Images = strings.Split(*images, ",")
	}

	game, err := fakegame.Start(cfg)
	if err != nil {
		return err
	}
	defer game.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: game.HookHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	ready := map[string]string{"socket": game.Socket(), "hook": "http://" + ln.Addr().String()}
	if err := json.NewEncoder(os.Stdout).Encode(ready); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}
