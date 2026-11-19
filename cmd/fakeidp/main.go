// Command fakeidp is a fake OpenID Connect identity provider for the
// end-to-end tests (SPEC 12.7). It is not part of the image. Foreman is
// started with FOREMAN_OIDC_ISSUER set to the issuer, the fake's client id
// and secret, and SSL_CERT_FILE set to the test certificate it writes into
// -dir. It prints one line of JSON,
//
//	{"issuer": "https://127.0.0.1:PORT", "ca": "...pem", "control": "http://127.0.0.1:PORT",
//	 "client_id": "...", "client_secret": "...", "group": "..."}
//
// once ready, and stops on SIGINT or SIGTERM. The client id and secret are
// the fake's own made-up credentials. The control hooks are listed at
// controlHandler.
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
	"syscall"
	"time"

	"github.com/toddawhittaker/foreman/cmd/fakeidp/idp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeidp:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "", "existing folder for the test certificate file")
	flag.Parse()
	if *dir == "" {
		return errors.New("-dir is required")
	}
	f, err := idp.Start(*dir, nil)
	if err != nil {
		return err
	}
	defer f.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	control := &http.Server{Handler: controlHandler(f), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = control.Serve(ln) }()
	defer func() { _ = control.Close() }()

	ready := map[string]string{
		"issuer": f.URL, "ca": f.CAFile, "control": "http://" + ln.Addr().String(),
		"client_id": idp.ClientID, "client_secret": idp.ClientSecret, "group": idp.Group,
	}
	if err := json.NewEncoder(os.Stdout).Encode(ready); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

// controlHandler serves the test hooks, all JSON:
//
//	GET  /state  {"authorizes": [...], "tokens": N}: the authorize requests
//	             seen and the tokens issued
//	POST /next   the claims of every sign-in from now on, as idp.Claims;
//	             fields left out take the default user's values
func controlHandler(f *idp.IdP) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"authorizes": f.Authorizes(), "tokens": f.Tokens()})
	})
	mux.HandleFunc("POST /next", func(w http.ResponseWriter, r *http.Request) {
		c := idp.DefaultClaims()
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&c); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.SetNext(c)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
