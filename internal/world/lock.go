package world

import (
	"errors"
	"sync"
	"time"
)

// ErrBusy is returned by Lock when another operation holds the world.
var ErrBusy = errors.New("another operation is running on this world. Wait for it to finish, then try again")

// locks is a set of per-world try-locks. A try-lock, not a queue: a second
// admin's request is refused at once rather than run after the first.
type locks struct {
	mu   sync.Mutex
	held map[string]bool
	// yield is SetLockYield's function, nil for none.
	yield func(id string) (done func())
}

// SetLockYield gives Lock work to call before it takes a world's lock:
// yield ends the background work Foreman started on the world by itself
// (a read of a copy of its save, SPEC 8.16), waits until that work has
// released the world, and keeps new work from beginning until the returned
// function is called, which Lock does once it has the lock or has refused.
// So a user's operation is never refused because of Foreman's own work.
func (m *Manager) SetLockYield(yield func(id string) (done func())) {
	m.locks.mu.Lock()
	defer m.locks.mu.Unlock()
	m.locks.yield = yield
}

// Lock takes the world's lock, or returns ErrBusy at once if another
// operation holds it. Background work that yields (SetLockYield) is ended
// first. Call the returned function to release it. Server control holds it
// across stop, snapshot, restore and start, so no two of them overlap.
func (m *Manager) Lock(id string) (unlock func(), err error) {
	m.locks.mu.Lock()
	yield := m.locks.yield
	m.locks.mu.Unlock()
	if yield != nil {
		defer yield(id)()
	}
	return m.LockBackground(id)
}

// LockBackground is Lock for the background work Lock yields to: it ends
// nothing first, so that work must not call Lock itself.
func (m *Manager) LockBackground(id string) (unlock func(), err error) {
	l := &m.locks
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[id] {
		return nil, ErrBusy
	}
	if l.held == nil {
		l.held = map[string]bool{}
	}
	l.held[id] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.held, id)
			l.mu.Unlock()
		})
	}, nil
}

// lockPoll is how often lockWait tries the lock again.
const lockPoll = 50 * time.Millisecond

// lockWait is Lock, but a held world is retried until wait has passed
// before ErrBusy is returned.
func (m *Manager) lockWait(id string, wait time.Duration) (unlock func(), err error) {
	deadline := time.Now().Add(wait)
	for {
		unlock, err = m.Lock(id)
		if !errors.Is(err, ErrBusy) || !time.Now().Before(deadline) {
			return unlock, err
		}
		time.Sleep(lockPoll)
	}
}
