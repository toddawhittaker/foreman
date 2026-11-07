// Command fakeportal is a fake Factorio mod portal for the end-to-end tests.
// It is built by `make e2e` and is not part of the image. It is an HTTPS
// proxy: Foreman is started with HTTPS_PROXY set to the proxy and
// SSL_CERT_FILE set to the test certificate authority it writes into -dir,
// and its portal client then reaches the fake as mods.factorio.com. The
// proxy refuses a tunnel to any other host. It prints one line of JSON,
//
//	{"proxy": "http://127.0.0.1:PORT", "ca": "...pem", "control": "http://127.0.0.1:PORT",
//	 "username": "...", "token": "..."}
//
// once ready, and stops on SIGINT or SIGTERM. The username and token are the
// fake's own made-up credentials. The control hooks are listed at
// controlHandler.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeportal:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "", "existing folder for the test certificate authority file")
	flag.Parse()
	if *dir == "" {
		return errors.New("-dir is required")
	}
	t := &exitTB{}
	defer t.cleanup()
	f, err := Start(t, *dir)
	if err != nil {
		return err
	}
	defer f.Close()

	ready := map[string]string{
		"proxy": f.Proxy, "ca": f.CAFile, "control": f.Control,
		"username": modportaltest.Username, "token": modportaltest.Token,
	}
	if err := json.NewEncoder(os.Stdout).Encode(ready); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

// exitTB lets modportaltest, which takes a testing.TB, run outside a test.
// The embedded nil TB means any method not written here would panic.
type exitTB struct {
	testing.TB
	cleanups []func()
}

func (*exitTB) Helper() {}

func (*exitTB) Fatal(args ...any) {
	fmt.Fprintln(os.Stderr, "fakeportal:", fmt.Sprint(args...))
	os.Exit(1)
}

func (t *exitTB) Fatalf(format string, args ...any) { t.Fatal(fmt.Sprintf(format, args...)) }

func (t *exitTB) Cleanup(f func()) { t.cleanups = append(t.cleanups, f) }

func (t *exitTB) cleanup() {
	for i := len(t.cleanups) - 1; i >= 0; i-- {
		t.cleanups[i]()
	}
}
