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
	"time"
)

// maxResponseBytes is as much of a target's reply as is read, so the
// connection can be reused; the reply itself is not used.
const maxResponseBytes = 64 << 10

// target is one webhook ready to send to. raw is the opened address; it is
// used only to make the request and never logged.
type target struct {
	hook Webhook
	raw  string
}

// deliver sends m to every webhook that has its event on, all at once, and
// returns when each has succeeded or used its retries.
func (n *Notifier) deliver(ctx context.Context, m Message) {
	rows, err := n.rows.ListWebhooks(ctx)
	if err != nil {
		n.log.Error("notification not sent: the webhooks could not be read", "event", string(m.Event), "error", err)
		return
	}
	if n.box == nil {
		return
	}
	var wg sync.WaitGroup
	for _, r := range rows {
		hook := fromRow(r)
		if !hook.On(m.Event) {
			continue
		}
		plain, err := n.box.Open(rowName(r.ID), r.URL)
		if err != nil {
			n.log.Warn("notification not sent: the webhook address cannot be read; type it again on the Notifications page",
				"webhook", r.ID, "preset", r.Preset, "host", r.Host, "event", string(m.Event))
			continue
		}
		wg.Go(func() { n.send(ctx, target{hook: hook, raw: string(plain)}, m) })
	}
	wg.Wait()
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
