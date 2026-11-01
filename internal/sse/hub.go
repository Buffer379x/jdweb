// Package sse is the fan-out from the sync engine to the open browsers. It
// knows nothing about HTTP: the API layer turns a Message into an event frame.
package sse

import "sync"

// BufferSize is how many messages may wait for one subscriber. The engine sends
// a few messages per second, so this is many seconds of a stalled browser.
const BufferSize = 64

// Message is one server-sent event. Data is ready-made JSON, encoded once no
// matter how many browsers are open.
type Message struct {
	Event string
	Data  []byte
}

// Hub hands every published message to every subscriber. It is safe for
// concurrent use.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Message]struct{}
}

// New returns a hub without subscribers.
func New() *Hub { return &Hub{subs: map[chan Message]struct{}{}} }

// Subscribe adds a subscriber. The channel closes after cancel, or when the
// subscriber fell BufferSize messages behind: it has then missed data and has
// to reconnect and fetch a fresh state. cancel may be called more than once.
func (h *Hub) Subscribe() (<-chan Message, func()) {
	ch := make(chan Message, BufferSize)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.drop(ch)
	}
}

// Publish sends m to all subscribers without ever waiting for one. All
// subscribers see all messages in the same order.
func (h *Hub) Publish(m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- m:
		default:
			h.drop(ch)
		}
	}
}

// Subscribers returns how many subscribers are connected.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// drop removes and closes a subscriber. The channel is closed under the lock,
// so Publish can never send on a closed channel.
func (h *Hub) drop(ch chan Message) {
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}
