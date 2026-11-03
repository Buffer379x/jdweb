//go:build integration

package integration

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// sseEvent is one event of GET /events with the time it arrived.
type sseEvent struct {
	Name string
	Data string
	At   time.Time
}

// stream is a browser-like reader of GET /events.
type stream struct {
	mu     sync.Mutex
	events []sseEvent
	status int
	ended  chan struct{}
	cancel context.CancelFunc
}

// openStream connects to /events with the session of a. The stream ends with
// the test.
func (a *app) openStream(t testing.TB) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &stream{ended: make(chan struct{}), cancel: cancel}
	req, _ := http.NewRequestWithContext(ctx, "GET", a.base+"/api/v1/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	// A client without the overall timeout of a.http: the stream is long.
	c := &http.Client{Jar: a.http.Jar, CheckRedirect: a.http.CheckRedirect}
	resp, err := c.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s.status = resp.StatusCode
	t.Cleanup(func() { cancel(); <-s.ended })
	go func() {
		defer close(s.ended)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		var name string
		var data []string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if name != "" {
					s.mu.Lock()
					s.events = append(s.events, sseEvent{name, strings.Join(data, "\n"), time.Now()})
					s.mu.Unlock()
				}
				name, data = "", nil
			case strings.HasPrefix(line, "event:"):
				name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
	}()
	return s
}

func (s *stream) snapshot() []sseEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sseEvent(nil), s.events...)
}

// mark returns the number of events so far; wait(mark, ...) only looks at newer ones.
func (s *stream) mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// find returns the first event after mark with this name whose data holds every part.
func (s *stream) find(from int, name string, parts ...string) (sseEvent, bool) {
	evs := s.snapshot()
	for i := from; i < len(evs); i++ {
		if evs[i].Name != name {
			continue
		}
		ok := true
		for _, p := range parts {
			if !strings.Contains(evs[i].Data, p) {
				ok = false
				break
			}
		}
		if ok {
			return evs[i], true
		}
	}
	return sseEvent{}, false
}

func (s *stream) wait(t testing.TB, from int, timeout time.Duration, name string, parts ...string) sseEvent {
	t.Helper()
	var ev sseEvent
	waitFor(t, "SSE event "+name+" "+strings.Join(parts, " "), timeout, func() bool {
		var ok bool
		ev, ok = s.find(from, name, parts...)
		return ok
	})
	return ev
}

func (s *stream) isEnded() bool {
	select {
	case <-s.ended:
		return true
	default:
		return false
	}
}
