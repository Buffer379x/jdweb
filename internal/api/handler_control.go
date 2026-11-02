package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// maxSpeedLimit is the highest limit accepted, 100 GiB/s.
const maxSpeedLimit = 100 << 30

// premiumOffWait is how long switching premium off waits for an answer.
// JDownloader asks for a confirmation in a dialog and does not answer until
// somebody answers the dialog.
var premiumOffWait = 3 * time.Second

func (s *Server) handleController(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := s.client()
		if err == nil {
			switch action {
			case "start":
				err = c.ControllerStart(r.Context())
			case "stop":
				err = c.ControllerStop(r.Context())
			case "pause":
				err = c.ControllerPause(r.Context(), true)
			case "resume":
				err = c.ControllerPause(r.Context(), false)
			}
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		noContent(w)
	}
}

func (s *Server) handleSpeedLimit(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled        bool  `json:"enabled"`
		BytesPerSecond int64 `json:"bytesPerSecond"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if b.BytesPerSecond < 0 || b.BytesPerSecond > maxSpeedLimit {
		s.fail(w, r, badRequest("bytesPerSecond must be between 0 and %d.", int64(maxSpeedLimit)))
		return
	}
	if b.Enabled && b.BytesPerSecond == 0 && s.engine.Status().Toggles.LimitBytes == 0 {
		s.fail(w, r, badRequest("Switching the limit on needs bytesPerSecond above 0."))
		return
	}
	c, err := s.client()
	if err == nil {
		err = c.SetSpeedLimit(r.Context(), b.Enabled, b.BytesPerSecond)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

// toggleDef describes one switch: how to read it from the status and how to flip it.
type toggleDef struct {
	current func(t togglesView) bool
	flip    func(c *jd.Client, ctx context.Context) (bool, error)
}

type togglesView struct{ premium, clipboard, reconnect, stopAfter bool }

var toggleDefs = map[string]toggleDef{
	"premium":    {func(t togglesView) bool { return t.premium }, (*jd.Client).TogglePremium},
	"clipboard":  {func(t togglesView) bool { return t.clipboard }, (*jd.Client).ToggleClipboard},
	"reconnect":  {func(t togglesView) bool { return t.reconnect }, (*jd.Client).ToggleReconnect},
	"stop-after": {func(t togglesView) bool { return t.stopAfter }, (*jd.Client).ToggleStopAfter},
}

// handleToggle flips a switch of JDownloader. JDownloader only offers "flip".
// The body may name the state the browser wants ({"enabled":true}); a switch
// that has that state already is left alone, so a browser with an out of date
// view cannot flip it the wrong way. The answer is {enabled} with the new state.
//
// Switching premium off makes JDownloader open a confirmation dialog and not
// answer. After premiumOffWait the answer is 202 {"pending":true}; the dialog
// shows up in the dialog list.
func (s *Server) handleToggle(name string) http.HandlerFunc {
	def := toggleDefs[name]
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decode(r, &b, true); err != nil {
			s.fail(w, r, err)
			return
		}
		c, err := s.client()
		if err != nil {
			s.fail(w, r, err)
			return
		}
		t := s.engine.Status().Toggles
		view := togglesView{t.Premium, t.Clipboard, t.Reconnect, t.StopAfter}
		if b.Enabled != nil && def.current(view) == *b.Enabled {
			s.writeJSON(w, r, http.StatusOK, map[string]bool{"enabled": *b.Enabled})
			return
		}
		ctx := r.Context()
		wantOff := name == "premium" && (b.Enabled != nil && !*b.Enabled || b.Enabled == nil && view.premium)
		if wantOff {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, premiumOffWait)
			defer cancel()
		}
		on, err := def.flip(c, ctx)
		if wantOff && errors.Is(err, context.DeadlineExceeded) && r.Context().Err() == nil {
			s.writeJSON(w, r, http.StatusAccepted, map[string]bool{"pending": true})
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]bool{"enabled": on})
	}
}

func (s *Server) handleReconnect(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err == nil {
		err = c.Reconnect(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}
