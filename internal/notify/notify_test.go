package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

const testKey = "keykeykeykeykeykeykeykeykeykeykey"

// token stands for the secret part of a webhook address; it must never
// appear in a log line, an error or a value a page could show.
const token = "never-shown-9f8e7d"

// syncBuffer is a log buffer that goroutines can write while a test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newTest builds a Notifier on a fresh store with short timings, trusting
// httptest's TLS certificate. It does not start Run.
func newTest(t *testing.T, key string) (*Notifier, *store.Store, *syncBuffer) {
	t.Helper()
	st := openStore(t)
	logs := &syncBuffer{}
	n, err := New(st, secret.New(key), slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatal(err)
	}
	tls := httptest.NewTLSServer(http.NotFoundHandler())
	n.client.Transport = tls.Client().Transport
	tls.Close()
	n.timeout = 200 * time.Millisecond
	n.backoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	return n, st, logs
}

// run starts delivery and stops it when the test ends.
func run(t *testing.T, n *Notifier) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { n.Run(ctx) })
	t.Cleanup(func() { cancel(); wg.Wait() })
}

// addRaw stores a webhook to any address, skipping CheckURL, so a test
// server on 127.0.0.1 can stand in for Discord.
func addRaw(t *testing.T, n *Notifier, st *store.Store, p Preset, raw string, lang string, off ...Event) int64 {
	t.Helper()
	on := map[Event]bool{}
	for _, e := range Events {
		on[e] = true
	}
	for _, e := range off {
		on[e] = false
	}
	id, err := st.AddWebhook(context.Background(), store.Webhook{Preset: string(p), Host: "127.0.0.1", EventsOff: eventsOff(on), Lang: lang, CreatedBy: "todd"},
		func(id int64) ([]byte, error) { return n.box.Seal(rowName(id), []byte(raw)) }, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// hits counts requests and keeps their bodies.
type hits struct {
	mu     sync.Mutex
	n      atomic.Int32
	bodies [][]byte
	heads  []http.Header
}

func (h *hits) record(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	h.bodies = append(h.bodies, b)
	h.heads = append(h.heads, r.Header.Clone())
	h.mu.Unlock()
	h.n.Add(1)
}

func server(t *testing.T, h *hits, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.record(r)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

var world = World{ID: "w-1", Name: "Main base"}

func TestDeliversAndRetriesAtMostTwice(t *testing.T) {
	n, st, logs := newTest(t, testKey)
	var h hits
	srv := server(t, &h, http.StatusInternalServerError)
	addRaw(t, n, st, Generic, srv.URL+"/hook/"+token, "en")
	run(t, n)
	n.Emit(Message{Event: Start, World: world})
	waitFor(t, "three attempts", func() bool { return strings.Count(logs.String(), "webhook delivery failed") == 3 })
	time.Sleep(100 * time.Millisecond)
	if got := h.n.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3 (one try and two retries)", got)
	}
	if !strings.Contains(logs.String(), "will_retry=false") {
		t.Errorf("the last failure does not say it gave up:\n%s", logs)
	}
}

func TestRetryStopsOnSuccess(t *testing.T) {
	n, st, _ := newTest(t, testKey)
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	addRaw(t, n, st, Generic, srv.URL+"/hook", "en")
	run(t, n)
	n.Emit(Message{Event: Stop, World: world})
	waitFor(t, "a second attempt", func() bool { return calls.Load() == 2 })
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

// A redirect is not followed, so the place it points to never hears the
// event, and repeating the request would only redirect again.
func TestNoRedirectIsFollowed(t *testing.T) {
	n, st, logs := newTest(t, testKey)
	var elsewhere hits
	other := server(t, &elsewhere, http.StatusOK)
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, other.URL+"/catch", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	addRaw(t, n, st, Generic, srv.URL+"/hook/"+token, "en")
	run(t, n)
	n.Emit(Message{Event: Crash, World: world})
	waitFor(t, "the failure to be logged", func() bool { return strings.Contains(logs.String(), "webhook delivery failed") })
	time.Sleep(100 * time.Millisecond)
	if got := elsewhere.n.Load(); got != 0 {
		t.Errorf("the redirect target got %d requests", got)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if !strings.Contains(logs.String(), "307") {
		t.Errorf("the log does not name the redirect:\n%s", logs)
	}
}

// Emit returns at once while a target hangs, and once the queue is full it
// drops and logs rather than waiting (SPEC 8.12).
func TestEmitNeverBlocks(t *testing.T) {
	n, st, logs := newTest(t, testKey)
	release := make(chan struct{})
	var hung atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hung.Add(1)
		<-release
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	n.timeout = time.Hour // the target hangs for as long as the test runs
	addRaw(t, n, st, Generic, srv.URL+"/hook", "en")
	run(t, n)

	n.Emit(Message{Event: PlayerJoin, World: world, Player: "ann"})
	waitFor(t, "the target to hang", func() bool { return hung.Load() == 1 })
	start := time.Now()
	for i := 0; i < QueueSize+50; i++ {
		n.Emit(Message{Event: PlayerLeave, World: world, Player: "ann"})
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("emitting %d events took %v", QueueSize+50, d)
	}
	waitFor(t, "the webhook's full queue to be logged", func() bool { return strings.Contains(logs.String(), "notifications dropped for one webhook") })
	time.Sleep(50 * time.Millisecond)
	// Each full queue logs one line, however many events it drops.
	if got := strings.Count(logs.String(), `msg="notifications dropped for one webhook`); got != 1 {
		t.Errorf("webhook drop lines = %d, want 1:\n%s", got, logs)
	}
	if got := strings.Count(logs.String(), `msg="notifications dropped: too many`); got > 1 {
		t.Errorf("queue drop lines = %d, want at most 1:\n%s", got, logs)
	}
}

// Without Run nothing drains the queue; Emit still returns.
func TestEmitWithoutRunDrops(t *testing.T) {
	n, _, logs := newTest(t, testKey)
	done := make(chan struct{})
	go func() {
		for i := 0; i < QueueSize*3; i++ {
			n.Emit(Message{Event: Start, World: world})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Emit blocked on a full queue")
	}
	// One line for the whole burst, not one per event.
	if got := strings.Count(logs.String(), "notifications dropped"); got != 1 {
		t.Errorf("dropped lines = %d, want 1", got)
	}
	var nilNotifier *Notifier
	nilNotifier.Emit(Message{Event: Start})
}

func TestEventsTurnedOffAreNotSent(t *testing.T) {
	n, st, _ := newTest(t, testKey)
	var h hits
	srv := server(t, &h, http.StatusNoContent)
	addRaw(t, n, st, Generic, srv.URL+"/hook", "en", Stop)
	run(t, n)
	n.Emit(Message{Event: Stop, World: world})
	n.Emit(Message{Event: Start, World: world})
	waitFor(t, "the start event", func() bool { return h.n.Load() >= 1 })
	time.Sleep(100 * time.Millisecond)
	if got := h.n.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	var body map[string]any
	_ = json.Unmarshal(h.bodies[0], &body)
	if body["event"] != "start" {
		t.Errorf("sent %v", body["event"])
	}
}

// The message is in the webhook's own language (SPEC 22).
func TestMessageInWebhookLanguage(t *testing.T) {
	n, st, _ := newTest(t, testKey)
	var h hits
	srv := server(t, &h, http.StatusNoContent)
	addRaw(t, n, st, Ntfy, srv.URL+"/topic", "de")
	run(t, n)
	n.Emit(Message{Event: Start, World: world})
	waitFor(t, "a request", func() bool { return h.n.Load() == 1 })
	h.mu.Lock()
	defer h.mu.Unlock()
	want := i18n.Locale("de").T("notify.msg.start", "world", "Main base")
	if string(h.bodies[0]) != want || want == i18n.English.T("notify.msg.start", "world", "Main base") {
		t.Errorf("body %q, want the German %q", h.bodies[0], want)
	}
	if h.heads[0].Get("Content-Type") != "text/plain; charset=utf-8" || h.heads[0].Get("Title") != "Foreman" {
		t.Errorf("headers %v", h.heads[0])
	}
}

func TestDiscordTurnsOffMentions(t *testing.T) {
	n, st, _ := newTest(t, testKey)
	var h hits
	srv := server(t, &h, http.StatusNoContent)
	addRaw(t, n, st, Discord, srv.URL+"/api/webhooks/1/"+token, "en")
	run(t, n)
	n.Emit(Message{Event: PlayerJoin, World: World{ID: "w", Name: "**Base** @here"}, Player: "@everyone"})
	waitFor(t, "a request", func() bool { return h.n.Load() == 1 })
	h.mu.Lock()
	defer h.mu.Unlock()
	var body struct {
		Content         string `json:"content"`
		AllowedMentions *struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal(h.bodies[0], &body); err != nil {
		t.Fatal(err)
	}
	if body.AllowedMentions == nil || body.AllowedMentions.Parse == nil || len(body.AllowedMentions.Parse) != 0 {
		t.Errorf("allowed_mentions = %s, want {\"parse\":[]}", h.bodies[0])
	}
	if !bytes.Contains(h.bodies[0], []byte(`"allowed_mentions":{"parse":[]}`)) {
		t.Errorf("body %s", h.bodies[0])
	}
	if strings.Contains(body.Content, "**Base**") || strings.Contains(body.Content, " @everyone") {
		t.Errorf("names are not escaped: %q", body.Content)
	}
}

// Every way a delivery can fail is logged with the host, never the address.
func TestNoAddressInLogs(t *testing.T) {
	n, st, logs := newTest(t, testKey)
	n.backoff = nil
	var h hits
	fail := server(t, &h, http.StatusBadGateway)
	redirect := httptest.NewTLSServer(http.RedirectHandler("/elsewhere/"+token, http.StatusFound))
	t.Cleanup(redirect.Close)
	release := make(chan struct{})
	hang := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); hang.Close() })
	closed := httptest.NewTLSServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	for _, base := range []string{fail.URL, redirect.URL, hang.URL, closedURL, "https://no-such-host.invalid"} {
		addRaw(t, n, st, Generic, base+"/hook/"+token+"?auth="+token, "en")
	}
	run(t, n)
	n.Emit(Message{Event: LowDisk, World: world, FreeBytes: 3 << 30})
	waitFor(t, "five failures", func() bool { return strings.Count(logs.String(), "webhook delivery failed") == 5 })
	if strings.Contains(logs.String(), token) {
		t.Errorf("the log holds the address:\n%s", logs)
	}
}

func TestAddressSealedAtRest(t *testing.T) {
	n, st, _ := newTest(t, testKey)
	ctx := context.Background()
	raw := "https://discord.com/api/webhooks/123/" + token
	w, err := n.Add(ctx, Setup{Preset: Discord, URL: secret.New(raw), Events: allEvents(), Lang: "fr"}, "todd")
	if err != nil {
		t.Fatal(err)
	}
	if w.Host != "discord.com" || w.Preset != Discord || w.Lang != "fr" || !w.AllOn() {
		t.Errorf("added %+v", w)
	}
	if s := fmt.Sprintf("%+v %#v", w, w); strings.Contains(s, token) {
		t.Errorf("the Webhook value holds the address: %s", s)
	}
	row, err := st.Webhook(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.URL, []byte(token)) || bytes.Contains(row.URL, []byte("discord")) {
		t.Errorf("the stored address is plain: %q", row.URL)
	}
	if plain, err := n.box.Open(rowName(w.ID), row.URL); err != nil || string(plain) != raw {
		t.Errorf("open = %q, %v", plain, err)
	}
	if _, err := n.box.Open(rowName(w.ID+1), row.URL); err == nil {
		t.Error("the sealed address opens under another row's name")
	}
	other, _ := secret.NewBox(secret.New("another-key-another-key-another-key"))
	if _, err := other.Open(rowName(w.ID), row.URL); err == nil {
		t.Error("the sealed address opens with another key")
	}
}

func allEvents() map[Event]bool {
	on := map[Event]bool{}
	for _, e := range Events {
		on[e] = true
	}
	return on
}

func TestAddUpdateDelete(t *testing.T) {
	n, _, _ := newTest(t, testKey)
	ctx := context.Background()
	w, err := n.Add(ctx, Setup{Preset: Ntfy, URL: secret.New("https://ntfy.sh/" + token), Events: map[Event]bool{Crash: true}, Lang: "de"}, "todd")
	if err != nil {
		t.Fatal(err)
	}
	if got := w.OnEvents(); len(got) != 1 || got[0] != Crash || w.AllOn() {
		t.Errorf("events on = %v", got)
	}
	// An edit without an address keeps it, and its preset and host.
	w, err = n.Update(ctx, w.ID, Setup{Preset: Generic, Events: allEvents(), Lang: "pl"}, "ann")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := n.Get(ctx, w.ID)
	if got.Preset != Ntfy || got.Host != "ntfy.sh" || got.Lang != "pl" || !got.AllOn() {
		t.Errorf("after edit: %+v", got)
	}
	if _, err := n.Update(ctx, w.ID, Setup{Preset: Generic, URL: secret.New("https://hooks.example.org/x"), Events: allEvents(), Lang: "pl"}, "ann"); err != nil {
		t.Fatal(err)
	}
	got, _ = n.Get(ctx, w.ID)
	if got.Preset != Generic || got.Host != "hooks.example.org" {
		t.Errorf("after a new address: %+v", got)
	}
	if _, err := n.Update(ctx, w.ID, Setup{Preset: Discord, URL: secret.New("ftp://discord.com/api/webhooks/" + token), Events: allEvents(), Lang: "en"}, "ann"); !errors.Is(err, ErrURLScheme) {
		t.Errorf("bad address on edit: %v", err)
	} else if strings.Contains(err.Error(), token) {
		t.Errorf("the error holds the address: %v", err)
	}
	if _, err := n.Update(ctx, w.ID, Setup{Preset: Generic, Events: map[Event]bool{}, Lang: "en"}, "ann"); !errors.Is(err, ErrNoEvents) {
		t.Errorf("no events: %v", err)
	}
	if _, err := n.Update(ctx, w.ID, Setup{Preset: Generic, Events: allEvents(), Lang: "xx"}, "ann"); !errors.Is(err, ErrBadLanguage) {
		t.Errorf("bad language: %v", err)
	}
	if err := n.Delete(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Get(ctx, w.ID); !errors.Is(err, ErrNoWebhook) {
		t.Errorf("after delete: %v", err)
	}
	if list, _ := n.List(ctx); len(list) != 0 {
		t.Errorf("list after delete: %+v", list)
	}
}

func TestWithoutKey(t *testing.T) {
	n, st, _ := newTest(t, "")
	ctx := context.Background()
	if n.CanStore() {
		t.Fatal("CanStore without a key")
	}
	if _, err := n.Add(ctx, Setup{Preset: Discord, URL: secret.New("https://discord.com/api/webhooks/1/x"), Events: allEvents(), Lang: "en"}, "todd"); !errors.Is(err, ErrNoSecretKey) {
		t.Errorf("add: %v", err)
	}
	// A row left from when a key was set is not listed: its address cannot be read.
	if _, err := st.AddWebhook(ctx, store.Webhook{Preset: "discord", Host: "discord.com", Lang: "en", CreatedBy: "todd"},
		func(int64) ([]byte, error) { return []byte{1}, nil }, time.Now()); err != nil {
		t.Fatal(err)
	}
	if list, err := n.List(ctx); err != nil || len(list) != 0 {
		t.Errorf("list = %v, %v", list, err)
	}
	if err := n.Delete(ctx, 1); !errors.Is(err, ErrNoSecretKey) {
		t.Errorf("delete: %v", err)
	}
	var none *Notifier
	if none.CanStore() {
		t.Error("a nil Notifier can store")
	}
}

func TestCheckURL(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", MaxURLLength)
	for _, c := range []struct {
		preset Preset
		raw    string
		host   string
		err    error
	}{
		{Discord, "https://discord.com/api/webhooks/1/" + token, "discord.com", nil},
		{Discord, "  https://Discord.com/api/webhooks/1/" + token + "  ", "discord.com", nil},
		{Discord, "https://discordapp.com/api/webhooks/1/x", "discordapp.com", nil},
		{Discord, "https://ptb.discord.com/api/webhooks/1/x", "ptb.discord.com", nil},
		{Discord, "https://example.com/api/webhooks/1/x", "", ErrURLNotDiscord},
		{Discord, "https://discord.com/channels/1", "", ErrURLNotDiscord},
		{Discord, "https://discord.com.evil.example/api/webhooks/1/x", "", ErrURLNotDiscord},
		{Ntfy, "https://ntfy.sh/" + token, "ntfy.sh", nil},
		{Ntfy, "https://ntfy.example.org:8443/alerts?auth=" + token, "ntfy.example.org", nil},
		{Ntfy, "https://ntfy.sh/", "", ErrURLNoTopic},
		{Ntfy, "https://ntfy.sh", "", ErrURLNoTopic},
		{Generic, "https://hooks.example.org", "hooks.example.org", nil},
		{Generic, "https://[::1]:8443/x", "::1", nil},
		{Generic, "", "", ErrURLMissing},
		{Generic, "   ", "", ErrURLMissing},
		{Generic, long, "", ErrURLTooLong},
		{Generic, "http://hooks.example.org/" + token, "hooks.example.org", nil},
		{Ntfy, "HTTP://ntfy.lan:8080/alerts", "ntfy.lan", nil},
		{Discord, "http://discord.com/api/webhooks/1/x", "discord.com", nil},
		{Generic, "ftp://hooks.example.org/x", "", ErrURLScheme},
		{Generic, "hooks.example.org/x", "", ErrURLScheme},
		{Generic, "http://user:" + token + "@hooks.example.org/x", "", ErrURLHasUser},
		{Generic, "https://user:" + token + "@hooks.example.org/x", "", ErrURLHasUser},
		{Generic, "https:///x", "", ErrURLInvalid},
		{Generic, "https://hooks.example.org/a b", "", ErrURLInvalid},
		{Generic, "https://hooks.example.org/\x00", "", ErrURLInvalid},
		{Generic, "https://hooks.example.org:bad/" + token, "", ErrURLInvalid},
		{Generic, "https:opaque", "", ErrURLInvalid},
	} {
		host, err := CheckURL(c.preset, c.raw)
		if host != c.host || !errors.Is(err, c.err) {
			t.Errorf("CheckURL(%s, %q) = %q, %v; want %q, %v", c.preset, c.raw, host, err, c.host, c.err)
		}
		if err != nil && strings.Contains(err.Error(), token) {
			t.Errorf("the error for %q holds the address: %v", c.raw, err)
		}
	}
}

// One dead target must not hold up another webhook (SPEC 8.12).
func TestSlowTargetDoesNotDelayOthers(t *testing.T) {
	n, st, _ := newTest(t, testKey)
	n.timeout = time.Hour
	release := make(chan struct{})
	hang := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); hang.Close() })
	var h hits
	ok := server(t, &h, http.StatusNoContent)
	addRaw(t, n, st, Generic, hang.URL+"/dead", "en")
	addRaw(t, n, st, Generic, ok.URL+"/alive", "en")
	run(t, n)
	start := time.Now()
	for i := 0; i < 5; i++ {
		n.Emit(Message{Event: PlayerJoin, World: world, Player: fmt.Sprint("p", i)})
	}
	waitFor(t, "five events at the healthy target", func() bool { return h.n.Load() == 5 })
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the healthy target waited %v", d)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, b := range h.bodies {
		if !bytes.Contains(b, []byte(fmt.Sprintf(`"player":"p%d"`, i))) {
			t.Errorf("event %d out of order: %s", i, b)
		}
	}
}

// A deleted webhook's sender stops, and nothing more is sent to it.
func TestDeletedWebhookGetsNothing(t *testing.T) {
	n, st, _ := newTest(t, testKey)
	var h hits
	srv := server(t, &h, http.StatusNoContent)
	id := addRaw(t, n, st, Generic, srv.URL+"/hook", "en")
	run(t, n)
	n.Emit(Message{Event: Start, World: world})
	waitFor(t, "the first event", func() bool { return h.n.Load() == 1 })
	if err := n.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	n.Emit(Message{Event: Stop, World: world})
	time.Sleep(100 * time.Millisecond)
	if got := h.n.Load(); got != 1 {
		t.Errorf("requests = %d after delete", got)
	}
}

// After FOREMAN_SECRET_KEY changes, every stored address is unreadable:
// the list says so, nothing is sent, and an edit must type the address again
// (ADR 0013).
func TestKeyChangeMakesAddressesUnreadable(t *testing.T) {
	n, st, logs := newTest(t, testKey)
	ctx := context.Background()
	var h hits
	srv := server(t, &h, http.StatusNoContent)
	addRaw(t, n, st, Generic, srv.URL+"/hook", "en")
	if list, _ := n.List(ctx); len(list) != 1 || list[0].Unreadable {
		t.Fatalf("before the change: %+v", list)
	}

	other, err := New(st, secret.New("another-key-another-key-another-key"), n.log)
	if err != nil {
		t.Fatal(err)
	}
	other.client, other.timeout, other.backoff = n.client, n.timeout, n.backoff
	list, err := other.List(ctx)
	if err != nil || len(list) != 1 || !list[0].Unreadable {
		t.Fatalf("after the change: %+v, %v", list, err)
	}
	got, _ := other.Get(ctx, list[0].ID)
	if !got.Unreadable {
		t.Error("Get does not say the address is unreadable")
	}
	run(t, other)
	other.Emit(Message{Event: Start, World: world})
	waitFor(t, "the unreadable address to be logged", func() bool { return strings.Contains(logs.String(), "cannot be read") })
	if h.n.Load() != 0 {
		t.Error("an event was sent with an unreadable address")
	}
	if _, err := other.Update(ctx, got.ID, Setup{Preset: Generic, Events: allEvents(), Lang: "en"}, "ann"); !errors.Is(err, ErrURLMissing) {
		t.Errorf("an edit kept an unreadable address: %v", err)
	}
	if _, err := other.Update(ctx, got.ID, Setup{Preset: Generic, URL: secret.New("http://hooks.example.org/x"), Events: allEvents(), Lang: "en"}, "ann"); err != nil {
		t.Fatal(err)
	}
	got, _ = other.Get(ctx, got.ID)
	if got.Unreadable || !got.Plain {
		t.Errorf("after typing it again: %+v", got)
	}
}
