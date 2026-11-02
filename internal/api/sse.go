package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// eventWriteTimeout limits one write to a browser. A stream has no overall
// write deadline, but a browser that stops reading must not hold a goroutine
// forever.
const eventWriteTimeout = 15 * time.Second

// handleEvents is GET /events. It counts as an open browser for the engine,
// sends the current status and connection state first, then every event of the
// hub as it comes. It ends when the browser leaves, when the hub dropped this
// subscriber because it fell behind (the browser then reconnects and loads the
// lists again), or when the server shuts down.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	select {
	case <-s.done:
		writeError(w, newError(http.StatusServiceUnavailable, codeInternal, "The server is shutting down."))
		return
	default:
	}
	// No deadlines for the stream itself, see eventWriteTimeout.
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Now().Add(eventWriteTimeout))

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // reverse proxies must not buffer the stream
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.hub.Subscribe()
	defer cancel()
	s.engine.Attach()
	defer s.engine.Detach()

	st := s.engine.Status()
	first := []struct {
		event string
		v     any
	}{
		{"status", st},
		{"connection", map[string]any{"connection": st.Connection, "detail": st.ConnectionDetail}},
	}
	if err := writeFrame(w, rc, []byte("retry: 3000")); err != nil {
		return
	}
	for _, f := range first {
		b, err := json.Marshal(f.v)
		if err != nil || writeEvent(w, rc, f.event, b) != nil {
			return
		}
	}

	tick := time.NewTicker(s.keep)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case m, ok := <-ch:
			if !ok {
				return // fell behind: end the stream, the browser reconnects
			}
			if writeEvent(w, rc, m.Event, m.Data) != nil {
				return
			}
		case <-tick.C:
			if writeFrame(w, rc, []byte(": keepalive")) != nil {
				return
			}
		}
	}
}

// writeEvent sends one event. Data holds JSON without line breaks; should it
// hold any, each line becomes its own data field as the format demands.
func writeEvent(w io.Writer, rc *http.ResponseController, event string, data []byte) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "event: %s\n", event)
	for _, line := range bytes.Split(data, []byte("\n")) {
		buf.WriteString("data: ")
		buf.Write(bytes.TrimSuffix(line, []byte("\r")))
		buf.WriteByte('\n')
	}
	return writeFrame(w, rc, buf.Bytes())
}

// writeFrame sends a block of lines followed by an empty line, and flushes.
func writeFrame(w io.Writer, rc *http.ResponseController, block []byte) error {
	_ = rc.SetWriteDeadline(time.Now().Add(eventWriteTimeout))
	if _, err := w.Write(append(bytes.TrimRight(block, "\n"), '\n', '\n')); err != nil {
		return err
	}
	return rc.Flush()
}
