// Package factorioaccount finds the factorio.com account used for mod
// downloads (SPEC 9.11): the environment first, else an account an admin
// entered, sealed in the app database. Neither value is ever returned
// except as a secret.Secret, and no error carries one (SPEC 14.8).
package factorioaccount

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

// RowName is the stored_secrets row that holds the account. It is also the
// authenticated data of the sealed value.
const RowName = "factorio-account"

// Account is a factorio.com username and token.
type Account struct {
	Username, Token secret.Secret
}

// Source says where the account came from.
type Source int

// The places an account can come from.
const (
	None       Source = iota // no account anywhere
	Env                      // the environment; the page cannot change it
	Stored                   // entered in the UI
	Unreadable               // a stored row exists but cannot be opened
)

// Meta says when a stored account was saved and by whom. It is zero for the
// other sources.
type Meta struct {
	UpdatedAt time.Time
	UpdatedBy string
}

// Errors from Set and Remove.
var (
	ErrFromEnvironment = errors.New("the account is set by environment variables; remove them to change it here")
	ErrNoSecretKey     = errors.New("FOREMAN_SECRET_KEY is not set, so an account cannot be stored")
	ErrIncomplete      = errors.New("both a username and a token are needed")
)

// Rows is the part of the app database this package uses.
type Rows interface {
	PutSecret(ctx context.Context, name string, sealed []byte, byUser string, now time.Time) error
	GetSecret(ctx context.Context, name string) (sealed []byte, updatedAt time.Time, updatedBy string, err error)
	DeleteSecret(ctx context.Context, name string) error
}

// Service finds, stores and removes the account.
type Service struct {
	env  Account // both set, or both empty
	box  *secret.Box
	rows Rows
	now  func() time.Time
}

// New builds a Service. env is the account from the environment (both
// values or neither). key is FOREMAN_SECRET_KEY, empty when unset: then
// nothing can be stored, and a stored row cannot be opened.
func New(env Account, key secret.Secret, rows Rows) (*Service, error) {
	s := &Service{env: env, rows: rows, now: time.Now}
	if key.Reveal() != "" {
		box, err := secret.NewBox(key)
		if err != nil {
			return nil, err
		}
		s.box = box
	}
	return s, nil
}

// CanStore reports whether an account can be stored: a secret key is set.
func (s *Service) CanStore() bool { return s.box != nil }

// fromEnv reports whether the environment holds the account.
func (s *Service) fromEnv() bool {
	return s.env.Username.Reveal() != "" && s.env.Token.Reveal() != ""
}

// wire is the sealed form. secret.Secret redacts itself when encoded, so
// the plain strings live here only for the moment of sealing.
type wire struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

// Get returns the account and where it came from. A stored row that cannot
// be opened (the key changed or was lost, or the row was changed) is
// Unreadable, not an error: the caller asks for the account again. The error
// is for the database failing.
func (s *Service) Get(ctx context.Context) (Account, Source, Meta, error) {
	if s.fromEnv() {
		return s.env, Env, Meta{}, nil
	}
	sealed, at, by, err := s.rows.GetSecret(ctx, RowName)
	if errors.Is(err, store.ErrNoSecret) {
		return Account{}, None, Meta{}, nil
	}
	if err != nil {
		return Account{}, None, Meta{}, fmt.Errorf("read the factorio.com account: %w", err)
	}
	meta := Meta{UpdatedAt: at, UpdatedBy: by}
	if s.box == nil {
		return Account{}, Unreadable, meta, nil
	}
	plain, err := s.box.Open(RowName, sealed)
	if err != nil {
		return Account{}, Unreadable, meta, nil
	}
	var w wire
	if json.Unmarshal(plain, &w) != nil || w.Username == "" || w.Token == "" {
		return Account{}, Unreadable, meta, nil
	}
	return Account{Username: secret.New(w.Username), Token: secret.New(w.Token)}, Stored, meta, nil
}

// Set stores the account, sealed, recording who saved it. The token is not
// checked against factorio.com.
func (s *Service) Set(ctx context.Context, acct Account, by string) error {
	if err := s.canChange(); err != nil {
		return err
	}
	if acct.Username.Reveal() == "" || acct.Token.Reveal() == "" {
		return ErrIncomplete
	}
	plain, err := json.Marshal(wire{Username: acct.Username.Reveal(), Token: acct.Token.Reveal()})
	if err != nil {
		return errors.New("encode the factorio.com account")
	}
	sealed, err := s.box.Seal(RowName, plain)
	if err != nil {
		return fmt.Errorf("seal the factorio.com account: %w", err)
	}
	if err := s.rows.PutSecret(ctx, RowName, sealed, by, s.now()); err != nil {
		return fmt.Errorf("store the factorio.com account: %w", err)
	}
	return nil
}

// Remove deletes the stored account.
func (s *Service) Remove(ctx context.Context) error {
	if err := s.canChange(); err != nil {
		return err
	}
	if err := s.rows.DeleteSecret(ctx, RowName); err != nil {
		return fmt.Errorf("remove the factorio.com account: %w", err)
	}
	return nil
}

func (s *Service) canChange() error {
	switch {
	case s.fromEnv():
		return ErrFromEnvironment
	case s.box == nil:
		return ErrNoSecretKey
	}
	return nil
}
