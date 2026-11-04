package fakegame

import (
	"fmt"
	"sync"
	"testing"
)

// tb lets dockertest and rcontest, which take a testing.TB, run outside a
// test. It covers the methods those two packages call and nothing else; the
// embedded interface is nil, so any other method would panic.
type tb struct {
	testing.TB
	report func(string)

	mu       sync.Mutex
	cleanups []func()
}

func (b *tb) Helper() {}

func (b *tb) Errorf(format string, args ...any) { b.report(fmt.Sprintf(format, args...)) }

// Fatalf cannot end the goroutine as a test would, so it panics. It is only
// reached when the machine has no free loopback port.
func (b *tb) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	b.report(msg)
	panic("fakegame: " + msg)
}

func (b *tb) Cleanup(f func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanups = append(b.cleanups, f)
}

// runCleanups runs the registered functions last to first, like a test does.
func (b *tb) runCleanups() {
	b.mu.Lock()
	fns := b.cleanups
	b.cleanups = nil
	b.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}
