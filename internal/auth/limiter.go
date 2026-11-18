package auth

import (
	"container/list"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Limits for failed sign-ins (SPEC 12.4).
const (
	LimitWindow      = 15 * time.Minute
	LimitPerUsername = 5
	LimitPerAddress  = 20
	limitMaxEntries  = 10000
)

// Limiter counts sign-in attempts per username and per client address. An
// attempt is counted when Allow lets it through, before the password is
// checked, so parallel guesses cannot outrun the limit; a success, or an
// attempt that never checked a password, gives its count back. A window
// starts at the first attempt; once a key reaches its limit it is refused
// until the window ends. State is in memory, so a restart clears it.
type Limiter struct {
	now   func() time.Time
	mu    sync.Mutex
	users *failureTable
	addrs *failureTable
}

// NewLimiter returns a Limiter with the settled limits.
func NewLimiter(now func() time.Time) *Limiter {
	return newLimiter(now, limitMaxEntries)
}

func newLimiter(now func() time.Time, maxEntries int) *Limiter {
	return &Limiter{
		now:   now,
		users: newFailureTable(LimitPerUsername, maxEntries),
		addrs: newFailureTable(LimitPerAddress, maxEntries),
	}
}

// userKey folds ASCII A to Z only, as SQLite NOCASE does in the store, so two
// names the database keeps apart never share a limiter key.
func userKey(username string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, strings.TrimSpace(username))
}

// addrKey groups IPv6 clients by their /64, since one host usually holds
// a whole /64. IPv4 addresses and unparsable strings are used as given.
func addrKey(addr string) string {
	a, err := netip.ParseAddr(addr)
	if err != nil || !a.Is6() || a.Is4In6() {
		return addr
	}
	return netip.PrefixFrom(a.WithZone(""), 64).Masked().String()
}

// Allow reports whether a sign-in attempt may proceed and, if so, counts it
// against the username and the address. When it may not, retryAfter is the
// time left in the longer of the two blocking windows.
func (l *Limiter) Allow(username, addr string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	uk, ak := userKey(username), addrKey(addr)
	ru := l.users.remaining(uk, now)
	ra := l.addrs.remaining(ak, now)
	if ru > 0 || ra > 0 {
		return false, max(ru, ra)
	}
	l.users.add(uk, now)
	l.addrs.add(ak, now)
	return true, 0
}

// Release gives back an attempt that Allow counted but that never checked
// a password, for example because the server was busy.
func (l *Limiter) Release(username, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.users.release(userKey(username))
	l.addrs.release(addrKey(addr))
}

// Succeed clears the username's count and gives back this attempt's address
// count. Earlier address failures stay, so one valid account cannot be used
// to reset an address's failures.
func (l *Limiter) Succeed(username, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.users.clear(userKey(username))
	l.addrs.release(addrKey(addr))
}

// AllowAddr is Allow for a sign-in that names no username, such as an OIDC
// callback (SPEC 12.7): it counts against the address only.
func (l *Limiter) AllowAddr(addr string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now, k := l.now(), addrKey(addr)
	if r := l.addrs.remaining(k, now); r > 0 {
		return false, r
	}
	l.addrs.add(k, now)
	return true, 0
}

// ReleaseAddr gives back an attempt AllowAddr counted, because it worked.
// Earlier failures stay.
func (l *Limiter) ReleaseAddr(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.addrs.release(addrKey(addr))
}

// AddrLimiter counts API token checks per client address, apart from
// sign-in's counts, so guessing at the MCP endpoint cannot lock anyone out
// of the browser sign-in (SPEC 14.19). It has sign-in's address limit and
// window, groups IPv6 by /64, and counts an attempt before the check, as
// Limiter does, so parallel guesses cannot outrun it.
type AddrLimiter struct {
	now   func() time.Time
	mu    sync.Mutex
	addrs *failureTable
}

// NewAddrLimiter returns an AddrLimiter with the settled limits.
func NewAddrLimiter(now func() time.Time) *AddrLimiter {
	return &AddrLimiter{now: now, addrs: newFailureTable(LimitPerAddress, limitMaxEntries)}
}

// Allow reports whether a token check from addr may proceed and, if so,
// counts it. When it may not, retryAfter is the time left in the window.
func (l *AddrLimiter) Allow(addr string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now, k := l.now(), addrKey(addr)
	if r := l.addrs.remaining(k, now); r > 0 {
		return false, r
	}
	l.addrs.add(k, now)
	return true, 0
}

// Succeed gives back the attempt Allow counted, because the token was
// accepted. Earlier failures stay.
func (l *AddrLimiter) Succeed(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.addrs.release(addrKey(addr))
}

type failure struct {
	key   string
	start time.Time
	count int
}

// failureTable keeps entries in start order. Windows all have the same
// length, so the front of the list is always the next to expire, and also
// the one to evict when the table is full.
type failureTable struct {
	limit, maxEntries int
	byKey             map[string]*list.Element
	order             *list.List
}

func newFailureTable(limit, maxEntries int) *failureTable {
	return &failureTable{limit: limit, maxEntries: maxEntries, byKey: map[string]*list.Element{}, order: list.New()}
}

func (t *failureTable) expire(now time.Time) {
	for e := t.order.Front(); e != nil; e = t.order.Front() {
		if now.Sub(e.Value.(*failure).start) < LimitWindow {
			return
		}
		t.remove(e)
	}
}

func (t *failureTable) remove(e *list.Element) {
	delete(t.byKey, e.Value.(*failure).key)
	t.order.Remove(e)
}

// remaining is zero unless the key is over its limit.
func (t *failureTable) remaining(key string, now time.Time) time.Duration {
	t.expire(now)
	e, ok := t.byKey[key]
	if !ok {
		return 0
	}
	f := e.Value.(*failure)
	if f.count < t.limit {
		return 0
	}
	return f.start.Add(LimitWindow).Sub(now)
}

func (t *failureTable) add(key string, now time.Time) {
	t.expire(now)
	if e, ok := t.byKey[key]; ok {
		e.Value.(*failure).count++
		return
	}
	for t.order.Len() >= t.maxEntries {
		t.remove(t.order.Front())
	}
	t.byKey[key] = t.order.PushBack(&failure{key: key, start: now, count: 1})
}

func (t *failureTable) release(key string) {
	e, ok := t.byKey[key]
	if !ok {
		return
	}
	if f := e.Value.(*failure); f.count > 1 {
		f.count--
		return
	}
	t.remove(e)
}

func (t *failureTable) clear(key string) {
	if e, ok := t.byKey[key]; ok {
		t.remove(e)
	}
}
