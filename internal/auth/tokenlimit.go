package auth

import (
	"sync"
	"time"
)

// Per-token request limits (SPEC 14.19).
const (
	TokenRequestsPerMinute = 120
	TokenConcurrent        = 4
)

// TokenLimiter limits each token to TokenRequestsPerMinute requests in a
// fixed minute that starts at its first request, and to TokenConcurrent at
// once. State is in memory, so a restart clears it.
type TokenLimiter struct {
	now func() time.Time
	mu  sync.Mutex
	by  map[int64]*tokenUse
}

type tokenUse struct {
	start    time.Time
	count    int
	inFlight int
}

// NewTokenLimiter returns a TokenLimiter with the settled limits.
func NewTokenLimiter(now func() time.Time) *TokenLimiter {
	return &TokenLimiter{now: now, by: map[int64]*tokenUse{}}
}

// Acquire counts a request for the token. When ok, the caller must call
// release once the request ends. When not, retryAfter says how long to
// wait: the rest of the minute, or one second when the token only has too
// many requests running.
func (l *TokenLimiter) Acquire(tokenID int64) (release func(), retryAfter time.Duration, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.expire(now)
	u := l.by[tokenID]
	if u == nil {
		u = &tokenUse{start: now}
		l.by[tokenID] = u
	}
	if u.count >= TokenRequestsPerMinute {
		return nil, u.start.Add(time.Minute).Sub(now), false
	}
	if u.inFlight >= TokenConcurrent {
		return nil, time.Second, false
	}
	u.count++
	u.inFlight++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			u.inFlight--
		})
	}, 0, true
}

// expire starts a new minute for each token whose minute ended, and drops
// idle entries so the map holds only tokens in use.
func (l *TokenLimiter) expire(now time.Time) {
	for id, u := range l.by {
		if now.Sub(u.start) < time.Minute {
			continue
		}
		if u.inFlight == 0 {
			delete(l.by, id)
			continue
		}
		u.start, u.count = now, 0
	}
}
