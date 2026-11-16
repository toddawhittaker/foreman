package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Event is one container event from Docker's event stream.
type Event struct {
	Action string    // "oom", "die" and so on
	ID     string    // the container's ID
	Time   time.Time // when Docker recorded it
}

// EventOptions picks which events Events returns.
type EventOptions struct {
	Actions []string  // the container actions wanted, such as "oom" and "die"
	Labels  []string  // "key" or "key=value"; a container must have every one
	Since   time.Time // also replay events from this time; zero means only new ones
}

// EventStream reads container events as Docker sends them.
type EventStream struct {
	body io.ReadCloser
	dec  *json.Decoder
}

// Events opens Docker's event stream for containers (SPEC 14.3). It stays
// open until ctx ends or the daemon or a proxy closes it; ctx is the only
// limit. A socket proxy that refuses events answers with an *APIError.
// Close the stream when done.
func (c *Client) Events(ctx context.Context, opts EventOptions) (*EventStream, error) {
	filters := map[string][]string{"type": {"container"}}
	if len(opts.Actions) > 0 {
		filters["event"] = opts.Actions
	}
	if len(opts.Labels) > 0 {
		filters["label"] = opts.Labels
	}
	f, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	q := url.Values{"filters": {string(f)}}
	if !opts.Since.IsZero() {
		q.Set("since", fmt.Sprintf("%d.%09d", opts.Since.Unix(), opts.Since.Nanosecond()))
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/events", query: q, long: true}) //nolint:bodyclose // the EventStream closes it
	if err != nil {
		return nil, err
	}
	return &EventStream{body: resp.Body, dec: json.NewDecoder(resp.Body)}, nil
}

// Close ends the stream.
func (s *EventStream) Close() error { return s.body.Close() }

// Next returns the next event, and io.EOF once the stream has ended.
func (s *EventStream) Next() (Event, error) {
	var raw struct {
		Type   string `json:"Type"`
		Action string `json:"Action"`
		Actor  struct {
			ID string `json:"ID"`
		} `json:"Actor"`
		TimeNano int64 `json:"timeNano"`
		Time     int64 `json:"time"`
	}
	for {
		if err := s.dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return Event{}, io.EOF
			}
			return Event{}, fmt.Errorf("the Docker event stream broke off: %w", err)
		}
		if raw.Type != "" && raw.Type != "container" {
			continue
		}
		at := time.Unix(raw.Time, 0)
		if raw.TimeNano != 0 {
			at = time.Unix(0, raw.TimeNano)
		}
		return Event{Action: raw.Action, ID: raw.Actor.ID, Time: at.UTC()}, nil
	}
}

// EventsRefused reports whether err is a socket proxy refusing the event
// stream for good, as one with EVENTS=0 does (401 or 403). Any other
// failure may pass, so the caller tries again.
func EventsRefused(err error) bool {
	var api *APIError
	return errors.As(err, &api) && (api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden)
}
