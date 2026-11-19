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
		// A different address each time, so only the username counts.
		if ok, _ := l.Allow("Alice", fmt.Sprintf("10.0.0.%d", i+1)); !ok {
			t.Fatalf("blocked after only %d attempts", i)
		}
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
		if ok, _ := l.Allow(fmt.Sprintf("user%d", i), "192.0.2.1"); !ok {
			t.Fatalf("blocked after only %d attempts", i)
		}
	}
	if ok, _ := l.Allow("fresh", "192.0.2.1"); ok {
		t.Error("address should be blocked")
	}
	if ok, _ := l.Allow("fresh", "192.0.2.2"); !ok {
		t.Error("another address should not be blocked")
	}
}

// SPEC 12.7, 12.11: an OIDC callback counts against its address in the
// same table as password sign-in, and a success gives its attempt back.
func TestLimiterAddrOnly(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	if ok, _ := l.AllowAddr("192.0.2.1"); !ok {
		t.Fatal("first attempt blocked")
	}
	l.ReleaseAddr("192.0.2.1")
	for i := range LimitPerAddress - 1 {
		if ok, _ := l.AllowAddr("192.0.2.1"); !ok {
			t.Fatalf("blocked after only %d attempts", i)
		}
	}
	// A password attempt shares the address's count.
	if ok, _ := l.Allow("alice", "192.0.2.1"); !ok {
		t.Fatal("password attempt blocked early")
	}
	if ok, left := l.AllowAddr("192.0.2.1"); ok || left <= 0 {
		t.Errorf("address not blocked: ok=%v left=%v", ok, left)
	}
	if ok, _ := l.Allow("bob", "192.0.2.1"); ok {
		t.Error("password sign-in not blocked by OIDC failures")
	}
	// No username was counted.
	if ok, _ := l.Allow("", "192.0.2.9"); !ok {
		t.Error("the empty username was counted")
	}
}

func TestLimiterWindowExpiry(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for range LimitPerUsername {
		l.Allow("alice", "10.0.0.1")
	}
	clk.Advance(10 * time.Minute)
	ok, left := l.Allow("alice", "10.0.0.1")
	if ok || left != 5*time.Minute {
		t.Fatalf("after 10 minutes: ok=%v left=%v", ok, left)
	}
	// Attempts while blocked do not extend the window.
	l.Allow("alice", "10.0.0.1")
	clk.Advance(5*time.Minute - time.Second)
	if ok, left := l.Allow("alice", "10.0.0.1"); ok || left != time.Second {
		t.Fatalf("one second early: ok=%v left=%v", ok, left)
	}
	clk.Advance(time.Second)
	if ok, _ := l.Allow("alice", "10.0.0.1"); !ok {
		t.Fatal("still blocked at the end of the window")
	}
	// The count starts again from zero: that was attempt 1 of the new window.
	for i := range LimitPerUsername - 1 {
		if ok, _ := l.Allow("alice", "10.0.0.1"); !ok {
			t.Fatalf("old attempts leaked into the new window: blocked at %d", i+2)
		}
	}
}

func TestLimiterSuccessClearsUsernameOnly(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for range LimitPerUsername - 1 {
		l.Allow("alice", "10.0.0.1")
	}
	l.Allow("Alice", "10.0.0.1")
	l.Succeed("Alice", "10.0.0.1")
	for i := range LimitPerUsername {
		if ok, _ := l.Allow("alice", "10.0.0.2"); !ok {
			t.Fatalf("success did not clear the username count: blocked at %d", i+1)
		}
	}
	// 4 failures stay against 10.0.0.1; the success gave its own count back.
	for i := range LimitPerAddress - 4 {
		if ok, _ := l.Allow(fmt.Sprintf("u%d", i), "10.0.0.1"); !ok {
			t.Fatalf("address blocked early at %d", i+5)
		}
	}
	if ok, _ := l.Allow("x", "10.0.0.1"); ok {
		t.Error("success reset the address count")
	}
}

// SPEC 12.4: an attempt counts before its password is checked, so parallel
// attempts cannot all pass Allow; one that never checked gives it back.
func TestLimiterAllowReservesAndReleaseGivesBack(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for range LimitPerUsername {
		if ok, _ := l.Allow("alice", "10.0.0.1"); !ok {
			t.Fatal("blocked too early")
		}
	}
	if ok, _ := l.Allow("alice", "10.0.0.2"); ok {
		t.Fatal("allowed more attempts than the limit before any finished")
	}
	l.Release("alice", "10.0.0.1")
	if ok, _ := l.Allow("alice", "10.0.0.1"); !ok {
		t.Fatal("a released attempt still counts")
	}
}

// SPEC 12.4: IPv6 clients share a count across their /64; IPv4 is per
// address.
func TestLimiterGroupsIPv6By64(t *testing.T) {
	clk := newClock()
	l := NewLimiter(clk.Now)
	for i := range LimitPerAddress {
		if ok, _ := l.Allow(fmt.Sprintf("u%d", i), fmt.Sprintf("2001:db8:1:2::%x", i+1)); !ok {
			t.Fatalf("blocked early at %d", i+1)
		}
	}
	if ok, _ := l.Allow("fresh", "2001:db8:1:2:ffff::1"); ok {
		t.Error("another address in the same /64 was not blocked")
	}
	if ok, _ := l.Allow("fresh", "2001:db8:1:3::1"); !ok {
		t.Error("a different /64 was blocked")
	}
	for i := range LimitPerAddress {
		l.Allow(fmt.Sprintf("v%d", i), "192.0.2.1")
	}
	if ok, _ := l.Allow("fresh", "192.0.2.2"); !ok {
		t.Error("IPv4 neighbours share a count")
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now, 100)
	for i := range 1000 {
		clk.Advance(time.Millisecond)
		l.Allow(fmt.Sprintf("user%d", i), fmt.Sprintf("addr%d", i))
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
		l.Allow(fmt.Sprintf("user%d", i), "10.0.0.1")
	}
	clk.Advance(LimitWindow)
	// Only this attempt's own entries remain.
	l.Allow("anyone", "10.0.0.1")
	if len(l.users.byKey) != 1 || len(l.addrs.byKey) != 1 {
		t.Errorf("expired entries remain: %d users, %d addrs", len(l.users.byKey), len(l.addrs.byKey))
	}
}

// The store compares usernames with SQLite NOCASE, which folds ASCII A to Z
// only. The limiter key must fold the same way, so names the database treats
// as different accounts never share a key.
func TestUserKeyFoldsASCIIOnly(t *testing.T) {
	if userKey("Kate") != userKey("kate") {
		t.Error(`"Kate" and "kate" should share a key`)
	}
	if userKey("Kate") == userKey("kate") {
		t.Error("the Kelvin sign must not share a key with ASCII k")
	}
}

func TestAddrLimiter(t *testing.T) {
	clk := newClock()
	l := NewAddrLimiter(clk.Now)
	l.Allow("192.0.2.9")
	l.Succeed("192.0.2.9") // a success costs nothing
	for i := range LimitPerAddress {
		// Addresses in one IPv6 /64 share a count.
		if ok, _ := l.Allow(fmt.Sprintf("2001:db8::%x", i+1)); !ok {
			t.Fatalf("blocked after %d failures", i)
		}
	}
	clk.Advance(time.Minute)
	if ok, left := l.Allow("2001:db8::ffff"); ok || left != LimitWindow-time.Minute {
		t.Errorf("over the limit: ok=%v left=%v", ok, left)
	}
	if ok, _ := l.Allow("2001:db8:0:1::1"); !ok {
		t.Error("another /64 was blocked")
	}
	if ok, _ := l.Allow("192.0.2.9"); !ok {
		t.Error("an IPv4 address was blocked")
	}
	clk.Advance(LimitWindow)
	if ok, _ := l.Allow("2001:db8::1"); !ok {
		t.Error("still blocked after the window")
	}
}

func TestAddrLimiterApartFromSignIn(t *testing.T) {
	clk := newClock()
	mcp, signIn := NewAddrLimiter(clk.Now), NewLimiter(clk.Now)
	for range LimitPerAddress {
		mcp.Allow("192.0.2.1")
	}
	if ok, _ := signIn.Allow("ann", "192.0.2.1"); !ok {
		t.Error("MCP failures blocked browser sign-in")
	}
}
