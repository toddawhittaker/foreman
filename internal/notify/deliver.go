package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxResponseBytes is as much of a target's reply as is read, so the
// connection can be reused; the reply itself is not used.
const maxResponseBytes = 64 << 10

// HookQueueSize is how many events wait for one webhook. Each webhook has
// its own queue and sender, so a slow or dead target delays only itself
// (SPEC 8.12).
const HookQueueSize = 20

// target is one webhook ready to send to. raw is the opened address; it is
// used only to make the request and never logged.
type target struct {
	hook Webhook
	raw  string
}

type job struct {
	t target
	m Message
}

// sender delivers one webhook's events in order.
type sender struct {
	ch chan job
	// full is set once a drop is logged and cleared when the queue drains.
	full atomic.Bool
}

// Run delivers queued events until ctx ends. Events still queued then are
// not sent, and none is kept across a restart.
func (n *Notifier) Run(ctx context.Context) {
	senders := map[int64]*sender{}
	var wg sync.WaitGroup
	defer func() {
		for _, s := range senders {
			close(s.ch)
		}
		wg.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-n.queue:
			if len(n.queue) == 0 {
				n.full.Store(false)
			}
			n.dispatch(ctx, m, senders, &wg)
		}
	}
}

// dispatch hands m to the sender of every webhook that has its event on,
// without waiting for any of them. Senders of deleted webhooks stop.
func (n *Notifier) dispatch(ctx context.Context, m Message, senders map[int64]*sender, wg *sync.WaitGroup) {
	if n.box == nil {
		return
	}
	rows, err := n.rows.ListWebhooks(ctx)
	if err != nil {
		n.log.Error("notification not sent: the webhooks could not be read", "event", string(m.Event), "error", err)
		return
	}
	live := map[int64]bool{}
	for _, r := range rows {
		live[r.ID] = true
		hook := fromRow(r)
		if !hook.On(m.Event) {
			continue
		}
		raw, ok := n.open(r)
		if !ok {
			n.log.Warn("notification not sent: the webhook address cannot be read; type it again on the Notifications page",
				"webhook", r.ID, "preset", r.Preset, "host", r.Host, "event", string(m.Event))
			continue
		}
		s := senders[r.ID]
		if s == nil {
			s = &sender{ch: make(chan job, HookQueueSize)}
			senders[r.ID] = s
			wg.Go(func() { n.drain(ctx, s) })
		}
		select {
		case s.ch <- job{t: target{hook: hook, raw: raw}, m: m}:
		default:
			if s.full.CompareAndSwap(false, true) {
				n.log.Warn("notifications dropped for one webhook: too many are waiting for it; more are dropped without a log line until its queue drains",
					"webhook", r.ID, "preset", r.Preset, "host", r.Host, "event", string(m.Event))
			}
		}
	}
	for id, s := range senders {
		if !live[id] {
			close(s.ch)
			delete(senders, id)
		}
	}
}

// drain sends one webhook's events until its queue is closed. Once ctx ends
// the rest are skipped.
func (n *Notifier) drain(ctx context.Context, s *sender) {
	for j := range s.ch {
		if len(s.ch) == 0 {
			s.full.Store(false)
		}
		if ctx.Err() == nil {
			n.send(ctx, j.t, j.m)
		}
	}
}

// send makes the first attempt and at most two retries (SPEC 8.12). A
// refusal that would only repeat, such as a 404 or a redirect, is not
// retried.
func (n *Notifier) send(ctx context.Context, t target, m Message) {
	req, err := build(t.hook.Preset, t.hook.Lang, m)
	if err != nil {
		n.log.Error("notification not sent: its body could not be made", "webhook", t.hook.ID, "event", string(m.Event), "error", err)
		return
	}
	for attempt := 1; ; attempt++ {
		retry, problem := n.attempt(ctx, t, req)
		if problem == "" {
			return
		}
		last := !retry || attempt > len(n.backoff)
		n.log.Warn("webhook delivery failed",
			"webhook", t.hook.ID, "preset", string(t.hook.Preset), "host", t.hook.Host, "event", string(m.Event),
			"attempt", attempt, "will_retry", !last, "problem", problem)
		if last {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(n.backoff[attempt-1]):
		}
	}
}

// attempt posts once. It returns a description of what went wrong, empty on
// success, which never holds the address, and whether trying again could help.
func (n *Notifier) attempt(ctx context.Context, t target, r request) (retry bool, problem string) {
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.raw, bytes.NewReader(r.body))
	if err != nil {
		return false, "the request could not be made"
	}
	req.Header.Set("Content-Type", r.contentType)
	req.Header.Set("User-Agent", "Foreman")
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return true, describe(err, t)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	_ = resp.Body.Close()
	switch c := resp.StatusCode; {
	case c >= 200 && c < 300:
		return false, ""
	case c >= 300 && c < 400:
		return false, fmt.Sprintf("the target answered %d, a redirect, which Foreman does not follow", c)
	case c == http.StatusRequestTimeout || c == http.StatusTooManyRequests || c >= 500:
		return true, fmt.Sprintf("the target answered %d", c)
	default:
		return false, fmt.Sprintf("the target answered %d", c)
	}
}

// describe says why a request failed without the address: Go's errors name
// the whole URL, so only the inner error is kept, and any trace of the
// address in it is replaced by the host.
func describe(err error, t target) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "the target did not answer in time"
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	msg := err.Error()
	if u, perr := url.Parse(t.raw); perr == nil {
		for _, s := range []string{t.raw, u.Path, u.RawQuery, u.EscapedPath()} {
			if s != "" && s != "/" {
				msg = strings.ReplaceAll(msg, s, "…")
			}
		}
	}
	return "the request failed: " + msg
}
