package notify

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// Foreman stopping while it looks up webhooks for a queued event is not a
// fault, so it logs no ERROR line; the e2e harness fails a test on one.
func TestDispatchStaysQuietWhenItsContextEnds(t *testing.T) {
	n, _, logs := newTest(t, testKey)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var wg sync.WaitGroup
	n.dispatch(ctx, Message{Event: TokenExpiring}, map[int64]*sender{}, &wg)
	if got := logs.String(); strings.Contains(got, "level=ERROR") {
		t.Errorf("a cancelled lookup logged an error: %s", got)
	}
}

// A real read failure while the context is live still logs an ERROR line.
func TestDispatchLogsAStoreFailure(t *testing.T) {
	n, st, logs := newTest(t, testKey)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	n.dispatch(context.Background(), Message{Event: TokenExpiring}, map[int64]*sender{}, &wg)
	if got := logs.String(); !strings.Contains(got, "level=ERROR") {
		t.Errorf("logs %s", got)
	}
}
