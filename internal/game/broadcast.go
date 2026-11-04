package game

import "sync"

// Hub fans values out to the pages that are watching. A subscriber that
// falls behind is dropped, so one slow page never holds up the others. It is
// safe for concurrent use.
type Hub[T any] struct {
	mu     sync.Mutex
	size   int
	subs   map[*subscriber[T]]struct{}
	closed bool
}

type subscriber[T any] struct {
	ch chan T
}

// NewHub returns a Hub whose subscribers each buffer up to size values.
func NewHub[T any](size int) *Hub[T] {
	return &Hub[T]{size: max(size, 1), subs: map[*subscriber[T]]struct{}{}}
}

// Subscribe returns a channel of published values and a function that
// unsubscribes. The channel is closed when the subscriber is dropped for
// being slow, when the Hub is closed, or when the function is called. The
// function may be called more than once. On a closed Hub the channel is
// already closed.
func (h *Hub[T]) Subscribe() (<-chan T, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &subscriber[T]{ch: make(chan T, h.size)}
	if h.closed {
		close(s.ch)
		return s.ch, func() {}
	}
	h.subs[s] = struct{}{}
	return s.ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.drop(s)
	}
}

// Publish offers v to every subscriber without blocking. A subscriber whose
// buffer is full is dropped and its channel closed.
func (h *Hub[T]) Publish(v T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		select {
		case s.ch <- v:
		default:
			h.drop(s)
		}
	}
}

// Close drops every subscriber and refuses new ones. It is for shutdown.
func (h *Hub[T]) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		h.drop(s)
	}
}

// drop removes s and closes its channel once. The caller holds h.mu.
func (h *Hub[T]) drop(s *subscriber[T]) {
	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	close(s.ch)
}
