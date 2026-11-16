package docker_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

func eventsClient(t *testing.T) (*dockertest.Daemon, *docker.Client) {
	t.Helper()
	d := dockertest.New(t)
	c, err := docker.New(d.Host())
	if err != nil {
		t.Fatal(err)
	}
	return d, c
}

// SPEC 14.3: events are optional; a proxy with EVENTS=0 answers 403, which
// a caller can tell apart from a stream that only dropped.
func TestEventsRefusedByTheProxy(t *testing.T) {
	_, c := eventsClient(t)
	_, err := c.Events(context.Background(), docker.EventOptions{Actions: []string{"oom", "die"}})
	if err == nil || !docker.EventsRefused(err) {
		t.Fatalf("err = %v, want one EventsRefused reports", err)
	}
	if docker.EventsRefused(&docker.UnreachableError{Target: "x", Err: errors.New("down")}) {
		t.Error("an unreachable daemon is not a refusal")
	}
	if !docker.EventsRefused(&docker.APIError{Status: 401}) {
		t.Error("a 401 is a refusal")
	}
	for _, st := range []int{400, 404, 429, 500, 502} {
		if docker.EventsRefused(&docker.APIError{Status: st}) {
			t.Errorf("a %d is not a refusal for good", st)
		}
	}
}

func TestEventsStreamsOnlyMatchingContainers(t *testing.T) {
	d, c := eventsClient(t)
	d.AllowEvents(true)
	d.Seed(dockertest.Container{ID: "aaa", Name: "game", Status: "running", Spec: docker.ContainerSpec{Labels: map[string]string{"foreman.install": "x"}}})
	d.Seed(dockertest.Container{ID: "bbb", Name: "other", Status: "running"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := c.Events(ctx, docker.EventOptions{Actions: []string{"oom", "die"}, Labels: []string{"foreman.install=x"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	d.Exit("bbb", 137, true)
	d.Exit("aaa", 137, true)
	var got []string
	for range 2 {
		ev, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.ID != "aaa" || ev.Time.IsZero() {
			t.Errorf("event %+v, want one for aaa with a time", ev)
		}
		got = append(got, ev.Action)
	}
	if got[0] != "oom" || got[1] != "die" {
		t.Errorf("actions = %v, want [oom die]", got)
	}
	d.CutEvents()
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("after the proxy cut the stream: %v, want io.EOF", err)
	}
}
