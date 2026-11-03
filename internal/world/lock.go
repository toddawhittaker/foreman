package world

import (
	"errors"
	"sync"
)

// ErrBusy is returned by Lock when another operation holds the world.
var ErrBusy = errors.New("another operation is running on this world. Wait for it to finish, then try again")

// locks is a set of per-world try-locks. A try-lock, not a queue: a second
// admin's request is refused at once rather than run after the first.
type locks struct {
	mu   sync.Mutex
	held map[string]bool
}

// Lock takes the world's lock, or returns ErrBusy at once if it is held.
// Call the returned function to release it. Server control holds it across
// stop, snapshot, restore and start, so no two of them overlap.
func (m *Manager) Lock(id string) (unlock func(), err error) {
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
