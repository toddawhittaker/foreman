package auth

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func newClock() *fakeClock                   { return &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func TestLimiterPerUsername(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for i := range LimitPerUsername {
		if ok, _ := l.Allow("Alice", "10.0.0.1"); !ok {
			t.Fatalf("blocked after only %d failures", i)
		}
		// A different address each time, so only the username counts.
		l.Fail("Alice", fmt.Sprintf("10.0.0.%d", i+1))
	}
	// Case and spaces do not give a fresh allowance.
	for _, name := range []string{"alice", "ALICE", " Alice "} {
		ok, left := l.Allow(name, "10.9.9.9")
		if ok || left != LimitWindow {
			t.Errorf("%q: ok=%v left=%v", name, ok, left)
		}
	}
	if ok, _ := l.Allow("bob", "10.9.9.9"); !ok {
		t.Error("another username should not be blocked")
	}
}

func TestLimiterPerAddress(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for i := range LimitPerAddress {
		if ok, _ := l.Allow("u", "192.0.2.1"); !ok {
			t.Fatalf("blocked after only %d failures", i)
		}
		l.Fail(fmt.Sprintf("user%d", i), "192.0.2.1")
	}
	if ok, _ := l.Allow("fresh", "192.0.2.1"); ok {
		t.Error("address should be blocked")
	}
	if ok, _ := l.Allow("fresh", "192.0.2.2"); !ok {
		t.Error("another address should not be blocked")
	}
}

func TestLimiterWindowExpiry(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for range LimitPerUsername {
		l.Fail("alice", "10.0.0.1")
	}
	clk.Advance(10 * time.Minute)
	ok, left := l.Allow("alice", "10.0.0.1")
	if ok || left != 5*time.Minute {
		t.Fatalf("after 10 minutes: ok=%v left=%v", ok, left)
	}
	// Further failures while blocked do not extend the window.
	l.Fail("alice", "10.0.0.1")
	clk.Advance(5*time.Minute - time.Second)
	if ok, left := l.Allow("alice", "10.0.0.1"); ok || left != time.Second {
		t.Fatalf("one second early: ok=%v left=%v", ok, left)
	}
	clk.Advance(time.Second)
	if ok, _ := l.Allow("alice", "10.0.0.1"); !ok {
		t.Fatal("still blocked at the end of the window")
	}
	// The count starts again from zero.
	for range LimitPerUsername - 1 {
		l.Fail("alice", "10.0.0.1")
	}
	if ok, _ := l.Allow("alice", "10.0.0.1"); !ok {
		t.Fatal("old failures leaked into the new window")
	}
}

func TestLimiterSuccessClearsUsernameOnly(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for range LimitPerUsername - 1 {
		l.Fail("alice", "10.0.0.1")
	}
	l.Succeed("Alice")
	l.Fail("alice", "10.0.0.1")
	if ok, _ := l.Allow("alice", "10.0.0.2"); !ok {
		t.Error("success did not clear the username count")
	}
	// The address count is untouched: 4 + 1 failures so far.
	for range LimitPerAddress - 5 {
		l.Fail("other", "10.0.0.1")
	}
	l.Succeed("other")
	if ok, _ := l.Allow("x", "10.0.0.1"); ok {
		t.Error("success reset the address count")
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now, 100)
	for i := range 1000 {
		clk.Advance(time.Millisecond)
		l.Fail(fmt.Sprintf("user%d", i), fmt.Sprintf("addr%d", i))
	}
	if n := len(l.users.byKey); n != 100 || l.users.order.Len() != 100 {
		t.Errorf("users: %d entries", n)
	}
	if n := len(l.addrs.byKey); n != 100 || l.addrs.order.Len() != 100 {
		t.Errorf("addrs: %d entries", n)
	}
	// The oldest were evicted, the newest kept.
	if _, ok := l.users.byKey["user0"]; ok {
		t.Error("oldest entry was kept")
	}
	if _, ok := l.users.byKey["user999"]; !ok {
		t.Error("newest entry was evicted")
	}
}

func TestLimiterExpiredEntriesAreDropped(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for i := range 50 {
		l.Fail(fmt.Sprintf("user%d", i), "10.0.0.1")
	}
	clk.Advance(LimitWindow)
	l.Allow("anyone", "10.0.0.1")
	if len(l.users.byKey) != 0 || len(l.addrs.byKey) != 0 {
		t.Errorf("expired entries remain: %d users, %d addrs", len(l.users.byKey), len(l.addrs.byKey))
	}
}
