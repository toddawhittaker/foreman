package game

import (
	"sync"
	"testing"
	"time"
)

func recv[T any](t *testing.T, ch <-chan T) (T, bool) {
	t.Helper()
	select {
	case v, ok := <-ch:
		return v, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the channel")
		panic("unreachable")
	}
}

func TestHubDeliversToEverySubscriberInOrder(t *testing.T) {
	h := NewHub[int](4)
	a, _ := h.Subscribe()
	b, _ := h.Subscribe()
	h.Publish(1)
	h.Publish(2)
	for _, ch := range []<-chan int{a, b} {
		for _, want := range []int{1, 2} {
			if got, ok := recv(t, ch); !ok || got != want {
				t.Errorf("got %d, %v; want %d", got, ok, want)
			}
		}
	}
}

func TestHubUnsubscribeClosesAndIsRepeatable(t *testing.T) {
	h := NewHub[int](1)
	ch, cancel := h.Subscribe()
	other, _ := h.Subscribe()
	cancel()
	cancel()
	if _, ok := recv(t, ch); ok {
		t.Error("channel still open after unsubscribe")
	}
	h.Publish(7)
	if got, _ := recv(t, other); got != 7 {
		t.Errorf("other subscriber got %d", got)
	}
}

func TestHubDropsASlowSubscriberWithoutBlockingOthers(t *testing.T) {
	h := NewHub[int](2)
	slow, _ := h.Subscribe()
	fast, _ := h.Subscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 1; i <= 5; i++ {
			h.Publish(i)
			if got, ok := recv(t, fast); !ok || got != i {
				t.Errorf("fast got %d, %v; want %d", got, ok, i)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	// The slow one got its two buffered values, then was closed.
	for _, want := range []int{1, 2} {
		if got, ok := recv(t, slow); !ok || got != want {
			t.Errorf("slow got %d, %v; want %d", got, ok, want)
		}
	}
	if _, ok := recv(t, slow); ok {
		t.Error("slow subscriber was not closed")
	}
	// The fast one is still subscribed.
	h.Publish(6)
	if got, ok := recv(t, fast); !ok || got != 6 {
		t.Errorf("fast got %d, %v after the drop", got, ok)
	}
}

func TestHubUnsubscribeAfterDropIsSafe(t *testing.T) {
	h := NewHub[int](1)
	_, cancel := h.Subscribe()
	h.Publish(1)
	h.Publish(2) // buffer full, dropped
	cancel()
}

func TestHubClose(t *testing.T) {
	h := NewHub[int](1)
	ch, cancel := h.Subscribe()
	h.Close()
	h.Close()
	if _, ok := recv(t, ch); ok {
		t.Error("channel still open after Close")
	}
	cancel()
	h.Publish(1)
	late, lateCancel := h.Subscribe()
	if _, ok := recv(t, late); ok {
		t.Error("subscribing to a closed hub gave an open channel")
	}
	lateCancel()
}

func TestHubZeroSizeStillBuffersOne(t *testing.T) {
	h := NewHub[int](0)
	ch, _ := h.Subscribe()
	h.Publish(1)
	if got, ok := recv(t, ch); !ok || got != 1 {
		t.Errorf("got %d, %v", got, ok)
	}
}

func TestHubConcurrent(t *testing.T) {
	h := NewHub[int](8)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				h.Publish(i)
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				ch, cancel := h.Subscribe()
				// Read a little, sometimes not at all, so some get dropped.
				if i%2 == 0 {
					select {
					case <-ch:
					default:
					}
				}
				cancel()
			}
		}()
	}
	wg.Wait()
	h.Close()
}
