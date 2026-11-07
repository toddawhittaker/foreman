package factorioaccount

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

const (
	keyA  = "keykeykeykeykeykeykeykeykeykeykey"
	keyB  = "otherotherotherotherotherotherother"
	user  = "portal-user-name"
	token = "portaltokenportaltoken"
)

var now = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newService(t *testing.T, env Account, key string, rows Rows) *Service {
	t.Helper()
	svc, err := New(env, secret.New(key), rows)
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now }
	return svc
}

func mine() Account { return Account{Username: secret.New(user), Token: secret.New(token)} }

func envAccount() Account {
	return Account{Username: secret.New("env-user"), Token: secret.New("env-token")}
}

func TestEmptyIsNone(t *testing.T) {
	svc := newService(t, Account{}, keyA, openStore(t))
	acct, src, _, err := svc.Get(context.Background())
	if err != nil || src != None || acct.Token.Reveal() != "" {
		t.Errorf("Get = %v, %v, %v", acct, src, err)
	}
}

func TestStoredRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	svc := newService(t, Account{}, keyA, st)
	if err := svc.Set(ctx, mine(), "todd"); err != nil {
		t.Fatal(err)
	}
	acct, src, meta, err := svc.Get(ctx)
	if err != nil || src != Stored {
		t.Fatalf("Get = %v, %v, %v", acct, src, err)
	}
	if acct.Username.Reveal() != user || acct.Token.Reveal() != token {
		t.Error("the account did not round trip")
	}
	if meta.UpdatedBy != "todd" || !meta.UpdatedAt.Equal(now) {
		t.Errorf("meta = %+v", meta)
	}

	// The database holds neither value in the clear.
	sealed, _, _, _ := st.GetSecret(ctx, RowName)
	if bytes.Contains(sealed, []byte(user)) || bytes.Contains(sealed, []byte(token)) {
		t.Error("the stored row holds a value in the clear")
	}

	if err := svc.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if _, src, _, _ := svc.Get(ctx); src != None {
		t.Errorf("source after Remove = %v, want None", src)
	}
}

func TestEnvironmentWinsOverStored(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	if err := newService(t, Account{}, keyA, st).Set(ctx, mine(), "todd"); err != nil {
		t.Fatal(err)
	}
	svc := newService(t, envAccount(), keyA, st)
	acct, src, _, err := svc.Get(ctx)
	if err != nil || src != Env || acct.Username.Reveal() != "env-user" {
		t.Errorf("Get = %v, %v, %v; want the environment account", acct, src, err)
	}
	if err := svc.Set(ctx, mine(), "todd"); !errors.Is(err, ErrFromEnvironment) {
		t.Errorf("Set err = %v, want ErrFromEnvironment", err)
	}
	if err := svc.Remove(ctx); !errors.Is(err, ErrFromEnvironment) {
		t.Errorf("Remove err = %v, want ErrFromEnvironment", err)
	}
	if _, _, _, err := st.GetSecret(ctx, RowName); err != nil {
		t.Errorf("the stored row was touched: %v", err)
	}
}

func TestWithoutKeyNothingIsStored(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	svc, err := New(Account{}, secret.Secret{}, st)
	if err != nil {
		t.Fatal(err)
	}
	if svc.CanStore() {
		t.Error("CanStore with no key")
	}
	if err := svc.Set(ctx, mine(), "todd"); !errors.Is(err, ErrNoSecretKey) {
		t.Errorf("Set err = %v, want ErrNoSecretKey", err)
	}
	if err := svc.Remove(ctx); !errors.Is(err, ErrNoSecretKey) {
		t.Errorf("Remove err = %v, want ErrNoSecretKey", err)
	}
	if _, _, _, err := st.GetSecret(ctx, RowName); !errors.Is(err, store.ErrNoSecret) {
		t.Errorf("a row was written: %v", err)
	}
	if !newService(t, Account{}, keyA, st).CanStore() {
		t.Error("CanStore false with a key")
	}
}

func TestShortKeyRefused(t *testing.T) {
	if _, err := New(Account{}, secret.New("too short"), openStore(t)); err == nil {
		t.Error("a short key was accepted")
	}
}

func TestIncompleteAccountRefused(t *testing.T) {
	svc := newService(t, Account{}, keyA, openStore(t))
	for _, a := range []Account{{}, {Username: secret.New(user)}, {Token: secret.New(token)}} {
		if err := svc.Set(context.Background(), a, "todd"); !errors.Is(err, ErrIncomplete) {
			t.Errorf("Set(%v) err = %v, want ErrIncomplete", a, err)
		}
	}
}

func TestUnreadableRows(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	if err := newService(t, Account{}, keyA, st).Set(ctx, mine(), "todd"); err != nil {
		t.Fatal(err)
	}
	sealed, _, _, _ := st.GetSecret(ctx, RowName)

	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 1
	box, _ := secret.NewBox(secret.New(keyA))
	moved, _ := box.Seal("some-other-row", []byte(`{"username":"u","token":"t"}`))
	notJSON, _ := box.Seal(RowName, []byte("not json"))
	noToken, _ := box.Seal(RowName, []byte(`{"username":"u","token":""}`))

	tests := []struct {
		name   string
		key    string
		sealed []byte
	}{
		{"key changed", keyB, sealed},
		{"key lost", "", sealed},
		{"tampered", keyA, flipped},
		{"moved from another row", keyA, moved},
		{"not json", keyA, notJSON},
		{"missing token", keyA, noToken},
		{"unknown version", keyA, append([]byte{0x02}, sealed[1:]...)},
		{"empty", keyA, []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openStore(t)
			if err := st.PutSecret(ctx, RowName, tt.sealed, "todd", now); err != nil {
				t.Fatal(err)
			}
			svc, err := New(Account{}, secret.New(tt.key), st)
			if err != nil {
				t.Fatal(err)
			}
			acct, src, meta, err := svc.Get(ctx)
			if err != nil || src != Unreadable || acct.Token.Reveal() != "" || acct.Username.Reveal() != "" {
				t.Errorf("Get = %v, %v, %v; want Unreadable", acct, src, err)
			}
			if meta.UpdatedBy != "todd" {
				t.Errorf("meta = %+v", meta)
			}
		})
	}

	// Entering the account again repairs an unreadable row.
	svc := newService(t, Account{}, keyB, st)
	if _, src, _, _ := svc.Get(ctx); src != Unreadable {
		t.Fatalf("source = %v, want Unreadable", src)
	}
	if err := svc.Set(ctx, mine(), "ann"); err != nil {
		t.Fatal(err)
	}
	if _, src, _, _ := svc.Get(ctx); src != Stored {
		t.Errorf("source after Set = %v, want Stored", src)
	}
}

// failingRows fails every call with an error that is safe to show.
type failingRows struct{}

var errDB = errors.New("database is down")

func (failingRows) PutSecret(context.Context, string, []byte, string, time.Time) error { return errDB }
func (failingRows) GetSecret(context.Context, string) ([]byte, time.Time, string, error) {
	return nil, time.Time{}, "", errDB
}
func (failingRows) DeleteSecret(context.Context, string) error { return errDB }

func TestDatabaseErrorsAreReturned(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, Account{}, keyA, failingRows{})
	if _, _, _, err := svc.Get(ctx); !errors.Is(err, errDB) {
		t.Errorf("Get err = %v", err)
	}
	if err := svc.Set(ctx, mine(), "todd"); !errors.Is(err, errDB) {
		t.Errorf("Set err = %v", err)
	}
	if err := svc.Remove(ctx); !errors.Is(err, errDB) {
		t.Errorf("Remove err = %v", err)
	}
}

// Nothing the service returns, formats or logs may carry either value.
func TestNoSecretEscapes(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	svc := newService(t, Account{}, keyA, st)

	var outputs []string
	collect := func(err error) {
		if err != nil {
			outputs = append(outputs, err.Error(), fmt.Sprintf("%v %+v %#v", err, err, err))
		}
	}
	collect(svc.Set(ctx, Account{Username: secret.New(user)}, "todd"))
	collect(svc.Set(ctx, mine(), "todd"))
	collect(newService(t, envAccount(), keyA, st).Set(ctx, mine(), "todd"))
	collect(newService(t, Account{}, "", st).Set(ctx, mine(), "todd"))
	collect(newService(t, Account{}, keyA, failingRows{}).Set(ctx, mine(), "todd"))

	acct, _, _, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs,
		fmt.Sprintf("%v|%+v|%#v|%s|%q|%d|%x", acct, acct, acct, acct, acct, acct, acct),
		fmt.Sprintf("%v|%+v|%#v", &acct, &acct, &acct),
	)

	var logs bytes.Buffer
	for _, h := range []slog.Handler{slog.NewTextHandler(&logs, nil), slog.NewJSONHandler(&logs, nil)} {
		slog.New(h).Info("account", "account", acct, "token", acct.Token, "user", acct.Username)
	}
	outputs = append(outputs, logs.String())

	if !strings.Contains(logs.String(), secret.Redacted) {
		t.Errorf("log line = %q, want the redaction marker", logs.String())
	}
	for _, out := range outputs {
		for _, v := range []string{user, token, keyA} {
			if strings.Contains(out, v) {
				t.Errorf("%q leaks a secret", out)
			}
		}
	}
}
