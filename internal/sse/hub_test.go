package sse_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/sse"
)

func msg(i int) sse.Message {
	return sse.Message{Event: "status", Data: []byte(fmt.Sprintf(`{"n":%d}`, i))}
}

func receive(t *testing.T, ch <-chan sse.Message) (sse.Message, bool) {
	t.Helper()
	select {
	case m, ok := <-ch:
		return m, ok
	case <-time.After(5 * time.Second):
		t.Fatal("no message within 5 s")
		return sse.Message{}, false
	}
}

func TestEverySubscriberGetsEveryMessageInOrder(t *testing.T) {
	h := sse.New()
	a, cancelA := h.Subscribe()
	defer cancelA()
	b, cancelB := h.Subscribe()
	defer cancelB()

	for i := range 5 {
		h.Publish(msg(i))
	}
	for _, ch := range []<-chan sse.Message{a, b} {
		for i := range 5 {
			m, ok := receive(t, ch)
			if !ok || string(m.Data) != fmt.Sprintf(`{"n":%d}`, i) || m.Event != "status" {
				t.Fatalf("message %d = %+v, open %v", i, m, ok)
			}
		}
	}
}

func TestPublishWithoutSubscribersDoesNothing(t *testing.T) {
	sse.New().Publish(msg(1))
}

func TestCancelClosesTheChannelAndIsIdempotent(t *testing.T) {
	h := sse.New()
	ch, cancel := h.Subscribe()
	cancel()
	cancel()
	if _, ok := receive(t, ch); ok {
		t.Fatal("channel still open after cancel")
	}
	h.Publish(msg(1)) // must not send on the closed channel
	if n := h.Subscribers(); n != 0 {
		t.Errorf("subscribers = %d", n)
	}
}

// A subscriber that does not read must neither block the publisher nor cost
// the others a message. It is disconnected and refetches when it reconnects.
func TestSlowSubscriberIsDisconnectedWithoutBlockingOthers(t *testing.T) {
	h := sse.New()
	slow, cancelSlow := h.Subscribe()
	defer cancelSlow()
	fast, cancelFast := h.Subscribe()
	defer cancelFast()

	total := sse.BufferSize*3 + 1
	var got []sse.Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for len(got) < total {
			m, ok := <-fast
			if !ok {
				return
			}
			got = append(got, m)
		}
	}()

	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := range total {
			h.Publish(msg(i))
		}
	}()
	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("fast subscriber got %d of %d", len(got), total)
	}
	if len(got) != total {
		t.Fatalf("fast subscriber got %d of %d", len(got), total)
	}

	// The slow one keeps what fitted into its buffer, then its channel ends.
	n := 0
	for {
		_, ok := receive(t, slow)
		if !ok {
			break
		}
		n++
	}
	if n != sse.BufferSize {
		t.Errorf("slow subscriber read %d messages before the end, want the buffer of %d", n, sse.BufferSize)
	}
	if s := h.Subscribers(); s != 1 {
		t.Errorf("subscribers = %d, want only the fast one", s)
	}
	cancelSlow() // a late cancel of a dropped subscriber is harmless
}

func TestConcurrentUse(t *testing.T) {
	h := sse.New()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := range 200 {
				h.Publish(msg(i))
			}
		}()
		go func() {
			defer wg.Done()
			for range 20 {
				ch, cancel := h.Subscribe()
				select {
				case <-ch:
				case <-time.After(time.Millisecond):
				}
				cancel()
			}
		}()
	}
	wg.Wait()
	if n := h.Subscribers(); n != 0 {
		t.Errorf("subscribers = %d", n)
	}
}
