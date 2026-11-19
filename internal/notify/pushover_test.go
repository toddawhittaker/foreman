package notify

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/secret"
)

// The keys are secrets (SPEC 14.8); no log line or error may hold them.
const (
	appToken = "aPPtokenSECRET4417aaaaaaaaaaaa" //nolint:gosec // made up, to prove it never leaks
	userKey  = "uSERkeySECRET9921bbbbbbbbbbbbb" //nolint:gosec // made up, to prove it never leaks
)

// SPEC 8.11: Pushover takes a form with the same text as the other
// presets; priority 1 for a crash, a crash loop and low disk, 0 otherwise,
// never 2.
func TestPushoverBodyGolden(t *testing.T) {
	raw := pushoverAddress(appToken, userKey)
	for _, tc := range []struct {
		m    Message
		want string
	}{
		{Message{Event: Start, World: world},
			"message=Main+base%3A+the+server+started.&priority=0&title=Foreman&token=" + appToken + "&user=" + userKey},
		{Message{Event: Crash, World: world},
			"message=Main+base%3A+the+server+crashed.&priority=1&title=Foreman&token=" + appToken + "&user=" + userKey},
	} {
		r, err := build(Pushover, raw, i18n.English, tc.m)
		if err != nil {
			t.Fatal(err)
		}
		if string(r.body) != tc.want {
			t.Errorf("%s body\n got %s\nwant %s", tc.m.Event, r.body, tc.want)
		}
		if r.url != PushoverEndpoint || r.contentType != "application/x-www-form-urlencoded" {
			t.Errorf("%s posts to %q as %q", tc.m.Event, r.url, r.contentType)
		}
	}
	for _, e := range Events {
		r, _ := build(Pushover, raw, i18n.English, Message{Event: e, World: world})
		q, _ := url.ParseQuery(string(r.body))
		want := "0"
		if e == Crash || e == CrashLoop || e == LowDisk {
			want = "1"
		}
		if q.Get("priority") != want {
			t.Errorf("%s priority = %s, want %s", e, q.Get("priority"), want)
		}
	}
}

// Pushover's limits are 1,024 characters of message and 250 of title; a
// cut falls on a character, never inside one.
func TestPushoverTruncates(t *testing.T) {
	for _, unit := range []string{"a", "é", "工", "🚀"} {
		q, _ := url.ParseQuery(string(pushoverForm("t", "u", strings.Repeat(unit, 2000), strings.Repeat(unit, 400), 0)))
		msg, title := q.Get("message"), q.Get("title")
		if !utf8.ValidString(msg) || !utf8.ValidString(title) {
			t.Errorf("%q: a cut split a character", unit)
		}
		if n := utf8.RuneCountInString(msg); n != 1024 {
			t.Errorf("%q: message has %d characters, want 1024", unit, n)
		}
		if n := utf8.RuneCountInString(title); n != 250 {
			t.Errorf("%q: title has %d characters, want 250", unit, n)
		}
	}
	q, _ := url.ParseQuery(string(pushoverForm("t", "u", strings.Repeat("x", 1024), strings.Repeat("y", 250), 0)))
	if len(q.Get("message")) != 1024 || len(q.Get("title")) != 250 {
		t.Error("text at the limit was cut")
	}
	// A long world name is cut in the real body too.
	r, _ := build(Pushover, pushoverAddress(appToken, userKey), i18n.English, Message{Event: Start, World: World{Name: strings.Repeat("ż", 3000)}})
	q, _ = url.ParseQuery(string(r.body))
	if n := utf8.RuneCountInString(q.Get("message")); n != 1024 {
		t.Errorf("message has %d characters", n)
	}
}

// SPEC 8.12: the keys go in the body, never the address; a 4xx is not
// retried, a 429 is, and no log line holds a key.
func TestPushoverDelivery(t *testing.T) {
	n, st, logs := newTest(t, testKey)
	var ok, bad, limited hits
	var paths []string
	srv := server(t, &ok, http.StatusOK)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		ok.record(r)
	})
	addRaw(t, n, st, Pushover, srv.URL+"/1/messages.json?"+url.Values{"token": {appToken}, "user": {userKey}}.Encode(), "en")
	bad400 := server(t, &bad, http.StatusBadRequest)
	addRaw(t, n, st, Pushover, bad400.URL+"/1/messages.json?"+url.Values{"token": {appToken}, "user": {userKey}}.Encode(), "en")
	rate := server(t, &limited, http.StatusTooManyRequests)
	addRaw(t, n, st, Pushover, rate.URL+"/1/messages.json?"+url.Values{"token": {appToken}, "user": {userKey}}.Encode(), "en")
	run(t, n)
	n.Emit(Message{Event: LowDisk, World: world, FreeBytes: 3 << 30})
	waitFor(t, "the deliveries", func() bool { return ok.n.Load() == 1 && bad.n.Load() == 1 && limited.n.Load() == 3 })
	time.Sleep(50 * time.Millisecond)
	if bad.n.Load() != 1 {
		t.Errorf("a 400 was retried: %d attempts", bad.n.Load())
	}
	if len(paths) != 1 || paths[0] != "/1/messages.json" {
		t.Errorf("posted to %q, want the endpoint with no query", paths)
	}
	q, _ := url.ParseQuery(string(ok.bodies[0]))
	if q.Get("token") != appToken || q.Get("user") != userKey || q.Get("priority") != "1" {
		t.Errorf("body %s", ok.bodies[0])
	}
	if ct := ok.heads[0].Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("content type %q", ct)
	}
	if strings.Contains(logs.String(), appToken) || strings.Contains(logs.String(), userKey) {
		t.Errorf("the log holds a key:\n%s", logs)
	}
}

// SPEC 14.8: on an edit, each key left empty keeps its stored value.
func TestPushoverAddAndEditKeys(t *testing.T) {
	n, _, _ := newTest(t, testKey)
	ctx := context.Background()
	if _, err := n.Add(ctx, Setup{Preset: Pushover, AppToken: secret.New(appToken), Events: allEvents(), Lang: "en"}, "todd"); !errors.Is(err, ErrKeyMissing) {
		t.Errorf("add without a user key: %v", err)
	}
	if _, err := n.Add(ctx, Setup{Preset: Pushover, AppToken: secret.New("has space"), UserKey: secret.New(userKey), Events: allEvents(), Lang: "en"}, "todd"); !errors.Is(err, ErrKeyInvalid) {
		t.Errorf("add with a bad token: %v", err)
	}
	w, err := n.Add(ctx, Setup{Preset: Pushover, AppToken: secret.New(appToken), UserKey: secret.New(userKey), Events: allEvents(), Lang: "en"}, "todd")
	if err != nil {
		t.Fatal(err)
	}
	if w.Host != PushoverHost || w.Preset != Pushover || w.Plain {
		t.Errorf("added %+v", w)
	}
	keys := func() (string, string) {
		r, _ := n.rows.Webhook(ctx, w.ID)
		raw, _ := n.open(r)
		_, tok, usr := pushoverKeys(raw)
		return tok, usr
	}
	// Only the user key changes; the token is kept.
	if _, err := n.Update(ctx, w.ID, Setup{Preset: Pushover, UserKey: secret.New("newUSER1"), Events: allEvents(), Lang: "en"}, "ann"); err != nil {
		t.Fatal(err)
	}
	if tok, usr := keys(); tok != appToken || usr != "newUSER1" {
		t.Errorf("after a new user key: %q %q", tok, usr)
	}
	// Only the token changes; the user key is kept.
	if _, err := n.Update(ctx, w.ID, Setup{Preset: Pushover, AppToken: secret.New("newTOKEN1"), Events: allEvents(), Lang: "en"}, "ann"); err != nil {
		t.Fatal(err)
	}
	if tok, usr := keys(); tok != "newTOKEN1" || usr != "newUSER1" {
		t.Errorf("after a new token: %q %q", tok, usr)
	}
	// Neither changes: both kept.
	if _, err := n.Update(ctx, w.ID, Setup{Preset: Pushover, Events: map[Event]bool{Crash: true}, Lang: "de"}, "ann"); err != nil {
		t.Fatal(err)
	}
	if tok, usr := keys(); tok != "newTOKEN1" || usr != "newUSER1" {
		t.Errorf("after an edit without keys: %q %q", tok, usr)
	}
	// Moving another preset to Pushover needs both keys.
	d, err := n.Add(ctx, Setup{Preset: Ntfy, URL: secret.New("https://ntfy.sh/" + token), Events: allEvents(), Lang: "en"}, "todd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Update(ctx, d.ID, Setup{Preset: Pushover, UserKey: secret.New(userKey), Events: allEvents(), Lang: "en"}, "ann"); !errors.Is(err, ErrKeyMissing) {
		t.Errorf("ntfy to Pushover with one key: %v", err)
	}
}
