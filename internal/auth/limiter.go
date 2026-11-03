package auth

import (
	"container/list"
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

// Limiter counts failed sign-ins per username and per client address. A
// window starts at the first failure; once a key reaches its limit it is
// refused until the window ends. State is in memory, so a restart clears it.
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

func userKey(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

// Allow reports whether a sign-in attempt may proceed. When it may not,
// retryAfter is the time left in the longer of the two blocking windows.
func (l *Limiter) Allow(username, addr string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	ru := l.users.remaining(userKey(username), now)
	ra := l.addrs.remaining(addr, now)
	return ru == 0 && ra == 0, max(ru, ra)
}

// Fail records a failed sign-in against the username and the address.
func (l *Limiter) Fail(username, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.users.add(userKey(username), now)
	l.addrs.add(addr, now)
}

// Succeed clears the username's count. The address count stays, so one
// valid account cannot be used to reset an address's failures.
func (l *Limiter) Succeed(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.users.clear(userKey(username))
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

func (t *failureTable) clear(key string) {
	if e, ok := t.byKey[key]; ok {
		t.remove(e)
	}
}
