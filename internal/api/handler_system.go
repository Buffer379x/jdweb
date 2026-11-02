package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Buffer379x/jdweb/internal/diag"
	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/store"
)

// handleSystem is GET /system.
func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	st := s.engine.Status()
	out := map[string]any{"version": s.version, "revision": st.JD.Revision, "docker": st.JD.Docker, "url": st.JD.URL}
	c, err := s.client()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	info, err := c.SystemInfos(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out["docker"] = info.Docker
	out["headless"] = info.Headless
	out["osFamily"] = info.OSFamily
	out["osString"] = info.OSString
	out["javaVersion"] = info.JavaVersion
	out["javaName"] = info.JavaName
	out["heapUsed"] = info.HeapUsed
	out["heapCommitted"] = info.HeapCommitted
	out["heapMax"] = info.HeapMax
	out["startedAt"] = info.StartupTimestamp
	s.writeJSON(w, r, http.StatusOK, out)
}

// handleSystemAction is POST /system/{action}. restart and exit end JDownloader
// itself and answer 202: JDownloader is gone a moment later, and the browser
// sees the connection state change. shutdown, hibernate and standby act on the
// machine, which JDownloader refuses to do in Docker, so they are not offered
// there: 403.
func (s *Server) handleSystemAction(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := s.client()
		if err != nil {
			s.fail(w, r, err)
			return
		}
		var b struct {
			Force bool `json:"force"`
		}
		if err := decode(r, &b, true); err != nil {
			s.fail(w, r, err)
			return
		}
		if name == "shutdown" || name == "hibernate" || name == "standby" {
			// A live answer, not the last status: this decides on a dangerous call.
			info, err := c.SystemInfos(r.Context())
			if err != nil {
				s.fail(w, r, err)
				return
			}
			if info.Docker {
				s.fail(w, r, newError(http.StatusForbidden, codeForbidden,
					"JDownloader runs in Docker. It cannot shut down, hibernate or suspend the machine."))
				return
			}
		}
		switch name {
		case "restart":
			err = c.RestartJD(r.Context())
		case "exit":
			err = c.ExitJD(r.Context())
		case "shutdown":
			err = c.ShutdownOS(r.Context(), b.Force)
		case "hibernate":
			err = c.HibernateOS(r.Context())
		case "standby":
			err = c.StandbyOS(r.Context())
		}
		if err != nil && !(isGoneWithTheCall(name, err)) {
			s.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// isGoneWithTheCall reports a connection that broke because the call worked:
// JDownloader may drop the connection while it shuts down.
func isGoneWithTheCall(name string, err error) bool {
	return (name == "restart" || name == "exit") && jd.IsConnection(err)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var avail bool
	if err == nil {
		avail, err = c.UpdateAvailable(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]bool{"available": avail})
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err == nil {
		err = c.CheckForUpdate(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err == nil {
		err = c.RestartAndUpdate(r.Context())
	}
	if err != nil && !jd.IsConnection(err) {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// diagTimeout bounds the whole setup check.
const diagTimeout = 20 * time.Second

func (s *Server) acknowledged(ctx context.Context) (bool, error) {
	var ack bool
	err := s.db.GetJSON(ctx, settingAcknowledged, &ack)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return ack, err
}

// handleDiagnostics is GET /diagnostics: the setup check plus whether the user
// confirmed the warning about the API without a login.
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ack, err := s.acknowledged(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diagTimeout)
	defer cancel()
	report := diag.Run(ctx, s.engine.Client(), s.own)
	s.writeJSON(w, r, http.StatusOK, struct {
		diag.Report
		Acknowledged bool `json:"acknowledged"`
	}{report, ack})
}

func (s *Server) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	if err := s.db.SetJSON(r.Context(), settingAcknowledged, true); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]bool{"acknowledged": true})
}

// connectionDTO is the answer of GET and PUT /connection.
type connectionDTO struct {
	URL        string `json:"url"`
	Source     string `json:"source"` // "ui" (saved here), "env" or "default"
	Connection string `json:"connection"`
	Detail     string `json:"detail"`
}

func (s *Server) connection(ctx context.Context) (connectionDTO, error) {
	st := s.engine.Status()
	src := s.source
	var saved string
	switch err := s.db.GetJSON(ctx, settingJDURL, &saved); {
	case err == nil:
		src = "ui"
	case !errors.Is(err, store.ErrNotFound):
		return connectionDTO{}, err
	}
	return connectionDTO{URL: st.JD.URL, Source: src, Connection: string(st.Connection), Detail: st.ConnectionDetail}, nil
}

func (s *Server) handleConnection(w http.ResponseWriter, r *http.Request) {
	dto, err := s.connection(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

// handleConnectionSet is PUT /connection. The address is checked and cleaned
// (scheme, slash at the end), saved, and the engine switches to it at once. A
// saved address wins over JDWEB_JD_URL. Whether JDownloader answers there is
// not checked here: the connection state and its hint tell.
func (s *Server) handleConnectionSet(w http.ResponseWriter, r *http.Request) {
	var b struct {
		URL string `json:"url"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	norm, err := jd.NormalizeURL(b.URL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Save first, switch second, and take the save back if the switch fails:
	// a saved address that was never used would be the address of every restart.
	var previous string
	hadPrevious := true
	switch err := s.db.GetJSON(r.Context(), settingJDURL, &previous); {
	case errors.Is(err, store.ErrNotFound):
		hadPrevious = false
	case err != nil:
		s.fail(w, r, err)
		return
	}
	if err := s.db.SetJSON(r.Context(), settingJDURL, norm); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.setURL(norm); err != nil {
		var undo error
		if hadPrevious {
			undo = s.db.SetJSON(r.Context(), settingJDURL, previous)
		} else {
			undo = s.db.DeleteSetting(r.Context(), settingJDURL)
		}
		if undo != nil {
			s.log.Error("cannot take back the saved JDownloader address", "err", undo)
		}
		s.fail(w, r, err)
		return
	}
	// A new address deserves a new look at the warning about the missing login.
	if err := s.db.DeleteSetting(r.Context(), settingAcknowledged); err != nil {
		s.log.Warn("cannot reset the acknowledged warning", "err", err)
	}
	dto, err := s.connection(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

func (s *Server) handlePrefs(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	switch err := s.db.GetJSON(r.Context(), settingPrefs, &raw); {
	case errors.Is(err, store.ErrNotFound):
		raw = json.RawMessage("{}")
	case err != nil:
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(raw, '\n'))
}

func (s *Server) handlePrefsSet(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := decode(r, &raw, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(raw) == 0 || raw[0] != '{' {
		s.fail(w, r, badRequest("The preferences must be a JSON object."))
		return
	}
	if err := s.db.SetJSON(r.Context(), settingPrefs, raw); err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}
