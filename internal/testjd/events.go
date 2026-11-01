package testjd

import (
	"sort"
	"sync"
)

// Event is one event as JDownloader reports it. EventData is any JSON value.
type Event struct {
	Publisher string `json:"publisher"`
	EventID   string `json:"eventid"`
	EventData any    `json:"eventdata"`
}

// sub is an event subscription with its queue.
type sub struct {
	id         int64
	publishers map[string]bool
	exclusions map[string]bool
	interval   int64
	queue      []Event

	wake chan struct{} // a signal that the queue grew
	gone chan struct{} // closed when the subscription is removed
	once sync.Once
}

func newSub(id int64, publishers, exclusions []string) *sub {
	sb := &sub{id: id, wake: make(chan struct{}, 1), gone: make(chan struct{})}
	sb.set(publishers, exclusions)
	return sb
}

func (sb *sub) set(publishers, exclusions []string) {
	sb.publishers = map[string]bool{}
	for _, p := range publishers {
		sb.publishers[p] = true
	}
	sb.exclusions = map[string]bool{}
	for _, e := range exclusions {
		sb.exclusions[e] = true
	}
}

func (sb *sub) close() { sb.once.Do(func() { close(sb.gone) }) }

func (sb *sub) names() []string {
	var out []string
	for p := range sb.publishers {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (sb *sub) accepts(e Event) bool {
	return sb.publishers[e.Publisher] && !sb.exclusions[e.Publisher+"."+e.EventID]
}

// Emit queues an event for every subscription that listens to its publisher.
func (s *Server) Emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitLocked(e)
}

func (s *Server) emitLocked(e Event) {
	for _, sb := range s.subs {
		if !sb.accepts(e) {
			continue
		}
		sb.queue = append(sb.queue, e)
		select {
		case sb.wake <- struct{}{}:
		default:
		}
	}
}

// emit queues an event from inside a handler.
func (s *Server) emit(publisher, id string, data any) {
	s.emitLocked(Event{Publisher: publisher, EventID: id, EventData: data})
}

// Subscriptions returns the ids of the live subscriptions.
func (s *Server) Subscriptions() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int64
	for id := range s.subs {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Queued returns how many events wait in a subscription, -1 if it does not exist.
func (s *Server) Queued(id int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sb, ok := s.subs[id]; ok {
		return len(sb.queue)
	}
	return -1
}
