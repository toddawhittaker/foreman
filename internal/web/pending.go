package web

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// pendingTTL is how long a previewed settings change waits for its
// confirmation (SPEC 11.3).
const pendingTTL = 15 * time.Minute

// maxPendingPerUser bounds the memory a person's open previews can hold; a
// new preview past it drops the oldest.
const maxPendingPerUser = 8

// pendingFile is one file of a previewed change: the text to write, and the
// hash the file had when the person saw the diff.
type pendingFile struct {
	File string
	// Hash is the file's hash at the preview; "" when it did not exist.
	Hash string
	New  []byte
}

// pendingChange is a settings change that has been previewed and not yet
// confirmed. It is held on the server, under a random token, so the page
// that confirms it carries no setting and no secret (SPEC 11.3, 11.4).
type pendingChange struct {
	UserID  int64
	WorldID string
	// Raw is true when the change came from the raw editor, so "go back"
	// returns to it.
	Raw   bool
	Files []pendingFile
	// Form is the form submission behind the change, nil for a raw edit.
	Form    *formEdit
	expires time.Time
	created time.Time
}

// pendingStore holds previewed changes in memory only. They are lost on a
// restart, which is harmless: the person previews again.
type pendingStore struct {
	mu  sync.Mutex
	m   map[string]*pendingChange
	now func() time.Time
}

func newPendingStore(now func() time.Time) *pendingStore {
	return &pendingStore{m: map[string]*pendingChange{}, now: now}
}

// add stores c and returns its token.
func (p *pendingStore) add(c pendingChange) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked()
	now := p.now()
	c.created, c.expires = now, now.Add(pendingTTL)
	var mine []string
	for tok, o := range p.m {
		if o.UserID == c.UserID {
			mine = append(mine, tok)
		}
	}
	for len(mine) >= maxPendingPerUser {
		oldest := 0
		for i, tok := range mine {
			if p.m[tok].created.Before(p.m[mine[oldest]].created) {
				oldest = i
			}
		}
		delete(p.m, mine[oldest])
		mine = append(mine[:oldest], mine[oldest+1:]...)
	}
	tok := newToken()
	p.m[tok] = &c
	return tok
}

// get returns the change under token when it is this user's, for this
// world, and has not expired. Anything else is "not found", so a token does
// not show that another person's change exists.
func (p *pendingStore) get(token string, userID int64, worldID string) (*pendingChange, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked()
	c, ok := p.m[token]
	if !ok || c.UserID != userID || c.WorldID != worldID {
		return nil, false
	}
	return c, true
}

// take is get that also removes the change, so one confirmation saves it
// once.
func (p *pendingStore) take(token string, userID int64, worldID string) (*pendingChange, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked()
	c, ok := p.m[token]
	if !ok || c.UserID != userID || c.WorldID != worldID {
		return nil, false
	}
	delete(p.m, token)
	return c, true
}

func (p *pendingStore) expireLocked() {
	now := p.now()
	for tok, c := range p.m {
		if !now.Before(c.expires) {
			delete(p.m, tok)
		}
	}
}

// newToken is 128 random bits, URL-safe.
func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// The system's random source failing is not survivable.
		panic("read random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
