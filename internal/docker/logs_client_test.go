package docker_test

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
)

func nextLine(t *testing.T, s *docker.LogStream) docker.LogLine {
	t.Helper()
	l, err := s.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return l
}

func TestLogsHistoryAndTail(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx := context.Background()
	id, _ := c.CreateContainer(ctx, "foreman-w1", fullSpec())
	for i := 1; i <= 5; i++ {
		d.EmitLog(id, docker.Stdout, "line "+strconv.Itoa(i))
	}
	d.EmitLog(id, docker.Stderr, "warning")

	s, err := c.Logs(ctx, id, docker.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var all []docker.LogLine
	for {
		l, err := s.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, l)
	}
	if len(all) != 6 || all[0].Text != "line 1" || all[5] != (docker.LogLine{Stream: docker.Stderr, Text: "warning"}) {
		t.Errorf("history = %v", all)
	}

	s2, err := c.Logs(ctx, id, docker.LogOptions{Tail: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	if l := nextLine(t, s2); l.Text != "line 5" {
		t.Errorf("tail 2 began with %v", l)
	}
	last := d.Calls()[len(d.Calls())-1]
	if last.Query.Get("tail") != "2" || last.Query.Get("stdout") != "1" || last.Query.Get("stderr") != "1" {
		t.Errorf("logs query = %v", last.Query)
	}
}

func TestLogsFollowSeesNewLinesAndEndsWhenTheContainerStops(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, _ := c.CreateContainer(ctx, "foreman-w1", fullSpec())
	_ = c.StartContainer(ctx, id)
	d.EmitLog(id, docker.Stdout, "before")

	s, err := c.Logs(ctx, id, docker.LogOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if l := nextLine(t, s); l.Text != "before" {
		t.Errorf("first line = %v", l)
	}
	d.EmitLog(id, docker.Stderr, "live")
	if l := nextLine(t, s); l != (docker.LogLine{Stream: docker.Stderr, Text: "live"}) {
		t.Errorf("live line = %v", l)
	}
	d.Exit(id, 0, false)
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("after the container exited: want EOF, got %v", err)
	}
}

func TestLogsFollowEndsWhenContextIsCancelled(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	id, _ := c.CreateContainer(context.Background(), "foreman-w1", fullSpec())
	_ = c.StartContainer(context.Background(), id)

	ctx, cancel := context.WithCancel(context.Background())
	s, err := c.Logs(ctx, id, docker.LogOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	cancel()
	if _, err := s.Next(); err == nil {
		t.Error("Next succeeded after cancel")
	}
}

func TestLogsOfMissingContainer(t *testing.T) {
	c, _ := newClient(t)
	if _, err := c.Logs(context.Background(), "nope", docker.LogOptions{}); !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestLogsSincePassedAsSecondsWithFraction(t *testing.T) {
	c, d := newClient(t)
	d.AddImage(testImage)
	id, _ := c.CreateContainer(context.Background(), "foreman-w1", fullSpec())
	s, err := c.Logs(context.Background(), id, docker.LogOptions{Since: time.Unix(1700000000, 5)})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	last := d.Calls()[len(d.Calls())-1]
	if got := last.Query.Get("since"); got != "1700000000.000000005" {
		t.Errorf("since = %q", got)
	}
}
