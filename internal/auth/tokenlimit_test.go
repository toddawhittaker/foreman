package auth

import (
	"testing"
	"time"
)

func TestTokenLimiterPerMinute(t *testing.T) {
	clk := newClock()
	l := NewTokenLimiter(clk.Now)
	for i := range TokenRequestsPerMinute {
		release, _, ok := l.Acquire(1)
		if !ok {
			t.Fatalf("refused after %d requests", i)
		}
		release()
	}
	clk.Advance(20 * time.Second)
	if _, wait, ok := l.Acquire(1); ok || wait != 40*time.Second {
		t.Errorf("121st: ok=%v wait=%v, want refused for 40s", ok, wait)
	}
	if release, _, ok := l.Acquire(2); !ok {
		t.Error("another token was limited")
	} else {
		release()
	}
	clk.Advance(40 * time.Second)
	if release, _, ok := l.Acquire(1); !ok {
		t.Error("a new minute did not reset the count")
	} else {
		release()
	}
}

func TestTokenLimiterConcurrent(t *testing.T) {
	clk := newClock()
	l := NewTokenLimiter(clk.Now)
	var releases []func()
	for range TokenConcurrent {
		release, _, ok := l.Acquire(1)
		if !ok {
			t.Fatal("refused under the limit")
		}
		releases = append(releases, release)
	}
	if _, wait, ok := l.Acquire(1); ok || wait != time.Second {
		t.Errorf("fifth at once: ok=%v wait=%v", ok, wait)
	}
	releases[0]()
	releases[0]() // a second call gives nothing more back
	if release, _, ok := l.Acquire(1); !ok {
		t.Error("a freed slot was not reused")
	} else {
		releases[0] = release
	}
	if _, _, ok := l.Acquire(1); ok {
		t.Error("a double release freed two slots")
	}
	// Running requests stay counted across a new minute.
	clk.Advance(time.Minute)
	if _, _, ok := l.Acquire(1); ok {
		t.Error("a new minute dropped the running requests")
	}
}
