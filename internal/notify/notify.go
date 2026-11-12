// Package notify sends Foreman's events to webhooks (SPEC 8.10 to 8.12) and
// keeps their settings, with each address sealed (SPEC 14.8). It is a leaf
// package: it does not import the game controller, which is handed a
// Notifier and calls Emit.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

// QueueSize is how many events wait to be sent. When it is full, a new
// event is dropped and logged, never waited for (SPEC 8.12).
const QueueSize = 100

// Errors from the settings methods.
var (
	ErrNoSecretKey = errors.New("FOREMAN_SECRET_KEY is not set, so webhooks cannot be stored")
	ErrNoWebhook   = store.ErrNoWebhook
	ErrNoEvents    = errors.New("a webhook needs at least one event")
	ErrBadPreset   = errors.New("unknown webhook preset")
	ErrBadLanguage = errors.New("unknown language")
)

// Rows is the part of the app database this package uses.
type Rows interface {
	AddWebhook(ctx context.Context, w store.Webhook, seal store.SealFunc, now time.Time) (int64, error)
	UpdateWebhook(ctx context.Context, w store.Webhook, sealedURL []byte, now time.Time) error
	Webhook(ctx context.Context, id int64) (store.Webhook, error)
	ListWebhooks(ctx context.Context) ([]store.Webhook, error)
	DeleteWebhook(ctx context.Context, id int64) error
}

// Notifier keeps the webhooks and delivers events to them.
type Notifier struct {
	rows  Rows
	box   *secret.Box
	log   *slog.Logger
	now   func() time.Time
	queue chan Message
	// client follows no redirect (SPEC 14.10); each attempt has its own
	// timeout, and backoff holds the wait before each retry.
	client  *http.Client
	timeout time.Duration
	backoff []time.Duration
}

// New builds a Notifier. key is FOREMAN_SECRET_KEY, empty when unset: then
// no webhook can be stored or read. Call Run to start delivering.
func New(rows Rows, key secret.Secret, log *slog.Logger) (*Notifier, error) {
	n := &Notifier{
		rows:  rows,
		log:   log,
		now:   time.Now,
		queue: make(chan Message, QueueSize),
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		timeout: 5 * time.Second,
		backoff: []time.Duration{time.Second, 5 * time.Second},
	}
	if key.Reveal() != "" {
		box, err := secret.NewBox(key)
		if err != nil {
			return nil, err
		}
		n.box = box
	}
	return n, nil
}

// CanStore reports whether webhooks can be kept: a secret key is set.
func (n *Notifier) CanStore() bool { return n != nil && n.box != nil }

// Webhook is a webhook as a page may show it: never its address.
type Webhook struct {
	ID     int64
	Preset Preset
	Host   string
	Lang   i18n.Locale
	off    map[Event]bool
}

// On reports whether the webhook is sent e.
func (w Webhook) On(e Event) bool { return !w.off[e] }

// OnEvents lists the events the webhook is sent, in page order.
func (w Webhook) OnEvents() []Event {
	var out []Event
	for _, e := range Events {
		if w.On(e) {
			out = append(out, e)
		}
	}
	return out
}

// AllOn reports whether every event is on.
func (w Webhook) AllOn() bool { return len(w.OnEvents()) == len(Events) }

func fromRow(r store.Webhook) Webhook {
	loc, _ := i18n.Parse(r.Lang)
	return Webhook{ID: r.ID, Preset: Preset(r.Preset), Host: r.Host, Lang: loc, off: parseEventsOff(r.EventsOff)}
}

// Setup is what an admin enters for a webhook. On an edit, an empty URL
// keeps the stored address.
type Setup struct {
	Preset Preset
	URL    secret.Secret
	Events map[Event]bool
	Lang   i18n.Locale
}

func (n *Notifier) check(s Setup) error {
	switch {
	case !n.CanStore():
		return ErrNoSecretKey
	case !s.Preset.Known():
		return ErrBadPreset
	}
	if _, ok := i18n.Parse(string(s.Lang)); !ok {
		return ErrBadLanguage
	}
	for _, e := range Events {
		if s.Events[e] {
			return nil
		}
	}
	return ErrNoEvents
}

// rowName is the sealed address's authenticated data, so a sealed value
// cannot be moved to another row.
func rowName(id int64) string { return "webhook-" + strconv.FormatInt(id, 10) }

// List returns every webhook. Without a key there are none to show, since
// no address can be read.
func (n *Notifier) List(ctx context.Context) ([]Webhook, error) {
	if !n.CanStore() {
		return nil, nil
	}
	rows, err := n.rows.ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Webhook, len(rows))
	for i, r := range rows {
		out[i] = fromRow(r)
	}
	return out, nil
}

// Get returns one webhook, or ErrNoWebhook.
func (n *Notifier) Get(ctx context.Context, id int64) (Webhook, error) {
	if !n.CanStore() {
		return Webhook{}, ErrNoSecretKey
	}
	r, err := n.rows.Webhook(ctx, id)
	if err != nil {
		return Webhook{}, err
	}
	return fromRow(r), nil
}

// Add stores a new webhook with its address sealed. The error is one of
// this package's, a CheckURL error, or the database failing; none holds
// the address.
func (n *Notifier) Add(ctx context.Context, s Setup, by string) (Webhook, error) {
	if err := n.check(s); err != nil {
		return Webhook{}, err
	}
	raw := s.URL.Reveal()
	host, err := CheckURL(s.Preset, raw)
	if err != nil {
		return Webhook{}, err
	}
	row := store.Webhook{Preset: string(s.Preset), Host: host, EventsOff: eventsOff(s.Events), Lang: string(s.Lang), CreatedBy: by}
	id, err := n.rows.AddWebhook(ctx, row, func(id int64) ([]byte, error) {
		return n.box.Seal(rowName(id), []byte(raw))
	}, n.now())
	if err != nil {
		return Webhook{}, err
	}
	row.ID = id
	return fromRow(row), nil
}

// Update changes a webhook. With an empty URL the address, preset and host
// stay as they were.
func (n *Notifier) Update(ctx context.Context, id int64, s Setup, by string) (Webhook, error) {
	if err := n.check(s); err != nil {
		return Webhook{}, err
	}
	old, err := n.rows.Webhook(ctx, id)
	if err != nil {
		return Webhook{}, err
	}
	row := store.Webhook{ID: id, Preset: old.Preset, Host: old.Host, EventsOff: eventsOff(s.Events), Lang: string(s.Lang), UpdatedBy: by}
	var sealed []byte
	if raw := s.URL.Reveal(); raw != "" {
		host, err := CheckURL(s.Preset, raw)
		if err != nil {
			return Webhook{}, err
		}
		if sealed, err = n.box.Seal(rowName(id), []byte(raw)); err != nil {
			return Webhook{}, fmt.Errorf("seal the webhook address: %w", err)
		}
		row.Preset, row.Host = string(s.Preset), host
	}
	if err := n.rows.UpdateWebhook(ctx, row, sealed, n.now()); err != nil {
		return Webhook{}, err
	}
	return fromRow(row), nil
}

// Delete removes a webhook.
func (n *Notifier) Delete(ctx context.Context, id int64) error {
	if !n.CanStore() {
		return ErrNoSecretKey
	}
	return n.rows.DeleteWebhook(ctx, id)
}

// Emit queues m to be sent and returns at once. It never blocks: when the
// queue is full, m is dropped and logged (SPEC 8.12). A nil Notifier drops
// everything, so a caller need not check.
func (n *Notifier) Emit(m Message) {
	if n == nil {
		return
	}
	if m.Time.IsZero() {
		m.Time = n.now()
	}
	select {
	case n.queue <- m:
	default:
		n.log.Warn("notification dropped: too many are waiting to be sent", "event", string(m.Event), "world", m.World.ID)
	}
}

// Run delivers queued events until ctx ends. Events still queued then are
// not sent, and none is kept across a restart.
func (n *Notifier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-n.queue:
			n.deliver(ctx, m)
		}
	}
}
