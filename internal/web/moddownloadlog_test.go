package web

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/secret"
)

// SPEC 14.10: every failed mod download is logged with the mod, its version
// and the reason, and never the account's username or token.
func TestFailedModDownloadsAreLoggedWithoutTheAccount(t *testing.T) {
	hn := newHarness(t, nil)
	acct, err := factorioaccount.New(factorioaccount.Account{
		Username: secret.New(modportaltest.Username), Token: secret.New(modportaltest.Token),
	}, secret.Secret{}, hn.store)
	if err != nil {
		t.Fatal(err)
	}
	portal := modportaltest.New(t)
	for _, tc := range []struct {
		name  string
		mode  modportaltest.Mode
		limit int64
		want  string
	}{
		{"checksum", modportaltest.WrongSHA1, 200 << 20, "checksum"},
		{"over the limit by its header", modportaltest.Normal, 10, "over the size limit"},
		{"refused", modportaltest.Unauthorized, 200 << 20, "refused"},
		{"portal error", modportaltest.ServerError, 200 << 20, "unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			served := portal.AddZip("fp-"+strings.ReplaceAll(tc.name, " ", "-"), "1.2.3")
			portal.SetMode(served, tc.mode)
			var logs bytes.Buffer
			f := accountFetcher{
				account: acct, portal: modportal.NewForTest(portal.URL),
				limit: func(context.Context) int64 { return tc.limit },
				log:   slog.New(slog.NewTextHandler(&logs, nil)),
			}
			dst, err := os.CreateTemp(t.TempDir(), "mod-*.zip")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = dst.Close() }()
			r := modportal.Release{Version: served.Version, FileName: served.FileName, DownloadURL: served.DownloadURL, SHA1: served.SHA1}
			if err := f.Download(context.Background(), r, dst); err == nil {
				t.Fatal("the download worked")
			}
			out := logs.String()
			for _, w := range []string{"mod=" + served.Name, "version=1.2.3", tc.want} {
				if !strings.Contains(out, w) {
					t.Errorf("log lacks %q:\n%s", w, out)
				}
			}
			for _, secretValue := range []string{modportaltest.Token, modportaltest.Username} {
				if strings.Contains(out, secretValue) {
					t.Errorf("log holds the account's %q:\n%s", secretValue, out)
				}
			}
		})
	}
}

// SPEC 14.10: text the portal supplied, here a file name, never reaches the
// log at more than a fixed length.
func TestFailedModDownloadLogCutsPortalText(t *testing.T) {
	hn := newHarness(t, nil)
	acct, err := factorioaccount.New(factorioaccount.Account{
		Username: secret.New(modportaltest.Username), Token: secret.New(modportaltest.Token),
	}, secret.Secret{}, hn.store)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	f := accountFetcher{
		account: acct, portal: modportal.NewForTest("http://127.0.0.1:1"),
		limit: func(context.Context) int64 { return 1 << 20 },
		log:   slog.New(slog.NewTextHandler(&logs, nil)),
	}
	dst, err := os.CreateTemp(t.TempDir(), "mod-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dst.Close() }()
	long := strings.Repeat("A", 100000)
	// The file name does not match the version, so the portal client refuses
	// it and its message quotes the name.
	r := modportal.Release{Version: "1.0.0", FileName: long + "_9.9.9.zip", DownloadURL: "/download/x"}
	if err := f.Download(context.Background(), r, dst); err == nil {
		t.Fatal("the download worked")
	}
	out := logs.String()
	if strings.Count(out, "A") == 0 {
		t.Fatalf("the log names nothing of the mod:\n%.300s", out)
	}
	if len(out) > 1500 {
		t.Errorf("the log line is %d bytes, want it cut:\n%.300s", len(out), out)
	}
}

// SPEC 14.10: an account problem is not a failed download, so this wrapper
// does not log it.
func TestAccountErrorsAreNotLoggedAsFailedDownloads(t *testing.T) {
	hn := newHarness(t, nil)
	acct, err := factorioaccount.New(factorioaccount.Account{}, secret.Secret{}, hn.store)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	f := accountFetcher{
		account: acct, portal: modportal.NewForTest("http://127.0.0.1:1"),
		limit: func(context.Context) int64 { return 1 << 20 },
		log:   slog.New(slog.NewTextHandler(&logs, nil)),
	}
	dst, err := os.CreateTemp(t.TempDir(), "mod-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dst.Close() }()
	r := modportal.Release{Version: "1.0.0", FileName: "fp-x_1.0.0.zip", DownloadURL: "/download/x"}
	if err := f.Download(context.Background(), r, dst); !errors.Is(err, game.ErrNoModFetcher) {
		t.Fatalf("error %v, want ErrNoModFetcher", err)
	}
	if logs.Len() != 0 {
		t.Errorf("the wrapper logged an account problem:\n%s", logs.String())
	}
}
